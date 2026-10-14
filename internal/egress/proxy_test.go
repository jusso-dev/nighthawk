package egress

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func upstream(t *testing.T) (net.Listener, *int32) {
	t.Helper()
	var hits int32
	up := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte("ok"))
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go up.Serve(ln)
	t.Cleanup(func() { up.Close() })
	return ln, &hits
}

func serveProxy(t *testing.T, p *Proxy) string {
	t.Helper()
	pln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.Serve(ctx, pln)
	return pln.Addr().String()
}

func proxyClient(t *testing.T, user, addr string) *http.Client {
	t.Helper()
	u := "http://" + addr
	if user != "" {
		u = "http://" + user + "@" + addr
	}
	proxyURL, _ := url.Parse(u)
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 3 * time.Second}
}

func get(t *testing.T, c *http.Client, u string) (int, string) {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// connect sends a raw CONNECT and returns the status line.
func connect(t *testing.T, proxy, target, auth string) string {
	t.Helper()
	c, err := net.DialTimeout("tcp", proxy, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if auth != "" {
		req += "Proxy-Authorization: " + auth + "\r\n"
	}
	if _, err := io.WriteString(c, req+"\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, _ := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line)
}

type recorder struct {
	mu   sync.Mutex
	evs  []event.Event
	decs []policy.Decision
}

func (r *recorder) on(e event.Event, d policy.Decision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, e)
	r.decs = append(r.decs, d)
}

func (r *recorder) snapshot() ([]event.Event, []policy.Decision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.evs...), append([]policy.Decision(nil), r.decs...)
}

func TestProxyAllowDenyAndPane(t *testing.T) {
	ln, _ := upstream(t)
	var rec recorder
	addr := serveProxy(t, &Proxy{
		Allow:   Allow{Hosts: []string{"127.0.0.1"}, AllowPrivate: []string{"127.0.0.1/32"}},
		OnEvent: rec.on,
		Pane:    func(tok string) string { return "pane-" + tok },
	})
	client := proxyClient(t, "abc", addr)
	if code, body := get(t, client, "http://"+ln.Addr().String()+"/x"); code != 200 || body != "ok" {
		t.Fatalf("%d %s", code, body)
	}
	got, _ := rec.snapshot()
	if len(got) != 1 || got[0].PaneID != "pane-abc" || got[0].Attrs["bytes_out"] == "0" {
		t.Fatalf("%+v", got)
	}
	if code, body := get(t, client, "http://evil.example/x"); code != 403 || body == "" {
		t.Fatalf("%d %s", code, body)
	}
}

// An allowlisted name, or an allowlisted IP, that points at a private address
// is refused at dial time even when Decide allows it.
func TestProxyPrivateCheckedAtDial(t *testing.T) {
	ln, hits := upstream(t)
	_, upPort, _ := net.SplitHostPort(ln.Addr().String())
	var decides int32
	var rec recorder
	addr := serveProxy(t, &Proxy{
		Allow: Allow{
			Hosts: []string{"localhost", "127.0.0.1", "rebind.example"},
			Resolve: func(host string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("127.0.0.1")}, nil
			},
		},
		Decide: func(context.Context, string, string, int) (policy.Decision, error) {
			atomic.AddInt32(&decides, 1)
			return policy.Decision{Action: policy.Allow, RuleID: "nh.allow.network"}, nil
		},
		OnEvent: rec.on,
	})
	client := proxyClient(t, "", addr)
	for _, h := range []string{"localhost", "127.0.0.1", "rebind.example"} {
		if code, body := get(t, client, "http://"+h+":"+upPort+"/x"); code != 403 || !strings.Contains(body, "nh.net.private") {
			t.Fatalf("GET %s: %d %s", h, code, body)
		}
		if line := connect(t, addr, h+":"+upPort, ""); !strings.Contains(line, " 403 ") {
			t.Fatalf("CONNECT %s: %s", h, line)
		}
	}
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Fatalf("upstream reached %d times", n)
	}
	_, decs := rec.snapshot()
	if len(decs) != 6 || int(atomic.LoadInt32(&decides)) != 6 {
		t.Fatalf("events %d decides %d", len(decs), decides)
	}
	for _, d := range decs {
		if d.Action != policy.Block || d.RuleID != "nh.net.private" {
			t.Fatalf("%+v", d)
		}
	}
}

