package ipc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/policy"
)

type allowHandler struct{}

func (allowHandler) Handle(ctx context.Context, req Request) Response {
	if req.Op == "block" {
		<-ctx.Done()
		return Response{OK: false, Error: "cancelled"}
	}
	return Response{OK: true, Decision: &policy.Decision{Action: policy.Allow}}
}

func TestPingRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nighthawk.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, path, allowHandler{}) }()
	waitSock(t, path)

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}

	var last time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		resp, err := Call(context.Background(), path, Request{Op: "ping"})
		last = time.Since(start)
		if err != nil || !resp.OK {
			t.Fatal(err, resp)
		}
	}
	if last > 5*time.Millisecond {
		t.Fatalf("ping %s", last)
	}
}

func TestDeadlineWhenStopped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Call(ctx, filepath.Join(t.TempDir(), "missing.sock"), Request{Op: "ping"})
	if err == nil {
		t.Fatal("want error")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("hung")
	}
}

func waitSock(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(path); err == nil && st.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("socket did not appear")
}
