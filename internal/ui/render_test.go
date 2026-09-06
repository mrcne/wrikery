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

// The ascii mode is for fonts without box drawing, bullets and arrows, and glamour reaches for all three.
func TestRenderDescriptionStaysASCII(t *testing.T) {
	html := `<p>Steps</p><ul><li>stop after the first 401</li></ul>` +
		`<table><tr><th>Code</th><th>Meaning</th></tr><tr><td>429</td><td>rate limit</td></tr></table>` +
		`<p><img alt="diagram" src="x"></p>`
	out := renderDescription(html, "", 40, "ascii")
	drawing := []rune{
		'\u2500', '\u2502',
		'\u250c', '\u2510', '\u2514', '\u2518',
		'\u253c', '\u251c', '\u2524', '\u252c', '\u2534',
		'\u2022', '\u2192',
	}
	for _, r := range drawing {
		if strings.ContainsRune(out, r) {
			t.Errorf("ascii mode drew %q:\n%s", r, out)
		}
	}
}

// A description is text from Wrike, not decoration, so the ascii setting has no business rewriting it.
func TestRenderDescriptionKeepsLetters(t *testing.T) {
	const city = "\u0142\u00f3d\u017a"
	out := renderDescription("<p>Deploy in "+city+" on Friday</p>", "", 40, "ascii")
	if !strings.Contains(out, city) {
		t.Errorf("ascii mode should leave the words alone:\n%s", out)
	}
}
