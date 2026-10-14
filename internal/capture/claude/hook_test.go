package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jusso-dev/nighthawk/internal/policy"
)

func TestFixtures(t *testing.T) {
	dir := "testdata"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		evs := ToEvents(in, "p1", "task")
		if len(evs) != 1 {
			t.Fatalf("%s: %+v", e.Name(), evs)
		}
		ev := evs[0]
		switch e.Name() {
		case "bash.json":
			if ev.Kind != "command" || ev.Attrs["command"] != "ls" || !ev.Pre {
				t.Fatal(ev)
			}
		case "write.json":
			if ev.Kind != "file_write" || ev.Attrs["path"] != "a.txt" {
				t.Fatal(ev)
			}
		case "mcp.json":
			if ev.Kind != "mcp_call" || ev.Attrs["mcp_server"] != "fs" || ev.Attrs["mcp_tool"] != "read" {
				t.Fatal(ev)
			}
		case "skill.json":
			if ev.Kind != "skill" || ev.Attrs["name"] != "caveman" {
				t.Fatal(ev)
			}
		case "webfetch.json":
			if ev.Kind != "network" || ev.Attrs["host"] != "example.com" {
				t.Fatal(ev)
			}
		case "skillread.json":
			if ev.Kind != "skill" || ev.Attrs["name"] != "caveman" {
				t.Fatal(ev)
			}
		case "readtraversal.json":
			if ev.Kind != "tool_call" || ev.Attrs["path"] != "/Users/x/.ssh/id_rsa" {
				t.Fatal(ev)
			}
		case "websearch.json":
			if ev.Kind != "network" || ev.Attrs["host"] != "" || ev.Attrs["query"] != "go regexp syntax" {
				t.Fatal(ev)
			}
		case "grep.json":
			if ev.Kind != "tool_call" || ev.Attrs["path"] != "/Users/x/proj/.env" {
				t.Fatal(ev)
			}
		case "glob.json":
			if ev.Kind != "tool_call" || ev.Attrs["path"] != "/Users/x/proj/**/.env" {
				t.Fatal(ev)
			}
		default:
			t.Fatal(e.Name())
		}
	}
}

func TestFixturesAgainstDefaultPolicy(t *testing.T) {
	eng, err := policy.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	eng.Facts.HostAllowed = func(h string) bool { return h == "example.com" }
	cases := []struct {
		file string
		mode policy.Mode
		rule string
	}{
		{"readtraversal.json", policy.Unattended, "nh.secret.read"},
		{"grep.json", policy.Unattended, "nh.secret.read"},
		{"glob.json", policy.Unattended, "nh.secret.read"},
		{"websearch.json", policy.Attended, "nh.allow.network"},
		{"websearch.json", policy.Unattended, "nh.allow.network"},
	}
	for _, c := range cases {
		b, err := os.ReadFile(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		d, err := eng.Evaluate(ToEvents(in, "p1", "task")[0], c.mode)
		if err != nil || d.RuleID != c.rule {
			t.Errorf("%s %s: %+v %v", c.file, c.mode, d, err)
		}
	}
}

func TestFetchHostNeverEmpty(t *testing.T) {
	for raw, want := range map[string]string{"https://example.com/a": "example.com", "example.com/a": "example.com", "": "invalid"} {
		if got := fetchHost(raw); got != want {
			t.Errorf("%q: %q", raw, got)
		}
	}
}
