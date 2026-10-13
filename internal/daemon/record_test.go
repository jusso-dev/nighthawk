package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestAuditCoversEventAndTablesAppendOnly(t *testing.T) {
	s := testServer(t, config.Default())
	ev := event.Event{ID: "ev-audit", Kind: event.KindCommand, Source: event.SourceAgentHook, Agent: "claude",
		PaneID: "w1:p1", Pre: true, Attrs: map[string]string{"command": "ls AKIAABCDEFGHIJKLMNOP"}}
	dec, _ := s.decide(context.Background(), ev)

	rec, err := s.au.Head(s.st.DB())
	if err != nil {
		t.Fatal(err)
	}
	p := string(rec.Payload)
	for _, want := range []string{`"id":"ev-audit"`, `"pane":"w1:p1"`, `"kind":"command"`, `"command":"ls [REDACTED]"`, `"action":"` + string(dec.Action) + `"`, `"reason":`} {
		if !strings.Contains(p, want) {
			t.Errorf("payload missing %s: %s", want, p)
		}
	}
	if strings.Contains(p, "AKIAABCDEFGHIJKLMNOP") {
		t.Fatal("secret in audit payload")
	}
	db := s.st.DB()
	for _, q := range []string{
		`UPDATE events SET attrs = '{}' WHERE id = 'ev-audit'`,
		`DELETE FROM events WHERE id = 'ev-audit'`,
		`UPDATE decisions SET action = 'allow' WHERE event_id = 'ev-audit'`,
		`DELETE FROM decisions WHERE event_id = 'ev-audit'`,
	} {
		if _, err := db.Exec(q); err == nil || !strings.Contains(err.Error(), "append only") {
			t.Errorf("%s: %v", q, err)
		}
	}
}

func TestTimedOutDecisionIsRecorded(t *testing.T) {
	s := testServer(t, config.Default())
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	ev := event.Event{ID: "ev-late", Kind: event.KindCommand, Source: event.SourceAgentHook, Pre: true,
		Attrs: map[string]string{"command": "ls"}}
	if _, approval := s.finish(ctx, ev, policy.Decision{Action: policy.AskHuman, RuleID: "r"}); approval == "" {
		t.Fatal("no approval id")
	} else if _, err := s.st.GetApproval(context.Background(), approval); err != nil {
		t.Fatalf("approval %s not stored: %v", approval, err)
	}
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(1) FROM decisions WHERE event_id = 'ev-late'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("decisions %d %v", n, err)
	}
	if _, err := s.au.VerifyDB(1); err != nil {
		t.Fatal(err)
	}
}

func TestModeDefaultWhenNoModeFile(t *testing.T) {
	cfg := config.Default()
	cfg.ModeDefault = "unattended"
	if m := testServer(t, cfg).currentMode(); m != policy.Unattended {
		t.Fatalf("mode %s", m)
	}
}
