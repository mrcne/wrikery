package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
)

func natoRows(n int) []checkRow {
	names := []string{"Alpha", "Bravo", "Charlie", "Dashboards", "Deploy", "Echo", "Foxtrot", "Golf", "Hotel", "India", "Juliet", "Kilo", "Lima", "Mike", "November", "Oscar"}
	rows := make([]checkRow, n)
	for i := range rows {
		rows[i] = checkRow{id: fmt.Sprint(i), label: names[i]}
	}
	return rows
}

// The cursor opened on Deploy, one typed letter narrows the list, and the highlighted row must be the first match,
// not whatever now sits at the old position.
func TestChecklistGoesToTheFirstMatchWhenTheQueryChanges(t *testing.T) {
	c, _ := newChecklist(natoRows(9), []string{"4"}, "")
	c.selectID("4")
	c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if r, _ := c.current(); r.label != "Dashboards" || c.cursor != 0 {
		t.Errorf("after typing d the cursor is on %q at %d, want Dashboards at 0", r.label, c.cursor)
	}
	c.update(tea.KeyMsg{Type: tea.KeyDown})
	c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if r, _ := c.current(); r.label != "Deploy" || c.cursor != 0 {
		t.Errorf("after typing de the cursor is on %q at %d, want Deploy at 0", r.label, c.cursor)
	}
}

func TestChecklistWindowFollowsTheCursor(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	c, _ := newChecklist(natoRows(16), nil, "")
	view := c.view(th, 40, 5)
	if !strings.Contains(view, "> [ ] Alpha") || !strings.Contains(view, "[ ] Dashboards") || strings.Contains(view, "Deploy") || !strings.Contains(view, "... 12 below") {
		t.Errorf("at the top four rows and a marker:\n%s", view)
	}
	for range 15 {
		c.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	view = c.view(th, 40, 5)
	if !strings.Contains(view, "> [ ] Oscar") || !strings.Contains(view, "... 12 above") || strings.Contains(view, "below") {
		t.Errorf("at the bottom the last four rows and a marker:\n%s", view)
	}
	for range 8 {
		c.update(tea.KeyMsg{Type: tea.KeyUp})
	}
	view = c.view(th, 40, 5)
	if !strings.Contains(view, "> [ ] Golf") || !strings.Contains(view, "... 5 above, 7 below") {
		t.Errorf("in the middle the cursor sits inside the window:\n%s", view)
	}
	if lines := strings.Count(c.view(th, 40, 5), "\n"); lines != 7 {
		t.Errorf("filter, blank, four rows and a marker are 7 lines, got %d", lines)
	}
}
