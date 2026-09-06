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
	input   textinput.Model
	results []store.Task
	crumbs  map[string]string
	cursor  int
	seq     int
	keys    KeyMap
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
	return s.input.Focus()
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
				return s, intent(searchOpenMsg{task: s.results[s.cursor]})
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

func (s searchModel) View(th Theme, ref refData, width int) string {
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
		if i >= 12 {
			b.WriteString(muted.Render(fmt.Sprintf("... %d more", len(s.results)-12)))
			break
		}
		cs := ref.statuses[t.CustomStatusID]
		if cs.Group == "" {
			cs.Group = t.Status
		}
		glyph := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group))
		label := glyph + " " + t.Title + "  " + muted.Render(s.crumbs[t.ID])
		b.WriteString(rowLine(th, label, width-2, i == s.cursor, true) + "\n")
	}
	return th.box("Search", b.String(), width, lipgloss.Height(b.String())+2, true)
}
