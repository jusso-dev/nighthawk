package egress

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

// Proxy is an HTTP and CONNECT proxy bound to one listener.
type Proxy struct {
	Allow   Allow
	Decide  func(ctx context.Context, pane, host string, port int) (policy.Decision, error)
	OnEvent func(event.Event)
	Pane    func(token string) string
}

// Serve accepts until ctx is cancelled.
func (p *Proxy) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: p}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ServeHTTP enforces the allowlist, then tunnels or forwards.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, port := splitHost(r)
	pane := p.pane(r)
	dec := p.gate(r.Context(), pane, host, port)
	if dec.Action != policy.Allow {
		rule := dec.RuleID
		if rule == "" {
			rule = "nh.failclosed"
		}
		http.Error(w, "blocked by Nighthawk: "+rule+"\n", http.StatusForbidden)
		return
	}
	start := time.Now()
	var in, out int64
	var err error
	if r.Method == http.MethodConnect {
		in, out, err = p.tunnel(w, r, host, port)
	} else {
		in, out, err = p.forward(w, r)
	}
	if err != nil {
		return
	}
	p.emit(pane, host, port, in, out, time.Since(start), dec.RuleID)
}

func (p *Proxy) pane(r *http.Request) string {
	tok := PaneFromRequest(r)
	if p.Pane == nil || tok == "" {
		return tok
	}
	if pane := p.Pane(tok); pane != "" {
		return pane
	}
	return tok
}

// PaneFromRequest reads the proxy URL user. That user is the pane token.
func PaneFromRequest(r *http.Request) string {
	if r.URL != nil && r.URL.User != nil {
		if u := r.URL.User.Username(); u != "" {
			return u
		}
	}
	if u := proxyUser(r.Header.Get("Proxy-Authorization")); u != "" {
		return u
	}
	if u, _, ok := r.BasicAuth(); ok {
		return u
	}
	return ""
}

func proxyUser(h string) string {
	const prefix = "basic "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(h[len(prefix):]))
	if err != nil {
		return ""
	}
	user, _, _ := strings.Cut(string(b), ":")
	return user
}

func (p *Proxy) gate(ctx context.Context, pane, host string, port int) policy.Decision {
	if p.Decide != nil {
		d, err := p.Decide(ctx, pane, host, port)
		if err != nil {
			return policy.Decision{Action: policy.Block, RuleID: "nh.failclosed", Reason: "policy error"}
		}
		return d
	}
	if p.Allow.Allows(host, port) {
		return policy.Decision{Action: policy.Allow, RuleID: "nh.allow.network"}
	}
	return policy.Decision{Action: policy.Block, RuleID: "nh.net.unknown-host"}
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) (int64, int64, error) {
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.Header.Del("Proxy-Authorization")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "blocked by Nighthawk: nh.net.dial\n", http.StatusBadGateway)
		return 0, 0, err
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	n, err := io.Copy(w, resp.Body)
	var in int64
	if r.ContentLength > 0 {
		in = r.ContentLength
	}
	return in, n, err
}

func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request, host string, port int) (int64, int64, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "blocked by Nighthawk: nh.net.dial\n", http.StatusInternalServerError)
		return 0, 0, errors.New("hijack")
	}
	dest, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		http.Error(w, "blocked by Nighthawk: nh.net.dial\n", http.StatusBadGateway)
		return 0, 0, err
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		dest.Close()
		return 0, 0, err
	}
	_, _ = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = buf.Flush()
	var in, out int64
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(dest, conn)
		atomic.AddInt64(&in, n)
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(conn, dest)
		atomic.AddInt64(&out, n)
		done <- struct{}{}
	}()
	<-done
	conn.Close()
	dest.Close()
	<-done
	return atomic.LoadInt64(&in), atomic.LoadInt64(&out), nil
}

func (p *Proxy) emit(pane, host string, port int, in, out int64, dur time.Duration, rule string) {
	if p.OnEvent == nil {
		return
	}
	p.OnEvent(event.Event{
		Kind:   event.KindNetwork,
		Source: event.SourceEgress,
		PaneID: pane,
		Attrs: map[string]string{
			"host":        host,
			"port":        strconv.Itoa(port),
			"bytes_in":    strconv.FormatInt(in, 10),
			"bytes_out":   strconv.FormatInt(out, 10),
			"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
			"rule":        rule,
		},
	})
}

func splitHost(r *http.Request) (string, int) {
	h := r.Host
	if r.URL != nil && r.URL.Host != "" {
		h = r.URL.Host
	}
	host, portStr, err := net.SplitHostPort(h)
	if err != nil {
		host = h
		if r.Method == http.MethodConnect {
			portStr = "443"
		} else {
			portStr = "80"
		}
	}
	port, _ := strconv.Atoi(portStr)
	return normHost(host), port
}
