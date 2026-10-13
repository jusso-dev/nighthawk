package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// herdrSettings mimics a settings.json after `herdr integration install claude`
// plus a user hook. Formatting is deliberately not Go's, to check byte restore.
const herdrSettings = `{
    "hooks": {
        "SessionStart": [{"hooks": [{"type": "command", "command": "bash '/home/u/.claude/hooks/herdr-agent-state.sh' session"}]}],
        "Stop": [{"hooks": [{"type": "command", "command": "bash '/home/u/.claude/hooks/herdr-agent-state.sh' stop"}]}],
        "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo keep"}]}]
    },
    "other": 1
}
`

func TestClaudeInstallIdempotentDryAndUninstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	initial := []byte(herdrSettings)
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
	if b, _ := os.ReadFile(path); !bytes.Equal(b, initial) {
		t.Fatal("dry wrote file")
	}
	for i := 0; i < 2; i++ {
		if _, err := ClaudeInstall(path, "/usr/local/bin/nighthawk", false); err != nil {
			t.Fatal(err)
		}
	}
	if side, err := os.ReadFile(path + ".nighthawk-original"); err != nil || !bytes.Equal(side, initial) {
		t.Fatalf("sidecar: %v %s", err, side)
	}
	if baks, _ := filepath.Glob(path + ".nighthawk.bak.*"); len(baks) != 1 {
		t.Fatalf("backups %v", baks)
	}
	root, err := parseJSONObject(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	hooks := root["hooks"].(map[string]any)
	for _, ev := range claudeEvents {
		n := 0
		for _, e := range hooks[ev].([]any) {
			for _, h := range e.(map[string]any)["hooks"].([]any) {
				if isOurClaudeHook(h) {
					n++
				}
			}
		}
		if n != 1 {
			t.Fatalf("%s has %d nighthawk hooks", ev, n)
		}
	}
	after := string(mustRead(t, path))
	for _, keep := range []string{"herdr-agent-state.sh' session", "herdr-agent-state.sh' stop", "echo keep", `"timeout": 120`} {
		if !strings.Contains(after, keep) {
			t.Fatalf("missing %q: %s", keep, after)
		}
	}
	if _, err := ClaudeUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	if final := mustRead(t, path); !bytes.Equal(final, initial) {
		t.Fatalf("not restored byte for byte:\n%s", final)
	}
	if _, err := os.Stat(path + ".nighthawk-original"); !os.IsNotExist(err) {
		t.Fatal("sidecar left")
	}
}

func TestClaudeUninstallKeepsLaterEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(herdrSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaudeInstall(path, "/usr/local/bin/nighthawk", false); err != nil {
		t.Fatal(err)
	}
	root, _ := parseJSONObject(mustRead(t, path))
	root["model"] = "opus"
	b, _ := marshalJSON(root)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaudeUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	final := string(mustRead(t, path))
	if strings.Contains(final, "nighthawk hook claude") || !strings.Contains(final, `"model": "opus"`) ||
		!strings.Contains(final, "herdr-agent-state.sh' session") || !strings.Contains(final, "echo keep") {
		t.Fatalf("%s", final)
	}
}

func TestClaudeInstallNoFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := ClaudeInstall(path, "nighthawk", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaudeUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file left: %v", err)
	}
}

func TestCodexStringNotify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	initial := []byte("keep_me = 1\nnotify = \"oldcmd\"\n")
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CodexInstall(path, "nighthawk", false); err != nil {
		t.Fatal(err)
	}
	m := readTOML(t, path)
	if v, ok := m["keep_me"].(int64); !ok || v != 1 {
		t.Fatalf("keep_me=%#v", m["keep_me"])
	}
	if n, ok := m["notify"].([]any); !ok || len(n) != 3 || n[0] != "nighthawk" || n[1] != "hook" || n[2] != "codex" {
		t.Fatalf("notify=%#v", m["notify"])
	}
	if m["nighthawk_original_command"] != "oldcmd" {
		t.Fatalf("orig=%v", m["nighthawk_original_command"])
	}
	if _, err := CodexUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	if final := mustRead(t, path); !bytes.Equal(final, initial) {
		t.Fatalf("not restored: %s", final)
	}
}

func TestCodexArrayNotifyRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	initial := []byte("model = \"gpt-5-codex\"\nnotify = [\"notify-send\", \"Codex\"]\n\n[mcp_servers.docs]\ncommand = \"docs-mcp\"\n")
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := CodexInstall(path, "/opt/nighthawk", false); err != nil {
			t.Fatal(err)
		}
	}
	m := readTOML(t, path)
	if orig, ok := m["nighthawk_original_command"].([]any); !ok || len(orig) != 2 || orig[0] != "notify-send" {
		t.Fatalf("orig=%#v", m["nighthawk_original_command"])
	}
	if !isOurNotify(m["notify"]) {
		t.Fatalf("notify=%#v", m["notify"])
	}
	// A later edit stops the byte restore, but notify must still come back.
	m["model"] = "o3"
	b, err := toml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CodexUninstall(path, false); err != nil {
		t.Fatal(err)
	}
	m2 := readTOML(t, path)
	if n, ok := m2["notify"].([]any); !ok || len(n) != 2 || n[0] != "notify-send" || n[1] != "Codex" {
		t.Fatalf("notify=%#v", m2["notify"])
	}
	if _, ok := m2["nighthawk_original_command"]; ok {
		t.Fatal("orig key left")
	}
	if _, ok := m2["mcp_servers"]; !ok || m2["model"] != "o3" {
		t.Fatalf("lost keys: %#v", m2)
	}
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(mustRead(t, path), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
