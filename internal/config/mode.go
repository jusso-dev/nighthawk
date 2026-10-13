package config

import (
	"os"
	"strings"

	"github.com/jusso-dev/nighthawk/internal/policy"
)

// ReadMode reads the mode cache. Missing or unknown means attended.
func ReadMode(stateDir string) policy.Mode {
	b, err := os.ReadFile(ModePath(stateDir))
	if err != nil {
		return policy.Attended
	}
	if policy.Mode(strings.TrimSpace(string(b))) == policy.Unattended {
		return policy.Unattended
	}
	return policy.Attended
}

// WriteMode writes the mode cache as 0600.
func WriteMode(stateDir string, m policy.Mode) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(ModePath(stateDir), []byte(string(m)+"\n"), 0o600)
}
