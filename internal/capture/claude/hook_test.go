package claude

import (
	"os"
	"path/filepath"
	"testing"
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
		default:
			t.Fatal(e.Name())
		}
	}
}
