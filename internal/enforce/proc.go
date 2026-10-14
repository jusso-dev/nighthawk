package enforce

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// InterruptKeys are the keys sent to pause an agent.
// Checked 2026-10-13: Claude Code treats Escape as an interrupt, Codex treats Ctrl-C as an interrupt.
// Key names are Herdr 0.9.3 logical keys (`herdr --skill`: esc, ctrl+c).
var InterruptKeys = map[string][]string{
	"claude":  {"esc"},
	"codex":   {"ctrl+c"},
	"grok":    {"ctrl+c"},
	"default": {"ctrl+c"},
}

// KeysFor returns the interrupt sequence for an agent.
func KeysFor(agent string) []string {
	if k, ok := InterruptKeys[agent]; ok {
		return k
	}
	return InterruptKeys["default"]
}

// Descended reports whether pid is ancestor or a descendant of ancestor.
func Descended(pid, ancestor int) bool {
	if pid <= 0 || ancestor <= 0 {
		return false
	}
	if pid == ancestor {
		return true
	}
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		seen[pid] = true
		pp, err := ppid(pid)
		if err != nil || pp <= 0 {
			return false
		}
		if pp == ancestor {
			return true
		}
		pid = pp
	}
	return false
}

func ppid(pid int) (int, error) {
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return 0, err
		}
		s := string(b)
		i := strings.LastIndex(s, ")")
		if i < 0 || i+2 >= len(s) {
			return 0, fmt.Errorf("stat")
		}
		fields := strings.Fields(s[i+2:])
		if len(fields) < 2 {
			return 0, fmt.Errorf("stat fields")
		}
		return strconv.Atoi(fields[1])
	}
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}
