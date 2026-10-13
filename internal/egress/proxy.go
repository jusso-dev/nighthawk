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
	Allow  Allow
	Decide func(ctx context.Context, pane, host string, port int) (policy.Decision, error)
	// OnEvent receives each finished connection with the decision already
	// made for it. It must not evaluate policy again.
	OnEvent func(event.Event, policy.Decision)
	// Pane maps a proxy token to a pane id. When set, requests with a missing
	// or unknown token get 407. When nil, requests run with no pane.
	Pane func(token string) string
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
	if host == "" || (r.Method != http.MethodConnect && (r.URL == nil || r.URL.Host == "")) {
		http.Error(w, "nighthawk: not a proxy request\n", http.StatusBadRequest)
		return
	}
	pane, ok := p.pane(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="nighthawk"`)
		http.Error(w, "nighthawk: proxy token required\n", http.StatusProxyAuthRequired)
		return
	}
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
	if errors.Is(err, ErrPrivate) {
		p.emit(pane, host, port, 0, 0, time.Since(start), policy.Decision{Action: policy.Block, RuleID: "nh.net.private", Reason: "private address"})
		return
	}
	if err != nil {
		return
	}
	p.emit(pane, host, port, in, out, time.Since(start), dec)
}

// pane maps the request's proxy token to a pane. Without a Pane lookup the
// token is not trusted as a pane id and the request runs with no pane.
func (p *Proxy) pane(r *http.Request) (string, bool) {
	if p.Pane == nil {
		return "", true
	}
	tok := PaneFromRequest(r)
	if tok == "" {
		return "", false
	}
	pane := p.Pane(tok)
	return pane, pane != ""
}

// PaneFromRequest returns the pane token: the user in Proxy-Authorization.
// The request URL and Authorization belong to the destination and are ignored.
func PaneFromRequest(r *http.Request) string {
	return proxyUser(r.Header.Get("Proxy-Authorization"))
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
	// The private address check runs at dial time, on the address dialled.
	if p.Allow.HostMatch(host) {
		return policy.Decision{Action: policy.Allow, RuleID: "nh.allow.network"}
	}
	return policy.Decision{Action: policy.Block, RuleID: "nh.net.unknown-host"}
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) (int64, int64, error) {
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.Header.Del("Proxy-Authorization")
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		h, ps, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		port, _ := strconv.Atoi(ps)
		return p.dial(ctx, h, port)
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		dialFail(w, err)
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
	dest, err := p.dial(r.Context(), host, port)
	if err != nil {
		dialFail(w, err)
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

// dial resolves host once, rejects private addresses, and dials the checked
// address. This runs whatever Decide returned, so an allowlisted name that
// resolves to loopback or link-local is still refused.
func (p *Proxy) dial(ctx context.Context, host string, port int) (net.Conn, error) {
	ips, err := p.Allow.Addrs(host)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		var c net.Conn
		c, err = d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		if err == nil {
			return c, nil
		}
	}
	return nil, err
}

func dialFail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrPrivate) {
		http.Error(w, "blocked by Nighthawk: nh.net.private\n", http.StatusForbidden)
		return
	}
	http.Error(w, "blocked by Nighthawk: nh.net.dial\n", http.StatusBadGateway)
}

func (p *Proxy) emit(pane, host string, port int, in, out int64, dur time.Duration, dec policy.Decision) {
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
			"rule":        dec.RuleID,
		},
	}, dec)
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
