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

func TestStateDirEnv(t *testing.T) {
	t.Setenv("NIGHTHAWK_STATE_DIR", "/tmp/nh-state")
	if StateDir() != "/tmp/nh-state" {
		t.Fatal(StateDir())
	}
}
