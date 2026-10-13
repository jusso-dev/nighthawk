package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, bytes.NewReader(nil), &out, &errOut); code != 0 || out.Len() == 0 {
		t.Fatalf("code %d out %q", code, out.String())
	}
}

func TestModeSetRejectsTypoOffline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NIGHTHAWK_STATE_DIR", dir)
	var out, errOut bytes.Buffer
	if code := run([]string{"mode", "set", "unatended"}, nil, &out, &errOut); code == 0 {
		t.Fatalf("typo accepted: out %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "mode")); !os.IsNotExist(err) {
		t.Fatalf("mode file written: %v", err)
	}
}

func TestConfigCheckRejectsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("NIGHTHAWK_CONFIG", p)
	if err := os.WriteFile(p, []byte("mode_default: unatended\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"config", "check"}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "mode_default") {
		t.Fatalf("code %d err %q", code, errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"daemon"}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "mode_default") {
		t.Fatalf("daemon code %d err %q", code, errOut.String())
	}
}

func TestManifestDeclaresPlugin(t *testing.T) {
	b, err := os.ReadFile("../../herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id = "nighthawk"`, `min_herdr_version = "0.9.3"`, "[[startup]]", "[[events]]"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("manifest missing %s", want)
		}
	}
}
