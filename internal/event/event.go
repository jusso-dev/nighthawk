// Package event defines the normalised event every capture path emits.
package event

import "time"

// Kind is the category of an observed action.
type Kind string

const (
	KindToolCall   Kind = "tool_call"
	KindMCPCall    Kind = "mcp_call"
	KindNetwork    Kind = "network"
	KindCommand    Kind = "command"
	KindFileWrite  Kind = "file_write"
	KindSkill      Kind = "skill"
	KindTokenUsage Kind = "token_usage"
	KindPane       Kind = "pane"
	KindTamper     Kind = "tamper"
)

// Source is the capture path that produced the event.
type Source string

const (
	SourceHerdr      Source = "herdr"
	SourceAgentHook  Source = "agent_hook"
	SourceMCPProxy   Source = "mcp_proxy"
	SourceEgress     Source = "egress_proxy"
	SourceTranscript Source = "transcript"
)

// Event is one observed action, before or after it happened.
type Event struct {
	ID        string            `json:"id"`
	Time      time.Time         `json:"time"`
	Kind      Kind              `json:"kind"`
	Source    Source            `json:"source"`
	Machine   string            `json:"machine,omitempty"`
	PaneID    string            `json:"pane_id,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	TaskID    string            `json:"task_id,omitempty"`
	Pre       bool              `json:"pre"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}
