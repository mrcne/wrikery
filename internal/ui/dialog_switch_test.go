package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

var moveToEU = &SwitchNeeded{From: "Acme on www.wrike.com", To: "Acme on app-eu.wrike.com", Pending: 5, Failed: 2, DBFile: "/home/ada/.local/share/wrikery/wrike.db"}

func TestSwitchBoxSaysWhatGoesWhere(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	view := squeeze(ansi.Strip(switchDialog{sw: moveToEU}.View(th, 120, 30)))
	for _, want := range []string{"5 queued and 2 failed writes", "belong to Acme on", "www.wrike.com.", "This sign in is for Acme on", "app-eu.wrike.com.",
		"Is this the new home of this cache?", "y sends the queued writes there"} {
		if !strings.Contains(view, want) {
			t.Errorf("box lacks %q:\n%s", want, view)
		}
	}
}

func TestSwitchBoxNamesTheFileOnNo(t *testing.T) {
	want := "This cache belongs to Acme on www.wrike.com. Sign in to that account, or move /home/ada/.local/share/wrikery/wrike.db away and start wrikery again for a fresh cache, the queued writes go with the file, x on the changes screen drops one first."
	if got := moveToEU.Declined(); got != want {
		t.Errorf("notice = %q", got)
	}
}

func TestAnotherAccountStateOpensTheBoxOnR(t *testing.T) {
	opts := rootTestOptions(t)
	asked := 0
	opts.Hooks.SwitchHome = func() (*SwitchNeeded, error) { asked++; return moveToEU, nil }
	m := New(opts)
	m.width, m.height = 120, 40
	next, _ := m.Update(SyncStateMsg{State: "another_account"})
	m = next.(Model)
	if bar := ansi.Strip(m.status.View(m.theme, 120, "", m.opts.Now())); !strings.Contains(bar, "another account, R decides") {
		t.Errorf("bar = %q", bar)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = next.(Model)
	for _, msg := range collect(cmd) {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if _, ok := m.dialog.(switchDialog); !ok || asked != 1 {
		t.Errorf("dialog = %T, asked %d times, want the switch box after one ask", m.dialog, asked)
	}
}

// The followed spaces of an account with new ids are not in the new folder tree, the user picks them again.
func TestARebuildThatLostTheFollowedSetOpensTheFollowBox(t *testing.T) {
	m := New(rootTestOptions(t))
	m.width, m.height = 120, 40
	m.settings.setScopes([]store.Scope{
		{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true},
		{ID: "F1", Kind: store.ScopeKindProject, Title: "Alpha", Followed: true},
	})
	m.switched = true
	next, _ := m.Update(StoreChangedMsg{Entities: []string{"folders"}})
	m = next.(Model)
	next, cmd := m.Update(treeLoadedMsg{nodes: []treeNode{{id: "F2", title: "Beta", kind: nodeProject}}})
	m = next.(Model)
	var opened, told bool
	for _, msg := range collect(cmd) {
		switch msg := msg.(type) {
		case openScopesMsg:
			opened = true
		case toastMsg:
			told = told || msg.text == "the followed spaces and projects are not in this account, pick them again"
		}
	}
	if !opened || !told || m.switched {
		t.Errorf("opened %v, told %v, still watching %v", opened, told, m.switched)
	}
}
