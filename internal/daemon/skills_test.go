package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func TestHookSkills(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("NIGHTHAWK_SKILL_PATHS", "")
	dir := t.TempDir()
	skill := filepath.Join(t.TempDir(), "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Egress.Port = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, Options{StateDir: dir, Config: cfg}) }()
	sock := config.SocketPath(dir)
	waitSock(t, sock)

	submit := func(hook string, attrs map[string]string) {
		t.Helper()
		attrs["hook"] = hook
		ev := event.Event{Kind: event.KindSkill, Source: event.SourceAgentHook, Agent: "claude", Pre: hook == "PreToolUse", Attrs: attrs}
		if resp, err := ipc.Call(context.Background(), sock, ipc.Request{Op: "submit", Event: &ev}); err != nil || !resp.OK {
			t.Fatal(err, resp.Error)
		}
	}
	list := func() []store.Skill {
		t.Helper()
		resp, err := ipc.Call(context.Background(), sock, ipc.Request{Op: "skills.list"})
		if err != nil || !resp.OK {
			t.Fatal(err, resp.Error)
		}
		var out []store.Skill
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	submit("PreToolUse", map[string]string{"name": "caveman"})
	submit("PreToolUse", map[string]string{"name": "demo", "path": skill})
	submit("PostToolUse", map[string]string{"name": "demo", "path": skill})
	got := list()
	if len(got) != 2 {
		t.Fatalf("want 2 skills, got %+v", got)
	}
	var demo store.Skill
	for _, sk := range got {
		if sk.Name == "demo" {
			demo = sk
		}
	}
	if demo.Uses != 1 || demo.Hash == "" {
		t.Fatalf("demo after one use: %+v", demo)
	}

	if err := os.WriteFile(skill, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	submit("PreToolUse", map[string]string{"name": "demo", "path": skill})
	for _, sk := range list() {
		if sk.Name == "demo" && (sk.Uses != 2 || sk.Hash == demo.Hash) {
			t.Fatalf("demo after edit: %+v", sk)
		}
	}
	resp, err := ipc.Call(context.Background(), sock, ipc.Request{Op: "events.list"})
	if err != nil || !resp.OK {
		t.Fatal(err, resp.Error)
	}
	var evs []event.Event
	_ = json.Unmarshal(resp.Data, &evs)
	changed := 0
	for _, e := range evs {
		if e.Kind == event.KindSkill && e.Attrs["change"] == "skill_changed" {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("want one skill_changed event, got %d", changed)
	}
}
