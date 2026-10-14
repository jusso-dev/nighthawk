package siem

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestMapAndSyslog(t *testing.T) {
	e := event.Event{
		Time:   time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Kind:   event.KindToolCall,
		Source: event.SourceAgentHook,
		PaneID: "p1",
		Agent:  "claude",
	}
	d := policy.Decision{Action: policy.Block, RuleID: "nh.x"}
	env := Map(e, d)
	if env.Spec != "nighthawk.event.v1" || env.Message != "nh.x" || env.RuleID != "nh.x" {
		t.Fatalf("%+v", env)
	}
	line := SyslogLine(env)
	if !strings.HasPrefix(line, "<133>") || !strings.Contains(line, `"spec":"nighthawk.event.v1"`) {
		t.Fatalf("%s", line)
	}
	raw, _ := json.Marshal(env)
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"spec", "time", "kind", "action", "rule_id", "pane_id", "agent", "source", "message"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}

func TestPostNDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != "{}\n" {
			t.Errorf("%q", b)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	if err := PostNDJSON(context.Background(), srv.URL, []byte("{}\n"), srv.Client()); err != nil {
		t.Fatal(err)
	}
}
