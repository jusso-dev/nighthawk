package integrity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCheckModify(t *testing.T) {
	state := t.TempDir()
	data := t.TempDir()
	p := filepath.Join(data, "watched.txt")
	if err := os.WriteFile(p, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(state, []string{p}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(state, "integrity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	changed, err := Check(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("unexpected change: %v", changed)
	}
	if err := os.WriteFile(p, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err = Check(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != p {
		t.Fatalf("changed=%v", changed)
	}
}
