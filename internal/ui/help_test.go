package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"

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
