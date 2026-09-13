package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type checkRow struct{ id, label string }

// checklist is the filterable list of rows with a checkbox that the assignee and the folders boxes share.
// The cursor is a position in visible and goes back to the top whenever the query changes,
// so after typing the highlighted row is the first match and never the row that slid under the old position.
// That matters in the folders box, where enter writes for the highlighted row.
type checklist struct {
	rows     []checkRow
	visible  []int
	checked  map[string]bool
	original map[string]bool
	cursor   int
	query    string
	filter   textinput.Model
}

func newChecklist(rows []checkRow, checkedIDs []string, placeholder string) (checklist, tea.Cmd) {
	c := checklist{rows: rows, checked: map[string]bool{}, original: map[string]bool{}}
	for _, id := range checkedIDs {
		c.checked[id], c.original[id] = true, true
	}
	c.filter = textinput.New()
	c.filter.Prompt = "> "
	c.filter.Placeholder = placeholder
	c.applyFilter()
	return c, c.filter.Focus()
}

func (c *checklist) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(c.filter.Value()))
	if q != c.query {
		c.query, c.cursor = q, 0
	}
	c.visible = c.visible[:0]
	for i, r := range c.rows {
		if q == "" || wordPrefix(strings.ToLower(r.label), q) {
			c.visible = append(c.visible, i)
		}
	}
	if c.cursor >= len(c.visible) {
		c.cursor = max(0, len(c.visible)-1)
	}
}

// wordPrefix matches the query against the start of the label or of any word in it, so "sys" finds "Design system" and "now" finds "Ada Nowak".
func wordPrefix(label, q string) bool {
	label = strings.TrimSpace(label)
	if strings.HasPrefix(label, q) {
		return true
	}
	for _, w := range strings.Fields(label) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}

func (c checklist) current() (checkRow, bool) {
	if c.cursor < 0 || c.cursor >= len(c.visible) {
		return checkRow{}, false
	}
	return c.rows[c.visible[c.cursor]], true
}

func (c *checklist) selectID(id string) {
	for i, ri := range c.visible {
		if c.rows[ri].id == id {
			c.cursor = i
			return
		}
	}
}

func (c *checklist) toggle() {
	if r, ok := c.current(); ok {
		c.checked[r.id] = !c.checked[r.id]
	}
}

func (c checklist) checkedCount() int {
	n := 0
	for _, on := range c.checked {
		if on {
			n++
		}
	}
	return n
}

// diff is the change since the box opened, sorted so messages compare in tests.
func (c checklist) diff() (add, remove []string) {
	for id := range c.checked {
		if c.checked[id] && !c.original[id] {
			add = append(add, id)
		}
	}
	for id := range c.original {
		if !c.checked[id] {
			remove = append(remove, id)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	return add, remove
}

// update moves the cursor or feeds the filter. Enter and space are left to the box, they mean different things in each.
func (c *checklist) update(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyDown, tea.KeyCtrlN:
		if c.cursor < len(c.visible)-1 {
			c.cursor++
		}
		return nil
	case tea.KeyUp, tea.KeyCtrlP:
		if c.cursor > 0 {
			c.cursor--
		}
		return nil
	}
	var cmd tea.Cmd
	c.filter, cmd = c.filter.Update(msg)
	c.applyFilter()
	return cmd
}

// view draws the filter, a blank line and at most rows lines of the list.
// A list longer than that shows a window kept around the cursor and one marker line counting what sits outside it.
func (c checklist) view(th Theme, width, rows int) string {
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	b.WriteString(c.filter.View() + "\n\n")
	window := len(c.visible)
	marker := window > rows
	if marker {
		window = max(rows-1, 1)
	}
	first := min(max(c.cursor-window/2, 0), len(c.visible)-window)
	for i := first; i < first+window; i++ {
		r := c.rows[c.visible[i]]
		mark := "[ ]"
		if c.checked[r.id] {
			mark = "[x]"
		}
		indent, title := splitIndent(r.label)
		b.WriteString(rowLine(th, indent+mark+" "+title, width, i == c.cursor, true) + "\n")
	}
	if marker {
		above, below := first, len(c.visible)-first-window
		var parts []string
		if above > 0 {
			parts = append(parts, fmt.Sprintf("%d above", above))
		}
		if below > 0 {
			parts = append(parts, fmt.Sprintf("%d below", below))
		}
		b.WriteString(muted.Render("... "+strings.Join(parts, ", ")+", type to narrow down") + "\n")
	}
	return b.String()
}

// splitIndent keeps a label's leading spaces in front of the checkbox, which is how the folders box draws its tree.
func splitIndent(label string) (indent, rest string) {
	rest = strings.TrimLeft(label, " ")
	return label[:len(label)-len(rest)], rest
}
