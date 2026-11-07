package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func testSettings() settingsModel {
	cfg := config.Config{
		LogLevel:     "info",
		PollInterval: 90 * time.Second,
		UI:           config.UIConfig{Theme: "dark", ASCII: true, BranchTemplate: "{id}-{slug}", HidePrefixes: []string{"(MX)"}},
	}
	s := newSettings(cfg, "dark", "/home/ada/.config/wrikery/config.toml", defaultKeyMap())
	s.editable = true
	s.setScopes([]store.Scope{
		{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true},
		{ID: "MOB", Kind: store.ScopeKindSpace, Title: "Mobile", Followed: true},
		{ID: "PLT", Kind: store.ScopeKindSpace, Title: "Platform", Followed: true},
	})
	return s
}

func TestSettingsScreenShowsTheFollowedScopesAndTheFileValues(t *testing.T) {
	s := testSettings()
	view := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 100, 30)
	for _, want := range []string{"Mobile, Platform", "90s", "(MX)", "{id}-{slug}", "/home/ada/.config/wrikery/config.toml"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings screen should show %q:\n%s", want, view)
		}
	}
	// My tasks is always followed and never a choice, so it has no place in the followed row.
	if strings.Contains(view, "My tasks") {
		t.Errorf("the followed row should not list My tasks:\n%s", view)
	}
}

func TestSettingsEnterOpensThePickerOnTheFollowedRow(t *testing.T) {
	s := testSettings()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{openScopesMsg{}}) {
		t.Errorf("enter on the followed row sent %#v", msgs)
	}
}

// Without a hook that writes the file, the demo, a file row still names its key and the file.
func TestSettingsEnterOnAFileRowNamesTheKeyWhenNothingCanWriteTheFile(t *testing.T) {
	s := testSettings()
	s.editable = false
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter on a file row sent %#v", msgs)
	}
	toast, ok := msgs[0].(toastMsg)
	if !ok || !strings.Contains(toast.text, "poll_interval") || !strings.Contains(toast.text, "/home/ada/.config/wrikery/config.toml") {
		t.Errorf("enter on the poll interval row should name the key and the file, got %#v", msgs[0])
	}
}

// The data center is set at the sign in, so enter on the host row says where to change it instead of naming a key to edit.
func TestSettingsEnterOnTheHostRowSaysTheSignInSetsIt(t *testing.T) {
	s := testSettings()
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter on the host row sent %#v", msgs)
	}
	toast, ok := msgs[0].(toastMsg)
	if !ok || !strings.Contains(toast.text, "wrikery --logout") || !strings.Contains(toast.text, "/home/ada/.config/wrikery/config.toml") {
		t.Errorf("enter on the host row should say a new sign in or the config key changes it, got %#v", msgs[0])
	}
}

// With host in the config file a new sign in checks that host alone, so only the file changes it and the toast says so.
func TestSettingsHostRowNamesTheFileAloneWhenTheConfigSetsTheHost(t *testing.T) {
	s := testSettings()
	s.cfg.Host = "app-eu.wrike.com"
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	toast, ok := msgs[0].(toastMsg)
	if len(msgs) != 1 || !ok || strings.Contains(toast.text, "--logout") || !strings.Contains(toast.text, "/home/ada/.config/wrikery/config.toml") {
		t.Errorf("enter on the host row with a config host should name the file alone, got %#v", msgs)
	}
}

// The rows show what the file holds next to its key, so the user can read a row and know what to write.
func TestSettingsRowsShowTheFileValuesNotTheResolvedOnes(t *testing.T) {
	s := testSettings()
	s.theme, s.host = "auto", "app-eu.wrike.com"
	view := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 100, 30)
	for _, want := range []string{"auto, dark now", "app-eu.wrike.com (detected)", "ui.ascii", "true"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings screen should show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "ascii ") && !strings.Contains(view, "true") {
		t.Errorf("ui.ascii is a boolean in the file:\n%s", view)
	}
}

// The sidebar toggle is app owned state like the followed scopes, so the settings screen shows it and enter flips it.
func TestSettingsSidebarRowTogglesIt(t *testing.T) {
	s := testSettings()
	s.setSidebarHidden(true)
	rows := s.rows()
	i := slices.IndexFunc(rows, func(r settingRow) bool { return r.label == "Sidebar" })
	if i < 0 || rows[i].value != "hidden" {
		t.Fatalf("rows = %+v, want a Sidebar row reading hidden", rows)
	}
	s.cursor = i
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter on the Sidebar row emitted %#v, want one message", msgs)
	}
	if _, ok := msgs[0].(toggleSidebarMsg); !ok {
		t.Errorf("enter on the Sidebar row emitted %#v, want toggleSidebarMsg", msgs[0])
	}
	s.setSidebarHidden(false)
	if rows := s.rows(); rows[i].value != "shown" {
		t.Errorf("Sidebar row = %q, want shown", rows[i].value)
	}
}

