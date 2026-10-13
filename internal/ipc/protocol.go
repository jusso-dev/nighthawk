package ipc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

// Request is one line-delimited JSON call.
type Request struct {
	Op    string          `json:"op"`
	Event *event.Event    `json:"event,omitempty"`
	Mode  policy.Mode     `json:"mode,omitempty"`
	ID    string          `json:"id,omitempty"`
	Who   string          `json:"who,omitempty"`
	Since string          `json:"since,omitempty"`
	By    string          `json:"by,omitempty"`
	Pane  string          `json:"pane,omitempty"`
	Kind  string          `json:"kind,omitempty"`
	Agent string          `json:"agent,omitempty"`
	Wait  time.Duration   `json:"wait_ns,omitempty"`
	Extra json.RawMessage `json:"extra,omitempty"`
}

// EventFilter selects stored events.
type EventFilter struct {
	Agent string     `json:"agent,omitempty"`
	Kind  event.Kind `json:"kind,omitempty"`
	From  time.Time  `json:"from,omitempty"`
	To    time.Time  `json:"to,omitempty"`
}

// Response is one line-delimited JSON reply.
type Response struct {
	OK       bool             `json:"ok"`
	Decision *policy.Decision `json:"decision,omitempty"`
	Error    string           `json:"error,omitempty"`
	Mode     policy.Mode      `json:"mode,omitempty"`
	Data     json.RawMessage  `json:"data,omitempty"`
}

// Handler serves one request.
type Handler interface {
	Handle(ctx context.Context, req Request) Response
}
