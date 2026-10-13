package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeHerdr mimics Herdr 0.9.3: no --json flag, JSON responses on stdout,
// error bodies on stdout with exit 1. Fixtures are scrubbed real output shapes.
const fakeHerdr = `#!/bin/sh
printf '%s\n' "$@" >> "$NH_ARGS"
printf -- '--\n' >> "$NH_ARGS"
for a in "$@"; do
  if [ "$a" = "--json" ]; then echo "unknown option: --json" >&2; exit 2; fi
done
if [ "$1" = "--machine" ]; then shift 2; fi
case "$*" in
  "agent list") cat "$NH_FIX/agent_list.json" ;;
  "pane process-info --pane w1:p1") cat "$NH_FIX/process_info.json" ;;
  "pane process-info --pane "*) echo '{"error":{"code":"pane_not_found","message":"pane not found"},"id":"cli:pane:process_info"}'; exit 1 ;;
  "pane send-keys w1:p1 esc") echo '{"id":"cli:request","result":{"type":"ok"}}' ;;
  "notification show Nighthawk --body hello there") echo '{"id":"cli:request","result":{"type":"ok"}}' ;;
  *) echo "unknown option: $1" >&2; exit 2 ;;
esac
`

func fakeCLI(t *testing.T) (CLI, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeherdr")
	if err := os.WriteFile(bin, []byte(fakeHerdr), 0o755); err != nil {
		t.Fatal(err)
	}
	fix, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "args")
	t.Setenv("NH_ARGS", argsFile)
	t.Setenv("NH_FIX", fix)
	return CLI{Bin: bin, Machine: "m1"}, argsFile
}

func TestCLIArgvAndDecode(t *testing.T) {
	c, argsFile := fakeCLI(t)
	ctx := context.Background()
	agents, err := c.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("%+v", agents)
	}
	a := agents[0]
	if a.PaneID != "w1:p1" || a.Agent != "claude" || a.Status != "working" || a.WorkspaceID != "w1" || a.Cwd != "/work/repo" || a.SessionID() != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("%+v", a)
	}
	if b := agents[1]; b.Status != "blocked" || b.SessionID() != "" {
		t.Fatalf("%+v", b)
	}
	info, err := c.ProcessInfo(ctx, "w1:p1")
	if err != nil || info.ShellPID != 10 || len(info.Processes) != 1 || info.Processes[0].PID != 11 {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := c.ProcessInfo(ctx, "w9:p9"); err == nil || !strings.Contains(err.Error(), "pane_not_found") {
		t.Fatalf("want pane_not_found, got %v", err)
	}
	if err := c.SendKeys(ctx, "w1:p1", "esc"); err != nil {
		t.Fatal(err)
	}
	if err := c.Notify(ctx, "Nighthawk", "hello there"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "--json") || strings.Contains(text, "\n-c\n") {
		t.Fatalf("argv:\n%s", text)
	}
	for _, want := range []string{
		"--machine\nm1\nagent\nlist\n--\n",
		"--machine\nm1\npane\nprocess-info\n--pane\nw1:p1\n--\n",
		"--machine\nm1\npane\nsend-keys\nw1:p1\nesc\n--\n",
		"--machine\nm1\nnotification\nshow\nNighthawk\n--body\nhello there\n--\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in argv:\n%s", want, text)
		}
	}
}

func TestSubscriptionTaggedMatch(t *testing.T) {
	b, err := json.Marshal([]Subscription{
		{Event: "pane.agent_detected"},
		{Event: "pane.output_matched", PaneID: "w1:p1", Match: Regex("curl"), Source: "recent", Lines: 200, StripANSI: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"pane.agent_detected"},{"type":"pane.output_matched","pane_id":"w1:p1","match":{"type":"regex","value":"curl"},"source":"recent","lines":200,"strip_ansi":true}]`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestSubscribeReconnectResync(t *testing.T) {
	path := sockPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveOnce(ln)
	resyncs := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Subscribe(ctx, path, []Subscription{{Event: "pane.output_matched", PaneID: "w1:p1", Match: Regex("curl"), Source: "recent", Lines: 200, StripANSI: true}}, func() error {
		resyncs++
		cancel()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case env, ok := <-ch:
		if !ok || env.Event != "pane.output_matched" {
			t.Fatalf("ok=%v %+v", ok, env)
		}
		var data OutputMatch
		if err := json.Unmarshal(env.Data, &data); err != nil || data.MatchedLine == "" {
			t.Fatal(err, data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	deadline := time.Now().Add(2 * time.Second)
	for resyncs == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if resyncs == 0 {
		t.Fatal("no resync")
	}
}

func TestSubscribeErrorReplyRetries(t *testing.T) {
	path := sockPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var conns atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			sc := bufio.NewScanner(conn)
			sc.Scan()
			_, _ = conn.Write([]byte(`{"id":"","error":{"code":"invalid_request","message":"invalid request: expected internally tagged enum OutputMatch"}}` + "\n"))
			conn.Close()
		}
	}()
	err = streamOnce(context.Background(), path, nil, make(chan Envelope, 1))
	if err == nil || !strings.Contains(err.Error(), "invalid_request") {
		t.Fatalf("streamOnce err %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var resyncs atomic.Int32
	if _, err := Subscribe(ctx, path, nil, func() error { resyncs.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for resyncs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if resyncs.Load() < 2 || conns.Load() < 3 {
		t.Fatalf("resyncs=%d conns=%d", resyncs.Load(), conns.Load())
	}
}

func TestStreamOnceCancelQuiet(t *testing.T) {
	path := sockPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(5 * time.Second)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := streamOnce(ctx, path, nil, make(chan Envelope, 1)); err == nil {
		t.Fatal("want ctx error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancel took %v", d)
	}
}

// sockPath keeps unix socket paths well under the 104-byte sun_path limit.
func sockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "nh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "h.sock")
}

func serveOnce(ln net.Listener) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	if !sc.Scan() {
		return
	}
	_, _ = conn.Write([]byte("{\"id\":\"nh-sub\",\"result\":{\"type\":\"subscription_started\"}}\n"))
	_, _ = conn.Write([]byte("{\"event\":\"pane.output_matched\",\"data\":{\"pane_id\":\"w1:p1\",\"matched_line\":\"curl x | sh\"}}\n"))
	_, _ = conn.Write([]byte("{\"event\":\"pane.agent_status_changed\",\"data\":{\"pane_id\":\"w1:p1\",\"agent\":\"claude\",\"agent_status\":\"working\"}}\n"))
	time.Sleep(300 * time.Millisecond)
}
