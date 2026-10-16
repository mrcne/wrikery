package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/store"
)

type changeRow struct {
	row      store.OutboxRow
	taskID   string
	parentID string // the task's first parent folder, empty if the task has none or is gone
	title    string
	summary  string // the field with its old and new value, one line
	detail   string // the cursor's second line, the values in full, the error takes its place on a row the engine still retries
}

type changesModel struct {
	rows   []changeRow
	cursor int
	offset int
	height int // set by the root from the computed layout, View cannot remember it on its own
	keys   KeyMap
}

type changesLoadedMsg struct{ rows []changeRow }
type retryWriteMsg struct{ id int64 }
type discardWriteMsg struct{ id int64 }

// inQueue tells a row the engine still works on from one that went out or was dropped.
func inQueue(r store.OutboxRow) bool {
	return r.State == store.StatePending || r.State == store.StateInflight || r.State == store.StateFailed
}

// refusal is the status line for a key that cannot act on the row, empty when it can:
// a row being sent belongs to the engine, and a finished row is history.
func refusal(r store.OutboxRow, verb string) string {
	switch r.State {
	case store.StateInflight:
		return "being sent right now"
	case store.StateSent, store.StateDiscarded:
		return "already " + string(r.State) + ", nothing to " + verb
	}
	return ""
}

// stateText is the muted text after a row: where a queued write stands, or when a finished one went out.
func stateText(r store.OutboxRow, now time.Time) string {
	switch r.State {
	case store.StateInflight:
		return "sending"
	case store.StatePending:
		if r.LastError != "" {
			return "retrying " + relTime(r.CreatedAt, now)
		}
		return "queued"
	case store.StateFailed:
		return "failed " + relTime(r.CreatedAt, now)
	case store.StateDiscarded:
		return "discarded " + relTime(r.DoneAt, now)
	}
	return relTime(r.DoneAt, now)
}

// arrow joins an old and a new value, with nothing before the arrow when the row has no old value,
// a task the cache did not have at enqueue.
func arrow(old, now string) string {
	if old == "" {
		return "-> " + now
	}
	return old + " -> " + now
}

func plusMinus(add, remove []string, name func(string) string) string {
	var parts []string
	for _, id := range add {
		parts = append(parts, "+ "+name(id))
	}
	for _, id := range remove {
		parts = append(parts, "- "+name(id))
	}
	return strings.Join(parts, "  ")
}

func datesText(d *store.TaskDates) string {
	switch {
	case d == nil:
		return ""
	case d.Type == "Backlog":
		return "Backlog"
	case d.Type == "Milestone":
		return "milestone " + shortDate(d.Due)
	case d.Start != "" && d.Due != "":
		return shortDate(d.Start) + " to " + shortDate(d.Due)
	case d.Due != "":
		return "due " + shortDate(d.Due)
	case d.Start != "":
		return "from " + shortDate(d.Start)
	}
	return d.Type
}

func timelogText(hours float64, date, note string) string {
	s := hoursText(hours) + " on " + shortDate(date)
	if note != "" {
		s += ", " + note
	}
	return s
}

