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

func TestSettingsEnterOpensThePickerOnTheFollowedRowAndNamesTheKeyOnAFileRow(t *testing.T) {
	s := testSettings()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msgs := collect(cmd); !reflect.DeepEqual(msgs, []tea.Msg{openScopesMsg{}}) {
		t.Errorf("enter on the followed row sent %#v", msgs)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter on a file row sent %#v", msgs)
	}
	toast, ok := msgs[0].(toastMsg)
	if !ok || !strings.Contains(toast.text, "poll_interval") || !strings.Contains(toast.text, "/home/ada/.config/wrikery/config.toml") {
		t.Errorf("enter on the poll interval row should name the key and the file, got %#v", msgs[0])
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
