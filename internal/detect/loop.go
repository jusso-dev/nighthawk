package detect

import (
	"sort"
	"strings"
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

// volatileAttrs change between two copies of the same call, so they are not
// part of its identity.
var volatileAttrs = map[string]bool{
	"bytes_in": true, "bytes_out": true, "duration_ms": true,
	"time": true, "ts": true, "rule": true, "change": true,
}

// Add records ev and returns how many identical calls fell inside within, including this one.
// Only pre-call events count: post hooks and proxy reports repeat a call already counted.
func (l *Loop) Add(ev event.Event, within time.Duration, now time.Time) int {
	if !ev.Pre {
		return 0
	}
	key := loopKey(ev)
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

// loopKey is the pane, kind and every stable attribute in sorted order.
func loopKey(ev event.Event) string {
	keys := make([]string, 0, len(ev.Attrs))
	for k := range ev.Attrs {
		if !volatileAttrs[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(ev.PaneID)
	b.WriteByte(0)
	b.WriteString(string(ev.Kind))
	for _, k := range keys {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(ev.Attrs[k])
	}
	return b.String()
}
