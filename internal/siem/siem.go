// Package siem maps nighthawk events to a forwardable envelope.
package siem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

// Envelope is nighthawk.event.v1 — our field map, not a vendor schema.
type Envelope struct {
	Spec    string `json:"spec"`
	Time    string `json:"time"`
	Kind    string `json:"kind"`
	Action  string `json:"action"`
	RuleID  string `json:"rule_id"`
	PaneID  string `json:"pane_id"`
	Agent   string `json:"agent"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

// Map builds an Envelope from an event and decision.
func Map(e event.Event, d policy.Decision) Envelope {
	ts := e.Time
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return Envelope{
		Spec:    "nighthawk.event.v1",
		Time:    ts.UTC().Format(time.RFC3339Nano),
		Kind:    string(e.Kind),
		Action:  string(d.Action),
		RuleID:  d.RuleID,
		PaneID:  e.PaneID,
		Agent:   e.Agent,
		Source:  string(e.Source),
		Message: d.RuleID,
	}
}

// SyslogLine returns an RFC 5424-shaped line containing PRI and the JSON body.
func SyslogLine(env Envelope) string {
	raw, _ := json.Marshal(env)
	// facility local0 (16), severity notice (5) => PRI 133
	ts := env.Time
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("<133>1 %s nighthawk - - - %s", ts, raw)
}

// PostNDJSON POSTs body to url.
func PostNDJSON(ctx context.Context, url string, body []byte, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("siem status %d", resp.StatusCode)
	}
	return nil
}
