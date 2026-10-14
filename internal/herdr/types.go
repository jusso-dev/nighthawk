package herdr

import "encoding/json"

// Subscription is one events.subscribe entry.
// Checked against Herdr 0.9.3 `herdr api schema --json` on 2026-10-14.
// pane.output_matched requires pane_id, source and match. pane.agent_detected
// takes only the type.
type Subscription struct {
	Event     string `json:"type"`
	PaneID    string `json:"pane_id,omitempty"`
	Match     *Match `json:"match,omitempty"`
	Source    string `json:"source,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	StripANSI bool   `json:"strip_ansi,omitempty"`
}

// Match is the internally tagged OutputMatch: type is "regex" or "substring".
type Match struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Regex returns a regex OutputMatch.
func Regex(re string) *Match { return &Match{Type: "regex", Value: re} }

// Envelope is one pushed event. Lifecycle names use underscores.
// Per-pane subscriptions echo the dotted subscription type.
type Envelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// ErrorBody is a Herdr error response body.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ErrorBody) Error() string { return e.Code + ": " + e.Message }

// AgentInfo is one row of result.agents from `herdr agent list`.
type AgentInfo struct {
	PaneID       string        `json:"pane_id"`
	Agent        string        `json:"agent"`
	Status       string        `json:"agent_status"`
	Cwd          string        `json:"cwd"`
	WorkspaceID  string        `json:"workspace_id"`
	AgentSession *AgentSession `json:"agent_session"`
	Title        string        `json:"title"`
}

// AgentSession is the agent's own session identity reported to Herdr.
type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// SessionID returns agent_session.value, or "".
func (a AgentInfo) SessionID() string {
	if a.AgentSession == nil {
		return ""
	}
	return a.AgentSession.Value
}

// ProcessInfo is result.process_info from `herdr pane process-info`.
type ProcessInfo struct {
	PaneID    string    `json:"pane_id"`
	ShellPID  int       `json:"shell_pid"`
	Processes []Process `json:"foreground_processes"`
}

// Process is one foreground process of a pane.
type Process struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// OutputMatch is the data of pane.output_matched.
type OutputMatch struct {
	PaneID      string `json:"pane_id"`
	MatchedLine string `json:"matched_line"`
}
