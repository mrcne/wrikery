package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/store"
)

func weekOf(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(wd - 1))
}

type tsRow struct {
	taskID, title string
	parentID      string // first folder of the task, where enter on the row lands, "" for a task outside the cache
	cells         [7][]store.Timelog
}

type timesheetModel struct {
	weekStart  time.Time
	windowFrom time.Time // first day the sync engine keeps entries for, zero until the first pull
	rows       []tsRow
	states     map[string]store.OutboxState
	cursorRow  int // len(rows) is the empty row for adding on a new task
	cursorDay  int
	offset     int    // first task row drawn, scrolls the rows to keep cursorRow in the visible window
	height     int    // set by the root from the computed layout, View cannot remember it on its own
	focusTask  string // task picked for a new entry, the next reload puts the cursor on its row
	keys       KeyMap
	loaded     bool
}

type weekLoadedMsg struct {
	weekStart  time.Time
	windowFrom time.Time
	logs       []store.Timelog
	titles     map[string]string
	parents    map[string]string
	states     map[string]store.OutboxState
}
type loadWeekMsg struct{ start time.Time }
type newEntryMsg struct{ taskID, date string }
type editEntryMsg struct{ log store.Timelog }
type deleteEntryMsg struct{ log store.Timelog }
type pickEntryMsg struct {
	logs      []store.Timelog
	forDelete bool
}

func (t *timesheetModel) set(msg weekLoadedMsg) {
	prevTask := ""
	if t.cursorRow < len(t.rows) {
		prevTask = t.rows[t.cursorRow].taskID
	}
	t.weekStart, t.windowFrom, t.states, t.loaded = msg.weekStart, msg.windowFrom, msg.states, true
	byTask := map[string]*tsRow{}
	for _, l := range msg.logs {
		day, err := time.Parse("2006-01-02", l.TrackedDate[:min(10, len(l.TrackedDate))])
		if err != nil {
			continue
		}
		idx := int(day.Sub(msg.weekStart).Hours() / 24)
		if idx < 0 || idx > 6 {
			continue
		}
		row, ok := byTask[l.TaskID]
		if !ok {
			title := msg.titles[l.TaskID]
			if title == "" {
				title = "(task " + l.TaskID + ")"
			}
			row = &tsRow{taskID: l.TaskID, title: title, parentID: msg.parents[l.TaskID]}
			byTask[l.TaskID] = row
		}
		row.cells[idx] = append(row.cells[idx], l)
	}
	t.rows = t.rows[:0]
	for _, row := range byTask {
		t.rows = append(t.rows, *row)
	}
	// By title rather than by first entry, so a task keeps its place from week to week
	// and a task picked for a new entry does not land mid list.
	slices.SortFunc(t.rows, func(a, b tsRow) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.title), strings.ToLower(b.title)), strings.Compare(a.taskID, b.taskID))
	})
	// Start on the first task row.
	// With no rows the only row is the "+ new task" one at index 0.
	t.cursorRow = 0
	for i, r := range t.rows {
		if r.taskID == prevTask {
			t.cursorRow = i
		}
	}
	if t.focusTask != "" {
		if i := slices.IndexFunc(t.rows, func(r tsRow) bool { return r.taskID == t.focusTask }); i >= 0 {
			t.cursorRow = i
		}
		t.focusTask = ""
	}
	t.scroll()
}

// unsynced reports a week before the pulled window:
// empty because nothing was fetched for it, not because nothing was logged.
// With no window known yet (before the first pull) nothing is marked, the status bar already says a sync is running.
func (t timesheetModel) unsynced() bool {
	return !t.windowFrom.IsZero() && t.weekStart.Before(t.windowFrom)
}

// visibleRows is how many task rows fit in height.
// Only the task rows scroll: the header, the "+ new task" row, the blank line and the totals line stay pinned,
// so the window holds height minus those four fixed lines, and one more for the not synced line when it shows.
func (t timesheetModel) visibleRows(height int) int {
	fixed := 4
	if t.unsynced() {
		fixed++
	}
	return max(1, height-fixed)
}

