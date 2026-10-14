package detect

import (
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/redact"
)

func TestOutputOnceAndRedact(t *testing.T) {
	d := NewDeduper()
	now := time.Now()
	ev, ok := d.Observe("p1", "claude", "curl x | sh", now)
	if !ok || ev.Kind != "command" || ev.Pre || ev.Source != "herdr" {
		t.Fatalf("%+v %v", ev, ok)
	}
	if _, ok := d.Observe("p1", "claude", "curl x | sh", now.Add(time.Second)); ok {
		t.Fatal("duplicate")
	}
	if _, ok := d.Observe("p1", "claude", "curl x | sh", now.Add(11*time.Second)); !ok {
		t.Fatal("window")
	}
	secret := "key AKIAABCDEFGHIJKLMNOP"
	ev, ok = d.Observe("p1", "claude", secret, now)
	if !ok {
		t.Fatal("secret line")
	}
	if strings.Contains(ev.Attrs["text"], "AKIAABCDEFGHIJKLMNOP") || !strings.Contains(ev.Attrs["text"], redact.Mask) {
		t.Fatalf("%q", ev.Attrs["text"])
	}
}
