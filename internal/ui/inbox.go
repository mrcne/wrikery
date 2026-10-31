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

type inboxRow struct {
	row    store.InboxRow
	task   store.Task // the task as the cache has it, zero when it went between the two reads of the loader
	author string     // the author's name, or the id when the contacts table lacks it
}

// inboxModel is the inbox screen: the comments addressed to the user, newest first, drawn like the changes screen.
type inboxModel struct {
	rows   []inboxRow
	seenTo string // the marker the rows were loaded with, a row that arrived after it is unread
	seen   string // the newest arrival among the rows drawn since the load, what leaving writes as the marker
	cursor int
	offset int
	height int // set by the root from the computed layout, View cannot remember it on its own
	keys   KeyMap
}

type inboxLoadedMsg struct {
	rows   []inboxRow
	seenTo string
}

// inboxLimit caps the rows the screen reads, a month of comments on a busy account is more than anyone reads in a list.
const inboxLimit = 200

// inboxTextLines is how many lines of the comment the cursor's row shows under itself.
// A comment of a paragraph fits in three at a hundred columns, and the thread has the rest.
const inboxTextLines = 3

// set keeps the cursor on the comment it was on: a reload lands a new comment at the top, so the index would move down one.
func (s *inboxModel) set(rows []inboxRow, seenTo string) {
	var on string
	if s.cursor < len(s.rows) {
		on = s.rows[s.cursor].row.ID
	}
	s.rows, s.seenTo = rows, seenTo
	s.cursor = min(s.cursor, max(0, len(rows)-1))
	for i, r := range rows {
		if r.row.ID == on {
			s.cursor = i
			break
		}
	}
	s.scroll()
}

// capacity is how many rows fit above the cursor's text lines and, on a full list, the line that says so.
func (s inboxModel) capacity() int {
	n := s.height - inboxTextLines
	if len(s.rows) == inboxLimit {
		n--
	}
	return n
}

// scroll clamps offset so the cursor row stays inside the pane, the changes screen's rule with the lines of text
// under the cursor's row counted in, and notes the newest arrival among the rows now on screen.
func (s *inboxModel) scroll() {
	capacity := s.capacity()
	if capacity <= 0 {
		return
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+capacity {
		s.offset = s.cursor - capacity + 1
	}
	for _, r := range s.rows[s.offset:min(s.offset+capacity, len(s.rows))] {
		s.seen = max(s.seen, r.row.ArrivedAt)
	}
}

func (s *inboxModel) move(n int) {
	s.cursor = min(max(s.cursor+n, 0), max(len(s.rows)-1, 0))
	s.scroll()
}

func (s inboxModel) Update(msg tea.KeyMsg) (inboxModel, tea.Cmd) {
	page := max(1, s.capacity()/2)
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
	case key.Matches(msg, s.keys.Enter):
		if s.cursor < len(s.rows) {
			r := s.rows[s.cursor]
			if r.task.ID == "" {
				return s, intent(toastMsg{text: "the task is not in the cache"})
			}
			return s, intent(openTaskMsg{task: r.task})
		}
	}
	return s, nil
}

func (s inboxModel) View(th Theme, now time.Time, width, height int) string {
	if len(s.rows) == 0 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render(fmt.Sprintf("No comments for you in the last %d days.", store.InboxDays))
	}
	capacity := max(1, s.capacity())
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	accent := lipgloss.NewStyle().Foreground(th.Accent)
	var b strings.Builder
	for i := s.offset; i < len(s.rows) && i-s.offset < capacity; i++ {
		r := s.rows[i]
		marker := "  "
		if r.row.ArrivedAt > s.seenTo {
			marker = accent.Render("*") + " "
		}
		sep := ": "
		if r.row.Mentioned {
			sep = " mentioned you: "
		}
		// Measured after stableWidth, as rowLine will draw it: an emoji drawn as its parts is wider than the joined form measures.
		head := marker + ansi.Truncate(stableWidth(displayTitle(r.row.TaskTitle, th.HidePrefixes)), 30, "...") + "  " + stableWidth(r.author) + sep
		when := relTime(r.row.CreatedDate, now)
		// The text takes what the width leaves after the head and the time, the row never hides the time behind it.
		room := width - 2 - ansi.StringWidth(head) - ansi.StringWidth(when) - 2
		text := ansi.Truncate(stableWidth(strings.Join(strings.Fields(r.row.Text), " ")), max(room, 0), "...")
		b.WriteString(rowLine(th, head+text+"  "+muted.Render(when), width, i == s.cursor, true) + "\n")
		if i == s.cursor {
			for _, line := range wrapText(r.row.Text, width-6, inboxTextLines) {
				b.WriteString("    " + muted.Render(line) + "\n")
			}
		}
	}
	if len(s.rows) == inboxLimit && s.offset+capacity >= len(s.rows) {
		// The count in the status bar is not cut, so a full list says where the rest is, on the line capacity() kept for it.
		b.WriteString(muted.Render(fmt.Sprintf("  the list stops at %d rows, older comments are in their threads", inboxLimit)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// wrapText wraps the text to the width and keeps the first lines, the last of them cut with dots when there is more.
func wrapText(text string, width, lines int) []string {
	if width <= 0 {
		return nil
	}
	out := strings.Split(ansi.Wrap(stableWidth(strings.TrimSpace(text)), width, ""), "\n")
	if len(out) > lines {
		rest := strings.Join(out[lines-1:], " ")
		out = append(out[:lines-1], ansi.Truncate(rest, width, "..."))
	}
	return out
}
