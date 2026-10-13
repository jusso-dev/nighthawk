// Package policy decides what happens to an event. The full YAML rule engine
// arrives in M2; this skeleton fixes the decision types and the fail-closed
// rule so every capture path can depend on them from M0.
package policy

import "github.com/jusso-dev/nighthawk/internal/event"

// Action is what Nighthawk does with an event.
type Action string

const (
	Allow       Action = "allow"
	Block       Action = "block"
	PausePane   Action = "pause_pane"
	AskHuman    Action = "ask_human"
	KillSession Action = "kill_session"
)

// Mode is attended or unattended.
type Mode string

const (
	Attended   Mode = "attended"
	Unattended Mode = "unattended"
)

// Decision is the result of evaluating an event.
type Decision struct {
	Action Action `json:"action"`
	RuleID string `json:"rule_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Evaluator evaluates pre-action events.
type Evaluator interface {
	Evaluate(e event.Event, m Mode) (Decision, error)
}

// OnError is the fail-closed rule: if evaluation fails, unattended mode blocks
// and attended mode asks a human. Nothing ever silently allows on error.
func OnError(m Mode, err error) Decision {
	if m == Unattended {
		return Decision{Action: Block, RuleID: "nh.failclosed", Reason: "policy error: " + err.Error()}
	}
	return Decision{Action: AskHuman, RuleID: "nh.failclosed", Reason: "policy error: " + err.Error()}
}
