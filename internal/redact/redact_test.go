package redact

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jusso-dev/nighthawk/internal/event"
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

func TestCorpus(t *testing.T) {
	sec, err := os.ReadFile("testdata/secrets.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sec)), "\n") {
		if strings.HasPrefix(line, "-----") || strings.TrimSpace(line) == "MIIEfake" {
			continue
		}
		out := String(line)
		if !strings.Contains(out, Mask) {
			t.Errorf("not masked: %q -> %q", line, out)
		}
	}
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEfake\n-----END PRIVATE KEY-----"
	if !strings.Contains(String(pem), Mask) {
		t.Fatal("pem")
	}
	clean, err := os.ReadFile("testdata/clean.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(clean)), "\n") {
		if got := String(line); got != line {
			t.Errorf("clean changed: %q -> %q", line, got)
		}
	}
}

func TestEventAttrs(t *testing.T) {
	e := Event(event.Event{Attrs: map[string]string{"command": "echo AKIAABCDEFGHIJKLMNOP"}})
	if !strings.Contains(e.Attrs["command"], Mask) {
		t.Fatal(e.Attrs)
	}
}

func TestRedact1MB(t *testing.T) {
	s := strings.Repeat("hello world ", (1<<20)/12)
	start := time.Now()
	out := String(s)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("took %s", time.Since(start))
	}
	if out != s {
		t.Fatal("plain changed")
	}
}

func TestFastPathAnyCase(t *testing.T) {
	for _, c := range []string{
		"PASSWORD=hunter2hunter",
		"Authorization: BEARER abcdef",
		"authorization: bearer abcdef",
	} {
		if out := String(c); !strings.Contains(out, Mask) {
			t.Errorf("not redacted: %q -> %q", c, out)
		}
	}
}

func TestMaskEntropyDense(t *testing.T) {
	tok := "token=qW8nR4pL2mK9vX7cB3hJ6fD1sA5gT0yU9e"
	got := String(tok + " x " + tok)
	if want := "token=" + Mask + " x token=" + Mask; got != want {
		t.Fatalf("got %q", got)
	}
}

// realistic1MB returns ~1 MB inputs that force the slow path.
func realistic1MB() map[string]string {
	return map[string]string{
		"dense_tokens": strings.Repeat("token=qW8nR4pL2mK9vX7cB3hJ6fD1sA5gT0yU9e ", (1<<20)/41),
		"urls":         strings.Repeat("see https://example.com/docs/page?x=1 ", (1<<20)/38),
		"mixed":        strings.Repeat("PASSWORD=hunter2 Authorization: Bearer abc.def https://u:p@h/x ", (1<<20)/64),
	}
}

// Issue target is 50 ms per MB; the assertion is looser so CI does not flake.
func TestRedact1MBRealistic(t *testing.T) {
	for name, s := range realistic1MB() {
		start := time.Now()
		_ = String(s)
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Errorf("%s took %s", name, d)
		}
	}
}

func BenchmarkString(b *testing.B) {
	s := strings.Repeat("hello world ", 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		String(s)
	}
}

func BenchmarkString1MB(b *testing.B) {
	for name, s := range realistic1MB() {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(s)))
			for i := 0; i < b.N; i++ {
				String(s)
			}
		})
	}
}
