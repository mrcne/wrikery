package ui

import (
	"fmt"
	"slices"
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
	nodeID     string
	crumb      string
	all        []taskRow
	rows       []int // positions into all in group order, a task in two groups is here twice
	groupBy    groupKey
	groups     []taskGroup
	groupStart []int // position in rows where each group starts
	columns    []boardColumn
	rowCol     []int // per position in rows, the column of the row's status
	folders    folderIndex
	ref        refData
	cursor     int
	offset     int // a visual line, section lines counted
	height     int // set by the root from the computed layout, View cannot remember it on its own
	showDone   bool
	filtering  bool
	filter     textinput.Model
	keys       KeyMap
}

func newTaskList(keys KeyMap) taskListModel {
	in := textinput.New()
	in.Prompt = "/"
	in.CharLimit = 80
	return taskListModel{keys: keys, filter: in}
}

// setRows replaces the rows and reports whether keepID, or the task selected before, is still among them.
func (l *taskListModel) setRows(nodeID, crumb string, tasks []store.Task, states map[string]store.OutboxState, keepID string) bool {
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
	found := l.selectByID(keepID)
	if !found {
		l.cursor = min(l.cursor, max(0, len(l.rows)-1))
	}
	l.scroll()
	return found
}

// applyFilter is the one place the rows are built: the filter and the done toggle narrow all, the grouping orders what is left.
// The columns are computed here too, the board and the by status sections share them.
func (l *taskListModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(l.filter.Value()))
	var kept []int
	for i, r := range l.all {
		if !l.showDone && isDone(r.task) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.task.Title), q) {
			continue
		}
		kept = append(kept, i)
	}
	var colOf map[int]int
	l.columns, colOf = boardColumns(l.all, kept, l.ref, l.showDone)
	switch l.groupBy {
	case groupFolder:
		l.groups = groupByFolder(l.all, kept, l.nodeID, l.folders)
	case groupAssignee:
		l.groups = groupByAssignee(l.all, kept, l.ref)
	case groupStatus:
		l.groups = groupByStatus(l.columns)
	default:
		l.groups = []taskGroup{{rows: kept}}
	}
	l.rows, l.groupStart, l.rowCol = l.rows[:0], l.groupStart[:0], l.rowCol[:0]
	for _, g := range l.groups {
		l.groupStart = append(l.groupStart, len(l.rows))
		for _, ri := range g.rows {
			l.rows = append(l.rows, ri)
			l.rowCol = append(l.rowCol, colOf[ri])
		}
	}
	if l.cursor >= len(l.rows) {
		l.cursor = max(0, len(l.rows)-1)
	}
}

func (l taskListModel) sectioned() bool { return l.groupBy != groupNone }

// visual is the line of row position p once the section lines above it are counted.
func (l taskListModel) visual(p int) int {
	if !l.sectioned() {
		return p
	}
	n := 0
	for _, s := range l.groupStart {
		if s <= p {
			n++
		}
	}
	return p + n
}

// groupOf is the group holding row position p, -1 with no rows.
func (l taskListModel) groupOf(p int) int {
	g := -1
	for i, s := range l.groupStart {
		if s <= p && i < len(l.groups) && len(l.groups[i].rows) > 0 {
			g = i
		}
	}
	return g
}

// setGroup regroups and keeps the selection by id, since the rows come back in another order.
func (l *taskListModel) setGroup(g groupKey) {
	id := ""
	if cur, ok := l.current(); ok {
		id = cur.task.ID
	}
	l.groupBy = g
	l.applyFilter()
	if !l.selectByID(id) {
		l.cursor = min(l.cursor, max(0, len(l.rows)-1))
	}
	l.scroll()
}

