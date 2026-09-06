package ui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// fitHints returns the widest leading run of bindings whose short help text fits available cells.
// Hints shrink from the right: dropping the least important, listed-last binding beats showing nothing.
func fitHints(h help.Model, bindings []key.Binding, available int) string {
	for n := len(bindings); n > 0; n-- {
		hints := h.ShortHelpView(bindings[:n])
		if lipgloss.Width(hints) <= available {
			return hints
		}
	}
	return ""
}

// helpView is the ? overlay: every binding in effect, grouped, drawn from the same key map the hints use.
// title fills the box title, the root passes "wrikery <version>" so the overlay doubles as a version display.
func helpView(th Theme, h help.Model, groups [][]key.Binding, width int, title string) string {
	h.ShowAll = true
	h.Width = width - 6
	h.Styles.FullKey = lipgloss.NewStyle().Foreground(th.Accent)
	h.Styles.FullDesc = lipgloss.NewStyle().Foreground(th.Text)
	body := h.FullHelpView(groups)
	return th.box(title, body+"\n\n"+lipgloss.NewStyle().Foreground(th.Muted).Render("esc or ? to close"), min(width-4, 90), lipgloss.Height(body)+5, true)
}
