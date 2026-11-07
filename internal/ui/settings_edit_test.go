package ui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
)

// editModel is the root model on the settings screen with a hook that answers every save with answer.
func editModel(t *testing.T, hook func(key string, value any) (config.Config, error)) Model {
	t.Helper()
	opts := rootTestOptions(t)
	opts.Hooks.SetConfig = hook
	opts.AutoTheme = "light"
	opts.Config = config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "dark", ASCII: true, BranchTemplate: "{id}-{slug}"}}
	m := New(opts)
	m.width, m.height = 120, 40
	m.screen = screenSettings
	return m
}

func answering(cfg config.Config) func(string, any) (config.Config, error) {
	return func(string, any) (config.Config, error) { return cfg, nil }
}

func keyAt(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

// saveThrough runs enter's command to the hook and the hook's answer back into the model, the toast's timer is not run.
func saveThrough(m Model, cmd tea.Cmd) Model {
	for _, msg := range collect(cmd) {
		next, c := m.Update(msg)
		m = next.(Model)
		for _, answer := range collect(c) {
			next, _ = m.Update(answer)
			m = next.(Model)
		}
	}
	return m
}

func TestSavingAThemeRebuildsTheThemeAtOnce(t *testing.T) {
	var gotKey string
	var gotValue any
	saved := config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "light", Accent: "red", ASCII: true, BranchTemplate: "{id}-{slug}"}}
	m := editModel(t, func(key string, value any) (config.Config, error) {
		gotKey, gotValue = key, value
		return saved, nil
	})
	m.settings = rowAt(t, m.settings, "Theme")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyRight})
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if gotKey != "ui.theme" || gotValue != "light" {
		t.Errorf("the hook got %q = %#v, want ui.theme = light", gotKey, gotValue)
	}
	if m.theme.mode != "light" || m.theme.Accent != lipgloss.Color("1") {
		t.Errorf("theme after the save: mode %q accent %v, want light and the red ansi color", m.theme.mode, m.theme.Accent)
	}
	if m.settings.theme != "light" || m.settings.editing {
		t.Errorf("settings after the save: theme %q editing %v", m.settings.theme, m.settings.editing)
	}
	if !strings.Contains(m.status.toast, "ui.theme saved") || m.status.toastErr {
		t.Errorf("toast = %q, want ui.theme saved", m.status.toast)
	}
}

func TestSavingAutoThemeUsesTheDetectedBackground(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "auto", ASCII: true}}))
	m.settings = rowAt(t, m.settings, "Theme")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyLeft})
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if m.theme.mode != "light" || m.opts.Config.UI.Theme != "light" {
		t.Errorf("auto resolved to %q in the theme and %q in the config, want the detected light", m.theme.mode, m.opts.Config.UI.Theme)
	}
	rows := m.settings.rows()
	i := slices.IndexFunc(rows, func(r settingRow) bool { return r.label == "Theme" })
	if rows[i].value != "auto, light now" {
		t.Errorf("Theme row = %q, want auto, light now", rows[i].value)
	}
}

func TestSavingPrefixesReachesTheSidebarAndTheBoard(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "dark", ASCII: true, HidePrefixes: []string{"(MX)"}}}))
	m.settings = rowAt(t, m.settings, "Hidden title prefixes")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.settings = withText(m.settings, "(MX)")
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	want := []string{"(MX)"}
	if !reflect.DeepEqual(m.theme.HidePrefixes, want) || !reflect.DeepEqual(m.sidebar.hide, want) {
		t.Errorf("prefixes after the save: theme %v sidebar %v, want %v in both", m.theme.HidePrefixes, m.sidebar.hide, want)
	}
}

func TestSavingGlyphsSwitchesTheHelpSeparators(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "dark", ASCII: false}}))
	m.settings = rowAt(t, m.settings, "ASCII glyphs")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyRight})
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if m.theme.ASCII || m.help.Ellipsis == "..." {
		t.Errorf("after glyphs off: theme ASCII %v, help ellipsis %q, want the unicode glyphs back", m.theme.ASCII, m.help.Ellipsis)
	}
	back := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "dark", ASCII: true}}))
	back.opts.Config.UI.ASCII = false
	back.settings = rowAt(t, back.settings, "ASCII glyphs")
	back, _ = keyAt(back, tea.KeyMsg{Type: tea.KeyEnter})
	back, cmd = keyAt(back, tea.KeyMsg{Type: tea.KeyEnter})
	back = saveThrough(back, cmd)
	if !back.theme.ASCII || back.help.Ellipsis != "..." || back.help.ShortSeparator != "  " {
		t.Errorf("after glyphs on: theme ASCII %v, ellipsis %q, separator %q", back.theme.ASCII, back.help.Ellipsis, back.help.ShortSeparator)
	}
}

func TestAFailedSaveKeepsTheEdit(t *testing.T) {
	m := editModel(t, func(string, any) (config.Config, error) {
		return config.Config{}, errors.New("reading /x/config.toml: poll_interval 5s is below 10s")
	})
	m.opts.ConfigFile, m.settings.file = "/x/config.toml", "/x/config.toml"
	m.settings = rowAt(t, m.settings, "Check Wrike every")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.settings = withText(m.settings, "5s")
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if !m.settings.editing || m.settings.input.Value() != "5s" {
		t.Errorf("after the refusal editing = %v, field = %q, want the edit kept", m.settings.editing, m.settings.input.Value())
	}
	// The bar is narrow and the footer names the file already, so the loader's "reading <file>: " goes.
	if strings.TrimSpace(m.status.toast) != "poll_interval 5s is below 10s" || !m.status.toastErr {
		t.Errorf("toast = %q (err %v), want the loader's words without the file as an error", m.status.toast, m.status.toastErr)
	}
	// The refusal frees the row for the next try.
	m.settings = withText(m.settings, "30s")
	if _, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter}); len(collect(cmd)) != 1 {
		t.Error("enter after a refused save sent nothing, the row is still waiting on the first save")
	}
}