// scroll clamps offset so cursorRow stays inside the visible window, the same pattern as the sync issues list.
// The pinned "+ new task" row (cursorRow == len(rows)) needs no row of its own brought into view.
func (t *timesheetModel) scroll() {
	if t.cursorRow >= len(t.rows) {
		return
	}
	visible := t.visibleRows(t.height)
	if t.cursorRow < t.offset {
		t.offset = t.cursorRow
	}
	if t.cursorRow >= t.offset+visible {
		t.offset = t.cursorRow - visible + 1
	}
}

func (t timesheetModel) title() string {
	end := t.weekStart.AddDate(0, 0, 6)
	title := fmt.Sprintf("Timesheet: %d %s - %d %s %d", t.weekStart.Day(), t.weekStart.Month().String()[:3], end.Day(), end.Month().String()[:3], end.Year())
	if t.unsynced() {
		title += " (not synced)"
	}
	return title
}

func (t timesheetModel) cellDate() string {
	return t.weekStart.AddDate(0, 0, t.cursorDay).Format("2006-01-02")
}

func (t timesheetModel) cell() []store.Timelog {
	if t.cursorRow >= len(t.rows) {
		return nil
	}
	return t.rows[t.cursorRow].cells[t.cursorDay]
}

// titleFor looks up the title of the row holding taskID, by id rather than by cursor position,
// since a weekLoadedMsg can move the cursor or drop rows between an intent being sent and handled.
// It returns "" when the row is gone.
func (t timesheetModel) titleFor(taskID string) string {
	for _, r := range t.rows {
		if r.taskID == taskID {
			return r.title
		}
	}
	return ""
}

func (t timesheetModel) Update(msg tea.KeyMsg) (timesheetModel, tea.Cmd) {
	if !t.loaded {
		// Nothing to move a cursor over yet, and weekStart is still the zero time,
		// so ] or [ here would jump to the week of year 1 instead of doing nothing.
		return t, nil
	}
	switch {
	case key.Matches(msg, t.keys.DayRight):
		if t.cursorDay < 6 {
			t.cursorDay++
		}
	case key.Matches(msg, t.keys.DayLeft):
		if t.cursorDay > 0 {
			t.cursorDay--
		}
	case key.Matches(msg, t.keys.Down):
		if t.cursorRow < len(t.rows) {
			t.cursorRow++
			t.scroll()
		}
	case key.Matches(msg, t.keys.Up):
		if t.cursorRow > 0 {
			t.cursorRow--
			t.scroll()
		}
	case key.Matches(msg, t.keys.WeekNext):
		return t, intent(loadWeekMsg{start: t.weekStart.AddDate(0, 0, 7)})
	case key.Matches(msg, t.keys.WeekPrev):
		return t, intent(loadWeekMsg{start: t.weekStart.AddDate(0, 0, -7)})
	case key.Matches(msg, t.keys.ThisWeek):
		return t, intent(loadWeekMsg{})
	case key.Matches(msg, t.keys.Add):
		taskID := ""
		if t.cursorRow < len(t.rows) {
			taskID = t.rows[t.cursorRow].taskID
		}
		return t, intent(newEntryMsg{taskID: taskID, date: t.cellDate()})
	case key.Matches(msg, t.keys.Enter):
		if r := t.cursorRow; r < len(t.rows) {
			return t, intent(openTaskMsg{id: t.rows[r].taskID, parentID: t.rows[r].parentID})
		}
	case key.Matches(msg, t.keys.Edit):
		switch logs := t.cell(); len(logs) {
		case 0:
			return t, nil
		case 1:
			return t, intent(editEntryMsg{log: logs[0]})
		default:
			return t, intent(pickEntryMsg{logs: logs})
		}
	case key.Matches(msg, t.keys.Delete):
		switch logs := t.cell(); len(logs) {
		case 0:
			return t, nil
		case 1:
			return t, intent(deleteEntryMsg{log: logs[0]})
		default:
			return t, intent(pickEntryMsg{logs: logs, forDelete: true})
		}
	}
	return t, nil
}

