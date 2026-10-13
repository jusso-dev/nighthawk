package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := FileHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 64 {
		t.Fatalf("hash len %d: %s", len(h), h)
	}
	h2, err := FileHash(p)
	if err != nil || h2 != h {
		t.Fatalf("not stable: %s vs %s err=%v", h, h2, err)
	}
}

func TestNameFromPath(t *testing.T) {
	cases := map[string]string{
		"/home/u/.claude/skills/my-skill/SKILL.md": "my-skill",
		"/x/skills/foo.md":                         "foo",
		"/tmp/bar.md":                              "bar",
		"plain":                                    "plain",
	}
	for in, want := range cases {
		if got := NameFromPath(in); got != want {
			t.Errorf("%q => %q want %q", in, got, want)
		}
	}
}
