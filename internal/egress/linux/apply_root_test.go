//go:build linux && linux_root

package linux

import (
	"testing"
)

func TestApplyAndDisable(t *testing.T) {
	if err := Apply(t.Context(), "/"); err != nil {
		t.Fatal(err)
	}
	if err := Disable(t.Context()); err != nil {
		t.Fatal(err)
	}
}
