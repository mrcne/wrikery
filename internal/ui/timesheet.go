package ui

import (
	"fmt"
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
	cells         [7][]store.Timelog
}

type timesheetModel struct {
	weekStart time.Time
	rows      []tsRow
	states    map[string]store.OutboxState
	cursorRow int // len(rows) is the empty row for adding on a new task
	cursorDay int
	keys      KeyMap
	loaded    bool
}

type weekLoadedMsg struct {
	weekStart time.Time
	logs      []store.Timelog
	titles    map[string]string
	states    map[string]store.OutboxState
}
type loadWeekMsg struct{ start time.Time }
type newEntryMsg struct{ taskID, date string }
type editEntryMsg struct{ log store.Timelog }
type pickEntryMsg struct {
	logs      []store.Timelog
	forDelete bool
}

func (t *timesheetModel) set(msg weekLoadedMsg) {
	prevTask := ""
	if t.cursorRow < len(t.rows) {
		prevTask = t.rows[t.cursorRow].taskID
	}
	t.weekStart, t.states, t.loaded = msg.weekStart, msg.states, true
	byTask := map[string]*tsRow{}
	var order []string
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
			row = &tsRow{taskID: l.TaskID, title: title}
			byTask[l.TaskID] = row
			order = append(order, l.TaskID)
		}
		row.cells[idx] = append(row.cells[idx], l)
	}
	t.rows = t.rows[:0]
	for _, id := range order {
		t.rows = append(t.rows, *byTask[id])
	}
	// Start on the first task row. With no rows the only row is the "+ new task" one at index 0.
	t.cursorRow = 0
	for i, r := range t.rows {
		if r.taskID == prevTask {
			t.cursorRow = i
		}
	}
}

func (t timesheetModel) title() string {
	end := t.weekStart.AddDate(0, 0, 6)
	return fmt.Sprintf("Timesheet: %d %s - %d %s %d", t.weekStart.Day(), t.weekStart.Month().String()[:3], end.Day(), end.Month().String()[:3], end.Year())
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

func (t timesheetModel) Update(msg tea.KeyMsg) (timesheetModel, tea.Cmd) {
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
		}
	case key.Matches(msg, t.keys.Up):
		if t.cursorRow > 0 {
			t.cursorRow--
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
	case key.Matches(msg, t.keys.Edit), key.Matches(msg, t.keys.Enter):
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
			return t, intent(openConfirmMsg{prompt: fmt.Sprintf("Delete %.1f h on %s?", logs[0].Hours, logs[0].TrackedDate), onYes: deleteTimelogMsg{id: logs[0].ID}})
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
	titleW := width - 8*cellW - 2
	if titleW < 10 {
		titleW = 10
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", titleW+2))
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

	var dayTotals [7]float64
	for ri, r := range t.rows {
		rowTotal := 0.0
		line := ansi.Truncate(r.title, titleW, "...")
		line += strings.Repeat(" ", titleW+2-ansi.StringWidth(line))
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
		b.WriteString(line + "\n")
	}
	// The empty row: n here logs time on a task picked through search.
	addLabel := muted.Render("+ new task")
	if t.cursorRow == len(t.rows) {
		addLabel = lipgloss.NewStyle().Foreground(th.Accent).Render("+ new task")
	}
	b.WriteString(addLabel + strings.Repeat(" ", titleW+2-10))
	for di := 0; di < 7; di++ {
		text := fmt.Sprintf("%*s", cellW, "")
		if t.cursorRow == len(t.rows) && di == t.cursorDay {
			text = lipgloss.NewStyle().Background(th.Accent).Render(text)
		}
		b.WriteString(text)
	}
	b.WriteString("\n\n")
	total := 0.0
	line := bold.Render(fmt.Sprintf("%-*s", titleW+2, "Total"))
	for _, v := range dayTotals {
		total += v
		line += fmt.Sprintf("%*.1f", cellW, v)
	}
	line += bold.Render(fmt.Sprintf("%*.1f", cellW, total))
	b.WriteString(line)
	return b.String()
}
