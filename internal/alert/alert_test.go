package alert

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendRedact(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s := &Sender{SlackURL: srv.URL, Window: time.Minute}
	key := "AKIAIOSFODNN7EXAMPLE"
	if err := s.Send(context.Background(), "r1", "p1", "leak "+key); err != nil {
		t.Fatal(err)
	}
	if got == "" || !contains(got, "[REDACTED]") || contains(got, key) {
		t.Fatalf("body=%q", got)
	}
	if !contains(got, "r1") || !contains(got, "pane p1") || !contains(got, "leak ") {
		t.Fatalf("slack text lacks rule/pane/reason: %q", got)
	}
}

func TestRateLimit(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	now := time.Now()
	s := &Sender{
		SlackURL: srv.URL,
		Window:   time.Minute,
		Now:      func() time.Time { return now },
	}
	ctx := context.Background()
	_ = s.Send(ctx, "r", "p", "a")
	_ = s.Send(ctx, "r", "p", "b")
	if n.Load() != 1 {
		t.Fatalf("n=%d", n.Load())
	}
	now = now.Add(2 * time.Minute)
	_ = s.Send(ctx, "r", "p", "c")
	if n.Load() != 2 {
		t.Fatalf("n=%d after window", n.Load())
	}
}

func TestWebhookHMACRetry(t *testing.T) {
	var hits atomic.Int32
	var sig string
	var body []byte
	secret := "sekrit"
	when := time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC)
	bodyWant := `{"rule":"rule","pane":"pane","agent":"claude","reason":"curl piped to sh","time":"2026-10-14T01:02:03Z"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != bodyWant {
			t.Errorf("body %q", b)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type %q", ct)
		}
		body = b
		sig = r.Header.Get("X-Nighthawk-Signature")
		if hits.Add(1) < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s := &Sender{
		WebhookURL: srv.URL,
		Secret:     secret,
		Window:     time.Minute,
		Backoff:    time.Millisecond,
	}
	if err := s.SendAlert(context.Background(), Alert{Rule: "rule", Pane: "pane", Agent: "claude", Reason: "curl piped to sh", Time: when}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits=%d", hits.Load())
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Fatalf("sig=%q want %q", sig, want)
	}
}

func TestDefaultClientTimeout(t *testing.T) {
	if defaultClient.Timeout <= 0 || defaultClient.Timeout > 10*time.Second {
		t.Fatalf("timeout %v", defaultClient.Timeout)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