func TestSettingsShowsTheSignInRow(t *testing.T) {
	s := testSettings()
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	if view := s.View(th, 100, 30); strings.Contains(view, "Signed in with") {
		t.Errorf("a row for a sign in that did not happen:\n%s", view)
	}
	// The app comes from the session, the config may have lost its table since the sign in.
	s.setSignedIn(SignedInWithBrowser, "id1")
	if view := s.View(th, 100, 30); !strings.Contains(view, "Signed in with") || !strings.Contains(view, "the browser, app id1") {
		t.Errorf("the browser sign in row is missing:\n%s", view)
	}
	s.setSignedIn(SignedInWithToken, "")
	if view := s.View(th, 100, 30); !strings.Contains(view, "a permanent token") {
		t.Errorf("the token row is missing:\n%s", view)
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	toast, ok := msgs[0].(toastMsg)
	if !ok || !strings.Contains(toast.text, "--logout") {
		t.Errorf("enter on the sign in row should say how to change it, got %#v", msgs)
	}
}

func TestSettingsRowNamesTheBudgetSource(t *testing.T) {
	fields := []store.CustomField{{ID: "CF1", Title: "Budget", Type: "Duration"}, {ID: "CF2", Title: "Notes", Type: "Text"}}
	for name, tc := range map[string]struct {
		cfg    config.BudgetConfig
		effort bool
		want   string
	}{
		"none":       {config.BudgetConfig{}, false, "not set"},
		"effort":     {config.BudgetConfig{Source: "effort"}, false, "Effort field"},
		"no effort":  {config.BudgetConfig{Source: "effort"}, true, "Effort field, not available on this account"},
		"field":      {config.BudgetConfig{Source: "field", Field: "Budget"}, false, `custom field "Budget" (Duration)`},
		"missing":    {config.BudgetConfig{Source: "field", Field: "Nope"}, false, `custom field "Nope", not found on this account`},
		"wrong type": {config.BudgetConfig{Source: "field", Field: "Notes"}, false, `custom field "Notes" is a Text field, a Duration or Numeric field is needed`},
	} {
		if got := budgetRowText(tc.cfg, fields, tc.effort); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
	s := newSettings(config.Config{Budget: config.BudgetConfig{Source: "effort"}}, "dark", "/tmp/config.toml", defaultKeyMap())
	s.budget = budgetRowText(s.cfg.Budget, nil, false)
	found := false
	for _, r := range s.rows() {
		if r.label == "Budget" && r.value == "Effort field" && r.key == "budget.source" && r.group == "Sync" {
			found = true
		}
	}
	if !found {
		t.Errorf("rows lack the Budget row: %+v", s.rows())
	}
}

// rowAt moves the cursor to the row with the label.
func rowAt(t *testing.T, s settingsModel, label string) settingsModel {
	t.Helper()
	i := slices.IndexFunc(s.rows(), func(r settingRow) bool { return r.label == label })
	if i < 0 {
		t.Fatalf("no row %q in %+v", label, s.rows())
	}
	s.cursor = i
	return s
}

func withText(s settingsModel, text string) settingsModel {
	s.input.SetValue(text)
	return s
}

func TestSettingsEnterOnATextRowOpensAFieldWithTheValue(t *testing.T) {
	s := rowAt(t, testSettings(), "Check Wrike every")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.editing || s.input.Value() != "90s" {
		t.Fatalf("editing = %v, field = %q, want the row's value in a field", s.editing, s.input.Value())
	}
	if view := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 100, 30); !strings.Contains(view, "enter saves, esc cancels") {
		t.Errorf("the edited row should say how to leave:\n%s", view)
	}
}

func TestSettingsEnterSavesTheTypedDuration(t *testing.T) {
	s := rowAt(t, testSettings(), "Check Wrike every")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := withText(s, "2m").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "poll_interval", value: "2m"}}) {
		t.Errorf("enter sent %#v, want the key and the typed duration", msgs)
	}
}

func TestSettingsBareDigitsAreSeconds(t *testing.T) {
	s := rowAt(t, testSettings(), "Check Wrike every")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := withText(s, "90").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "poll_interval", value: "90s"}}) {
		t.Errorf("enter sent %#v, want 90s", msgs)
	}
}

func TestSettingsRefusesTextThatIsNotADuration(t *testing.T) {
	s := rowAt(t, testSettings(), "Check Wrike every")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s, cmd := withText(s, "soon").Update(tea.KeyMsg{Type: tea.KeyEnter})
	want := []tea.Msg{toastMsg{text: "poll_interval needs a duration such as 60s or 2m", isErr: true}}
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, want) {
		t.Errorf("enter sent %#v, want the duration toast", msgs)
	}
	if !s.editing {
		t.Error("a refused value should leave the row in edit mode")
	}
}

func TestSettingsEscRestoresTheValue(t *testing.T) {
	s := rowAt(t, testSettings(), "Check Wrike every")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s, cmd := withText(s, "2m").Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.editing || cmd != nil {
		t.Errorf("esc left editing = %v, cmd = %v", s.editing, cmd)
	}
	if rows := s.rows(); rows[s.cursor].value != "90s" {
		t.Errorf("row value after esc = %q, want 90s", rows[s.cursor].value)
	}
}

