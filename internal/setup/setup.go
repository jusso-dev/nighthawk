// Package setup installs and removes agent hook integrations.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	claudeHookSuffix = " hook claude"
	claudeHookMark   = "nighthawk hook claude"
	codexHookMark    = "nighthawk hook codex"
	sidecarSuffix    = ".nighthawk-original"
	backupSuffix     = ".nighthawk.bak."
	codexOrigKey     = "nighthawk_original_command"
	codexNotifyKey   = "notify"
)

// ClaudePreToolTimeout is the PreToolUse hook timeout in seconds that
// ClaudeInstall sets. A hook waiting on a human approval must answer before it.
const ClaudePreToolTimeout = 120

// claudeEvents are the Claude Code hook events Nighthawk installs.
var claudeEvents = []string{"PreToolUse", "PostToolUse", "SessionStart", "Stop"}

// ClaudeInstall adds Nighthawk command hooks for PreToolUse and PostToolUse
// (matcher "*"), SessionStart and Stop. Idempotent. dry=true returns a diff
// and does not write. First install saves path+".nighthawk-original"; every
// write first backs up the old file to path+".nighthawk.bak.<unix ts>".
func ClaudeInstall(path, hookBin string, dry bool) (string, error) {
	cmd := strings.TrimSpace(hookBin) + claudeHookSuffix
	orig, err := readOrEmpty(path)
	if err != nil {
		return "", err
	}
	root, err := parseJSONObject(orig)
	if err != nil {
		return "", err
	}
	if !addClaudeHooks(root, cmd) {
		return "", nil
	}
	next, err := marshalJSON(root)
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
	return diff, writeWithBackup(path, orig, next)
}

// ClaudeUninstall removes only Nighthawk's hooks. When nothing else changed
// since install, it restores the original file byte for byte.
func ClaudeUninstall(path string, dry bool) (string, error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root, err := parseJSONObject(orig)
	if err != nil {
		return "", err
	}
	removeClaudeHooks(root)
	next, err := marshalJSON(root)
	if err != nil {
		return "", err
	}
	return uninstallWrite(path, orig, next, root, dry, func(b []byte) (map[string]any, error) {
		return parseJSONObject(b)
	})
}

