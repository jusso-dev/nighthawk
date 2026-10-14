// Package tui is the bubbletea operator dashboard.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var tabs = []string{"Agents", "Spend", "MCP", "Network", "Blocks", "Approvals", "Audit"}

type model struct {
	socket string
	width  int
	height int
	tab    int
	quit   bool
}

// NewModel returns the TUI model. It does not open the socket.
func NewModel(socket string) tea.Model {
	return model{socket: socket, width: 80, height: 24}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "tab", "right", "l":
			m.tab = (m.tab + 1) % len(tabs)
		case "left", "h":
			m.tab = (m.tab - 1 + len(tabs)) % len(tabs)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.quit {
		return ""
	}
	tabStyle := lipgloss.NewStyle().Padding(0, 1)
	active := tabStyle.Bold(true).Underline(true)
	var parts []string
	for i, name := range tabs {
		if i == m.tab {
			parts = append(parts, active.Render(name))
		} else {
			parts = append(parts, tabStyle.Render(name))
		}
	}
	header := strings.Join(parts, " ")
	body := fmt.Sprintf("%s view", tabs[m.tab])
	if m.socket == "" {
		body = "daemon down"
	}
	return header + "\n\n" + body + "\n\nq quit"
}

// Run starts the TUI against socket. Empty socket shows daemon down.
func Run(ctx context.Context, socket string) error {
	p := tea.NewProgram(NewModel(socket), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
