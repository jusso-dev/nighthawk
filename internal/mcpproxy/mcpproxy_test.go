package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestStdioDeny(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen.txt")
	script := filepath.Join(dir, "child.sh")
	body := "#!/bin/bash\n" +
		"seen=" + seen + "\n" +
		"while IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\" >> \"$seen\"\n" +
		"  id=$(printf '%s' \"$line\" | sed -n 's/.*\"id\":\\([0-9]*\\).*/\\1/p')\n" +
		"  if [ -z \"$id\" ]; then id=1; fi\n" +
		"  printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"ok\":true}}\\n' \"$id\"\n" +
		"done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = rIn, wOut
	defer func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = rIn.Close()
		_ = wOut.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := Decider(func(ctx context.Context, ev event.Event) (policy.Decision, error) {
		if ev.Attrs["mcp_tool"] == "danger" {
			return policy.Decision{Action: policy.Block, RuleID: "nh.test.deny"}, nil
		}
		return policy.Decision{Action: policy.Allow, RuleID: "nh.allow.mcp"}, nil
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunStdio(ctx, "srv", []string{script}, d, nil)
	}()

	outCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(rOut)
		outCh <- b
	}()

	denyReq := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"danger","arguments":{"x":1}}}` + "\n"
	allowReq := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"safe","arguments":{"y":2}}}` + "\n"
	if _, err := wIn.Write([]byte(denyReq + allowReq)); err != nil {
		t.Fatal(err)
	}
	_ = wIn.Close()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		cancel()
		_ = wOut.Close()
		t.Fatal("timeout waiting RunStdio")
	}
	_ = wOut.Close()
	cancel()
	out := <-outCh

	got := string(out)
	if !strings.Contains(got, "-32001") || !strings.Contains(got, "blocked by Nighthawk:") {
		t.Fatalf("proxy stdout missing deny: %s", got)
	}

	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "danger") || strings.Contains(string(raw), `"id":7`) {
		t.Fatalf("denied call reached child: %s", raw)
	}
	if !strings.Contains(string(raw), "safe") {
		t.Fatalf("allowed call missing from child: %s", raw)
	}
}

func TestStdioPassthrough(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo.sh")
	body := "#!/bin/bash\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = rIn, wOut
	defer func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = rIn.Close()
		_ = wOut.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunStdio(ctx, "srv", []string{script}, nil, nil)
	}()

	outCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(rOut)
		outCh <- b
	}()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	if _, err := wIn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	_ = wIn.Close()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		cancel()
		_ = wOut.Close()
		t.Fatal("timeout waiting RunStdio")
	}
	_ = wOut.Close()
	cancel()
	buf := <-outCh

	if !strings.Contains(string(buf), `"method":"initialize"`) {
		t.Fatalf("expected echo of initialize, got %s", buf)
	}
}

func TestPinFilter(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	base := Tool{Name: "t", Description: "old", Schema: schema}
	h := HashTool(base)
	changed := Tool{Name: "t", Description: "new", Schema: schema}

	filtered, tampers, pending := Diff("s", []Tool{changed}, map[string]string{"t": h}, true)
	if len(filtered) != 0 {
		t.Fatalf("unattended should drop changed tool: %+v", filtered)
	}
	if len(tampers) != 1 || tampers[0].Old != h || tampers[0].New == h {
		t.Fatalf("tampers: %+v", tampers)
	}
	if pending["t"] == "" || pending["t"] == h {
		t.Fatalf("pending: %+v", pending)
	}

	filtered, tampers, pending = Diff("s", []Tool{changed}, map[string]string{"t": h}, false)
	if len(filtered) != 1 || filtered[0].Description != "new" {
		t.Fatalf("attended should keep: %+v", filtered)
	}
	if len(tampers) != 1 {
		t.Fatalf("tampers: %+v", tampers)
	}
	if pending["t"] == "" {
		t.Fatalf("pending: %+v", pending)
	}

	same := Tool{Name: "t", Description: "old", Schema: schema}
	filtered, tampers, pending = Diff("s", []Tool{same}, map[string]string{"t": h}, true)
	if len(filtered) != 1 || len(tampers) != 0 || len(pending) != 0 {
		t.Fatalf("unchanged: filt=%+v tampers=%+v pending=%+v", filtered, tampers, pending)
	}

	filtered, tampers, pending = Diff("s", []Tool{base}, map[string]string{}, true)
	if len(filtered) != 1 || len(tampers) != 0 || pending["t"] != h {
		t.Fatalf("first sight: filt=%+v tampers=%+v pending=%+v", filtered, tampers, pending)
	}
}

func TestRewriteDropsChangedTool(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	old := Tool{Name: "t", Description: "old", Schema: schema}
	h := HashTool(old)
	ids := newIDSet()
	noteToolsList([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), ids)
	pins := &PinState{Unattended: true, Pins: map[string]string{"t": h}}
	var n int
	pins.OnChange = func(fresh, changed map[string]string, tampers []Tamper) {
		n += len(tampers)
	}
	line := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"t","description":"new","inputSchema":{"type":"object"}}]}}`)
	out := rewriteToolsList(line, "srv", ids, pins)
	if bytes.Contains(out, []byte(`"name":"t"`)) {
		t.Fatalf("still offered %s", out)
	}
	if n != 1 {
		t.Fatalf("tampers %d", n)
	}
}

func TestHTTPDeny(t *testing.T) {
	var hits atomic.Int64
	up := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})}
	uln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go up.Serve(uln)
	defer up.Close()

	pln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var logs []LogLine
	d := Decider(func(ctx context.Context, ev event.Event) (policy.Decision, error) {
		return policy.Decision{Action: policy.Block, RuleID: "nh.http.deny"}, nil
	})
	go Serve(ctx, pln, "http://"+uln.Addr().String(), "httpsrv", d, func(ll LogLine) {
		logs = append(logs, ll)
	})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"a":1}}}`
	req, err := http.NewRequest(http.MethodPost, "http://"+pln.Addr().String()+"/", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer SecretValueXYZ")
	req.Header.Set("Mcp-Session-Id", "sess-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if hits.Load() != 0 {
		t.Fatalf("upstream hit %d times", hits.Load())
	}
	if !strings.Contains(string(raw), "-32001") || !strings.Contains(string(raw), "blocked by Nighthawk:") {
		t.Fatalf("deny body: %s", raw)
	}
	for _, ll := range logs {
		blob, _ := json.Marshal(ll)
		if strings.Contains(string(blob), "SecretValueXYZ") {
			t.Fatalf("secret leaked into log: %s", blob)
		}
	}
}
