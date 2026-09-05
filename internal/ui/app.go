// Package ui holds the bubbletea application. It reads from the store and
// never talks to the network.
package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	version string
	width   int
	height  int
}

func New(version string) Model {
	return Model{version: version}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	statusStyle = lipgloss.NewStyle().Faint(true)
)

func (m Model) View() string {
	var b strings.Builder
	// TODO: add screens
	b.WriteString(titleStyle.Render("wrikery " + m.version))
	b.WriteString("\n\nnothing here yet...\n\n")
	b.WriteString(statusStyle.Render("q quit"))
	return b.String()
}
