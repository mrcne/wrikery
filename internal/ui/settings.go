package ui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

type openScopesMsg struct{}    // intent: open the box that picks the followed spaces and projects
type toggleSidebarMsg struct{} // intent: hide the sidebar pane or show it again, what ctrl+b does
type toggleMoveAskMsg struct{} // intent: ask again before H and L move a task, or stop asking

type rowKind int

const (
	rowReadOnly rowKind = iota // enter sends msg, or names the key and the file
	rowText                    // enter opens a text field with edit in it
	rowChoice                  // enter opens choices, left and right pick, edit is the current one
)

type settingRow struct {
	group, label, value string
	key                 string  // the key in the config file, empty for a row the app owns
	msg                 tea.Msg // what enter on a row the app owns sends
	kind                rowKind
	edit                string   // what the edit starts from: the field's text, or the current choice
	choices             []string // the values a choice row offers
}

// settingsModel is the settings screen: what the app owns and can change, the followed scopes and the sidebar pane, next to what the config file owns.
// With a hook that writes the file the file rows are edited in place: enter opens the value, enter saves it, esc puts the old one back.
// Without the hook, the demo, enter on a file row names the key to set by hand.
// Two file rows stay read only either way: the data center is set once, at the sign in, and the budget is two keys with a sync behind them.
type settingsModel struct {
	cfg           config.Config
	theme         string // ui.theme as the file has it, cfg.UI.Theme is what auto resolved to
	host          string // the host the token probe found, empty until the meta row is read or when there is none
	file          string
	keys          KeyMap
	scopes        []store.Scope
	sidebarHidden bool
	moveConfirmed bool   // the saved yes to the question before the first H or L
	signedIn      string // SignedInWithToken or SignedInWithBrowser, empty in the demo and before the first sign in
	signedInApp   string // the client id of a browser session, from the session itself, the config may have lost its table since
	budget        string // the Budget row's value, the config read against the cache, see budgetRowText
	cursor        int

	editable bool // a hook writes the file, see Hooks.SetConfig
	editing  bool // the row under the cursor is open, the list keys go to the edit until enter or esc
	editKey  string
	input    textinput.Model
	choices  []string // the open choice row's values, nil while a text row is open
	choice   int
}

func newSettings(cfg config.Config, theme, file string, keys KeyMap) settingsModel {
	in := textinput.New()
	in.Prompt = ""
	in.Width = 40
	return settingsModel{cfg: cfg, theme: theme, file: file, keys: keys, budget: budgetRowText(cfg.Budget, nil, false), input: in}
}

// budgetRowText is the Budget row: the source the config names, read against the account's fields and the Effort flag.
func budgetRowText(cfg config.BudgetConfig, fields []store.CustomField, effortUnavailable bool) string {
	switch cfg.Source {
	case "effort":
		if effortUnavailable {
			return "Effort field, not available on this account"
		}
		return "Effort field"
	case "field":
		typed, firstType, found := budgetCandidates(cfg, fields)
		switch {
		case !found:
			return fmt.Sprintf("custom field %q, not found on this account", cfg.Field)
		case len(typed) == 0:
			return fmt.Sprintf("custom field %q is a %s field, a Duration or Numeric field is needed", cfg.Field, firstType)
		}
		return fmt.Sprintf("custom field %q (%s)", cfg.Field, typed[0].Type)
	}
	return "not set"
}

func (s *settingsModel) setScopes(scopes []store.Scope) { s.scopes = scopes }

func (s *settingsModel) setSidebarHidden(hidden bool) { s.sidebarHidden = hidden }

func (s *settingsModel) setMoveConfirmed(confirmed bool) { s.moveConfirmed = confirmed }

func (s *settingsModel) setSignedIn(with, app string) { s.signedIn, s.signedInApp = with, app }

// setConfig is the config after a save, theme is ui.theme as the file has it.
func (s *settingsModel) setConfig(cfg config.Config, theme string) { s.cfg, s.theme = cfg, theme }