func TestSettingsChoiceRowCyclesAndWraps(t *testing.T) {
	s := rowAt(t, testSettings(), "Theme")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	step := func(k tea.KeyMsg, want string) {
		t.Helper()
		s, _ = s.Update(k)
		if got := s.choices[s.choice]; got != want {
			t.Errorf("after %v the choice is %q, want %q", k, got, want)
		}
	}
	if got := s.choices[s.choice]; got != "dark" {
		t.Fatalf("the edit opens on %q, want the file's dark", got)
	}
	step(tea.KeyMsg{Type: tea.KeyRight}, "light")
	step(tea.KeyMsg{Type: tea.KeyRight}, "auto")
	step(tea.KeyMsg{Type: tea.KeyRight}, "dark")
	step(tea.KeyMsg{Type: tea.KeyLeft}, "auto")
	step(tea.KeyMsg{Type: tea.KeySpace}, "dark")
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}, "light")
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")}, "dark")
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "dark")
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}, "dark")
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "ui.theme", value: "dark"}}) {
		t.Errorf("enter sent %#v", msgs)
	}
}

func TestSettingsPrefixesAreTypedWithSpaces(t *testing.T) {
	s := rowAt(t, testSettings(), "Hidden title prefixes")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.input.Value() != "(MX)" {
		t.Fatalf("the field starts with %q, want (MX)", s.input.Value())
	}
	_, cmd := withText(s, "(MX) (TX)").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "ui.hide_prefixes", value: []string{"(MX)", "(TX)"}}}) {
		t.Errorf("enter sent %#v", msgs)
	}
	_, cmd = withText(s, "").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "ui.hide_prefixes", value: []string{}}}) {
		t.Errorf("an empty field sent %#v, want an empty list", msgs)
	}
}

func TestSettingsBoolRowsAreChoices(t *testing.T) {
	s := rowAt(t, testSettings(), "ASCII glyphs")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := s.choices[s.choice]; got != "true" {
		t.Fatalf("the glyph edit opens on %q, want true", got)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "ui.ascii", value: false}}) {
		t.Errorf("enter sent %#v, want ui.ascii false", msgs)
	}
	s = rowAt(t, testSettings(), "Start offline")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{setConfigMsg{key: "offline", value: true}}) {
		t.Errorf("enter sent %#v, want offline true", msgs)
	}
}

// The data center is set at the sign in and the budget is two keys with a sync behind them, both rows keep their toasts.
func TestSettingsHostAndBudgetRowsStayReadOnly(t *testing.T) {
	s := rowAt(t, testSettings(), "Wrike host")
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if toast, ok := msgs[0].(toastMsg); len(msgs) != 1 || !ok || !strings.Contains(toast.text, "--logout") || s.editing {
		t.Errorf("enter on the host row: %#v, editing %v", msgs, s.editing)
	}
	s = rowAt(t, testSettings(), "Budget")
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs = collect(cmd)
	if toast, ok := msgs[0].(toastMsg); len(msgs) != 1 || !ok || !strings.Contains(toast.text, "budget.source") || s.editing {
		t.Errorf("enter on the budget row: %#v, editing %v", msgs, s.editing)
	}
}

func TestSettingsFooterSaysEnterEdits(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	s := testSettings()
	if view := s.View(th, 100, 30); !strings.Contains(view, "enter on a row edits it.") {
		t.Errorf("the footer should say enter edits:\n%s", view)
	}
	s.editable = false
	if view := s.View(th, 100, 30); !strings.Contains(view, "read when the app starts.") {
		t.Errorf("without the hook the footer should say the file is read at start:\n%s", view)
	}
}

// On a narrow terminal the field takes the room the row has, so the cursor and the end of a long value stay on screen.
func TestSettingsFieldFitsTheRowOnANarrowTerminal(t *testing.T) {
	s := testSettings()
	s.cfg.UI.HidePrefixes = []string{"(MX)", "(TX)", "(AX)", "(BX)", "(CX)", "(DX)", "(EX)", "(FX)", "(GX)"}
	s = rowAt(t, s, "Hidden title prefixes")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := s.View(NewTheme(config.UIConfig{Theme: "dark", ASCII: true}), 80, 30); !strings.Contains(view, "(GX)") {
		t.Errorf("the end of the field, where the cursor is, is off screen at 80 columns:\n%s", view)
	}
}

// Two enters in a row must not start two writes of the file, the second waits for the first answer.
func TestSettingsSecondEnterWhileSavingSendsNothing(t *testing.T) {
	s := rowAt(t, testSettings(), "Check Wrike every")
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s, cmd := withText(s, "2m").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); len(msgs) != 1 {
		t.Fatalf("the first enter sent %#v", msgs)
	}
	s, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); msgs != nil || !s.editing {
		t.Errorf("the second enter sent %#v with editing %v, want nothing while the save runs", msgs, s.editing)
	}
}
