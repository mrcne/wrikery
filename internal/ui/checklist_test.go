package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

func TestChecklistSingleDrawsNoBoxes(t *testing.T) {
	c, _ := newChecklist([]checkRow{{id: "A", label: "Alpha"}, {id: "B", label: "  Beta"}}, nil, "")
	c.single = true
	view := c.view(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 40, 5)
	if strings.Contains(view, "[ ]") || !strings.Contains(view, "Beta") {
		t.Errorf("single pick view = %q, want the rows without boxes", view)
	}
}

func TestChecklistKeepsOneLineForALongLabel(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	c, _ := newChecklist([]checkRow{{id: "1", label: strings.Repeat("Long label ", 8)}}, nil, "")
	out := c.view(th, 20, 5)
	// The filter line, a blank line and one row, each closed by a newline.
	if got := strings.Count(out, "\n"); got != 3 {
		t.Errorf("view has %d newlines, want 3:\n%s", got, out)
	}
}

// A typed emoji usually carries its variation selector, which the drawn label no longer has, so the query is stripped the same way.
func TestChecklistMatchesATypedEmojiWithItsSelector(t *testing.T) {
	c, _ := newChecklist([]checkRow{{id: "1", label: "\u26a0\ufe0f Risks"}, {id: "2", label: "Plain"}}, nil, "")
	c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\u26a0\ufe0f")})
	if len(c.visible) != 1 || c.rows[c.visible[0]].id != "1" {
		t.Errorf("the typed sign should match its row, visible %v", c.visible)
	}
}

func TestChecklistDrawsASectionHeaderWhereItChanges(t *testing.T) {
	rows := []checkRow{{id: "1", label: "High", section: "Importance"}, {id: "2", label: "Low", section: "Importance"}, {id: "3", label: "Ada", section: "People"}}
	c, _ := newChecklist(rows, nil, "")
	lines := strings.Split(ansi.Strip(c.view(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 30, 10)), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	// The filter line and a blank line come first, then the rows under their headers.
	want := []string{"Importance", "> [ ] High", "  [ ] Low", "People", "  [ ] Ada"}
	if got := lines[2 : len(lines)-1]; !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
	c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if view := ansi.Strip(c.view(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 30, 10)); strings.Contains(view, "Importance") || !strings.Contains(view, "People") {
		t.Errorf("a narrowed list keeps only the headers of the rows it shows:\n%s", view)
	}
}
