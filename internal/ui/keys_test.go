package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultKeyMapGroupsCoverExpectedActions(t *testing.T) {
	k := defaultKeyMap()
	if len(k.global()) == 0 || len(k.list()) == 0 || len(k.task()) == 0 || len(k.timesheet()) == 0 || len(k.issues()) == 0 {
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
