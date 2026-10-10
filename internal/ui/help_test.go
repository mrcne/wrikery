package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
)

func TestHelpViewListsGroupedBindings(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	k := defaultKeyMap()
	out := helpView(th, help.New(), [][]key.Binding{k.global(), k.list()}, 60, "wrikery dev")
	for _, want := range []string{"wrikery dev", "quit", "up", "esc or ? to close"} {
		if !strings.Contains(out, want) {
			t.Errorf("help view %q lacks %q", out, want)
		}
	}
}

// The overlay has to fit a 24 row terminal, the classic default, with the list keys as the tallest column.
func TestHelpOverlayFitsTwentyFourRows(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	for _, sh := range []shape{shapeList, shapeBoard} {
		m := Model{keys: defaultKeyMap(), shape: sh, help: help.New()}
		out := helpView(th, m.help, m.helpGroups(), 100, "wrikery test")
		if h := lipgloss.Height(out); h > 24 {
			t.Errorf("help overlay is %d lines for shape %d, more than 24", h, sh)
		}
		if !strings.Contains(out, "f F") || !strings.Contains(out, "F clears it") {
			t.Errorf("the overlay should show f and F on one row:\n%s", out)
		}
	}
}
