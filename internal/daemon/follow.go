package daemon

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jusso-dev/nighthawk/internal/detect"
	"github.com/jusso-dev/nighthawk/internal/herdr"
)

func (s *Server) follow(ctx context.Context) {
	if os.Getenv("HERDR_SOCKET_PATH") == "" && os.Getenv("HERDR_BIN_PATH") == "" {
		return
	}
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	var subCancel context.CancelFunc
	var subKey string
	refresh := func() {
		s.reconcile(ctx)
		sock := os.Getenv("HERDR_SOCKET_PATH")
		if sock == "" {
			return
		}
		key := s.livePaneKey()
		if key == subKey && subCancel != nil {
			return
		}
		if subCancel != nil {
			subCancel()
			subCancel = nil
		}
		subKey = key
		subCtx, cancel := context.WithCancel(ctx)
		subCancel = cancel
		go s.subscribe(subCtx, sock)
	}
	refresh()
	for {
		select {
		case <-ctx.Done():
			if subCancel != nil {
				subCancel()
			}
			return
		case <-t.C:
			refresh()
		case <-s.subWake:
			refresh()
		}
	}
}

func (s *Server) wakeSubs() {
	if s.subWake == nil {
		return
	}
	select {
	case s.subWake <- struct{}{}:
	default:
	}
}

func (s *Server) livePaneKey() string {
	var ids []string
	for _, p := range s.sessions.List() {
		if p.ID == "" || !p.Ended.IsZero() {
			continue
		}
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}

func (s *Server) reconcile(ctx context.Context) {
	agents, err := (herdr.CLI{}).ListAgents(ctx)
	if err != nil {
		return
	}
	s.sessions.Reconcile(agents, time.Now())
	for _, p := range s.sessions.List() {
		s.savePane(p)
	}
}

// subscriptions always includes the global pane.agent_detected, so new panes
// are seen before the next reconcile tick.
func (s *Server) subscriptions() []herdr.Subscription {
	subs := []herdr.Subscription{{Event: "pane.agent_detected"}}
	for _, p := range s.sessions.List() {
		if p.ID == "" || !p.Ended.IsZero() {
			continue
		}
		subs = append(subs, herdr.Subscription{Event: "pane.agent_status_changed", PaneID: p.ID})
		for _, pat := range detect.OutputPatterns {
			subs = append(subs, herdr.Subscription{
				Event: "pane.output_matched", PaneID: p.ID, Match: herdr.Regex(pat.Re.String()),
				Source: "recent", Lines: 200, StripANSI: true,
			})
		}
	}
	return subs
}

func (s *Server) subscribe(ctx context.Context, sock string) {
	subs := s.subscriptions()
	ch, err := herdr.Subscribe(ctx, sock, subs, func() error {
		// Rebuild subscriptions if a pane closed or appeared while disconnected.
		s.wakeSubs()
		return nil
	})
	if err != nil || ch == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			now := time.Now()
			s.sessions.Apply(env, now)
			var probe struct {
				PaneID      string `json:"pane_id"`
				MatchedLine string `json:"matched_line"`
			}
			_ = json.Unmarshal(env.Data, &probe)
			if env.Event == "pane.output_matched" && probe.MatchedLine != "" {
				agent := s.sessions.Get(probe.PaneID).Agent
				if ev, hit := s.dedupe.Observe(probe.PaneID, agent, probe.MatchedLine, now); hit {
					_, _ = s.decide(ctx, ev)
				}
			}
			if probe.PaneID != "" {
				s.savePane(s.sessions.Get(probe.PaneID))
			}
			if env.Event == "pane.agent_detected" || env.Event == "pane_agent_detected" {
				s.wakeSubs()
			}
		}
	}
}
