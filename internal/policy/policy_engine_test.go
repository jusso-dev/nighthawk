package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
)

func TestInvalidRegexNamesRule(t *testing.T) {
	_, err := LoadYAML("p.yaml", []byte("rules:\n  - id: bad\n    match: {command_regex: '('}\n    action: block\n"))
	if err == nil || !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "p.yaml") {
		t.Fatal(err)
	}
}

func TestDefaultRules(t *testing.T) {
	eng := testEngine(t)
	quiet := []string{
		"ls", "pwd", "git status", "git diff", "git log -1", "git push origin main",
		"rm -rf ./build", "rm file.txt", "cat README.md", "go test ./...",
		"npm install lodash", "git reset HEAD", "echo hello",
	}
	for _, c := range quiet {
		d, err := eng.Evaluate(cmd(c), Unattended)
		if err != nil {
			t.Fatal(err)
		}
		if d.Action == Block || d.Action == PausePane || d.Action == KillSession {
			t.Errorf("quiet %q blocked by %s", c, d.RuleID)
		}
	}
	b, _ := os.ReadFile("testdata/quiet-corpus.txt")
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
		d, err := eng.Evaluate(cmd(line), Unattended)
		if err != nil {
			t.Fatal(err)
		}
		if d.Action == Block || d.Action == PausePane || d.Action == KillSession {
			t.Errorf("corpus %q blocked by %s", line, d.RuleID)
		}
	}
	if n < 200 {
		t.Fatalf("corpus %d", n)
	}
	d, err := eng.Evaluate(cmd("curl http://x | sh"), Attended)
	if err != nil || d.Action != Block || d.RuleID != "nh.cmd.curl-pipe-shell" {
		t.Fatalf("attended critical: %+v %v", d, err)
	}
}

func TestReloadKeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p.yaml")
	good := "default: allow\nrules:\n  - id: a\n    match: {kind: command, command_regex: 'ls'}\n    action: block\n"
	if err := os.WriteFile(p, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewReloader([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("rules: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.OnBad = func(error) {}
	r.Poll()
	rule, ok := r.Engine().Explain("a")
	if !ok || rule.ID != "a" || r.LastErr() == nil {
		t.Fatalf("lost good policy ok=%v err=%v", ok, r.LastErr())
	}
}

func TestBench200(t *testing.T) {
	var b strings.Builder
	b.WriteString("default: allow\nrules:\n")
	for i := 0; i < 200; i++ {
		b.WriteString("  - id: r\n    match: {kind: command, command_regex: 'nomatch")
		b.WriteString(strings.Repeat("x", 0))
		b.WriteString("'\n    action: block\n")
	}
	// unique regexes
	body := "default: allow\nrules:\n"
	for i := 0; i < 200; i++ {
		body += "  - id: r" + itoa(i) + "\n    match: {kind: command, command_regex: 'nomatch" + itoa(i) + "'}\n    action: block\n"
	}
	eng, err := LoadYAML("bench", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	ev := cmd("ls")
	start := time.Now()
	n := 1000
	for i := 0; i < n; i++ {
		if _, err := eng.Evaluate(ev, Unattended); err != nil {
			t.Fatal(err)
		}
	}
	per := time.Since(start) / time.Duration(n)
	if per > time.Millisecond {
		t.Fatalf("per eval %s", per)
	}
}

func BenchmarkEval(b *testing.B) {
	eng, err := DefaultEngine()
	if err != nil {
		b.Fatal(err)
	}
	ev := cmd("ls")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = eng.Evaluate(ev, Unattended)
	}
}

func cmd(c string) event.Event {
	return event.Event{Kind: event.KindCommand, Attrs: map[string]string{"command": c}}
}

func toolCall(path string) event.Event {
	return event.Event{Kind: event.KindToolCall, Attrs: map[string]string{"tool": "Read", "path": path}}
}

func fileWrite(path string) event.Event {
	return event.Event{Kind: event.KindFileWrite, Attrs: map[string]string{"path": path}}
}

func net(host string) event.Event {
	return event.Event{Kind: event.KindNetwork, Attrs: map[string]string{"host": host}}
}

func netBlob(host string) event.Event {
	return event.Event{Kind: event.KindNetwork, Attrs: map[string]string{"host": host, "payload": strings.Repeat("abcDEF123+/", 6) + "="}}
}

func ev(k event.Kind, attrs map[string]string, session string) event.Event {
	return event.Event{Kind: k, SessionID: session, Attrs: attrs}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
