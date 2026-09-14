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
		s.results, s.crumbs = msg.tasks, msg.crumbs
		if s.cursor >= len(s.results) {
			s.cursor = max(0, len(s.results)-1)
		}
		return s, nil
	case tea.KeyMsg:
		// j and k are letters in the search box, so only the arrows and ctrl+n / ctrl+p move the cursor.
		switch {
		case key.Matches(msg, s.keys.Down) && msg.Type != tea.KeyRunes:
			if s.cursor < len(s.results)-1 {
				s.cursor++
			}
			return s, nil
		case key.Matches(msg, s.keys.Up) && msg.Type != tea.KeyRunes:
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		case msg.Type == tea.KeyCtrlN:
			if s.cursor < len(s.results)-1 {
				s.cursor++
			}
			return s, nil
		case msg.Type == tea.KeyCtrlP:
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		case msg.Type == tea.KeyEnter:
			if s.cursor < len(s.results) {
				if s.pickMode {
					return s, intent(searchPickMsg{task: s.results[s.cursor]})
				}
				t := s.results[s.cursor]
				parentID := ""
				if len(t.ParentIDs) > 0 {
					parentID = t.ParentIDs[0]
				}
				return s, intent(openTaskMsg{id: t.ID, parentID: parentID})
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

// searchMaxRows caps the visible result rows so the overlay still fits a short terminal,
// leaving room for the input line, the blank line under it and the box border.
func searchMaxRows(height int) int {
	return min(12, max(3, height-8))
}

func (s searchModel) View(th Theme, ref refData, width, maxRows int) string {
	var b strings.Builder
	b.WriteString(s.input.View() + "\n\n")
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	if len(s.results) == 0 {
		if strings.TrimSpace(s.input.Value()) == "" {
			b.WriteString(muted.Render("type to search titles and descriptions"))
		} else {
			b.WriteString(muted.Render("no matches"))
		}
	}
	for i, t := range s.results {
		if i >= maxRows {
			b.WriteString(muted.Render(fmt.Sprintf("... %d more", len(s.results)-maxRows)))
			break
		}
		cs := ref.statuses[t.CustomStatusID]
		if cs.Group == "" {
			cs.Group = t.Status
		}
		glyph := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group))
		label := glyph + " " + th.styledTitle(t.Title) + "  " + muted.Render(s.crumbs[t.ID])
		b.WriteString(rowLine(th, label, width-2, i == s.cursor, true) + "\n")
	}
	title := "Search"
	if s.pickMode {
		title = "Pick a task"
	}
	return th.box(title, b.String(), width, lipgloss.Height(b.String())+2, true)
}
