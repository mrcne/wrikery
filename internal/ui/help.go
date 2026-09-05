package ui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// helpView is the ? overlay: every binding in effect, grouped, drawn from the same key map the hints use.
func helpView(th Theme, h help.Model, groups [][]key.Binding, width int) string {
	h.ShowAll = true
	h.Width = width - 6
	h.Styles.FullKey = lipgloss.NewStyle().Foreground(th.Accent)
	h.Styles.FullDesc = lipgloss.NewStyle().Foreground(th.Text)
	body := h.FullHelpView(groups)
	return th.box("Keys", body+"\n\n"+lipgloss.NewStyle().Foreground(th.Muted).Render("esc or ? to close"), min(width-4, 90), lipgloss.Height(body)+5, true)
}
