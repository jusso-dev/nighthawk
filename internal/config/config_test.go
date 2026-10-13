package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingFileIsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModeDefault != "attended" || cfg.DecisionTimeout.D() != 2*time.Second {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Egress.Port != 8765 || len(cfg.Egress.Allowlist) == 0 {
		t.Fatalf("egress defaults: %+v", cfg.Egress)
	}
}

func TestUnknownKeyLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	body := "mode_default: attended\nnot_a_field: 1\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "line") && !strings.Contains(err.Error(), "2") {
		t.Fatalf("error should name the line: %v", err)
	}
}

func TestPrintRedacts(t *testing.T) {
	t.Setenv("NH_TEST_HOOK", "https://hooks.slack.example/services/T00/B00/secretvalue")
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	body := "alerts:\n  slack_webhook: env:NH_TEST_HOOK\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Alerts.SlackWebhook == "" || strings.HasPrefix(cfg.Alerts.SlackWebhook, "env:") {
		t.Fatalf("env not resolved: %q", cfg.Alerts.SlackWebhook)
	}
	b, err := cfg.PrintYAML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secretvalue") || strings.Contains(string(b), "hooks.slack") {
		t.Fatalf("secret leaked: %s", b)
	}
	if !strings.Contains(string(b), "[REDACTED]") {
		t.Fatalf("not redacted: %s", b)
	}
}

func TestLoadValidates(t *testing.T) {
	for _, body := range []string{
		"mode_default: unatended\n",
		"decision_timeout: -5s\n",
		"redact:\n  extra: ['(unclosed']\n",
		"alerts:\n  slack_webhook: env:NH_TEST_SURELY_UNSET\n",
		"egress:\n  port: 70000\n",
	} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("mode_default: unattended\nredact:\n  extra: ['corp-[0-9]+']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
}

func TestReadModeDefault(t *testing.T) {
	state := t.TempDir()
	p := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("NIGHTHAWK_CONFIG", p)
	if ReadMode(state) != "attended" {
		t.Fatal("no config should mean attended")
	}
	if err := os.WriteFile(p, []byte("mode_default: unattended\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ReadMode(state) != "unattended" {
		t.Fatal("missing mode file must use mode_default")
	}
	if err := os.WriteFile(ModePath(state), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ReadMode(state) != "unattended" {
		t.Fatal("invalid mode file must use mode_default")
	}
	if err := WriteMode(state, "attended"); err != nil || ReadMode(state) != "attended" {
		t.Fatal("explicit mode file wins", err)
	}
	if err := WriteMode(state, "unatended"); err == nil {
		t.Fatal("WriteMode accepted a typo")
	}
}

func TestStateDirEnv(t *testing.T) {
	t.Setenv("NIGHTHAWK_STATE_DIR", "/tmp/nh-state")
	if StateDir() != "/tmp/nh-state" {
		t.Fatal(StateDir())
	}
}
