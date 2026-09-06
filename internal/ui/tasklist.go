package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/store"
)

type taskRow struct {
	task  store.Task
	state store.OutboxState
}

type taskListModel struct {
	nodeID    string
	crumb     string
	all       []taskRow
	rows      []int
	cursor    int
	offset    int
	height    int // set by the root from the computed layout, View cannot remember it on its own
	showDone  bool
	filtering bool
	filter    textinput.Model
	keys      KeyMap
}

func newTaskList(keys KeyMap) taskListModel {
	in := textinput.New()
	in.Prompt = "/"
	in.CharLimit = 80
	return taskListModel{keys: keys, filter: in}
}

func (l *taskListModel) setRows(nodeID, crumb string, tasks []store.Task, states map[string]store.OutboxState, keepID string) {
	if keepID == "" {
		if cur, ok := l.current(); ok {
			keepID = cur.task.ID
		}
	}
	l.nodeID, l.crumb = nodeID, crumb
	l.all = l.all[:0]
	for _, t := range tasks {
		l.all = append(l.all, taskRow{task: t, state: states[t.ID]})
	}
	l.applyFilter()
	if !l.selectByID(keepID) {
		l.cursor = min(l.cursor, max(0, len(l.rows)-1))
	}
	l.scroll()
}

func (l *taskListModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(l.filter.Value()))
	l.rows = l.rows[:0]
	for i, r := range l.all {
		if !l.showDone && isDone(r.task) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.task.Title), q) {
			continue
		}
		l.rows = append(l.rows, i)
	}
	if l.cursor >= len(l.rows) {
		l.cursor = max(0, len(l.rows)-1)
	}
}

func (l taskListModel) current() (taskRow, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return taskRow{}, false
	}
	return l.all[l.rows[l.cursor]], true
}

func (l *taskListModel) selectByID(id string) bool {
	for i, ri := range l.rows {
		if l.all[ri].task.ID == id {
			l.cursor = i
			return true
		}
	}
	return false
}

func (l taskListModel) title() string {
	name := "Tasks"
	if l.crumb != "" {
		name += ": " + l.crumb
	}
	return fmt.Sprintf("%s (%d)", name, len(l.rows))
}

// scroll clamps offset so the cursor row stays inside the pane, the same pattern as the sidebar.
// View has a value receiver, so it cannot persist the offset it would otherwise compute itself, this is done here instead.
func (l *taskListModel) scroll() {
	h := l.listHeight()
	if h <= 0 {
		return
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
}

// listHeight is the row count left for the list once the filter input takes its own line.
func (l taskListModel) listHeight() int {
	if l.filtering || l.filter.Value() != "" {
		return l.height - 1
	}
	return l.height
}

func (l taskListModel) Update(msg tea.KeyMsg) (taskListModel, tea.Cmd) {
	before, _ := l.current()
	if l.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			l.filter.SetValue("")
			l.filtering = false
			l.filter.Blur()
			l.applyFilter()
		case tea.KeyEnter:
			l.filtering = false
			l.filter.Blur()
		default:
			var cmd tea.Cmd
			l.filter, cmd = l.filter.Update(msg)
			l.applyFilter()
			l.scroll()
			return l.afterMove(before, cmd)
		}
		l.scroll()
		return l.afterMove(before, nil)
	}
	switch {
	case key.Matches(msg, l.keys.Down):
		if l.cursor < len(l.rows)-1 {
			l.cursor++
		}
	case key.Matches(msg, l.keys.Up):
		if l.cursor > 0 {
			l.cursor--
		}
	case key.Matches(msg, l.keys.Top):
		l.cursor = 0
	case key.Matches(msg, l.keys.Bottom):
		l.cursor = max(0, len(l.rows)-1)
	case key.Matches(msg, l.keys.HalfDown):
		l.cursor = min(len(l.rows)-1, l.cursor+10)
	case key.Matches(msg, l.keys.HalfUp):
		l.cursor = max(0, l.cursor-10)
	case key.Matches(msg, l.keys.Filter):
		l.filtering = true
		l.scroll()
		return l, l.filter.Focus()
	case key.Matches(msg, l.keys.ToggleDone):
		l.showDone = !l.showDone
		l.applyFilter()
	case key.Matches(msg, l.keys.Enter), key.Matches(msg, l.keys.Right):
		return l, intent(focusMsg{pane: paneDetail})
	case key.Matches(msg, l.keys.Left):
		return l, intent(focusMsg{pane: paneSidebar})
	}
	l.scroll()
	return l.afterMove(before, nil)
}

