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

func BenchmarkString(b *testing.B) {
	s := strings.Repeat("hello world ", 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		String(s)
	}
}
