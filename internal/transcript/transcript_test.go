package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseClaude(t *testing.T) {
	line := []byte(`{"message":{"model":"m","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`)
	model, in, out, cr, cw, key, ok := ParseClaude(line)
	if !ok || model != "m" || in != 1 || out != 2 || cr != 3 || cw != 4 {
		t.Fatalf("got model=%q in=%d out=%d cr=%d cw=%d key=%q ok=%v", model, in, out, cr, cw, key, ok)
	}
	if _, _, _, _, _, _, ok := ParseClaude([]byte(`{"no":"usage"}`)); ok {
		t.Fatal("expected !ok")
	}
}

func TestParseCodex(t *testing.T) {
	line := []byte(`{"type":"event_msg","payload":{"type":"token_count","info":{"model":"gpt-4o","last_token_usage":{"input_tokens":1,"output_tokens":2}}}}`)
	model, in, out, cr, cw, _, ok := ParseCodex(line)
	if !ok || model != "gpt-4o" || in != 1 || out != 2 || cr != 0 || cw != 0 {
		t.Fatalf("got model=%q in=%d out=%d cr=%d cw=%d ok=%v", model, in, out, cr, cw, ok)
	}
	if _, _, _, _, _, _, ok := ParseCodex([]byte(`{"type":"other"}`)); ok {
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