func TestSaveToastsSayWhatHappensNext(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", Offline: true, UI: config.UIConfig{Theme: "dark", ASCII: true}}))
	m.settings = rowAt(t, m.settings, "Start offline")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyRight})
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if !strings.Contains(m.status.toast, "offline saved, read at the next start") {
		t.Errorf("toast = %q", m.status.toast)
	}
	m = editModel(t, answering(config.Config{PollInterval: 30 * time.Second, LogLevel: "info", UI: config.UIConfig{Theme: "dark", ASCII: true}}))
	m.settings = rowAt(t, m.settings, "Check Wrike every")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.settings = withText(m.settings, "30s")
	m, cmd = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if !strings.Contains(m.status.toast, "poll_interval saved, next check in 30s") {
		t.Errorf("toast = %q", m.status.toast)
	}
}

// The screen's own keys, the settings key and quit among them, are letters while a field is open.
func TestKeysDuringAnEditStayOnTheRow(t *testing.T) {
	m := editModel(t, answering(config.Config{}))
	m.settings = rowAt(t, m.settings, "Accent")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range ",q" {
		var cmd tea.Cmd
		m, cmd = keyAt(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil && reflect.ValueOf(cmd).Pointer() == reflect.ValueOf(tea.Quit).Pointer() {
			t.Fatalf("%q quit the program during an edit", r)
		}
	}
	if m.screen != screenSettings || !m.settings.editing || !strings.HasSuffix(m.settings.input.Value(), ",q") {
		t.Errorf("after typing: screen %v editing %v field %q, want the letters in the field", m.screen, m.settings.editing, m.settings.input.Value())
	}
}

func TestWithoutTheHookTheFileRowsAreReadOnly(t *testing.T) {
	m := editModel(t, nil)
	m.settings = rowAt(t, m.settings, "Check Wrike every")
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	toast, ok := msgs[0].(toastMsg)
	if len(msgs) != 1 || !ok || !strings.Contains(toast.text, "set poll_interval in") || m.settings.editing {
		t.Errorf("enter without the hook: %#v, editing %v", msgs, m.settings.editing)
	}
}

// The screen can change under an open row, the sign in screen on a 401 or a control socket command, and the row must not stay open behind it.
func TestLeavingTheSettingsScreenClosesAnOpenRow(t *testing.T) {
	m := editModel(t, answering(config.Config{}))
	m.settings = rowAt(t, m.settings, "Accent")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.openScreen(screenMain)
	if m.settings.editing {
		t.Error("the row is still open after the screen changed")
	}
}

// No engine runs in offline mode, so the toast cannot promise a check in thirty seconds.
func TestPollIntervalToastInOfflineModePromisesNoCheck(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: 30 * time.Second, LogLevel: "info", UI: config.UIConfig{Theme: "dark", ASCII: true}}))
	m.opts.Offline, m.status.state = true, "offline_mode"
	m.settings = rowAt(t, m.settings, "Check Wrike every")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.settings = withText(m.settings, "30s")
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if !strings.Contains(m.status.toast, "poll_interval saved, applies to the next check") || strings.Contains(m.status.toast, "30s") {
		t.Errorf("toast = %q", m.status.toast)
	}
}

// The detail pane and the sidebar are laid out with the theme and the prefixes, so a save lays the panes out again.
func TestSavingLaysThePanesOutAgain(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "light", ASCII: true}}))
	if m.list.height != 0 {
		t.Fatalf("the list already has a height of %d before any layout", m.list.height)
	}
	m.settings = rowAt(t, m.settings, "Theme")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyRight})
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if m.list.height == 0 {
		t.Error("the panes were not laid out again after the save")
	}
}

// The interface gets the config as the file has it and resolves auto itself from what the start detected.
func TestNewResolvesAutoFromTheOptions(t *testing.T) {
	opts := rootTestOptions(t)
	opts.Config.UI.Theme, opts.AutoTheme = "auto", "light"
	m := New(opts)
	if m.theme.mode != "light" || m.opts.Config.UI.Theme != "light" {
		t.Errorf("theme mode %q, config theme %q, want the detected light in both", m.theme.mode, m.opts.Config.UI.Theme)
	}
	rows := m.settings.rows()
	i := slices.IndexFunc(rows, func(r settingRow) bool { return r.label == "Theme" })
	if rows[i].value != "auto, light now" {
		t.Errorf("Theme row = %q, want auto, light now", rows[i].value)
	}
}

// A start with a fixed theme never asked the terminal, so auto picked later keeps the current colors until the next start and says so.
func TestSavingAutoWithoutADetectedBackgroundWaitsForTheNextStart(t *testing.T) {
	m := editModel(t, answering(config.Config{PollInterval: time.Minute, LogLevel: "info", UI: config.UIConfig{Theme: "auto", ASCII: true}}))
	m.opts.AutoTheme = ""
	m.settings = rowAt(t, m.settings, "Theme")
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = keyAt(m, tea.KeyMsg{Type: tea.KeyLeft})
	m, cmd := keyAt(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = saveThrough(m, cmd)
	if m.theme.mode != "dark" || m.settings.theme != "auto" {
		t.Errorf("theme mode %q, settings theme %q, want the dark colors kept and auto on the row", m.theme.mode, m.settings.theme)
	}
	if !strings.Contains(m.status.toast, "auto applies at the next start") {
		t.Errorf("toast = %q", m.status.toast)
	}
}
