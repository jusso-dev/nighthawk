// Package alert sends rate-limited notifications to Slack, webhooks, and Notify.
package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jusso-dev/nighthawk/internal/redact"
)

// Sender delivers alerts with per-rule+pane rate limiting.
type Sender struct {
	SlackURL   string
	WebhookURL string
	Secret     string
	Notify     func(title, body string) error
	Now        func() time.Time
	HTTP       *http.Client
	Window     time.Duration
	Backoff    time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

// Send redacts body and delivers at most once per rule+pane per Window.
func (s *Sender) Send(ctx context.Context, rule, pane, body string) error {
	if s == nil {
		return nil
	}
	window := s.Window
	if window == 0 {
		window = 5 * time.Minute
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	key := rule + "\x00" + pane
	s.mu.Lock()
	if s.last == nil {
		s.last = make(map[string]time.Time)
	}
	if t, ok := s.last[key]; ok && now().Sub(t) < window {
		s.mu.Unlock()
		return nil
	}
	s.last[key] = now()
	s.mu.Unlock()

	body = redact.String(body)
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}

	var firstErr error
	if s.SlackURL != "" {
		payload, _ := json.Marshal(map[string]string{"text": body})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.SlackURL, bytes.NewReader(payload))
		if err != nil {
			firstErr = err
		} else {
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				firstErr = err
			} else {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 300 {
					firstErr = fmt.Errorf("slack status %d", resp.StatusCode)
				}
			}
		}
	}
	if s.WebhookURL != "" {
		if err := s.postWebhook(ctx, client, body); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.Notify != nil {
		if err := s.Notify(rule, body); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Sender) postWebhook(ctx context.Context, client *http.Client, body string) error {
	backoff := s.Backoff
	if backoff == 0 {
		backoff = 10 * time.Millisecond
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader([]byte(body)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "text/plain")
		if s.Secret != "" {
			mac := hmac.New(sha256.New, []byte(s.Secret))
			mac.Write([]byte(body))
			req.Header.Set("X-Nighthawk-Signature", hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("webhook status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("webhook status %d", resp.StatusCode)
		}
		return nil
	}
	return lastErr
}
