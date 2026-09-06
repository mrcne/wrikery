package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type firstRunStep int

const (
	stepToken firstRunStep = iota
	stepScopes
	stepSyncing
)

type pickerItem struct {
	scope    store.Scope
	depth    int
	checked  bool
	disabled bool
}

type firstRunModel struct {
	step      firstRunStep
	reauth    bool   // a 401 sent the user back here, so a good token returns to the main screen instead of asking again
	notice    string // shown above the token input, set after a 401
	input     textinput.Model
	spinner   spinner.Model
	verifying bool
	errText   string
	name      string
	items     []pickerItem
	cursor    int
	scopes    []store.Scope
	keys      KeyMap
}

func newFirstRun(step firstRunStep, notice string, keys KeyMap) firstRunModel {
	in := textinput.New()
	in.Placeholder = "paste the token here"
	in.EchoMode = textinput.EchoPassword
	// A permanent token is a JWT of several hundred characters and a character limit would cut it without a word,
	// so the input takes any length and scrolls it instead of wrapping out of the border.
	in.Width = 60
	in.Focus()
	sp := spinner.New(spinner.WithSpinner(spinner.Line))
	return firstRunModel{step: step, notice: notice, input: in, spinner: sp, keys: keys}
}

func (f firstRunModel) Update(msg tea.Msg) (firstRunModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tokenVerifiedMsg:
		f.verifying = false
		if msg.err != nil {
			f.errText = msg.err.Error()
			return f, nil
		}
		f.name, f.errText = msg.name, ""
		if f.reauth {
			return f, intent(firstRunFinishedMsg{})
		}
		f.step = stepScopes
		return f, nil
	case pickerLoadedMsg:
		f.items = f.mergePicker(msg)
		// The list can shrink when a space is unfollowed upstream, and a cursor past the end would panic on the next toggle.
		if f.cursor >= len(f.items) {
			f.cursor = max(0, len(f.items)-1)
		}
		return f, nil
	case scopesLoadedMsg:
		f.scopes = msg.scopes
		return f, nil
	case spinner.TickMsg:
		// Both the token check and the sync step animate, so the ticks have to keep coming for either one.
		if !f.verifying && f.step != stepSyncing {
			return f, nil
		}
		var cmd tea.Cmd
		f.spinner, cmd = f.spinner.Update(msg)
		return f, cmd
	case tea.KeyMsg:
		return f.handleKey(msg)
	}
	return f, nil
}

func (f firstRunModel) handleKey(msg tea.KeyMsg) (firstRunModel, tea.Cmd) {
	switch f.step {
	case stepToken:
		if msg.Type == tea.KeyEnter {
			token := strings.TrimSpace(f.input.Value())
			if token == "" || f.verifying {
				return f, nil
			}
			f.verifying, f.errText = true, ""
			return f, tea.Batch(intent(firstRunSubmitTokenMsg{token: token}), f.spinner.Tick)
		}
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		return f, cmd
	case stepScopes:
		switch {
		case key.Matches(msg, f.keys.Down):
			if f.cursor < len(f.items)-1 {
				f.cursor++
			}
		case key.Matches(msg, f.keys.Up):
			if f.cursor > 0 {
				f.cursor--
			}
		case msg.Type == tea.KeySpace:
			if len(f.items) > 0 && !f.items[f.cursor].disabled {
				f.items[f.cursor].checked = !f.items[f.cursor].checked
			}
		case msg.Type == tea.KeyEnter:
			var selected []store.Scope
			for _, it := range f.items {
				if it.checked && !it.disabled {
					selected = append(selected, it.scope)
				}
			}
			f.step = stepSyncing
			return f, intent(firstRunConfirmScopesMsg{scopes: selected})
		}
	case stepSyncing:
		if msg.Type == tea.KeyEnter {
			return f, intent(firstRunFinishedMsg{})
		}
	}
	return f, nil
}

// mergePicker rebuilds the checklist from the store and keeps what the user already ticked.
func (f firstRunModel) mergePicker(msg pickerLoadedMsg) []pickerItem {
	checked := map[string]bool{}
	for _, it := range f.items {
		checked[it.scope.ID] = it.checked
	}
	items := []pickerItem{{scope: store.Scope{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true}, checked: true, disabled: true}}
	for _, sp := range msg.spaces {
		items = append(items, pickerItem{scope: store.Scope{ID: sp.ID, Kind: store.ScopeKindSpace, Title: sp.Title, Followed: true}, checked: checked[sp.ID]})
		for _, p := range msg.projects[sp.ID] {
			items = append(items, pickerItem{scope: store.Scope{ID: p.ID, Kind: store.ScopeKindProject, Title: p.Title, Followed: true}, depth: 1, checked: checked[p.ID]})
		}
	}
	return items
}

func (f firstRunModel) View(th Theme, width, height int) string {
	accent := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	steps := []string{"1 Token", "2 Follow", "3 Sync"}
	var head []string
	for i, s := range steps {
		if firstRunStep(i) == f.step {
			head = append(head, accent.Render(s))
		} else {
			head = append(head, muted.Render(s))
		}
	}
	var body strings.Builder
	body.WriteString(strings.Join(head, muted.Render("  ->  ")) + "\n\n")
	switch f.step {
	case stepToken:
		if f.notice != "" {
			body.WriteString(lipgloss.NewStyle().Foreground(th.Warn).Render(f.notice) + "\n\n")
		}
		body.WriteString("Paste a permanent access token.\n")
		body.WriteString(muted.Render("Create one in the Wrike App Console (Get token). Wrike shows it once.") + "\n")
		body.WriteString(muted.Render("https://www.wrike.com/appconsole.htm?#/api") + "\n\n")
		body.WriteString(f.input.View() + "\n\n")
		switch {
		case f.verifying:
			body.WriteString(f.spinner.View() + " checking the token")
		case f.errText != "":
			body.WriteString(lipgloss.NewStyle().Foreground(th.Error).Render(f.errText))
		default:
			body.WriteString(muted.Render("enter to continue"))
		}
	case stepScopes:
		body.WriteString("Hello " + f.name + ". Pick the spaces and projects to follow.\n")
		body.WriteString(muted.Render("Only these are synced and searchable.") + "\n")
		body.WriteString(muted.Render("Your own tasks are always included.") + "\n\n")
		if len(f.items) == 0 {
			body.WriteString(f.spinner.View() + " loading spaces")
		}
		for i, it := range f.items {
			mark := "[ ]"
			if it.checked {
				mark = "[x]"
			}
			line := strings.Repeat("  ", it.depth) + mark + " " + it.scope.Title
			if it.disabled {
				line = muted.Render(line)
			}
			if i == f.cursor {
				line = accent.Render(th.Glyphs.Cursor+" ") + line
			} else {
				line = "  " + line
			}
			body.WriteString(line + "\n")
		}
		body.WriteString("\n" + muted.Render("space to toggle, enter to confirm"))
	case stepSyncing:
		body.WriteString("Syncing the followed scopes.\n\n")
		for _, sc := range f.scopes {
			glyph := f.spinner.View()
			if sc.Cursor != "" {
				glyph = lipgloss.NewStyle().Foreground(th.Success).Render(th.Glyphs.Completed)
			}
			body.WriteString(glyph + " " + sc.Title + "\n")
		}
		body.WriteString("\n" + muted.Render("enter to open the app, tasks keep arriving in the background"))
	}
	w := min(width-4, 72)
	box := th.box("Welcome to wrikery", body.String(), w, min(height, lipgloss.Height(body.String())+2), true)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
