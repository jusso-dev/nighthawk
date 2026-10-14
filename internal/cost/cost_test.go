package cost

import (
	"testing"
	"time"
)

func TestPriceAndUnknown(t *testing.T) {
	tab := Embedded()
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	got := tab.Line(Usage{Model: "claude-3-5-sonnet-20241022", Input: 1_000_000, Output: 1_000_000}, at, 1.5)
	if got.Unpriced || got.USD != 18 || got.AUD != 27 {
		t.Fatalf("%+v", got)
	}
	unk := tab.Line(Usage{Model: "no-such-model", Input: 1000}, at, 0)
	if !unk.Unpriced || unk.USD != 0 {
		t.Fatalf("%+v", unk)
	}
}

func TestFutureRateSkipped(t *testing.T) {
	tab, err := Parse([]byte("prices:\n  - {model: m, input: 9, effective_from: 2099-01-01, source: https://example.com}\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := tab.Line(Usage{Model: "m", Input: 1_000_000}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 0)
	if !got.Unpriced {
		t.Fatalf("%+v", got)
	}
}

func TestTaskID(t *testing.T) {
	if TaskID("hsc-job-9", "") != "hsc-job-9" || TaskID("claude", "t1") != "t1" || TaskID("claude", "") != "" {
		t.Fatal(TaskID("hsc-job-9", ""), TaskID("claude", "t1"))
	}
}
