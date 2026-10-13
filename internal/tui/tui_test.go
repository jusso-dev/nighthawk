package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestViewTabsAndDaemonDown(t *testing.T) {
	var m tea.Model = NewModel("")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated
	view := m.View()
	for _, name := range tabs {
		if !strings.Contains(view, name) {
			t.Fatalf("missing tab %q in %q", name, view)
		}
	}
	if !strings.Contains(view, "daemon down") {
		t.Fatalf("%q", view)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
	_ = m2
}