// describe writes the one line summary of a write and the longer line the cursor shows under it.
// The old side comes from the row's before, the new side from its payload.
// Names come from the reference data and the folder titles the loader looked up, an id nothing knows stays an id.
func describe(row store.OutboxRow, ref refData, folders map[string]string) (summary, detail string) {
	statusName := func(id string) string {
		if id == "" {
			return ""
		}
		if cs, ok := ref.statuses[id]; ok {
			return cs.Name
		}
		return id
	}
	folderName := func(id string) string {
		if title, ok := folders[id]; ok {
			return title
		}
		return id
	}
	contact := func(id string) string { return contactName(id, ref) }
	switch row.Kind {
	case store.KindCommentCreate:
		var p store.CommentCreatePayload
		_ = json.Unmarshal(row.Payload, &p)
		text := strings.Join(strings.Fields(p.Text), " ")
		return "comment: " + ansi.Truncate(text, 42, "..."), text
	case store.KindTaskCreate:
		var p store.TaskCreatePayload
		_ = json.Unmarshal(row.Payload, &p)
		return "new task: " + ansi.Truncate(p.Title, 42, "..."), p.Title
	case store.KindTaskUpdate:
		var p, b store.TaskUpdatePayload
		_ = json.Unmarshal(row.Payload, &p)
		_ = json.Unmarshal(row.Before, &b)
		switch {
		case p.CustomStatusID != "":
			s := "status  " + arrow(statusName(b.CustomStatusID), statusName(p.CustomStatusID))
			return s, s
		case len(p.AddResponsibles)+len(p.RemoveResponsibles) > 0:
			s := "assignee  " + plusMinus(p.AddResponsibles, p.RemoveResponsibles, contact)
			return s, s
		case len(p.AddParents)+len(p.RemoveParents) > 0:
			s := "folder  " + plusMinus(p.AddParents, p.RemoveParents, folderName)
			return s, s
		case p.Title != "":
			return "title  " + arrow(ansi.Truncate(b.Title, 24, "..."), ansi.Truncate(p.Title, 24, "...")), "title  " + arrow(b.Title, p.Title)
		case p.Description != "":
			return "description changed", "description  " + arrow(ansi.Truncate(store.StripHTML(b.Description), 60, "..."), ansi.Truncate(store.StripHTML(p.Description), 60, "..."))
		case p.Importance != "":
			s := "importance  " + arrow(b.Importance, p.Importance)
			return s, s
		case p.Dates != nil:
			s := "dates  " + arrow(datesText(b.Dates), datesText(p.Dates))
			return s, s
		}
		return "task change", ""
	case store.KindTimelogCreate:
		var p store.TimelogCreatePayload
		_ = json.Unmarshal(row.Payload, &p)
		return "time entry " + hoursText(p.Hours) + " on " + shortDate(p.TrackedDate), "time entry " + timelogText(p.Hours, p.TrackedDate, p.Comment)
	case store.KindTimelogUpdate:
		var p, b store.TimelogUpdatePayload
		_ = json.Unmarshal(row.Payload, &p)
		_ = json.Unmarshal(row.Before, &b)
		oldHours, oldDate, old := "", "", ""
		if b.TrackedDate != "" {
			oldHours, oldDate, old = hoursText(b.Hours), shortDate(b.TrackedDate), timelogText(b.Hours, b.TrackedDate, b.Comment)
		}
		detail = "time entry " + arrow(old, timelogText(p.Hours, p.TrackedDate, p.Comment))
		switch {
		case p.Hours > 0 && p.Hours != b.Hours:
			return "time entry " + arrow(oldHours, hoursText(p.Hours)), detail
		case p.TrackedDate != "" && p.TrackedDate != b.TrackedDate:
			return "time entry " + arrow(oldDate, shortDate(p.TrackedDate)), detail
		}
		return "time entry note changed", detail
	case store.KindTimelogDelete:
		var b store.TimelogCreatePayload
		_ = json.Unmarshal(row.Before, &b)
		if b.TrackedDate == "" {
			return "deleted time entry", ""
		}
		return "deleted time entry " + hoursText(b.Hours) + " on " + shortDate(b.TrackedDate), "deleted time entry " + timelogText(b.Hours, b.TrackedDate, b.Comment)
	}
	return string(row.Kind), ""
}

func (s *changesModel) set(rows []changeRow) {
	s.rows = rows
	if s.cursor >= len(rows) {
		s.cursor = max(0, len(rows)-1)
	}
	s.scroll()
}

