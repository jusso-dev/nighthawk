package config

import (
	"os"
	"path/filepath"
	"strings"
)

// StateDir is $NIGHTHAWK_STATE_DIR, else $XDG_STATE_HOME/nighthawk,
// else ~/.local/state/nighthawk.
func StateDir() string {
	if d := os.Getenv("NIGHTHAWK_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "nighthawk")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join("/tmp", "nighthawk")
	}
	return filepath.Join(home, ".local", "state", "nighthawk")
}

// ConfigPath is $NIGHTHAWK_CONFIG, else $XDG_CONFIG_HOME/nighthawk/config.yaml,
// else ~/.config/nighthawk/config.yaml.
func ConfigPath() string {
	if p := os.Getenv("NIGHTHAWK_CONFIG"); p != "" {
		return p
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "nighthawk", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join("/tmp", "nighthawk", "config.yaml")
	}
	return filepath.Join(home, ".config", "nighthawk", "config.yaml")
}

// OutsideDir reports whether path sits outside root. Unresolvable paths count as inside.
func OutsideDir(root, path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ModePath is the 0600 mode cache hooks read when the daemon is down.
func ModePath(stateDir string) string { return filepath.Join(stateDir, "mode") }

// SocketPath is the daemon unix socket.
func SocketPath(stateDir string) string { return filepath.Join(stateDir, "nighthawk.sock") }

// DBPath is the SQLite database.
func DBPath(stateDir string) string { return filepath.Join(stateDir, "nighthawk.db") }
