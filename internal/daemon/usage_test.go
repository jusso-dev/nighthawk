package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("HERDR_SOCKET_PATH", "")
	cfg := config.Default()
	cfg.Egress.Port = 0
	s, err := openServer(Options{StateDir: t.TempDir(), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.st.Close() })
	return s
}

func TestHookTranscriptUsageIsTagged(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	src, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s-1.jsonl")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	ev := event.Event{
		Kind: event.KindPane, Source: event.SourceAgentHook, Agent: "claude",
		PaneID: "p1", SessionID: "s-1", TaskID: "t1",
		Attrs: map[string]string{"hook": "SessionStart", "transcript_path": path},
	}
	if resp := s.Handle(ctx, ipc.Request{Op: "submit", Event: &ev}); !resp.OK {
		t.Fatal(resp.Error)
	}
	models := map[string]string{}
	s.tailTranscripts(ctx, models)
	if err := s.st.SetOffset(ctx, path, 0); err != nil {
		t.Fatal(err)
	}
	s.tailTranscripts(ctx, models) // rereading must not double count
	for by, key := range map[string]string{"session": "s-1", "task": "t1", "day": "2026-10-01"} {
		rows, err := s.st.UsageGrouped(ctx, by, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var out int64
		for _, r := range rows {
			if r.Key != key {
				t.Fatalf("%s: key %q want %q", by, r.Key, key)
			}
			out += r.Output
		}
		if out != 57 {
			t.Fatalf("%s: output %d want 57", by, out)
		}
	}
	resp := s.Handle(ctx, ipc.Request{Op: "spend", By: "session", Since: "100000d"})
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	if resp := s.Handle(ctx, ipc.Request{Op: "spend", Since: "7 days"}); resp.OK {
		t.Fatal("bad since accepted")
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"":                     {},
		"7d":                   now.AddDate(0, 0, -7),
		"36h":                  now.Add(-36 * time.Hour),
		"2026-10-01T00:00:00Z": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := parseSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Fatalf("%q: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"7", "d", "-1d", "yesterday"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestWaitApproval(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	status := func(resp ipc.Response) string {
		var a store.Approval
		if !resp.OK || json.Unmarshal(resp.Data, &a) != nil {
			t.Fatalf("resp %+v", resp)
		}
		return a.Status
	}
	add := func(id string, exp time.Time) {
		if err := s.st.InsertApproval(ctx, store.Approval{ID: id, Status: "pending", ExpiresAt: exp}); err != nil {
			t.Fatal(err)
		}
	}
	add("old", time.Now().Add(-time.Second))
	if got := status(s.Handle(ctx, ipc.Request{Op: "approval.wait", ID: "old", Wait: time.Second})); got != "expired" {
		t.Fatalf("old: %s", got)
	}
	add("slow", time.Now().Add(time.Minute))
	if got := status(s.Handle(ctx, ipc.Request{Op: "approval.wait", ID: "slow", Wait: 200 * time.Millisecond})); got != "pending" {
		t.Fatalf("slow: %s", got)
	}
	if resp := s.Handle(ctx, ipc.Request{Op: "approval.expire", ID: "slow"}); !resp.OK {
		t.Fatal(resp.Error)
	}
	if resp := s.Handle(ctx, ipc.Request{Op: "approval.resolve", ID: "slow", Kind: "approved"}); resp.OK {
		t.Fatal("resolved an expired approval")
	}
	add("ok", time.Now().Add(time.Minute))
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Handle(ctx, ipc.Request{Op: "approval.resolve", ID: "ok", Kind: "approved", Who: "test"})
	}()
	if got := status(s.Handle(ctx, ipc.Request{Op: "approval.wait", ID: "ok", Wait: 5 * time.Second})); got != "approved" {
		t.Fatalf("ok: %s", got)
	}
}
