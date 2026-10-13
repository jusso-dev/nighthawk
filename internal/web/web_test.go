package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthAndCSRF(t *testing.T) {
	const tok = "test-token-value"
	const csrf = "csrf-value"
	h := NewMux("/tmp/nh.sock", tok, csrf)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/?token="+tok, nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("with token: %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "Nighthawk") {
		t.Fatalf("body missing title")
	}
	if !strings.Contains(string(body), "function drawBars") {
		t.Fatal("missing embedded JS")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/approve?token="+tok, strings.NewReader("id=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("missing csrf: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	form := url.Values{"csrf": {csrf}, "id": {"1"}}
	req = httptest.NewRequest(http.MethodPost, "/approve?token="+tok, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("csrf ok expect 502 got %d", rr.Code)
	}
}

func TestServeRefusesRemote(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Serve(ctx, "0.0.0.0:18766", "", "t", false)
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("%v", err)
	}
}
