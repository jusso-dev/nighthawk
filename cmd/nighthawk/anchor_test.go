package main

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jusso-dev/nighthawk/internal/audit"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func TestAuditAnchorAndMismatch(t *testing.T) {
	state := t.TempDir()
	t.Setenv("NIGHTHAWK_STATE_DIR", state)
	t.Setenv("NIGHTHAWK_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))

	var out, errOut bytes.Buffer
	if code := run([]string{"audit", "anchor", "--key", filepath.Join(t.TempDir(), "nope")}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "empty audit") && !strings.Contains(errOut.String(), "key") {
		t.Fatalf("empty code %d out %q err %q", code, out.String(), errOut.String())
	}

	st, err := store.Open(config.DBPath(state))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.NewSQLStore(st.DB()).Append(tx, []byte(`{"event_id":"e1","action":"allow","rule_id":"nh.allow.commands"}`)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "anchor.key")
	if err := os.WriteFile(keyPath, priv.Seed(), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(state, "anchor.key")
	if err := os.WriteFile(inside, priv.Seed(), 0o600); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"audit", "anchor", "--key", inside}, nil, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "outside the state dir") {
		t.Fatalf("inside code %d err %q", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"audit", "anchor", "--key", keyPath}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "seq=1") {
		t.Fatalf("anchor code %d out %q err %q", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"audit", "verify", "--anchors"}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "ok") {
		t.Fatalf("verify code %d out %q err %q", code, out.String(), errOut.String())
	}

	st, err = store.Open(config.DBPath(state))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE audit_anchors SET hash = ? WHERE seq = 1`, strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"audit", "verify", "--anchors"}, nil, &out, &errOut); code != 3 || !strings.Contains(errOut.String(), "mismatch") {
		t.Fatalf("tamper code %d err %q", code, errOut.String())
	}
}
