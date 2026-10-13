package detect

import (
	"sync"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
)

// Loop counts identical calls per pane.
type Loop struct {
	mu  sync.Mutex
	buf map[string][]time.Time
}

// NewLoop returns an empty counter.
func NewLoop() *Loop { return &Loop{buf: map[string][]time.Time{}} }

// Add records ev and returns how many identical calls fell inside within, including this one.
func (l *Loop) Add(ev event.Event, within time.Duration, now time.Time) int {
	key := ev.PaneID + "\x00" + string(ev.Kind) + "\x00" + ev.Attrs["command"] + "\x00" + ev.Attrs["mcp_server"] + "\x00" + ev.Attrs["mcp_tool"] + "\x00" + ev.Attrs["args"]
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-within)
	prev := l.buf[key]
	kept := prev[:0]
	for _, ts := range prev {
		if ts.After(cut) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	l.buf[key] = kept
	return len(kept)
}