// cycleGroup moves to the next grouping. The board skips status, its columns already are the status.
func (l *taskListModel) cycleGroup(board bool) {
	next := (l.groupBy + 1) % 4
	if board && next == groupStatus {
		next = groupNone
	}
	l.setGroup(next)
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

func (l taskListModel) title() string { return l.titled("Tasks") }

// titled builds the pane title for the list or the board, the grouping named so the state is visible.
func (l taskListModel) titled(prefix string) string {
	name := prefix
	if l.crumb != "" {
		name += ": " + l.crumb
	}
	if l.sectioned() {
		name += ", by " + l.groupBy.String()
	}
	return fmt.Sprintf("%s (%d)", name, len(l.rows))
}

// scroll clamps offset so the cursor row stays inside the pane, the same pattern as the sidebar.
// View has a value receiver, so it cannot persist the offset it would otherwise compute itself, this is done here instead.
// Offsets are visual lines, so a section line counts, and the one above the first row of a group is kept in view with that row.
func (l *taskListModel) scroll() {
	h := l.listHeight()
	if h <= 0 {
		return
	}
	v := l.visual(l.cursor)
	if v < l.offset {
		l.offset = v
	}
	if l.sectioned() && slices.Contains(l.groupStart, l.cursor) && v-1 < l.offset {
		l.offset = max(0, v-1)
	}
	if v >= l.offset+h {
		l.offset = v - h + 1
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
	case key.Matches(msg, l.keys.PrevGroup):
		g := l.groupOf(l.cursor)
		if g >= 0 && l.cursor > l.groupStart[g] {
			l.cursor = l.groupStart[g]
		} else if g > 0 {
			l.cursor = l.groupStart[g-1]
		}
	case key.Matches(msg, l.keys.NextGroup):
		if g := l.groupOf(l.cursor); g >= 0 && g+1 < len(l.groupStart) {
			l.cursor = l.groupStart[g+1]
		}
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

// A name may start with a non ASCII letter.
func firstRune(s string) string {
	if s == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(s)
	return string(r)
}

// listLine is one drawn line: a section line of group when row is -1, else the row at that position.
type listLine struct{ group, row int }

func (l taskListModel) lines() []listLine {
	var out []listLine
	if !l.sectioned() {
		for p := range l.rows {
			out = append(out, listLine{row: p})
		}
		return out
	}
	for g, grp := range l.groups {
		out = append(out, listLine{group: g, row: -1})
		for i := range grp.rows {
			out = append(out, listLine{group: g, row: l.groupStart[g] + i})
		}
	}
	return out
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
	lines := l.lines()
	offset := l.offset
	if last := len(lines) - 1; offset > last {
		offset = max(0, last)
	}
	// In the by assignee view the section says who, so the initials give way to the status name, which a standup wants told apart.
	whoWidth := 3
	if l.groupBy == groupAssignee {
		whoWidth = 12
	}
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	for row := 0; row < listHeight; row++ {
		vi := offset + row
		if vi >= len(lines) {
			if row < listHeight-1 {
				b.WriteByte('\n')
			}
			continue
		}
		ln := lines[vi]
		if ln.row < 0 {
			g := l.groups[ln.group]
			b.WriteString(divider(th, fmt.Sprintf("%s (%d)", g.title, len(g.rows)), width))
			if row < listHeight-1 {
				b.WriteByte('\n')
			}
			continue
		}
		r := l.all[l.rows[ln.row]]
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
		who := initials(r.task.ResponsibleIDs, ref.contacts, ref.meID)
		if l.groupBy == groupAssignee {
			who = cs.Name
			if who == "" {
				who = r.task.Status
			}
			who = ansi.Truncate(who, whoWidth, "")
		}
		whoCell := muted.Render(fmt.Sprintf("%-*s", whoWidth, who))
		mark := " "
		switch r.state {
		case store.StatePending:
			mark = lipgloss.NewStyle().Foreground(th.Warn).Render(th.Glyphs.Pending)
		case store.StateFailed:
			mark = lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed)
		}
		// glyph(1) space title... space mark(1) space who(whoWidth) space due(6), cursor prefix takes 2
		titleWidth := width - 2 - 2 - 2 - (whoWidth + 1) - 7
		title := ansi.Truncate(r.task.Title, max(titleWidth, 4), "...")
		title += strings.Repeat(" ", max(0, titleWidth-ansi.StringWidth(title)))
		label := fmt.Sprintf("%s %s %s %s %s", glyph, title, mark, whoCell, dueStyle.Render(fmt.Sprintf("%6s", due)))
		b.WriteString(rowLine(th, label, width, ln.row == l.cursor, focused))
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
