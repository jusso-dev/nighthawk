package capture

import (
	"encoding/json"

	"github.com/jusso-dev/nighthawk/internal/capture/claude"
	"github.com/jusso-dev/nighthawk/internal/event"
)

type claudeAdapter struct{}

func (claudeAdapter) Name() string { return "claude" }
func (claudeAdapter) ToEvents(in []byte, pane, task string) ([]event.Event, error) {
	h, err := claude.Parse(in)
	if err != nil {
		return nil, err
	}
	return claude.ToEvents(h, pane, task), nil
}

type grokAdapter struct{}

func (grokAdapter) Name() string { return "grok" }
func (grokAdapter) ToEvents(in []byte, pane, task string) ([]event.Event, error) {
	h, err := claude.Parse(in)
	if err != nil {
		return nil, err
	}
	evs := claude.ToEvents(h, pane, task)
	for i := range evs {
		evs[i].Agent = "grok"
	}
	return evs, nil
}

type codexAdapter struct{}

func (codexAdapter) Name() string { return "codex" }
func (codexAdapter) ToEvents(in []byte, pane, task string) ([]event.Event, error) {
	var probe struct {
		HookEventName string `json:"hook_event_name"`
		Type          string `json:"type"`
	}
	if err := json.Unmarshal(in, &probe); err != nil {
		return nil, err
	}
	if probe.HookEventName != "" {
		h, err := claude.Parse(in)
		if err != nil {
			return nil, err
		}
		evs := claude.ToEvents(h, pane, task)
		for i := range evs {
			evs[i].Agent = "codex"
		}
		return evs, nil
	}
	ev := event.Event{
		Kind: event.KindPane, Source: event.SourceAgentHook, Agent: "codex",
		PaneID: pane, TaskID: task, Pre: false,
		Attrs: map[string]string{"notify": probe.Type},
	}
	return []event.Event{ev}, nil
}
