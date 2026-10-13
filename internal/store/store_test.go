package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
)

func TestOpenMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nighthawk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SchemaVersion()
	if err != nil || v1 < 3 {
		t.Fatalf("version %d %v", v1, err)
	}
	var tables int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name IN ('events','decisions','sessions','usage','approvals','meta')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 6 {
		t.Fatalf("tables %d", tables)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v2, err := s.SchemaVersion()
	if err != nil || v2 != v1 {
		t.Fatalf("rerun %d -> %d %v", v1, v2, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}

func TestListEventsFilter(t *testing.T) {
	s := openTest(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	ctx := context.Background()
	for _, e := range []event.Event{
		{ID: "a", Time: now, Kind: event.KindCommand, Source: event.SourceAgentHook, Agent: "claude", Attrs: map[string]string{"command": "ls"}},
		{ID: "b", Time: now.Add(time.Second), Kind: event.KindNetwork, Source: event.SourceEgress, Agent: "codex", Attrs: map[string]string{"host": "example.com"}},
		{ID: "c", Time: now.Add(2 * time.Second), Kind: event.KindCommand, Source: event.SourceAgentHook, Agent: "claude", Attrs: map[string]string{"command": "pwd"}},
	} {
		if err := s.InsertEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListEvents(ctx, Filter{Agent: "claude", Kind: event.KindCommand, From: now, To: now.Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("%+v", got)
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestResolveApprovalOnlyPending(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "nighthawk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.ResolveApproval(ctx, "missing", "approved", "cli"); err != ErrNoPendingApproval {
		t.Fatalf("unknown id: %v", err)
	}
	for id, exp := range map[string]time.Time{"live": time.Now().Add(time.Minute), "old": time.Now().Add(-time.Minute)} {
		if err := s.InsertApproval(ctx, Approval{ID: id, EventID: "e", RuleID: "r", Status: "pending", ExpiresAt: exp}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ResolveApproval(ctx, "old", "approved", "cli"); err != ErrNoPendingApproval {
		t.Fatalf("expired: %v", err)
	}
	if err := s.ResolveApproval(ctx, "live", "denied", "cli"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveApproval(ctx, "live", "approved", "cli"); err != ErrNoPendingApproval {
		t.Fatalf("second decision: %v", err)
	}
}
