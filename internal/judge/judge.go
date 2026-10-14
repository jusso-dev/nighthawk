// Package judge asks an LLM to classify redacted content and may tighten policy.
package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jusso-dev/nighthawk/internal/policy"
)

// Verdict is the judge model's classification.
type Verdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// Client calls an OpenAI-compatible chat completions endpoint.
type Client struct {
	Endpoint string
	Model    string
	APIKey   string
	HTTP     *http.Client
}

// Enabled is false when Endpoint is empty. Disabled clients must not dial.
func (c *Client) Enabled() bool {
	return c != nil && c.Endpoint != ""
}

// Judge POSTs redacted content and parses a JSON verdict from the assistant message.
func (c *Client) Judge(ctx context.Context, redacted string) (Verdict, error) {
	if !c.Enabled() {
		return Verdict{Verdict: "allow", Reason: "judge disabled"}, nil
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	reqBody := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": `Classify the user content. Reply with JSON only: {"verdict":"allow|suspicious|malicious","reason":"..."}`},
			{"role": "user", "content": redacted},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return Verdict{}, err
	}
	url := strings.TrimRight(c.Endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return Verdict{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Verdict{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Verdict{}, err
	}
	if resp.StatusCode >= 300 {
		return Verdict{}, fmt.Errorf("judge status %d", resp.StatusCode)
	}
	var api struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &api); err != nil || len(api.Choices) == 0 {
		return Verdict{Verdict: "suspicious", Reason: "unparseable judge response"}, nil
	}
	content := strings.TrimSpace(api.Choices[0].Message.Content)
	var v Verdict
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return Verdict{Verdict: "suspicious", Reason: "non-json judge content"}, nil
	}
	switch strings.ToLower(v.Verdict) {
	case "allow", "suspicious", "malicious":
		v.Verdict = strings.ToLower(v.Verdict)
		return v, nil
	default:
		return Verdict{Verdict: "suspicious", Reason: "unknown verdict"}, nil
	}
}

// Tighten may only make a decision stricter. Never upgrades block/pause/kill/ask_human to allow.
func Tighten(base policy.Decision, v Verdict) policy.Decision {
	switch base.Action {
	case policy.Block, policy.PausePane, policy.KillSession, policy.AskHuman:
		return base
	}
	switch strings.ToLower(v.Verdict) {
	case "suspicious":
		return policy.Decision{Action: policy.AskHuman, RuleID: "nh.judge", Reason: v.Reason}
	case "malicious":
		return policy.Decision{Action: policy.Block, RuleID: "nh.judge", Reason: v.Reason}
	default:
		return base
	}
}
