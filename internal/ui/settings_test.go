package ui

import (
	"reflect"
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
