package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/event"
)

func testServer(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	t.Setenv("HERDR_BIN_PATH", "")
	t.Setenv("HERDR_SOCKET_PATH", "")
	s, err := openServer(Options{StateDir: t.TempDir(), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.st.Close() })
	return s
}

func TestEnvTokenRandomAndStable(t *testing.T) {
	s := testServer(t, config.Default())
	tokOf := func(pane string) string {
		t.Helper()
		resp := s.env(pane)
		if !resp.OK {
			t.Fatal(resp.Error)
		}
		var vars map[string]string
		if err := json.Unmarshal(resp.Data, &vars); err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(vars["HTTPS_PROXY"])
		if err != nil || u.User == nil {
			t.Fatalf("%v %q", err, vars["HTTPS_PROXY"])
		}
		return u.User.Username()
	}
	a, b := tokOf("w1:p1"), tokOf("w1:p2")
	if len(a) != 32 || a == b {
		t.Fatalf("%q %q", a, b)
	}
	if again := tokOf("w1:p1"); again != a {
		t.Fatalf("token changed: %q %q", a, again)
	}
	if s.paneForToken(a) != "w1:p1" || s.paneForToken("w1:p1") != "" || s.paneForToken("") != "" {
		t.Fatal("token lookup")
	}
}

// The gate evaluates policy once. The finished-connection record must not
// evaluate again, or loop rules count every connection twice.
func TestProxyCountsLoopOnce(t *testing.T) {
	up := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})}
	uln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go up.Serve(uln)
	defer up.Close()

	cfg := config.Default()
	cfg.Egress.Allowlist = []string{"127.0.0.1"}
	cfg.Egress.AllowPrivate = []string{"127.0.0.1/32"}
	s := testServer(t, cfg)
	tok := s.remember("w1:p1")

	pln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.proxy().Serve(ctx, pln)

	proxyURL, _ := url.Parse("http://" + tok + "@" + pln.Addr().String())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 3 * time.Second}
	const n = 3
	for i := 0; i < n; i++ {
		resp, err := client.Get("http://" + uln.Addr().String() + "/x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
	}
	probe := event.Event{Kind: event.KindNetwork, PaneID: "w1:p1"}
	if got := s.loops.Add(probe, time.Minute, time.Now()); got != n+1 {
		t.Fatalf("loop count %d, want %d", got, n+1)
	}

	forged, _ := url.Parse("http://" + strings.Repeat("0", 32) + "@" + pln.Addr().String())
	client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(forged)}, Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + uln.Addr().String() + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatal(resp.StatusCode)
	}
}
