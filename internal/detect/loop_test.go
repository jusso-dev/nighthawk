package detect

import (
	"strconv"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
)

func TestLoopDistinctCallsDoNotCount(t *testing.T) {
	l := NewLoop()
	now := time.Now()
	for i := 0; i < 20; i++ {
		read := event.Event{PaneID: "p", Kind: event.KindToolCall, Pre: true,
			Attrs: map[string]string{"tool": "Read", "path": "/f" + strconv.Itoa(i)}}
		if n := l.Add(read, time.Minute, now); n != 1 {
			t.Fatalf("read %d counted %d", i, n)
		}
		net := event.Event{PaneID: "p", Kind: event.KindNetwork, Pre: true,
			Attrs: map[string]string{"host": "h" + strconv.Itoa(i) + ".example", "port": "443"}}
		if n := l.Add(net, time.Minute, now); n != 1 {
			t.Fatalf("host %d counted %d", i, n)
		}
	}
}

func TestLoopSameCallCountsPreOnly(t *testing.T) {
	l := NewLoop()
	now := time.Now()
	var n int
	for i := 0; i < 3; i++ {
		pre := event.Event{PaneID: "p", Kind: event.KindNetwork, Pre: true,
			Attrs: map[string]string{"host": "a.example", "port": "443"}}
		n = l.Add(pre, time.Minute, now)
		post := event.Event{PaneID: "p", Kind: event.KindNetwork,
			Attrs: map[string]string{"host": "a.example", "port": "443", "bytes_in": strconv.Itoa(i)}}
		if got := l.Add(post, time.Minute, now); got != 0 {
			t.Fatalf("post counted %d", got)
		}
	}
	if n != 3 {
		t.Fatalf("count %d want 3", n)
	}
	if got := l.Add(event.Event{PaneID: "p", Kind: event.KindNetwork, Pre: true,
		Attrs: map[string]string{"host": "a.example", "port": "443"}}, time.Minute, now.Add(2*time.Minute)); got != 1 {
		t.Fatalf("window not applied: %d", got)
	}
}