func TestProxyTokenRequired(t *testing.T) {
	ln, hits := upstream(t)
	var rec recorder
	addr := serveProxy(t, &Proxy{
		Allow:   Allow{Hosts: []string{"127.0.0.1"}, AllowPrivate: []string{"127.0.0.1/32"}},
		OnEvent: rec.on,
		Pane: func(tok string) string {
			if tok == "good" {
				return "w1:p1"
			}
			return ""
		},
	})
	target := "http://" + ln.Addr().String() + "/x"
	for _, user := range []string{"", "forgedpane"} {
		if code, _ := get(t, proxyClient(t, user, addr), target); code != http.StatusProxyAuthRequired {
			t.Fatalf("user %q: %d", user, code)
		}
		auth := ""
		if user != "" {
			auth = "Basic Zm9yZ2VkcGFuZTo=" // forgedpane:
		}
		if line := connect(t, addr, ln.Addr().String(), auth); !strings.Contains(line, " 407 ") {
			t.Fatalf("CONNECT user %q: %s", user, line)
		}
	}
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Fatalf("upstream reached %d times", n)
	}
	if code, body := get(t, proxyClient(t, "good", addr), target); code != 200 || body != "ok" {
		t.Fatalf("%d %s", code, body)
	}
	got, _ := rec.snapshot()
	if len(got) != 1 || got[0].PaneID != "w1:p1" {
		t.Fatalf("%+v", got)
	}
}

// Without a Pane lookup the proxy user is not trusted as a pane id.
func TestProxyStandaloneIgnoresUser(t *testing.T) {
	ln, _ := upstream(t)
	var panes []string
	var mu sync.Mutex
	addr := serveProxy(t, &Proxy{
		Allow: Allow{Hosts: []string{"127.0.0.1"}, AllowPrivate: []string{"127.0.0.1/32"}},
		Decide: func(_ context.Context, pane, _ string, _ int) (policy.Decision, error) {
			mu.Lock()
			panes = append(panes, pane)
			mu.Unlock()
			return policy.Decision{Action: policy.Allow, RuleID: "nh.allow.network"}, nil
		},
	})
	if code, _ := get(t, proxyClient(t, "forgedpane", addr), "http://"+ln.Addr().String()+"/x"); code != 200 {
		t.Fatal(code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(panes) != 1 || panes[0] != "" {
		t.Fatalf("%q", panes)
	}
}

// emit passes the gate's decision along; Decide runs once per connection.
func TestProxyDecidesOncePerConnection(t *testing.T) {
	ln, _ := upstream(t)
	var decides int32
	var rec recorder
	addr := serveProxy(t, &Proxy{
		Allow: Allow{Hosts: []string{"127.0.0.1"}, AllowPrivate: []string{"127.0.0.1/32"}},
		Decide: func(context.Context, string, string, int) (policy.Decision, error) {
			atomic.AddInt32(&decides, 1)
			return policy.Decision{Action: policy.Allow, RuleID: "rule-x"}, nil
		},
		OnEvent: rec.on,
	})
	client := proxyClient(t, "", addr)
	for i := 0; i < 3; i++ {
		if code, _ := get(t, client, "http://"+ln.Addr().String()+"/x"); code != 200 {
			t.Fatal(code)
		}
	}
	evs, decs := rec.snapshot()
	if n := atomic.LoadInt32(&decides); n != 3 || len(decs) != 3 {
		t.Fatalf("decides %d events %d", n, len(decs))
	}
	for i, d := range decs {
		if d.RuleID != "rule-x" || evs[i].Pre {
			t.Fatalf("%+v %+v", d, evs[i])
		}
	}
}

// A request without a destination host is rejected before policy runs, so no
// unknown-host decision is made for an empty host.
func TestProxyEmptyHostNotDecided(t *testing.T) {
	var decides int32
	addr := serveProxy(t, &Proxy{
		Decide: func(context.Context, string, string, int) (policy.Decision, error) {
			atomic.AddInt32(&decides, 1)
			return policy.Decision{Action: policy.Block, RuleID: "nh.net.unknown-host"}, nil
		},
	})
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || atomic.LoadInt32(&decides) != 0 {
		t.Fatalf("%d %d", resp.StatusCode, decides)
	}
	if line := connect(t, addr, ":443", ""); !strings.Contains(line, " 400 ") {
		t.Fatal(line)
	}
	if n := atomic.LoadInt32(&decides); n != 0 {
		t.Fatal(n)
	}
}
