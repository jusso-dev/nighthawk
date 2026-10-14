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

// maxTranscripts bounds how many transcript files the daemon tails.
const maxTranscripts = 256

// transcriptTail is one transcript file and who its usage belongs to.
type transcriptTail struct {
	agent, pane, session, task string
}

// watchTranscript starts tailing the transcript a hook event names, and
// keeps its pane, session and task tags current.
func (s *Server) watchTranscript(ev event.Event) {
	path := ev.Attrs["transcript_path"]
	if path == "" || !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return
	}
	path = filepath.Clean(path)
	session, task := ev.SessionID, ev.TaskID
	if ev.PaneID != "" {
		p := s.sessions.Get(ev.PaneID)
		if session == "" {
			session = p.AgentSessionID
		}
		if task == "" {
			task = p.TaskID
		}
	}
	s.trMu.Lock()
	defer s.trMu.Unlock()
	if s.trs == nil {
		s.trs = map[string]*transcriptTail{}
	}
	t, ok := s.trs[path]
	if !ok {
		if len(s.trs) >= maxTranscripts {
			return
		}
		t = &transcriptTail{}
		s.trs[path] = t
	}
	setTag(&t.agent, ev.Agent)
	setTag(&t.pane, ev.PaneID)
	setTag(&t.session, session)
	setTag(&t.task, task)
}

func setTag(dst *string, v string) {
	if v != "" && v != "unknown" {
		*dst = v
	}
}

func (s *Server) transcriptLoop(ctx context.Context) {
	for _, p := range splitList(os.Getenv("NIGHTHAWK_TRANSCRIPT_PATHS")) {
		s.watchTranscript(event.Event{Attrs: map[string]string{"transcript_path": p}})
	}
	models := map[string]string{}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	s.tailTranscripts(ctx, models)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tailTranscripts(ctx, models)
		}
	}
}

// tailTranscripts reads new lines from every watched transcript. models holds
// the current Codex model per path, since Codex token lines do not name it.
func (s *Server) tailTranscripts(ctx context.Context, models map[string]string) {
	s.trMu.Lock()
	tails := make(map[string]transcriptTail, len(s.trs))
	for p, t := range s.trs {
		tails[p] = *t
	}
	s.trMu.Unlock()
	for path, tag := range tails {
		off, err := s.st.GetOffset(ctx, path)
		if err != nil {
			continue
		}
		if _, ok := models[path]; !ok && off > 0 {
			models[path] = codexModelBefore(path, off)
		}
		lines, next, err := transcript.ReadNew(path, off)
		if err != nil {
			continue
		}
		for _, line := range lines {
			if m, ok := transcript.CodexModel(line); ok {
				models[path] = m
				continue
			}
			u, ok := transcript.ParseClaude(line)
			agent := tag.agent
			if agent == "" {
				agent = "claude"
			}
			if !ok {
				u, ok = transcript.ParseCodex(line)
				agent = "codex"
				if u.Model == "" {
					u.Model = models[path]
				}
				if u.Key != "" {
					u.Key = path + "#" + u.Key
				}
			}
			if !ok {
				continue
			}
			if u.TS.IsZero() {
				u.TS = time.Now().UTC()
			}
			_ = s.st.InsertUsage(ctx, store.Usage{
				TS: u.TS, PaneID: tag.pane, Agent: agent, SessionID: tag.session, TaskID: tag.task,
				Model: u.Model, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
				CacheWrite: u.CacheWrite, MessageKey: u.Key,
			})
		}
		if next != off {
			_ = s.st.SetOffset(ctx, path, next)
		}
	}
}

// codexModelBefore finds the last Codex model named before byte off, for a
// transcript resumed after a daemon restart.
func codexModelBefore(path string, off int64) string {
	lines, _, err := transcript.ReadNew(path, 0)
	if err != nil {
		return ""
	}
	var model string
	var pos int64
	for _, line := range lines {
		if pos >= off {
			break
		}
		pos += int64(len(line)) + 1
		if m, ok := transcript.CodexModel(line); ok {
			model = m
		}
	}
	return model
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
