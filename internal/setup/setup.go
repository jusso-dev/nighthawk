// Package setup installs and removes agent hook integrations.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	claudeHookSuffix = " hook claude"
	codexHookSuffix  = " hook codex"
	sidecarSuffix    = ".nighthawk-original"
	codexOrigKey     = "nighthawk_original_command"
	codexNotifyKey   = "notify"
)

// ClaudeInstall adds a PreToolUse command hook. Idempotent. dry=true returns a
// diff and does not write. First install saves path+".nighthawk-original".
func ClaudeInstall(path, hookBin string, dry bool) (string, error) {
	cmd := strings.TrimSpace(hookBin) + claudeHookSuffix
	orig, err := readOrEmpty(path)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if len(bytes.TrimSpace(orig)) == 0 {
		root = map[string]any{}
	} else if err := json.Unmarshal(orig, &root); err != nil {
		return "", err
	}
	if claudeHasHook(root, cmd) {
		return "", nil
	}
	addClaudeHook(root, cmd)
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	next = append(next, '\n')
	diff := unifiedDiff(path, orig, next)
	if dry {
		return diff, nil
	}
	if err := saveSidecarOnce(path, orig); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		return "", err
	}
	return diff, nil
}

// ClaudeUninstall removes only our PreToolUse hook command.
func ClaudeUninstall(path string, dry bool) (string, error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(orig, &root); err != nil {
		return "", err
	}
	removeClaudeHook(root)
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	next = append(next, '\n')
	diff := unifiedDiff(path, orig, next)
	if dry {
		return diff, nil
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		return "", err
	}
	return diff, nil
}

func claudeHasHook(root map[string]any, cmd string) bool {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	arr, _ := hooks["PreToolUse"].([]any)
	for _, entry := range arr {
		m, _ := entry.(map[string]any)
		if m == nil {
			continue
		}
		inners, _ := m["hooks"].([]any)
		for _, h := range inners {
			hm, _ := h.(map[string]any)
			if hm == nil {
				continue
			}
			c, _ := hm["command"].(string)
			if c == cmd || strings.Contains(c, "nighthawk hook claude") {
				return true
			}
		}
	}
	return false
}

func addClaudeHook(root map[string]any, cmd string) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	arr, _ := hooks["PreToolUse"].([]any)
	entry := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": cmd,
			},
		},
	}
	hooks["PreToolUse"] = append(arr, entry)
}

func removeClaudeHook(root map[string]any) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	arr, _ := hooks["PreToolUse"].([]any)
	var kept []any
	for _, entry := range arr {
		m, _ := entry.(map[string]any)
		if m == nil {
			kept = append(kept, entry)
			continue
		}
		inners, _ := m["hooks"].([]any)
		var keepInner []any
		for _, h := range inners {
			hm, _ := h.(map[string]any)
			if hm == nil {
				keepInner = append(keepInner, h)
				continue
			}
			c, _ := hm["command"].(string)
			if strings.Contains(c, "nighthawk hook claude") {
				continue
			}
			keepInner = append(keepInner, h)
		}
		if len(keepInner) == 0 {
			continue
		}
		m["hooks"] = keepInner
		kept = append(kept, m)
	}
	if len(kept) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = kept
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
}

// CodexInstall wraps notify with hookBin + " hook codex", saving the prior
// value under nighthawk_original_command. Preserves unknown keys.
func CodexInstall(path, hookBin string, dry bool) (string, error) {
	cmd := strings.TrimSpace(hookBin) + codexHookSuffix
	orig, err := readOrEmpty(path)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if len(bytes.TrimSpace(orig)) == 0 {
		root = map[string]any{}
	} else if err := toml.Unmarshal(orig, &root); err != nil {
		return "", err
	}
	cur, _ := root[codexNotifyKey].(string)
	if cur == cmd || strings.Contains(cur, "nighthawk hook codex") {
		return "", nil
	}
	if cur != "" {
		if _, ok := root[codexOrigKey]; !ok {
			root[codexOrigKey] = cur
		}
	}
	root[codexNotifyKey] = cmd
	next, err := toml.Marshal(root)
	if err != nil {
		return "", err
	}
	diff := unifiedDiff(path, orig, next)
	if dry {
		return diff, nil
	}
	if err := saveSidecarOnce(path, orig); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		return "", err
	}
	return diff, nil
}

// CodexUninstall restores nighthawk_original_command or removes notify.
func CodexUninstall(path string, dry bool) (string, error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := toml.Unmarshal(orig, &root); err != nil {
		return "", err
	}
	if saved, ok := root[codexOrigKey].(string); ok {
		root[codexNotifyKey] = saved
		delete(root, codexOrigKey)
	} else {
		cur, _ := root[codexNotifyKey].(string)
		if strings.Contains(cur, "nighthawk hook codex") {
			delete(root, codexNotifyKey)
		}
	}
	next, err := toml.Marshal(root)
	if err != nil {
		return "", err
	}
	diff := unifiedDiff(path, orig, next)
	if dry {
		return diff, nil
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		return "", err
	}
	return diff, nil
}

func readOrEmpty(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

func saveSidecarOnce(path string, orig []byte) error {
	side := path + sidecarSuffix
	if _, err := os.Stat(side); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if orig == nil {
		orig = []byte{}
	}
	return os.WriteFile(side, orig, 0o600)
}

func unifiedDiff(path string, oldB, newB []byte) string {
	if bytes.Equal(oldB, newB) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)
	oldLines := strings.Split(string(oldB), "\n")
	newLines := strings.Split(string(newB), "\n")
	for _, l := range oldLines {
		if l == "" && len(oldLines) == 1 {
			continue
		}
		fmt.Fprintf(&b, "-%s\n", l)
	}
	for _, l := range newLines {
		if l == "" && len(newLines) == 1 {
			continue
		}
		fmt.Fprintf(&b, "+%s\n", l)
	}
	return b.String()
}