func (s settingsModel) rows() []settingRow {
	var names []string
	for _, sc := range s.scopes {
		if sc.Kind != store.ScopeKindMe {
			names = append(names, sc.Title)
		}
	}
	ui := s.cfg.UI
	// The rows show the file's values next to their keys, so a user can read a row and know what to write.
	theme, fileTheme := ui.Theme, orText(s.theme, ui.Theme)
	if s.theme != "" && s.theme != ui.Theme {
		theme = s.theme + ", " + ui.Theme + " now"
	}
	host := s.cfg.Host
	if host == "" {
		host = orText(s.host, "not detected yet")
		if s.host != "" {
			host += " (detected)"
		}
	}
	sidebar := "shown"
	if s.sidebarHidden {
		sidebar = "hidden"
	}
	ask := "yes"
	if s.moveConfirmed {
		ask = "no"
	}
	hostNote := "the data center is set at the sign in, wrikery --logout and sign in again to change it, or set host in " + s.file
	if s.cfg.Host != "" {
		// A sign in checks the config host alone, so a new one would not change it.
		hostNote = "host is set in " + s.file + ", change it there and start the app again"
	}
	poll := fmt.Sprintf("%ds", int(s.cfg.PollInterval.Seconds()))
	prefixes := strings.Join(ui.HidePrefixes, " ")
	yesNo := []string{"false", "true"}
	var rows []settingRow
	if s.signedIn != "" {
		with := "a permanent token"
		if s.signedIn == SignedInWithBrowser {
			with = "the browser, app " + s.signedInApp
		}
		// No config key changes a stored sign in, only a sign out and a new one do.
		rows = append(rows, settingRow{group: "Account", label: "Signed in with", value: with,
			msg: toastMsg{text: "wrikery --logout signs out, the next start asks for a new sign in"}})
	}
	return append(rows, []settingRow{
		{group: "Sync", label: "Followed spaces and projects", value: orText(strings.Join(names, ", "), "none"), msg: openScopesMsg{}},
		{group: "Sync", label: "Check Wrike every", value: poll, key: "poll_interval", kind: rowText, edit: poll},
		{group: "Sync", label: "Wrike host", value: host, key: "host", msg: toastMsg{text: hostNote}},
		{group: "Sync", label: "Start offline", value: fmt.Sprint(s.cfg.Offline), key: "offline", kind: rowChoice, edit: fmt.Sprint(s.cfg.Offline), choices: yesNo},
		{group: "Sync", label: "Budget", value: s.budget, key: "budget.source"},
		{group: "Interface", label: "Sidebar", value: sidebar, msg: toggleSidebarMsg{}},
		{group: "Interface", label: "Ask before H and L", value: ask, msg: toggleMoveAskMsg{}},
		{group: "Interface", label: "Theme", value: theme, key: "ui.theme", kind: rowChoice, edit: fileTheme, choices: []string{"auto", "dark", "light"}},
		{group: "Interface", label: "Accent", value: orText(ui.Accent, "default"), key: "ui.accent", kind: rowText, edit: ui.Accent},
		{group: "Interface", label: "ASCII glyphs", value: fmt.Sprint(ui.ASCII), key: "ui.ascii", kind: rowChoice, edit: fmt.Sprint(ui.ASCII), choices: yesNo},
		{group: "Interface", label: "Hidden title prefixes", value: orText(prefixes, "none"), key: "ui.hide_prefixes", kind: rowText, edit: prefixes},
		{group: "Interface", label: "Branch name template", value: ui.BranchTemplate, key: "ui.branch_template", kind: rowText, edit: ui.BranchTemplate},
		{group: "Logging", label: "Log level", value: s.cfg.LogLevel, key: "log_level", kind: rowChoice, edit: s.cfg.LogLevel, choices: []string{"debug", "info", "warn", "error"}},
	}...)
}

