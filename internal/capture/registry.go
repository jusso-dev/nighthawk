package capture

import "github.com/jusso-dev/nighthawk/internal/event"

// Adapter parses an agent hook payload.
type Adapter interface {
	Name() string
	ToEvents(in []byte, pane, task string) ([]event.Event, error)
}

// ByName returns a built-in adapter.
func ByName(name string) (Adapter, bool) {
	switch name {
	case "claude":
		return claudeAdapter{}, true
	case "codex":
		return codexAdapter{}, true
	case "grok":
		return grokAdapter{}, true
	default:
		return nil, false
	}
}
