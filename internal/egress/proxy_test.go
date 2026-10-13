package egress

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
)

func TestProxyAllowDenyAndPane(t *testing.T) {
	up := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go up.Serve(ln)
	defer up.Close()

	pln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var got []event.Event
	p := &Proxy{
		Allow:   Allow{Hosts: []string{"127.0.0.1"}, AllowPrivate: []string{"127.0.0.1/32"}},
		OnEvent: func(e event.Event) { got = append(got, e) },
		Pane:    func(tok string) string { return "pane-" + tok },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Serve(ctx, pln)

	proxyURL, _ := url.Parse("http://abc@" + pln.Addr().String())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if len(got) != 1 || got[0].PaneID != "pane-abc" || got[0].Attrs["bytes_out"] == "0" {
		t.Fatalf("%+v", got)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://evil.example/x", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 || len(b) == 0 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}