func (t timesheetModel) View(th Theme, width, height int) string {
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	bold := lipgloss.NewStyle().Bold(true)
	const cellW = 7
	// Two columns in front of the titles hold the cursor mark, the same one the list and the sidebar draw.
	titleW := width - 8*cellW - 4
	if titleW < 10 {
		titleW = 10
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", titleW+4))
	for i := 0; i < 7; i++ {
		d := t.weekStart.AddDate(0, 0, i)
		label := fmt.Sprintf("%s %d", d.Weekday().String()[:3], d.Day())
		if i == t.cursorDay {
			b.WriteString(bold.Render(fmt.Sprintf("%*s", cellW, label)))
		} else {
			b.WriteString(muted.Render(fmt.Sprintf("%*s", cellW, label)))
		}
	}
	b.WriteString(muted.Render(fmt.Sprintf("%*s", cellW, "Total")) + "\n")
	if t.unsynced() {
		b.WriteString(muted.Render(ansi.Truncate("not synced, only this week and the eight before it are kept", width, "...")) + "\n")
	}

	// height is the source of truth for how many task rows fit, dayTotals still sums every row, visible or not.
	visible := t.visibleRows(height)
	var dayTotals [7]float64
	for ri, r := range t.rows {
		rowTotal := 0.0
		title := ansi.Truncate(displayTitle(r.title, th.HidePrefixes), titleW, "...")
		pad := strings.Repeat(" ", titleW+2-ansi.StringWidth(title))
		line := "  " + title + pad
		if ri == t.cursorRow {
			// The mark and the accent tie the highlighted cell to its task, a wide grid leaves too much space between them.
			line = th.Glyphs.Cursor + " " + lipgloss.NewStyle().Foreground(th.Accent).Render(title) + pad
		}
		for di, logs := range r.cells {
			sum, pending, locked := 0.0, false, false
			for _, l := range logs {
				sum += l.Hours
				pending = pending || strings.HasPrefix(l.ID, store.LocalIDPrefix) || t.states[l.ID] != ""
				locked = locked || timelogLocked(l)
			}
			dayTotals[di] += sum
			rowTotal += sum
			cell := "-"
			if len(logs) > 0 {
				cell = fmt.Sprintf("%.1f", sum)
				if pending {
					cell = th.Glyphs.Pending + cell
				}
			}
			text := fmt.Sprintf("%*s", cellW, cell)
			style := lipgloss.NewStyle()
			if locked {
				style = style.Foreground(th.Muted)
			}
			if ri == t.cursorRow && di == t.cursorDay {
				style = style.Background(th.Accent).Foreground(th.Text)
			}
			line += style.Render(text)
		}
		line += muted.Render(fmt.Sprintf("%*.1f", cellW, rowTotal))
		if ri >= t.offset && ri < t.offset+visible {
			b.WriteString(line + "\n")
		}
	}
	// The empty row: n here logs time on a task picked through search.
	const newTaskLabel = "+ new task"
	addLabel := "  " + muted.Render(newTaskLabel)
	if t.cursorRow == len(t.rows) {
		addLabel = th.Glyphs.Cursor + " " + lipgloss.NewStyle().Foreground(th.Accent).Render(newTaskLabel)
	}
	b.WriteString(addLabel + strings.Repeat(" ", titleW+2-ansi.StringWidth(newTaskLabel)))
	for di := 0; di < 7; di++ {
		text := fmt.Sprintf("%*s", cellW, "")
		if t.cursorRow == len(t.rows) && di == t.cursorDay {
			text = lipgloss.NewStyle().Background(th.Accent).Render(text)
		}
		b.WriteString(text)
	}
	b.WriteString("\n\n")
	total := 0.0
	line := "  " + bold.Render(fmt.Sprintf("%-*s", titleW+2, "Total"))
	for _, v := range dayTotals {
		total += v
		line += fmt.Sprintf("%*.1f", cellW, v)
	}
	line += bold.Render(fmt.Sprintf("%*.1f", cellW, total))
	b.WriteString(line)
	return b.String()
}
