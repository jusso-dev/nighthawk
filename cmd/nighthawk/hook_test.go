package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestHookOfflineUsesModeDefault(t *testing.T) {
	t.Setenv("NIGHTHAWK_STATE_DIR", t.TempDir())
	p := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("NIGHTHAWK_CONFIG", p)
	if err := os.WriteFile(p, []byte("mode_default: unattended\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	var out, errOut bytes.Buffer
	if code := run([]string{"hook", "claude"}, in, &out, &errOut); code != 0 || !strings.Contains(out.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("code %d out %q err %q", code, out.String(), errOut.String())
	}
}

type fakeDaemon struct {
	mu     sync.Mutex
	status string
}

func (f *fakeDaemon) set(s string) { f.mu.Lock(); f.status = s; f.mu.Unlock() }

func (f *fakeDaemon) get() string { f.mu.Lock(); defer f.mu.Unlock(); return f.status }

func (f *fakeDaemon) Handle(_ context.Context, req ipc.Request) ipc.Response {
	switch req.Op {
	case "decide":
		dec := policy.Decision{Action: policy.AskHuman, RuleID: "nh.test.ask"}
		return ipc.Response{OK: true, Decision: &dec, Data: json.RawMessage(`{"approval_id":"a1"}`)}
	case "approval.wait":
		return ipc.Response{OK: true, Data: json.RawMessage(`{"ID":"a1","Status":"` + f.get() + `"}`)}
	case "approval.expire":
		f.set("expired")
	}
	return ipc.Response{OK: true}
}

func TestHookWaitsForApproval(t *testing.T) {
	dir, err := os.MkdirTemp("", "nh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("NIGHTHAWK_STATE_DIR", dir)
	t.Setenv("NIGHTHAWK_APPROVAL_WAIT", "200ms")
	fake := &fakeDaemon{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ipc.Serve(ctx, config.SocketPath(dir), fake) }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(config.SocketPath(dir)); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	in := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`
	for _, tc := range []struct {
		status string
		deny   bool
	}{{"approved", false}, {"denied", true}, {"pending", true}} {
		fake.set(tc.status)
		var out, errOut bytes.Buffer
		if code := run([]string{"hook", "claude"}, strings.NewReader(in), &out, &errOut); code != 0 {
			t.Fatalf("%s: code %d %s", tc.status, code, errOut.String())
		}
		if denied := strings.Contains(out.String(), `"permissionDecision":"deny"`); denied != tc.deny {
			t.Fatalf("%s: out %q", tc.status, out.String())
		}
	}
	if got := fake.get(); got != "expired" {
		t.Fatalf("timed out approval not expired: %s", got)
	}
}

// Codex passes the notify payload as the last argument, not on stdin.
func TestHookCodexNotifyArgv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NIGHTHAWK_STATE_DIR", dir)
	if err := config.WriteMode(dir, policy.Attended); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	payload := `{"type":"agent-turn-complete","thread-id":"th-1","cwd":"/w"}`
	if code := run([]string{"hook", "codex", payload}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code %d %s", code, errOut.String())
	}
}

func TestHookBadInputFailsClosedUnattended(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NIGHTHAWK_STATE_DIR", dir)
	big := `{"x":"` + strings.Repeat("a", 1<<20) + `"}`
	for _, mode := range []policy.Mode{policy.Unattended, policy.Attended} {
		if err := config.WriteMode(dir, mode); err != nil {
			t.Fatal(err)
		}
		for _, in := range []string{"not json", big} {
			var out, errOut bytes.Buffer
			code := run([]string{"hook", "claude"}, strings.NewReader(in), &out, &errOut)
			denied := strings.Contains(out.String(), `"permissionDecision":"deny"`)
			if mode == policy.Unattended && (code != 0 || !denied) {
				t.Fatalf("unattended: code %d out %q", code, out.String())
			}
			if mode == policy.Attended && (code != 1 || denied) {
				t.Fatalf("attended: code %d out %q", code, out.String())
			}
		}
	}
}