// afterMove tells the root when the selected task changed so the detail pane follows the cursor.
func (l taskListModel) afterMove(before taskRow, cmd tea.Cmd) (taskListModel, tea.Cmd) {
	if cur, ok := l.current(); ok && cur.task.ID != before.task.ID {
		return l, tea.Batch(cmd, intent(taskSelectedMsg{id: cur.task.ID}))
	}
	return l, cmd
}

func dueLabel(t store.Task, now time.Time) (string, bool) {
	if isDone(t) {
		return "done", false
	}
	if t.Dates == nil || t.Dates.Due == "" {
		return "--", false
	}
	if len(t.Dates.Due) < 10 {
		return t.Dates.Due, false
	}
	due, err := time.Parse("2006-01-02", t.Dates.Due[:10])
	if err != nil {
		return t.Dates.Due, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := int(due.Sub(today).Hours() / 24)
	switch {
	case days < 0:
		return fmt.Sprintf("%d %s", due.Day(), due.Month().String()[:3]), true
	case days == 0:
		return "today", false
	case days < 7:
		return due.Weekday().String()[:3], false
	}
	return fmt.Sprintf("%d %s", due.Day(), due.Month().String()[:3]), false
}

// initials shows the current user first when they are among the responsibles.
// That way a My tasks row reads as mine rather than a colleague's, whichever contact the API listed first.
func initials(ids []string, contacts map[string]store.Contact, meID string) string {
	if len(ids) == 0 {
		return "--"
	}
	id := ids[0]
	if meID != "" {
		for _, i := range ids {
			if i == meID {
				id = meID
				break
			}
		}
	}
	c := contacts[id]
	s := firstRune(c.FirstName) + firstRune(c.LastName)
	if s == "" {
		s = "??"
	}
	if len(ids) > 1 {
		s += "+"
	}
	return strings.ToUpper(s)
}

// firstRune takes the first character by rune, not by byte, a name may start with a non ASCII letter.
func firstRune(s string) string {
	if s == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(s)
	return string(r)
}

func (l taskListModel) View(th Theme, ref refData, now time.Time, width, height int, focused bool) string {
	if height <= 0 {
		return ""
	}
	listHeight := height
	if l.filtering || l.filter.Value() != "" {
		listHeight--
	}
	// offset lives on the model and is advanced by scroll().
	// This only guards against it landing past the end, for example right after the filter shrinks the row count.
	offset := l.offset
	if last := len(l.rows) - 1; offset > last {
		offset = max(0, last)
	}
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	for row := 0; row < listHeight; row++ {
		vi := offset + row
		if vi >= len(l.rows) {
			if row < listHeight-1 {
				b.WriteByte('\n')
			}
			continue
		}
		r := l.all[l.rows[vi]]
		cs := ref.statuses[r.task.CustomStatusID]
		if cs.Group == "" {
			cs.Group = r.task.Status
		}
		glyph := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group))
		due, overdue := dueLabel(r.task, now)
		dueStyle := muted
		if overdue {
			dueStyle = lipgloss.NewStyle().Foreground(th.Error)
		}
		who := muted.Render(fmt.Sprintf("%-3s", initials(r.task.ResponsibleIDs, ref.contacts, ref.meID)))
		mark := " "
		switch r.state {
		case store.StatePending:
			mark = lipgloss.NewStyle().Foreground(th.Warn).Render(th.Glyphs.Pending)
		case store.StateFailed:
			mark = lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed)
		}
		// glyph(1) space title... space mark(1) space who(3) space due(6), cursor prefix takes 2
		titleWidth := width - 2 - 2 - 2 - 4 - 7
		title := ansi.Truncate(r.task.Title, max(titleWidth, 4), "...")
		title += strings.Repeat(" ", max(0, titleWidth-ansi.StringWidth(title)))
		label := fmt.Sprintf("%s %s %s %s %s", glyph, title, mark, who, dueStyle.Render(fmt.Sprintf("%6s", due)))
		b.WriteString(rowLine(th, label, width, vi == l.cursor, focused))
		if row < listHeight-1 {
			b.WriteByte('\n')
		}
	}
	if listHeight < height {
		b.WriteByte('\n')
		b.WriteString(l.filter.View())
	}
	return b.String()
}
