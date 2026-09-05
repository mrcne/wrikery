package ui

import "testing"

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
