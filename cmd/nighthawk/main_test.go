package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, bytes.NewReader(nil), &out, &errOut); code != 0 || out.Len() == 0 {
		t.Fatalf("code %d out %q", code, out.String())
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
