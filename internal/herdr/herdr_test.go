package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIArgvAndDecode(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeherdr")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$NH_ARGS\"\nif [ \"$1\" = \"--machine\" ]; then shift 2; fi\nshift\ncase \"$1:$2\" in\n  agent:list) echo '[{\"pane_id\":\"w1:p1\",\"agent\":\"claude\",\"status\":\"idle\",\"cwd\":\"/work\",\"workspace_id\":\"w1\",\"agent_session_id\":\"s1\"}]' ;;\n  pane:process-info) echo '{\"pane_id\":\"w1:p1\",\"shell_pid\":10,\"pids\":[11]}' ;;\n  *) echo '{}' ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "args")
	t.Setenv("NH_ARGS", argsFile)
	c := CLI{Bin: bin, Machine: "m1"}
	ctx := context.Background()
	agents, err := c.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Agent != "claude" || agents[0].AgentSessionID != "s1" {
		t.Fatalf("%+v", agents)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, " -c ") || strings.Contains(text, "\n-c\n") {
		t.Fatalf("shell: %q", text)
	}
	if !strings.Contains(text, "--machine\nm1\n--json\nagent\nlist\n") {
		t.Fatalf("argv:\n%s", text)
	}
	info, err := c.ProcessInfo(ctx, "w1:p1")
	if err != nil || info.ShellPID != 10 || info.PIDs[0] != 11 {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestSubscribeReconnectResync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveOnce(ln)
	resyncs := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Subscribe(ctx, path, []Subscription{{Event: "pane.output_matched", PaneID: "w1:p1", Match: "curl", Source: "recent", Lines: 200, StripANSI: true}}, func() error {
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
