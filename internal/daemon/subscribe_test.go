package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/detect"
	"github.com/jusso-dev/nighthawk/internal/herdr"
	"github.com/jusso-dev/nighthawk/internal/sessions"
)

func TestSubscriptionsPerPane(t *testing.T) {
	s := &Server{sessions: sessions.NewMap()}
	now := time.Now()
	s.sessions.Apply(herdr.Envelope{
		Event: "pane.agent_detected",
		Data:  json.RawMessage(`{"pane_id":"w1:p1","agent":"claude"}`),
	}, now)
	s.sessions.ApplyPlugin("closed", "w1:p2", now)
	subs := s.subscriptions()
	want := 2 + len(detect.OutputPatterns)
	if len(subs) != want {
		t.Fatalf("subs %d want %d", len(subs), want)
	}
	if subs[0].Event != "pane.agent_detected" || subs[0].PaneID != "" {
		t.Fatalf("global sub %+v", subs[0])
	}
	for _, sub := range subs[1:] {
		if sub.PaneID != "w1:p1" {
			t.Fatalf("pane %s", sub.PaneID)
		}
		if sub.Event == "pane.output_matched" && (sub.Match == nil || sub.Match.Type != "regex" || sub.Match.Value == "" || sub.Source != "recent") {
			t.Fatalf("bad match %+v", sub)
		}
	}
}
