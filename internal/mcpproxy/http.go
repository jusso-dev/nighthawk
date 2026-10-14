package mcpproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Serve proxies streamable-HTTP MCP JSON-RPC on ln to upstream.
// Mcp-Session-Id and Authorization are forwarded; Authorization is never logged.
// Denied tools/call is answered locally and never dials upstream.
// Serve proxies with no pin filter.
func Serve(ctx context.Context, ln net.Listener, upstream, server string, d Decider, logf func(LogLine)) error {
	return serve(ctx, ln, upstream, server, d, logf, nil)
}

// ServePinned is Serve plus tools/list pin filtering.
func ServePinned(ctx context.Context, ln net.Listener, upstream, server string, d Decider, logf func(LogLine), pins *PinState) error {
	return serve(ctx, ln, upstream, server, d, logf, pins)
}

func serve(ctx context.Context, ln net.Listener, upstream, server string, d Decider, logf func(LogLine), pins *PinState) error {
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			proxyPass(r.Context(), client, w, r, upstream)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		fwd, local := gateMessage(r.Context(), body, server, d, logf)
		if fwd == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if len(local) > 0 {
				_, _ = w.Write(local[0])
				if local[0][len(local[0])-1] != '\n' {
					_, _ = w.Write([]byte{'\n'})
				}
			}
			return
		}

		out, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstream, bytes.NewReader(fwd))
		if err != nil {
			http.Error(w, "bad upstream", http.StatusBadGateway)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "" {
			out.Header.Set("Content-Type", ct)
		} else {
			out.Header.Set("Content-Type", "application/json")
		}
		if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
			out.Header.Set("Mcp-Session-Id", sid)
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			out.Header.Set("Authorization", auth)
		}
		if accept := r.Header.Get("Accept"); accept != "" {
			out.Header.Set("Accept", accept)
		}

		resp, err := client.Do(out)
		if err != nil {
			http.Error(w, "upstream error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			if strings.EqualFold(k, "Content-Length") {
				continue
			}
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if pins == nil {
			_, _ = io.Copy(w, resp.Body)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return
		}
		ids := newIDSet()
		noteToolsList(fwd, ids)
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			raw = rewriteSSE(raw, server, ids, pins)
		} else {
			raw = rewriteToolsList(raw, server, ids, pins)
		}
		_, _ = w.Write(raw)
	})

	srv := &http.Server{Handler: mux}
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

func proxyPass(ctx context.Context, client *http.Client, w http.ResponseWriter, r *http.Request, upstream string) {
	out, err := http.NewRequestWithContext(ctx, r.Method, upstream, r.Body)
	if err != nil {
		http.Error(w, "bad upstream", http.StatusBadGateway)
		return
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			out.Header.Add(k, v)
		}
	}
	resp, err := client.Do(out)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
