package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestHookBadInputFailsClosedUnattended(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NIGHTHAWK_STATE_DIR", dir)
	big := `{"x":"` + strings.Repeat("a", 1<<20) + `"}`
	for _, mode := range []policy.Mode{policy.Unattended, policy.Attended} {
		if err := config.WriteMode(dir, mode); err != nil {
			t.Fatal(err)
		}
		for _, in := range []string{"not json", big} {
			var out, errOut bytes.Buffer
			code := run([]string{"hook", "claude"}, strings.NewReader(in), &out, &errOut)
			denied := strings.Contains(out.String(), `"permissionDecision":"deny"`)
			if mode == policy.Unattended && (code != 0 || !denied) {
				t.Fatalf("unattended: code %d out %q", code, out.String())
			}
			if mode == policy.Attended && (code != 1 || denied) {
				t.Fatalf("attended: code %d out %q", code, out.String())
			}
		}
	}
}
