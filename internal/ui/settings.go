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

type openScopesMsg struct{} // intent: open the box that picks the followed spaces and projects

type settingRow struct {
	group, label, value string
	key                 string // the key in the config file, empty for the one row the app owns
}

// settingsModel is the settings screen: what the app owns and can change, the followed scopes, next to what the config file owns.
// The file rows are read only, the app never writes the file, so enter on one names the key to edit.
type settingsModel struct {
	cfg    config.Config
	theme  string // ui.theme as the file has it, cfg.UI.Theme is what auto resolved to
	host   string // the host the token probe found, empty until the meta row is read or when there is none
	file   string
	keys   KeyMap
	scopes []store.Scope
	cursor int
}

func newSettings(cfg config.Config, theme, file string, keys KeyMap) settingsModel {
	return settingsModel{cfg: cfg, theme: theme, file: file, keys: keys}
}

func (s *settingsModel) setScopes(scopes []store.Scope) { s.scopes = scopes }

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
	return []settingRow{
		{group: "Sync", label: "Followed spaces and projects", value: orText(strings.Join(names, ", "), "none")},
		{group: "Sync", label: "Check Wrike every", value: fmt.Sprintf("%ds", int(s.cfg.PollInterval.Seconds())), key: "poll_interval"},
		{group: "Sync", label: "Wrike host", value: host, key: "host"},
		{group: "Interface", label: "Theme", value: theme, key: "ui.theme"},
		{group: "Interface", label: "Accent", value: orText(ui.Accent, "default"), key: "ui.accent"},
		{group: "Interface", label: "ASCII glyphs", value: fmt.Sprint(ui.ASCII), key: "ui.ascii"},
		{group: "Interface", label: "Hidden title prefixes", value: orText(strings.Join(ui.HidePrefixes, " "), "none"), key: "ui.hide_prefixes"},
		{group: "Interface", label: "Branch name template", value: ui.BranchTemplate, key: "ui.branch_template"},
		{group: "Logging", label: "Log level", value: s.cfg.LogLevel, key: "log_level"},
	}
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
		if row.key == "" {
			return s, intent(openScopesMsg{})
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
