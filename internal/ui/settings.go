package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

type openScopesMsg struct{}    // intent: open the box that picks the followed spaces and projects
type toggleSidebarMsg struct{} // intent: hide the sidebar pane or show it again, what ctrl+b does
type toggleMoveAskMsg struct{} // intent: ask again before H and L move a task, or stop asking

type settingRow struct {
	group, label, value string
	key                 string  // the key in the config file, empty for a row the app owns
	msg                 tea.Msg // what enter on a row the app owns sends
}

// settingsModel is the settings screen: what the app owns and can change, the followed scopes and the sidebar pane, next to what the config file owns.
// The file rows are read only, the app never writes the file, so enter on one names the key to edit.
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
}

func newSettings(cfg config.Config, theme, file string, keys KeyMap) settingsModel {
	return settingsModel{cfg: cfg, theme: theme, file: file, keys: keys, budget: budgetRowText(cfg.Budget, nil, false)}
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
		candidates := store.BudgetCandidates(fields, cfg.Field)
		if len(candidates) == 0 {
			return fmt.Sprintf("custom field %q, not found on this account", cfg.Field)
		}
		for _, f := range candidates {
			if store.IsBudgetType(f) {
				return fmt.Sprintf("custom field %q (%s)", cfg.Field, f.Type)
			}
		}
		return fmt.Sprintf("custom field %q is a %s field, a Duration or Numeric field is needed", cfg.Field, candidates[0].Type)
	}
	return "not set"
}

func (s *settingsModel) setScopes(scopes []store.Scope) { s.scopes = scopes }

func (s *settingsModel) setSidebarHidden(hidden bool) { s.sidebarHidden = hidden }

func (s *settingsModel) setMoveConfirmed(confirmed bool) { s.moveConfirmed = confirmed }

func (s *settingsModel) setSignedIn(with, app string) { s.signedIn, s.signedInApp = with, app }

func (s settingsModel) rows() []settingRow {
	var names []string
	for _, sc := range s.scopes {
		if sc.Kind != store.ScopeKindMe {
			names = append(names, sc.Title)
		}
	}
	ui := s.cfg.UI
	// The rows show the file's values next to their keys, so a user can read a row and know what to write.
	theme := ui.Theme
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
		{group: "Sync", label: "Check Wrike every", value: fmt.Sprintf("%ds", int(s.cfg.PollInterval.Seconds())), key: "poll_interval"},
		{group: "Sync", label: "Wrike host", value: host, key: "host"},
		{group: "Sync", label: "Start offline", value: fmt.Sprint(s.cfg.Offline), key: "offline"},
		{group: "Sync", label: "Budget", value: s.budget, key: "budget.source"},
		{group: "Interface", label: "Sidebar", value: sidebar, msg: toggleSidebarMsg{}},
		{group: "Interface", label: "Ask before H and L", value: ask, msg: toggleMoveAskMsg{}},
		{group: "Interface", label: "Theme", value: theme, key: "ui.theme"},
		{group: "Interface", label: "Accent", value: orText(ui.Accent, "default"), key: "ui.accent"},
		{group: "Interface", label: "ASCII glyphs", value: fmt.Sprint(ui.ASCII), key: "ui.ascii"},
		{group: "Interface", label: "Hidden title prefixes", value: orText(strings.Join(ui.HidePrefixes, " "), "none"), key: "ui.hide_prefixes"},
		{group: "Interface", label: "Branch name template", value: ui.BranchTemplate, key: "ui.branch_template"},
		{group: "Logging", label: "Log level", value: s.cfg.LogLevel, key: "log_level"},
	}...)
}

func orText(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (s settingsModel) Update(msg tea.KeyMsg) (settingsModel, tea.Cmd) {
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
		if row.msg != nil {
			return s, intent(row.msg)
		}
		return s, intent(toastMsg{text: fmt.Sprintf("set %s in %s and start the app again", row.key, s.file)})
	}
	return s, nil
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
		hint := row.key
		if hint == "" {
			hint = "enter to change"
		}
		line := fmt.Sprintf("%-30s %s", row.label, row.value)
		line = ansi.Truncate(line, max(width-2-len(hint)-2, 10), "...")
		line += strings.Repeat(" ", max(width-2-ansi.StringWidth(line)-len(hint), 1)) + muted.Render(hint)
		if i == s.cursor {
			line = accent.Render(th.Glyphs.Cursor+" ") + line
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", muted.Render(ansi.Truncate("Config file: "+s.file+", read when the app starts.", full, "...")))
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	return strings.Join(lines, "\n")
}
