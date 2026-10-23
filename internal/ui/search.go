package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type searchModel struct {
	input    textinput.Model
	results  []store.Task
	crumbs   map[string]string
	cursor   int
	seq      int
	keys     KeyMap
	pickMode bool // true while search stands in for the timesheet's task picker
	// The reach row: a reference the cache lacks, drawn last, whether its fetch runs, and why the last one failed.
	ref      string
	fetching bool
	failed   string
	visible  int // rows the box can show, set by the root, the reach row stays inside them
}

func newSearch(keys KeyMap) searchModel {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "search tasks"
	in.CharLimit = 120
	return searchModel{input: in, keys: keys}
}

func (s *searchModel) reset() tea.Cmd {
	s.input.SetValue("")
	s.results, s.cursor = nil, 0
	s.ref, s.fetching, s.failed = "", false, ""
	// Cleared here, not just where a pick starts, so a plain ctrl+f after a cancelled pick opens tasks again.
	s.pickMode = false
	return s.input.Focus()
}

// blur stops the cursor from blinking once the overlay is no longer on screen.
func (s *searchModel) blur() {
	s.input.Blur()
}

func (s searchModel) Update(msg tea.Msg) (searchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case searchResultsMsg:
		// Results from an older keystroke arrive late and would overwrite the newer list.
		if msg.seq != s.seq {
			return s, nil
		}
		s.results, s.crumbs, s.ref = msg.tasks, msg.crumbs, msg.ref
		s.fetching, s.failed = false, ""
		if s.cursor >= s.rowCount() {
			s.cursor = max(0, s.rowCount()-1)
		}
		return s, nil
	case tea.KeyMsg:
		// j and k are letters in the search box, so only the arrows and ctrl+n / ctrl+p move the cursor.
		switch {
		case key.Matches(msg, s.keys.Down) && msg.Type != tea.KeyRunes:
			if s.cursor < s.rowCount()-1 {
				s.cursor++
			}
			return s, nil
		case key.Matches(msg, s.keys.Up) && msg.Type != tea.KeyRunes:
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		case msg.Type == tea.KeyCtrlN:
			if s.cursor < s.rowCount()-1 {
				s.cursor++
			}
			return s, nil
		case msg.Type == tea.KeyCtrlP:
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		case msg.Type == tea.KeyEnter:
			if s.ref != "" && s.cursor == s.hitsShown() {
				ref, ok := store.ParseTaskRef(s.ref)
				if s.fetching || !ok {
					return s, nil
				}
				s.fetching, s.failed = true, ""
				origin := reachFromSearch
				if s.pickMode {
					origin = reachFromPick
				}
				return s, intent(reachMsg{ref: ref, text: s.ref, seq: s.seq, origin: origin})
			}
			if s.cursor < len(s.results) {
				if s.pickMode {
					return s, intent(searchPickMsg{task: s.results[s.cursor]})
				}
				t := s.results[s.cursor]
				return s, intent(openTaskMsg{task: t})
			}
			return s, nil
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		s.seq++
		return s, tea.Batch(cmd, intent(runSearchMsg{seq: s.seq, query: s.input.Value()}))
	}
	return s, nil
}

// hitsDrawn is how many hits the box draws: the visible rows, one fewer when the reach row needs the last line,
// and all of them before the first size message.
func (s searchModel) hitsDrawn() int {
	switch {
	case s.visible <= 0:
		return len(s.results)
	case s.ref != "":
		return min(len(s.results), max(s.visible-1, 1))
	}
	return min(len(s.results), s.visible)
}

// hitsShown is how many hits the cursor walks: all of them, or only the drawn ones when the reach row needs the last line,
// so the row is reached right after the last visible hit and never sits behind hidden ones.
func (s searchModel) hitsShown() int {
	if s.ref == "" {
		return len(s.results)
	}
	return s.hitsDrawn()
}

// rowCount is the hits the cursor walks plus the reach row when the query is a reference the cache lacks.
func (s searchModel) rowCount() int {
	if s.ref != "" {
		return s.hitsShown() + 1
	}
	return len(s.results)
}

// reachFailed shows why the fetch of ref did not bring a task, an answer for an older query is dropped.
func (s *searchModel) reachFailed(ref, text string) {
	if ref != s.ref {
		return
	}
	s.fetching, s.failed = false, text
}

// searchMaxRows caps the visible result rows so the overlay still fits a short terminal,
// leaving room for the input line, the blank line under it and the box border.
func searchMaxRows(height int) int {
	return min(12, max(3, height-8))
}

func (s searchModel) View(th Theme, ref refData, width int) string {
	var b strings.Builder
	b.WriteString(s.input.View() + "\n\n")
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	if len(s.results) == 0 && s.ref == "" {
		if strings.TrimSpace(s.input.Value()) == "" {
			b.WriteString(muted.Render("type to search titles and descriptions"))
		} else {
			b.WriteString(muted.Render("no matches"))
		}
	}
	shown := s.hitsDrawn()
	for i, t := range s.results {
		if i >= shown {
			b.WriteString(muted.Render(fmt.Sprintf("... %d more", len(s.results)-shown)))
			if s.ref != "" {
				b.WriteString("\n")
			}
			break
		}
		cs := ref.statusOf(t)
		glyph := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group))
		label := glyph + " " + th.styledTitle(t.Title) + "  " + muted.Render(s.crumbs[t.ID])
		b.WriteString(rowLine(th, label, width-2, i == s.cursor, true) + "\n")
	}
	if s.ref != "" {
		// Last, under the hits: a number can be part of a title and a word can have the shape of an id.
		label := "open from Wrike: " + s.ref
		switch {
		case s.fetching:
			label = "fetching from Wrike..."
		case s.failed != "":
			label = s.failed
		}
		b.WriteString(rowLine(th, muted.Render(label), width-2, s.cursor == s.hitsShown(), true) + "\n")
	}
	title := "Search"
	if s.pickMode {
		title = "Pick a task"
	}
	return th.box(title, b.String(), width, lipgloss.Height(b.String())+2, true)
}
