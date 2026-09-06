package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDialogEmitsOnYes(t *testing.T) {
	type payload struct{ n int }
	d := dialog(confirmDialog{prompt: "Sure?", onYes: payload{7}})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	msgs := collect(cmd)
	if len(msgs) != 2 || msgs[0] != (payload{7}) || msgs[1] != (closeDialogMsg{}) {
		t.Errorf("msgs = %#v", msgs)
	}
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if msgs := collect(cmd); len(msgs) != 1 || msgs[0] != (closeDialogMsg{}) {
		t.Errorf("n -> %#v", msgs)
	}
}

// collect runs a command tree and returns the messages it produces. tea.Batch returns a BatchMsg of commands.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
