package herdr

import "encoding/json"

// Subscription is one events.subscribe entry.
// Checked against Herdr 0.9.3 socket schema on 2026-10-13.
type Subscription struct {
	Event     string `json:"type"`
	PaneID    string `json:"pane_id,omitempty"`
	Match     string `json:"match,omitempty"`
	Source    string `json:"source,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	StripANSI bool   `json:"strip_ansi,omitempty"`
}

// Envelope is one pushed event. Lifecycle names use underscores.
// Per-pane subscriptions echo the dotted subscription type.
type Envelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// AgentInfo is one row from `herdr agent list --json`.
type AgentInfo struct {
	PaneID         string `json:"pane_id"`
	Agent          string `json:"agent"`
	Status         string `json:"status"`
	Cwd            string `json:"cwd"`
	WorkspaceID    string `json:"workspace_id"`
	AgentSessionID string `json:"agent_session_id"`
	Title          string `json:"title"`
}

// ProcessInfo is `herdr pane process-info --json`.
type ProcessInfo struct {
	PaneID   string `json:"pane_id"`
	ShellPID int    `json:"shell_pid"`
	PIDs     []int  `json:"pids"`
}

// OutputMatch is the data of pane.output_matched.
type OutputMatch struct {
	PaneID      string `json:"pane_id"`
	MatchedLine string `json:"matched_line"`
}
