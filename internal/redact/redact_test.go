package redact

import (
	"strings"
	"testing"
)

func TestRedacts(t *testing.T) {
	cases := []string{
		"key AKIAABCDEFGHIJKLMNOP here",
		"token ghp_abcdefghijklmnopqrstuvwxyz0123456789AB",
		"Authorization: Bearer abc.def.ghi",
	}
	for _, c := range cases {
		out := String(c)
		if !strings.Contains(out, Mask) {
			t.Errorf("not redacted: %q -> %q", c, out)
		}
	}
	if String("hello") != "hello" {
		t.Error("plain text changed")
	}
}
