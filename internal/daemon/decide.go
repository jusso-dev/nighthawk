package daemon

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jusso-dev/nighthawk/internal/audit"
	"github.com/jusso-dev/nighthawk/internal/cost"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/redact"
	"github.com/jusso-dev/nighthawk/internal/sessions"
	"github.com/jusso-dev/nighthawk/internal/skills"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func (s *Server) decide(ctx context.Context, ev event.Event) (policy.Decision, string) {
	ev = s.prepare(ev)
	s.noteSkill(ctx, &ev)
	timeout := s.opt.Config.DecisionTimeout.D()
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return s.finish(ctx, ev, policy.OnError(s.currentMode(), err))
	}
	dec := s.eval(ev)
	if s.opt.Judge != nil {
		dec = s.opt.Judge(ctx, ev, dec)
	}
	if err := ctx.Err(); err != nil && dec.Action == policy.Allow {
		dec = policy.OnError(s.currentMode(), err)
	}
	return s.finish(ctx, ev, dec)
}

// prepare copies ev and fills id, time, machine and pane session fields.
func (s *Server) prepare(ev event.Event) event.Event {
	ev = cloneEvent(ev)
	if ev.ID == "" {
		ev.ID = event.NewID()
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if ev.Machine == "" {
		ev.Machine = s.host
	}
	if ev.PaneID != "" {
		p := s.sessions.Get(ev.PaneID)
		if ev.Agent == "" || ev.Agent == "unknown" {
			ev.Agent = "unknown"
			if p.Agent != "" {
				ev.Agent = p.Agent
			}
		}
		if ev.SessionID == "" {
			ev.SessionID = p.AgentSessionID
		}
		if ev.TaskID == "" {
			ev.TaskID = p.TaskID
		}
	}
	if ev.TaskID == "" {
		ev.TaskID = cost.TaskID(ev.Agent, "")
	}
	return ev
}

// noteSkill records a skill load or invocation from an agent hook and flags
// a hash change on the event. Post hooks are skipped so one call counts once.
func (s *Server) noteSkill(ctx context.Context, ev *event.Event) {
	if ev.Kind != event.KindSkill || ev.Attrs == nil || ev.Attrs["change"] != "" || ev.Attrs["hook"] == "PostToolUse" {
		return
	}
	name, path := ev.Attrs["name"], ev.Attrs["path"]
	if name == "" && path != "" {
		name = skills.NameFromPath(path)
	}
	if name == "" {
		return
	}
	var h string
	if path != "" {
		h, _ = skills.FileHash(path)
	}
	_, changed, err := s.st.UpsertSkill(ctx, name, path, h, false)
	if err == nil && changed {
		ev.Attrs["change"] = "skill_changed"
	}
}

func (s *Server) eval(ev event.Event) policy.Decision {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	eng := s.pol.Engine()
	eng.Facts = policy.Facts{
		HostAllowed: func(host string) bool { return s.allow.HostMatch(host) },
		SpendUSD:    s.spendOf,
		LoopCount: func(e event.Event, within time.Duration) int {
			return s.loops.Add(e, within, time.Now())
		},
	}
	mode := s.currentMode()
	d, err := eng.Evaluate(ev, mode)
	if err != nil {
		return policy.OnError(mode, err)
	}
	return d
}

func (s *Server) finish(ctx context.Context, ev event.Event, dec policy.Decision) (policy.Decision, string) {
	stored := ev
	stored.Attrs = redactMap(ev.Attrs)
	// The decision ctx may already be past its deadline (that is how a timeout
	// gets here). Persist on a detached ctx so timed-out decisions are recorded.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	var approval string
	if dec.Action == policy.AskHuman {
		approval = event.NewID()
		raw, _ := json.Marshal(stored)
		if err := s.st.InsertApproval(pctx, store.Approval{
			ID: approval, EventID: ev.ID, RuleID: dec.RuleID, Status: "pending",
			ExpiresAt: time.Now().Add(10 * time.Minute), EventJSON: string(raw),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "nighthawk: approval for event %s: %v\n", ev.ID, err)
			approval = ""
		}
	}
	if err := s.record(pctx, stored, dec); err != nil {
		fmt.Fprintf(os.Stderr, "nighthawk: record event %s: %v\n", ev.ID, err)
	}
	s.enqueueSIEM(stored, dec)
	if dec.Action == policy.PausePane || dec.Action == policy.KillSession {
		ectx := s.run
		if ectx == nil {
			ectx = context.Background()
		}
		go s.enforce(ectx, ev.PaneID, ev.Agent, dec.Action)
	}
	if s.opt.Alert != nil && dec.Action != policy.Allow {
		rule, pane := dec.RuleID, ev.PaneID
		go s.opt.Alert(rule, pane, dec.Reason)
	}
	return dec, approval
}

// record stores the event and decision and appends an audit row whose payload
// is the full redacted event plus the full decision, so editing either table
// no longer matches the chain.
func (s *Server) record(ctx context.Context, ev event.Event, dec policy.Decision) error {
	return s.st.Record(ctx, ev, dec, func(tx *sql.Tx) error {
		payload, err := audit.Canonical(map[string]any{
			"event": map[string]any{
				"id":      ev.ID,
				"kind":    string(ev.Kind),
				"source":  string(ev.Source),
				"machine": ev.Machine,
				"agent":   ev.Agent,
				"pane":    ev.PaneID,
				"session": ev.SessionID,
				"task":    ev.TaskID,
				"pre":     ev.Pre,
				"time":    ev.Time.UnixMilli(),
				"attrs":   ev.Attrs,
			},
			"decision": map[string]string{
				"action":  string(dec.Action),
				"rule_id": dec.RuleID,
				"reason":  dec.Reason,
			},
		})
		if err != nil {
			return err
		}
		_, err = s.au.Append(tx, payload)
		return err
	})
}

func (s *Server) enforce(ctx context.Context, pane, agent string, action policy.Action) {
	if pane == "" || s.acts == nil {
		return
	}
	switch action {
	case policy.PausePane:
		_ = s.acts.Pause(ctx, pane, agent)
	case policy.KillSession:
		_ = s.acts.Kill(ctx, pane)
	}
}

func (s *Server) auditRaw(payload []byte) {
	tx, err := s.st.DB().Begin()
	if err != nil {
		return
	}
	if _, err = s.au.Append(tx, payload); err != nil {
		_ = tx.Rollback()
		return
	}
	_ = tx.Commit()
}

func (s *Server) spendOf(scope, id string) float64 {
	// Untagged usage shares the empty key; it is nobody's budget.
	if id == "" {
		return 0
	}
	rows, err := s.st.UsageGrouped(context.Background(), scope, time.Unix(0, 0))
	if err != nil {
		return 0
	}
	var usd float64
	for _, r := range rows {
		if r.Key != id {
			continue
		}
		line := s.prices.Line(cost.Usage{
			Key: r.Key, Model: r.Model, Input: r.Input, Output: r.Output,
			CacheRead: r.CacheRead, CacheWrite: r.CacheWrite,
		}, r.At, 0)
		if !line.Unpriced {
			usd += line.USD
		}
	}
	return usd
}

// remember returns the pane's proxy token, minting a random 128-bit one on
// first use. Tokens live in memory only; env hands out the same one again.
func (s *Server) remember(pane string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tok, ok := s.paneToks[pane]; ok {
		return tok
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	tok := hex.EncodeToString(b[:])
	s.tokens[tok] = pane
	s.paneToks[pane] = tok
	return tok
}

func (s *Server) paneForToken(tok string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[tok]
}

func (s *Server) savePane(p sessions.Pane) {
	if p.Machine == "" {
		p.Machine = s.host
	}
	_ = s.st.UpsertSession(context.Background(), store.Session{
		PaneID: p.ID, Workspace: p.Workspace, Agent: p.Agent, AgentSessionID: p.AgentSessionID,
		Cwd: p.Cwd, Status: p.Status, Started: p.Started, Ended: p.Ended, Machine: p.Machine, TaskID: p.TaskID,
	})
	if p.ID != "" {
		s.remember(p.ID)
	}
}

func cloneEvent(e event.Event) event.Event {
	if e.Attrs == nil {
		return e
	}
	cp := make(map[string]string, len(e.Attrs))
	for k, v := range e.Attrs {
		cp[k] = v
	}
	e.Attrs = cp
	return e
}

func redactMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = redact.String(v)
	}
	return out
}
