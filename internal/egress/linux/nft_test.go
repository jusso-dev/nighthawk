package linux

import (
	"runtime"
	"strings"
	"testing"
)

func TestTableText(t *testing.T) {
	got := Table("/user.slice/agent.scope")
	for _, want := range []string{"table inet nighthawk", "socket cgroupv2", "/user.slice/agent.scope", "redirect to :8765"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s\n%s", want, got)
		}
	}
}

func TestApplyRefusesUnprivileged(t *testing.T) {
	err := Apply(t.Context(), "/x")
	if err == nil {
		t.Fatal("expected error")
	}
	if runtime.GOOS != "linux" && !strings.Contains(err.Error(), "linux") {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" && osGeteuid() == 0 {
		t.Skip("root")
	}
}

func osGeteuid() int { return osGeteuidImpl() }
