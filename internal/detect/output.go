package detect

import (
	"regexp"
	"sync"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/redact"
)

// Pattern is one pane-output detection.
type Pattern struct {
	ID string
	Re *regexp.Regexp
}

// OutputPatterns are the default subscriptions.
var OutputPatterns = []Pattern{
	{ID: "nh.out.curl-sh", Re: regexp.MustCompile(`(curl|wget)[^|]*\|\s*(ba|z)?sh`)},
	{ID: "nh.out.rm-rf", Re: regexp.MustCompile(`rm\s+-[A-Za-z]*r[A-Za-z]*f`)},
	{ID: "nh.out.secret", Re: regexp.MustCompile(`AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}`)},
}

// Deduper suppresses the same pane and pattern inside a window.
type Deduper struct {
	mu     sync.Mutex
	Window time.Duration
	last   map[string]time.Time
}

// NewDeduper returns a 10s deduper.
func NewDeduper() *Deduper {
	return &Deduper{Window: 10 * time.Second, last: map[string]time.Time{}}
}

// Observe returns one command event when a pattern matches and is not a repeat.
func (d *Deduper) Observe(pane, agent, line string, now time.Time) (event.Event, bool) {
	var hit Pattern
	ok := false
	for _, p := range OutputPatterns {
		if p.Re.MatchString(line) {
			hit = p
			ok = true
			break
		}
	}
	if !ok {
		return event.Event{}, false
	}
	key := pane + "\x00" + hit.ID + "\x00" + line
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, seen := d.last[key]; seen && now.Sub(prev) < d.Window {
		return event.Event{}, false
	}
	d.last[key] = now
	return event.Event{
		Time:   now,
		Kind:   event.KindCommand,
		Source: event.SourceHerdr,
		PaneID: pane,
		Agent:  agent,
		Pre:    false,
		Attrs: map[string]string{
			"command": redact.String(line),
			"rule":    hit.ID,
			"text":    redact.String(line),
		},
	}, true
}
