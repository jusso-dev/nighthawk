package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestClaudeInstallIdempotentDryAndUninstall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	initial := []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo keep"}]}]},"other":1}`)
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err := ClaudeInstall(path, "/usr/local/bin/nighthawk", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "nighthawk hook claude") {
		t.Fatalf("diff=%s", diff)
	}
	b, _ := os.ReadFile(path)
	if !bytesEqual(b, initial) {
		t.Fatal("dry wrote file")
	}
	if _, err := ClaudeInstall(path, "/usr/local/bin/nighthawk", false); err != nil {
		t.Fatal(err)
	}
	side, err := os.ReadFile(path + ".nighthawk-original")
	if err != nil || !bytesEqual(side, initial) {
		t.Fatalf("sidecar: %v %s", err, side)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "nighthawk hook claude") || !strings.Contains(string(after), "echo keep") {
		t.Fatalf("%s", after)
	}
	if _, err := ClaudeInstall(path, "/usr/local/bin/nighthawk", false); err != nil {
		t.Fatal(err)
	}
	after2, _ := os.ReadFile(path)
	if strings.Count(string(after2), "nighthawk hook claude") != 1 {
		t.Fatalf("not idempotent: %s", after2)
	}
	if _, err := ClaudeUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	final, _ := os.ReadFile(path)
	if strings.Contains(string(final), "nighthawk hook claude") {
		t.Fatalf("hook left: %s", final)
	}
	if !strings.Contains(string(final), "echo keep") {
		t.Fatalf("lost other hook: %s", final)
	}
}

func TestCodexKeepMe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("keep_me = 1\nnotify = \"oldcmd\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CodexInstall(path, "nighthawk", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["keep_me"] != int64(1) && m["keep_me"] != 1 {
		// pelletier may decode as int64
		switch v := m["keep_me"].(type) {
		case int64:
			if v != 1 {
				t.Fatalf("keep_me=%v", m["keep_me"])
			}
		case int:
			if v != 1 {
				t.Fatalf("keep_me=%v", m["keep_me"])
			}
		default:
			t.Fatalf("keep_me type %T %v", m["keep_me"], m["keep_me"])
		}
	}
	if m["notify"] != "nighthawk hook codex" {
		t.Fatalf("notify=%v", m["notify"])
	}
	if m["nighthawk_original_command"] != "oldcmd" {
		t.Fatalf("orig=%v", m["nighthawk_original_command"])
	}
	if _, err := CodexUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(path)
	var m2 map[string]any
	if err := toml.Unmarshal(b2, &m2); err != nil {
		t.Fatal(err)
	}
	if m2["notify"] != "oldcmd" {
		t.Fatalf("restored notify=%v", m2["notify"])
	}
	if _, ok := m2["nighthawk_original_command"]; ok {
		t.Fatal("orig key left")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
