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

type issueRow struct {
	row     store.OutboxRow
	taskID  string
	title   string
	summary string
}

type issuesModel struct {
	rows   []issueRow
	cursor int
	offset int
	height int // set by the root from the computed layout, View cannot remember it on its own
	keys   KeyMap
}

type issuesLoadedMsg struct{ rows []issueRow }
type retryIssueMsg struct{ id int64 }
type discardIssueMsg struct{ id int64 }
type openTaskMsg struct{ id string }

func summarize(row store.OutboxRow) string {
	switch row.Kind {
	case store.KindCommentCreate:
		var p store.CommentCreatePayload
		_ = json.Unmarshal(row.Payload, &p)
		return "comment: " + ansi.Truncate(p.Text, 42, "...")
	case store.KindTaskUpdate:
		var p store.TaskUpdatePayload
		_ = json.Unmarshal(row.Payload, &p)
		switch {
		case p.CustomStatusID != "":
			return "status change"
		case len(p.AddResponsibles)+len(p.RemoveResponsibles) > 0:
			return "assignee change"
		case p.Dates != nil:
			return "dates change"
		}
		return "task change"
	case store.KindTimelogCreate, store.KindTimelogUpdate:
		var p store.TimelogCreatePayload
		_ = json.Unmarshal(row.Payload, &p)
		return fmt.Sprintf("time entry %.1f h on %s", p.Hours, p.TrackedDate)
	case store.KindTimelogDelete:
		return "delete time entry"
	}
	return string(row.Kind)
}

func (s *issuesModel) set(rows []issueRow) {
	s.rows = rows
	if s.cursor >= len(rows) {
		s.cursor = max(0, len(rows)-1)
	}
	s.scroll()
}

// scroll clamps offset so the cursor row stays inside the pane, the same pattern as the task list.
func (s *issuesModel) scroll() {
	if s.height <= 0 {
		return
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+s.height {
		s.offset = s.cursor - s.height + 1
	}
}

func (s issuesModel) Update(msg tea.KeyMsg) (issuesModel, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Down):
		if s.cursor < len(s.rows)-1 {
			s.cursor++
			s.scroll()
		}
	case key.Matches(msg, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
			s.scroll()
		}
	case key.Matches(msg, s.keys.Retry):
		if s.cursor < len(s.rows) {
			return s, intent(retryIssueMsg{id: s.rows[s.cursor].row.ID})
		}
	case key.Matches(msg, s.keys.Discard):
		if s.cursor < len(s.rows) {
			r := s.rows[s.cursor]
			// store.Outbox().Discard only rolls back the optimistic row for a create, a task update
			// leaves the cache ahead of the server until the next pull corrects it, so the prompt makes
			// no promise that does not hold for every kind.
			prompt := "Discard this write? The next refresh brings back the server state.\n" + r.summary
			return s, intent(openConfirmMsg{prompt: prompt, onYes: discardIssueMsg{id: r.row.ID}})
		}
	case key.Matches(msg, s.keys.Enter):
		if s.cursor < len(s.rows) && s.rows[s.cursor].taskID != "" {
			return s, intent(openTaskMsg{id: s.rows[s.cursor].taskID})
		}
	}
	return s, nil
}

func (s issuesModel) View(th Theme, now time.Time, width, height int) string {
	if len(s.rows) == 0 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("No failed writes.")
	}
	// offset lives on the model and is advanced by scroll(), this only guards against it landing
	// past the end, for example right after a discard shrinks the row count.
	offset := s.offset
	if last := len(s.rows) - 1; offset > last {
		offset = max(0, last)
	}
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var b strings.Builder
	for i := offset; i < len(s.rows) && i-offset < height; i++ {
		r := s.rows[i]
		when := relTime(r.row.CreatedAt, now)
		label := fmt.Sprintf("%s  %s  %s", ansi.Truncate(r.title, 30, "..."), r.summary, muted.Render(when))
		b.WriteString(rowLine(th, label, width, i == s.cursor, true) + "\n")
		if i == s.cursor {
			b.WriteString("    " + lipgloss.NewStyle().Foreground(th.Error).Render(ansi.Truncate(r.row.LastError, width-6, "...")) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
