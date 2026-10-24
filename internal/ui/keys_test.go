package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultKeyMapGroupsCoverExpectedActions(t *testing.T) {
	k := defaultKeyMap()
	if len(k.global()) == 0 || len(k.list()) == 0 || len(k.task()) == 0 || len(k.timesheet()) == 0 || len(k.changes()) == 0 {
		t.Fatal("a key map group came back empty")
	}
	if h := k.Quit.Help(); h.Key != "q" || h.Desc != "quit" {
		t.Errorf("quit help = %+v, want q/quit", h)
	}
	if !k.Down.Enabled() {
		t.Error("down binding should be enabled by default")
	}
}

func TestDefaultKeyMapMatchesTheKeysItAdvertises(t *testing.T) {
	k := defaultKeyMap()
	for _, tc := range []struct {
		name string
		msg  tea.KeyMsg
		want key.Binding
	}{
		{"q quits", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, k.Quit},
		{"ctrl+c quits", tea.KeyMsg{Type: tea.KeyCtrlC}, k.Quit},
		{"? opens help", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}, k.Help},
	} {
		if !key.Matches(tc.msg, tc.want) {
			t.Errorf("%s: %v did not match", tc.name, tc.msg)
		}
	}
}

func TestKeyGroupsFollowTheOverlay(t *testing.T) {
	groups := KeyGroups()
	want := []string{"Everywhere", "The list", "The task", "The board", "The timesheet", "The changes screen", "The settings screen"}
	if len(groups) != len(want) {
		t.Fatalf("%d groups, want %d", len(groups), len(want))
	}
	has := func(g KeyGroup, key string) bool {
		for _, b := range g.Keys {
			if b.Help().Key == key {
				return true
			}
		}
		return false
	}
	for i, g := range groups {
		if g.Title != want[i] {
			t.Errorf("group %d is %q, want %q", i, g.Title, want[i])
		}
		if len(g.Keys) == 0 {
			t.Errorf("%s has no keys", g.Title)
		}
		for _, b := range g.Keys {
			if h := b.Help(); strings.TrimSpace(h.Key) == "" || strings.TrimSpace(h.Desc) == "" {
				t.Errorf("%s has a binding without help text: %+v", g.Title, h)
			}
		}
	}
	if !has(groups[0], "?") || !has(groups[0], "q") {
		t.Errorf("Everywhere lacks ? or q: %+v", groups[0])
	}
	if !has(groups[5], "r") || !has(groups[5], "x") {
		t.Errorf("the changes screen lacks r or x: %+v", groups[5])
	}
}
