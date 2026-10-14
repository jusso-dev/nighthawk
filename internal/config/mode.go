package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/jusso-dev/nighthawk/internal/policy"
)

// ReadMode reads the mode cache. Missing or unknown means mode_default from
// the config file, or attended when the config cannot be loaded.
func ReadMode(stateDir string) policy.Mode {
	def := policy.Attended
	if cfg, err := Load(ConfigPath()); err == nil {
		def = policy.Mode(cfg.ModeDefault)
	}
	return ReadModeOr(stateDir, def)
}

// ReadModeOr reads the mode cache. Missing or unknown means def.
func ReadModeOr(stateDir string, def policy.Mode) policy.Mode {
	if def != policy.Unattended {
		def = policy.Attended
	}
	b, err := os.ReadFile(ModePath(stateDir))
	if err != nil {
		return def
	}
	switch m := policy.Mode(strings.TrimSpace(string(b))); m {
	case policy.Attended, policy.Unattended:
		return m
	}
	return def
}

// WriteMode writes the mode cache as 0600. Only attended and unattended are valid.
func WriteMode(stateDir string, m policy.Mode) error {
	if m != policy.Attended && m != policy.Unattended {
		return fmt.Errorf("mode %q is not attended or unattended", m)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(ModePath(stateDir), []byte(string(m)+"\n"), 0o600)
}