// scroll clamps offset so the cursor row stays inside the pane, the same pattern as the task
// list. The cursor's own row always costs an extra line for its second line, so the usable
// capacity is one row less than the pane height, or the row scrolled to last would still overflow.
func (s *changesModel) scroll() {
	capacity := s.height - 1
	if capacity <= 0 {
		return
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+capacity {
		s.offset = s.cursor - capacity + 1
	}
}

// move puts the cursor n rows down, or up for a negative n, clamped to the list, and keeps it on screen.
func (s *changesModel) move(n int) {
	s.cursor = min(max(s.cursor+n, 0), max(len(s.rows)-1, 0))
	s.scroll()
}

func (s changesModel) Update(msg tea.KeyMsg) (changesModel, tea.Cmd) {
	page := max(1, (s.height-1)/2)
	switch {
	case key.Matches(msg, s.keys.Down):
		s.move(1)
	case key.Matches(msg, s.keys.Up):
		s.move(-1)
	case key.Matches(msg, s.keys.HalfDown):
		s.move(page)
	case key.Matches(msg, s.keys.HalfUp):
		s.move(-page)
	case key.Matches(msg, s.keys.Top):
		s.move(-len(s.rows))
	case key.Matches(msg, s.keys.Bottom):
		s.move(len(s.rows))
	case key.Matches(msg, s.keys.Retry):
		if s.cursor < len(s.rows) {
			r := s.rows[s.cursor]
			if text := refusal(r.row, "retry"); text != "" {
				return s, intent(toastMsg{text: text})
			}
			return s, intent(retryWriteMsg{id: r.row.ID})
		}
	case key.Matches(msg, s.keys.Discard):
		if s.cursor < len(s.rows) {
			r := s.rows[s.cursor]
			if text := refusal(r.row, "discard"); text != "" {
				return s, intent(toastMsg{text: text})
			}
			// store.Outbox().Discard only rolls back the optimistic row for a create,
			// a task update leaves the cache ahead of the server until the next pull corrects it.
			// A new task has no server state to come back to, and the comments and time entries queued on it exist nowhere else.
			prompt := "Discard this write? The next refresh brings back the server state."
			if r.row.Kind == store.KindTaskCreate {
				prompt = "Discard this new task? Its queued comments and time entries go with it."
			}
			prompt += "\n" + r.summary
			return s, intent(openConfirmMsg{prompt: prompt, onYes: discardWriteMsg{id: r.row.ID}})
		}
	case key.Matches(msg, s.keys.Enter):
		if s.cursor < len(s.rows) {
			r := s.rows[s.cursor]
			if r.taskID == "" {
				// A discarded new task, or a task the pull sweep removed since, has no detail to open.
				return s, intent(toastMsg{text: "the task is not in the cache"})
			}
			return s, intent(openTaskMsg{id: r.taskID, parentID: r.parentID})
		}
	}
	return s, nil
}

func (s changesModel) View(th Theme, now time.Time, width, height int) string {
	if len(s.rows) == 0 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render(fmt.Sprintf("No changes in the last %d days.", store.HistoryDays))
	}
	// The cursor's row always prints a second line, so only height-1 rows are drawn here,
	// the same capacity scroll() clamps offset against, or that second line would be the one
	// the surrounding box trims off the end.
	capacity := max(1, height-1)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	for i := s.offset; i < len(s.rows) && i-s.offset < capacity; i++ {
		r := s.rows[i]
		when := stateText(r.row, now)
		if r.row.Source == store.SourceCLI {
			when += "  cli"
		}
		label := fmt.Sprintf("%s  %s  %s", ansi.Truncate(displayTitle(r.title, th.HidePrefixes), 30, "..."), r.summary, muted.Render(when))
		b.WriteString(rowLine(th, label, width, i == s.cursor, true) + "\n")
		if i == s.cursor {
			second, style := r.detail, muted
			if r.row.LastError != "" && inQueue(r.row) {
				second, style = r.row.LastError, lipgloss.NewStyle().Foreground(th.Error)
			}
			b.WriteString("    " + style.Render(ansi.Truncate(second, width-6, "...")) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
