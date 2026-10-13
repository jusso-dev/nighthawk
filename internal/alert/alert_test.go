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
	secret := "sekrit"
	bodyWant := "rule-x"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != bodyWant {
			t.Errorf("body %q", b)
		}
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
	if err := s.Send(context.Background(), "rule", "pane", bodyWant); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits=%d", hits.Load())
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(bodyWant))
	want := hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Fatalf("sig=%q want %q", sig, want)
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
