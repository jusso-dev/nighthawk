package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/daemon"
)

func TestWriteExportsEvalRoundTrip(t *testing.T) {
	vars := map[string]string{
		"HTTPS_PROXY": "http://abc@127.0.0.1:8765",
		"ODD":         `it's $HOME "x"`,
		"pane":        "w1:p1",
		"bad-name":    "x",
	}
	var out bytes.Buffer
	writeExports(&out, vars)
	if strings.Contains(out.String(), "pane") || strings.Contains(out.String(), "bad-name") {
		t.Fatalf("exported metadata: %s", out.String())
	}
	cmd := exec.Command("bash", "-c", `eval "$1"; printf '%s\n%s' "$HTTPS_PROXY" "$ODD"`, "x", out.String())
	got, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != vars["HTTPS_PROXY"]+"\n"+vars["ODD"] {
		t.Fatalf("%q", got)
	}
}

func TestEnvPrintsExportsAndJSON(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("HERDR_SOCKET_PATH", "")
	dir, err := os.MkdirTemp("", "nh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("NIGHTHAWK_STATE_DIR", dir)
	t.Setenv("NIGHTHAWK_CONFIG", filepath.Join(dir, "missing.yaml"))
	cfg := config.Default()
	cfg.Egress.Port = 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = daemon.Run(ctx, daemon.Options{StateDir: dir, Config: cfg}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(config.SocketPath(dir)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"env", "--pane", "w1:p1"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("code %d err %s", code, errOut.String())
	}
	cmd := exec.Command("bash", "-c", `eval "$1"; printf '%s' "$HTTPS_PROXY"`, "x", out.String())
	proxy, err := cmd.Output()
	if err != nil || !strings.HasPrefix(string(proxy), "http://") || !strings.Contains(string(proxy), "@127.0.0.1:") {
		t.Fatalf("%v %q from %q", err, proxy, out.String())
	}

	out.Reset()
	if code := run([]string{"env", "--pane", "w1:p1", "--json"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("code %d err %s", code, errOut.String())
	}
	var vars map[string]string
	if err := json.Unmarshal(out.Bytes(), &vars); err != nil || vars["HTTPS_PROXY"] != string(proxy) || vars["pane"] != "w1:p1" {
		t.Fatalf("%v %s", err, out.String())
	}
}
