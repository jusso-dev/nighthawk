package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseClaude(t *testing.T) {
	line := []byte(`{"uuid":"u","message":{"id":"msg_x","model":"m","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`)
	u, ok := ParseClaude(line)
	if !ok || u.Model != "m" || u.Input != 1 || u.Output != 2 || u.CacheRead != 3 || u.CacheWrite != 4 || u.Key != "msg_x" {
		t.Fatalf("got %+v ok=%v", u, ok)
	}
	if _, ok := ParseClaude([]byte(`{"no":"usage"}`)); ok {
		t.Fatal("expected !ok")
	}
}

// Claude Code repeats one message's usage on each content block line.
// Counting unique message ids must not multiply the tokens.
func TestClaudeFixtureDedupesByMessageID(t *testing.T) {
	lines, _, err := ReadNew(filepath.Join("testdata", "claude.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]Usage{}
	for _, l := range lines {
		if u, ok := ParseClaude(l); ok {
			if _, dup := seen[u.Key]; !dup {
				seen[u.Key] = u
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("keys %d", len(seen))
	}
	var out int64
	for _, u := range seen {
		out += u.Output
	}
	if out != 57 {
		t.Fatalf("output %d want 57", out)
	}
	if got := seen["msg_1"].TS; got.IsZero() || got.Day() != 1 {
		t.Fatalf("ts %v", got)
	}
}

func TestParseCodexFixture(t *testing.T) {
	lines, _, err := ReadNew(filepath.Join("testdata", "codex.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	var model string
	seen := map[string]Usage{}
	for _, l := range lines {
		if m, ok := CodexModel(l); ok {
			model = m
			continue
		}
		u, ok := ParseCodex(l)
		if !ok {
			continue
		}
		if u.Model == "" {
			u.Model = model
		}
		if _, dup := seen[u.Key]; !dup {
			seen[u.Key] = u
		}
	}
	if len(seen) != 2 {
		t.Fatalf("keys %d %+v", len(seen), seen)
	}
	first := seen["total:1030"]
	if first.Model != "gpt-5-codex" || first.Input != 800 || first.CacheRead != 200 || first.Output != 30 || first.TS.IsZero() {
		t.Fatalf("first %+v", first)
	}
	second := seen["total:2145"]
	if second.Input != 200 || second.CacheRead != 900 || second.Output != 15 {
		t.Fatalf("second %+v", second)
	}
	if _, ok := ParseCodex([]byte(`{"type":"other"}`)); ok {
		t.Fatal("expected !ok")
	}
}

func TestReadNewPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	content := "line1\nline2\npartial"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, next, err := ReadNew(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || string(lines[0]) != "line1" || string(lines[1]) != "line2" {
		t.Fatalf("lines=%q", lines)
	}
	if next != int64(len("line1\nline2\n")) {
		t.Fatalf("next=%d want %d", next, len("line1\nline2\n"))
	}
	// append rest of partial + new line
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("-done\nline3\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	lines2, next2, err := ReadNew(path, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines2) != 2 || string(lines2[0]) != "partial-done" || string(lines2[1]) != "line3" {
		t.Fatalf("lines2=%q", lines2)
	}
	_ = next2
}