func parseJSONObject(b []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(b)) == 0 {
		return root, nil
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func marshalJSON(root map[string]any) ([]byte, error) {
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(next, '\n'), nil
}

func isOurClaudeHook(h any) bool {
	hm, _ := h.(map[string]any)
	c, _ := hm["command"].(string)
	return strings.Contains(c, claudeHookMark)
}

// addClaudeHooks adds our entry to each event that lacks one and keeps the
// PreToolUse timeout current. It reports whether anything changed.
func addClaudeHooks(root map[string]any, cmd string) bool {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for _, name := range claudeEvents {
		arr, _ := hooks[name].([]any)
		found := false
		for _, entry := range arr {
			m, _ := entry.(map[string]any)
			inners, _ := m["hooks"].([]any)
			for _, h := range inners {
				if !isOurClaudeHook(h) {
					continue
				}
				found = true
				hm := h.(map[string]any)
				if name == "PreToolUse" && hm["timeout"] != float64(ClaudePreToolTimeout) {
					hm["timeout"] = float64(ClaudePreToolTimeout)
					changed = true
				}
			}
		}
		if found {
			continue
		}
		inner := map[string]any{"type": "command", "command": cmd}
		entry := map[string]any{"hooks": []any{inner}}
		switch name {
		case "PreToolUse":
			entry["matcher"] = "*"
			inner["timeout"] = float64(ClaudePreToolTimeout)
		case "PostToolUse":
			entry["matcher"] = "*"
		}
		hooks[name] = append(arr, entry)
		changed = true
	}
	if changed {
		root["hooks"] = hooks
	}
	return changed
}

// removeClaudeHooks drops our command from every event, then any entry or
// event that only held it.
func removeClaudeHooks(root map[string]any) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for name, v := range hooks {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		var kept []any
		ours := false
		for _, entry := range arr {
			m, _ := entry.(map[string]any)
			inners, isArr := m["hooks"].([]any)
			if !isArr {
				kept = append(kept, entry)
				continue
			}
			var keepInner []any
			for _, h := range inners {
				if isOurClaudeHook(h) {
					ours = true
					continue
				}
				keepInner = append(keepInner, h)
			}
			if len(keepInner) == 0 && len(inners) > 0 {
				continue
			}
			m["hooks"] = keepInner
			kept = append(kept, m)
		}
		if !ours {
			continue
		}
		if len(kept) == 0 {
			delete(hooks, name)
		} else {
			hooks[name] = kept
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
}

// CodexInstall points notify at hookBin hook codex, as the argv array Codex
// expects, saving any prior value under nighthawk_original_command.
// Preserves unknown keys.
func CodexInstall(path, hookBin string, dry bool) (string, error) {
	orig, err := readOrEmpty(path)
	if err != nil {
		return "", err
	}
	root, err := parseTOML(orig)
	if err != nil {
		return "", err
	}
	cur, has := root[codexNotifyKey]
	if isOurNotify(cur) {
		return "", nil
	}
	if has {
		if _, ok := root[codexOrigKey]; !ok {
			root[codexOrigKey] = cur
		}
	}
	root[codexNotifyKey] = []any{strings.TrimSpace(hookBin), "hook", "codex"}
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
	return diff, writeWithBackup(path, orig, next)
}

// CodexUninstall restores nighthawk_original_command or removes our notify.
// When nothing else changed since install, it restores the original bytes.
func CodexUninstall(path string, dry bool) (string, error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root, err := parseTOML(orig)
	if err != nil {
		return "", err
	}
	if saved, ok := root[codexOrigKey]; ok {
		if isOurNotify(root[codexNotifyKey]) {
			root[codexNotifyKey] = saved
		}
		delete(root, codexOrigKey)
	} else if isOurNotify(root[codexNotifyKey]) {
		delete(root, codexNotifyKey)
	}
	next, err := toml.Marshal(root)
	if err != nil {
		return "", err
	}
	return uninstallWrite(path, orig, next, root, dry, parseTOML)
}

func parseTOML(b []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(b)) == 0 {
		return root, nil
	}
	if err := toml.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// isOurNotify reports whether a notify value, string or argv array, runs our hook.
func isOurNotify(v any) bool {
	switch n := v.(type) {
	case string:
		return strings.Contains(n, codexHookMark)
	case []any:
		parts := make([]string, 0, len(n))
		for _, p := range n {
			s, _ := p.(string)
			parts = append(parts, s)
		}
		return strings.Contains(strings.Join(parts, " "), codexHookMark)
	}
	return false
}

// uninstallWrite writes next, or the saved original bytes when cleaned (next
// parsed) has the same content as the original, then drops the sidecar.
func uninstallWrite(path string, orig, next []byte, cleaned map[string]any, dry bool, parse func([]byte) (map[string]any, error)) (string, error) {
	side := path + sidecarSuffix
	restore := false
	if saved, err := os.ReadFile(side); err == nil {
		if want, err := parse(saved); err == nil && reflect.DeepEqual(want, cleaned) {
			next = saved
			restore = true
		}
	}
	diff := unifiedDiff(path, orig, next)
	if dry {
		return diff, nil
	}
	if restore && len(bytes.TrimSpace(next)) == 0 {
		// The file did not exist (or was empty) before install.
		if err := backup(path, orig); err != nil {
			return "", err
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
	} else if err := writeWithBackup(path, orig, next); err != nil {
		return "", err
	}
	if err := os.Remove(side); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return diff, nil
}

// writeWithBackup copies the current file to path+".nighthawk.bak.<unix ts>"
// and then writes next. A missing file has nothing to back up.
func writeWithBackup(path string, orig, next []byte) error {
	if bytes.Equal(orig, next) {
		return nil
	}
	if err := backup(path, orig); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, next, 0o600)
}

func backup(path string, orig []byte) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	base := path + backupSuffix + strconv.FormatInt(time.Now().Unix(), 10)
	name := base
	// Never overwrite an earlier backup from the same second.
	for i := 1; ; i++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) && i < 100 {
			name = base + "." + strconv.Itoa(i)
			continue
		}
		if err != nil {
			return err
		}
		if _, err := f.Write(orig); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
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
