package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/herdr"
)

func TestCreateCloseAndUnknown(t *testing.T) {
	m := NewMap()
	now := time.Now().Truncate(time.Second)
	m.ApplyPlugin("created", "w1:p1", now)
	m.Apply(env("pane.agent_detected", map[string]any{"pane_id": "w1:p1", "agent": "claude", "cwd": "/w"}), now)
	m.ApplyPlugin("closed", "w1:p1", now.Add(time.Minute))
	p := m.Get("w1:p1")
	if p.Started.IsZero() || p.Ended.IsZero() || p.Agent != "claude" {
		t.Fatalf("%+v", p)
	}
	u := m.Get("missing")
	if u.Agent != "unknown" {
		t.Fatal(u)
	}
}

func TestReconcileRebuilds(t *testing.T) {
	m := NewMap()
	now := time.Now()
	m.Reconcile([]herdr.AgentInfo{{PaneID: "w1:p2", Agent: "codex", Status: "idle", Cwd: "/x", WorkspaceID: "w1", AgentSessionID: "as"}}, now)
	p := m.Get("w1:p2")
	if p.Agent != "codex" || p.AgentSessionID != "as" || !p.Ended.IsZero() {
		t.Fatalf("%+v", p)
	}
}

func env(name string, data map[string]any) herdr.Envelope {
	b, _ := json.Marshal(data)
	return herdr.Envelope{Event: name, Data: b}
}