func orText(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (s settingsModel) Update(msg tea.KeyMsg) (settingsModel, tea.Cmd) {
	if s.editing {
		return s.updateEdit(msg)
	}
	rows := s.rows()
	switch {
	case key.Matches(msg, s.keys.Down):
		s.cursor = min(s.cursor+1, len(rows)-1)
	case key.Matches(msg, s.keys.Up):
		s.cursor = max(s.cursor-1, 0)
	case key.Matches(msg, s.keys.Top):
		s.cursor = 0
	case key.Matches(msg, s.keys.Bottom):
		s.cursor = len(rows) - 1
	case key.Matches(msg, s.keys.Enter):
		row := rows[s.cursor]
		switch {
		case row.msg != nil:
			return s, intent(row.msg)
		case s.editable && row.kind != rowReadOnly:
			return s.startEdit(row), nil
		}
		return s, intent(toastMsg{text: fmt.Sprintf("set %s in %s and start the app again", row.key, s.file)})
	}
	return s, nil
}

func (s settingsModel) startEdit(row settingRow) settingsModel {
	s.editing, s.editKey, s.choices = true, row.key, nil
	if row.kind == rowChoice {
		s.choices, s.choice = row.choices, max(slices.Index(row.choices, row.edit), 0)
		return s
	}
	s.input.SetValue(row.edit)
	s.input.CursorEnd()
	s.input.Focus()
	return s
}

// editBindings are the bar's keys while a row is open.
func (s settingsModel) editBindings() []key.Binding {
	save := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	if s.choices != nil {
		return []key.Binding{s.keys.Pick, save, s.keys.CancelEdit}
	}
	return []key.Binding{save, s.keys.CancelEdit}
}

func (s settingsModel) endEdit() settingsModel {
	s.editing, s.choices = false, nil
	s.input.Blur()
	return s
}

// updateEdit has the keys while a row is open: enter saves, esc cancels, left and right pick on a choice row, everything else types into the field.
func (s settingsModel) updateEdit(msg tea.KeyMsg) (settingsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Back):
		return s.endEdit(), nil
	case key.Matches(msg, s.keys.Enter):
		value, err := s.editedValue()
		if err != nil {
			return s, intent(toastMsg{text: err.Error(), isErr: true})
		}
		return s, intent(setConfigMsg{key: s.editKey, value: value})
	case s.choices != nil:
		switch {
		case key.Matches(msg, s.keys.Right), msg.Type == tea.KeySpace:
			s.choice = (s.choice + 1) % len(s.choices)
		case key.Matches(msg, s.keys.Left):
			s.choice = (s.choice + len(s.choices) - 1) % len(s.choices)
		}
		return s, nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

// editedValue is what the hook gets for the open row, typed the way the config package takes it.
// The poll interval travels as the text typed, with the unit added to bare digits, so the file reads as the user wrote it.
func (s settingsModel) editedValue() (any, error) {
	if s.choices != nil {
		picked := s.choices[s.choice]
		if s.editKey == "ui.ascii" || s.editKey == "offline" {
			return picked == "true", nil
		}
		return picked, nil
	}
	text := strings.TrimSpace(s.input.Value())
	switch s.editKey {
	case "poll_interval":
		if _, err := strconv.Atoi(text); err == nil {
			text += "s"
		}
		if _, err := time.ParseDuration(text); err != nil {
			return nil, errors.New("poll_interval needs a duration such as 60s or 2m")
		}
		return text, nil
	case "ui.hide_prefixes":
		return strings.Fields(text), nil
	}
	return text, nil
}

func (s settingsModel) View(th Theme, width, height int) string {
	accent := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	// On a wide terminal the key names would otherwise sit far to the right of the values they belong to.
	full := width
	width = min(width, 100)
	var lines []string
	group := ""
	for i, row := range s.rows() {
		if row.group != group {
			if group != "" {
				lines = append(lines, "")
			}
			group = row.group
			lines = append(lines, accent.Render(group))
		}
		hint, value := row.key, row.value
		switch {
		case hint == "":
			hint = "enter to change"
		case s.editing && i == s.cursor && s.choices != nil:
			hint, value = "left and right pick, enter saves, esc cancels", s.choices[s.choice]
		case s.editing && i == s.cursor:
			hint, value = "enter saves, esc cancels", s.input.View()
		}
		line := fmt.Sprintf("%-30s %s", row.label, value)
		line = ansi.Truncate(line, max(width-2-len(hint)-2, 10), "...")
		line += strings.Repeat(" ", max(width-2-ansi.StringWidth(line)-len(hint), 1)) + muted.Render(hint)
		if i == s.cursor {
			line = accent.Render(th.Glyphs.Cursor+" ") + line
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	footer := "Config file: " + s.file + ", read when the app starts."
	if s.editable {
		footer = "Config file: " + s.file + ", enter on a row edits it."
	}
	lines = append(lines, "", muted.Render(ansi.Truncate(footer, full, "...")))
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	return strings.Join(lines, "\n")
}
