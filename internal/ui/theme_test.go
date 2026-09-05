package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestBoxHasTitleInTopBorderAndExactSize(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	out := th.box("Tasks", "one\ntwo", 20, 6, true)
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("height = %d, want 6:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "+- Tasks ") || !strings.HasSuffix(lines[0], "+") {
		t.Errorf("top line = %q", lines[0])
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 20 {
			t.Errorf("line %d width = %d, want 20: %q", i, w, l)
		}
	}
}

func TestBoxTruncatesLongTitle(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	top := strings.Split(th.box(strings.Repeat("x", 50), "", 12, 3, false), "\n")[0]
	if lipgloss.Width(top) != 12 {
		t.Errorf("top = %q", top)
	}
}

func TestStatusColorFallsBackToGroup(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark"})
	if th.StatusColor(store.CustomStatus{Color: "NoSuchColor", Group: "Completed"}) != th.Success {
		t.Error("unknown color did not fall back to the group color")
	}
	if th.StatusGlyph("Cancelled") != th.Glyphs.Cancelled || th.StatusGlyph("whatever") != th.Glyphs.Active {
		t.Error("glyph mapping")
	}
}
