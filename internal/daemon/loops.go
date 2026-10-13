package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/integrity"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/siem"
	"github.com/jusso-dev/nighthawk/internal/skills"
	"github.com/jusso-dev/nighthawk/internal/store"
	"github.com/jusso-dev/nighthawk/internal/transcript"
)

func (s *Server) enqueueSIEM(ev event.Event, dec policy.Decision) {
	if s.opt.Config.SIEM.WebhookURL == "" && s.opt.Config.SIEM.SyslogAddr == "" {
		return
	}
	b, err := json.Marshal(siem.Map(ev, dec))
	if err != nil {
		return
	}
	_ = s.st.EnqueueOutbox(context.Background(), string(b), time.Now())
}

func (s *Server) sideLoops(ctx context.Context) {
	go s.integrityLoop(ctx)
	go s.siemLoop(ctx)
	go s.transcriptLoop(ctx)
	go s.skillLoop(ctx)
}

func (s *Server) integrityLoop(ctx context.Context) {
	var paths []string
	for _, p := range []string{config.ConfigPath(), filepath.Join(s.opt.StateDir, "mode")} {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	if err := integrity.Write(s.opt.StateDir, paths); err != nil {
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	var last string
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed, err := integrity.Check(s.opt.StateDir)
			if err != nil || len(changed) == 0 {
				last = ""
				continue
			}
			now := strings.Join(changed, "\n")
			if now == last {
				continue
			}
			last = now
			s.decide(ctx, event.Event{
				Kind: event.KindTamper, Source: event.SourceHerdr,
				Attrs: map[string]string{"paths": now, "rule": "nh.integrity"},
			})
		}
	}
}

func (s *Server) siemLoop(ctx context.Context) {
	if s.opt.Config.SIEM.WebhookURL == "" && s.opt.Config.SIEM.SyslogAddr == "" {
		return
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.drainSIEM(ctx)
		}
	}
}

func (s *Server) drainSIEM(ctx context.Context) {
	rows, err := s.st.DueOutbox(ctx, time.Now())
	if err != nil {
		return
	}
	for _, row := range rows {
		if err := s.deliverSIEM(ctx, row.Payload); err != nil {
			_ = s.st.FailOutbox(ctx, row.ID, time.Now().Add(30*time.Second))
			continue
		}
		_ = s.st.MarkDelivered(ctx, row.ID)
	}
}

func (s *Server) deliverSIEM(ctx context.Context, payload string) error {
	if u := s.opt.Config.SIEM.WebhookURL; u != "" {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := siem.PostNDJSON(cctx, u, []byte(payload), nil); err != nil {
			return err
		}
	}
	if addr := s.opt.Config.SIEM.SyslogAddr; addr != "" {
		var env siem.Envelope
		if json.Unmarshal([]byte(payload), &env) != nil {
			return nil
		}
		d := net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		_, err = conn.Write([]byte(siem.SyslogLine(env) + "\n"))
		_ = conn.Close()
		return err
	}
	return nil
}

func (s *Server) transcriptLoop(ctx context.Context) {
	paths := splitList(os.Getenv("NIGHTHAWK_TRANSCRIPT_PATHS"))
	if len(paths) == 0 {
		return
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	s.tailTranscripts(ctx, paths)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tailTranscripts(ctx, paths)
		}
	}
}

func (s *Server) tailTranscripts(ctx context.Context, paths []string) {
	for _, path := range paths {
		off, err := s.st.GetOffset(ctx, path)
		if err != nil {
			continue
		}
		lines, next, err := transcript.ReadNew(path, off)
		if err != nil {
			continue
		}
		for _, line := range lines {
			model, in, out, cr, cw, key, ok := transcript.ParseClaude(line)
			agent := "claude"
			if !ok {
				model, in, out, cr, cw, key, ok = transcript.ParseCodex(line)
				agent = "codex"
			}
			if !ok {
				continue
			}
			_ = s.st.InsertUsage(ctx, store.Usage{
				TS: time.Now().UTC(), Agent: agent, Model: model,
				Input: in, Output: out, CacheRead: cr, CacheWrite: cw, MessageKey: key,
			})
		}
		if next != off {
			_ = s.st.SetOffset(ctx, path, next)
		}
	}
}

func (s *Server) skillLoop(ctx context.Context) {
	paths := splitList(os.Getenv("NIGHTHAWK_SKILL_PATHS"))
	if len(paths) == 0 {
		return
	}
	seen := map[string]string{}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	s.scanSkills(ctx, paths, seen)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.scanSkills(ctx, paths, seen)
		}
	}
}

func (s *Server) scanSkills(ctx context.Context, paths []string, seen map[string]string) {
	for _, path := range paths {
		h, err := skills.FileHash(path)
		if err != nil || seen[path] == h {
			continue
		}
		seen[path] = h
		name := skills.NameFromPath(path)
		_, changed, err := s.st.UpsertSkill(ctx, name, path, h, false)
		if err != nil || !changed {
			continue
		}
		s.decide(ctx, event.Event{
			Kind: event.KindSkill, Source: event.SourceAgentHook,
			Attrs: map[string]string{"skill": name, "path": path, "change": "skill_changed"},
		})
	}
}

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, string(filepath.ListSeparator))
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
