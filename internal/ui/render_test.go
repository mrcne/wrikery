package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderDescriptionConvertsAndWraps(t *testing.T) {
	html := `<h2>Steps</h2><ol><li>Generate the new key pair</li><li>Deploy</li></ol><pre><code>make rotate</code></pre><p>` + strings.Repeat("word ", 30) + `</p>`
	out := renderDescription(html, "", 40, "dark")
	for _, want := range []string{"Steps", "1. Generate", "make rotate"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line wider than 40 (%d): %q", w, line)
		}
	}
}

func TestRenderDescriptionFallsBackToPlain(t *testing.T) {
	if out := renderDescription("", "plain text", 40, "dark"); !strings.Contains(out, "plain text") {
		t.Errorf("empty html should show the plain text: %q", out)
	}
}

// The ascii mode is for terminals that cannot draw the theme glyphs.
// glamour's own ascii style still prefixes list items with a bullet, so the substitution is worth pinning.
func TestRenderDescriptionStaysASCII(t *testing.T) {
	out := renderDescription(`<p>Steps</p><ul><li>stop after the first 401</li></ul>`, "", 40, "ascii")
	for _, r := range out {
		if r > 127 {
			t.Fatalf("ascii mode rendered %q:\n%s", r, out)
		}
	}
}
