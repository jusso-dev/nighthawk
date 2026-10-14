package mcpproxy

import (
	"context"
	"encoding/json"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/redact"
)

const maxLogBytes = 8 * 1024

// Decider evaluates one MCP event for allow or deny.
type Decider func(ctx context.Context, ev event.Event) (policy.Decision, error)

// LogLine is one redacted MCP proxy log record.
type LogLine struct {
	Method     string
	Server     string
	Tool       string
	ID         string
	Args       string
	Err        string
	ResultSize int
}

// Denied reports whether d is anything other than allow.
func Denied(d policy.Decision) bool {
	return d.Action != policy.Allow
}

// DenyError is a JSON-RPC error object. Message is exactly
// "blocked by Nighthawk: "+rule. Tool arguments are never included.
func DenyError(id json.RawMessage, rule string) []byte {
	if rule == "" {
		rule = "nh.failclosed"
	}
	type rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcErr          `json:"error"`
	}
	idOut := id
	if len(idOut) == 0 {
		idOut = []byte("null")
	}
	b, _ := json.Marshal(resp{
		JSONRPC: "2.0",
		ID:      idOut,
		Error:   rpcErr{Code: -32001, Message: "blocked by Nighthawk: " + rule},
	})
	return b
}

func clipLog(s string) string {
	s = redact.String(s)
	if len(s) > maxLogBytes {
		return s[:maxLogBytes]
	}
	return s
}

func decideCall(ctx context.Context, d Decider, server, tool, args string) (policy.Decision, error) {
	ev := event.Event{
		Kind:   event.KindMCPCall,
		Source: event.SourceMCPProxy,
		Pre:    true,
		Attrs: map[string]string{
			"mcp_server": server,
			"mcp_tool":   tool,
			"args":       args,
		},
	}
	if d == nil {
		return policy.Decision{Action: policy.Allow, RuleID: "nh.allow.mcp"}, nil
	}
	dec, err := d(ctx, ev)
	if err != nil {
		return policy.Decision{Action: policy.Block, RuleID: "nh.failclosed", Reason: "policy error"}, err
	}
	return dec, nil
}
