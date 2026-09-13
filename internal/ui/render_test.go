package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestUnderlineMarksArmAfterEveryReset(t *testing.T) {
	in := "x " + underlineOn + "a \x1b[1mb\x1b[0m c\x1b[0m\n  \x1b[2md" + underlineOff + " e"
	want := "x \x1b[4ma \x1b[1mb\x1b[0m\x1b[4m c\x1b[0m\n  \x1b[2m\x1b[4md\x1b[24m e"
	if got := underlineMarks(in, false); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := underlineMarks("a "+underlineOn+"b"+underlineOff+" c", true); got != "a _b_ c" {
		t.Errorf("plain: got %q", got)
	}
}

func TestRenderDescriptionShowsStrikeUnderlineAndBoxes(t *testing.T) {
	src := `<p>keep <u>under</u> and <s>gone</s></p>` +
		`<ul class="checklist" style="list-style-type: none;"><li><label><input type="checkbox" checked="checked" />done</label></li><li><label><input type="checkbox" />open</label></li></ul>`
	out := renderDescription(src, "", 60, "dark")
	if !strings.Contains(out, "\x1b[4munder") || !strings.Contains(out, ";9mgone") {
		t.Errorf("dark render lacks the underline or the crossed out attribute:\n%q", out)
	}
	plain := ansi.Strip(out)
	// The dark style ticks a box with a check mark, so the ticked one is matched by its text.
	for _, want := range []string{"keep under and gone", "] done", "[ ] open"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered description lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(plain, underlineOn+underlineOff+"~<>") {
		t.Errorf("markers or markup leaked into the render:\n%q", out)
	}
	// The ascii style marks instead of styling, the way it does for bold.
	ascii := ansi.Strip(renderDescription(src, "", 60, "ascii"))
	for _, want := range []string{"keep _under_ and ~~gone~~", "[x] done"} {
		if !strings.Contains(ascii, want) {
			t.Errorf("ascii render lacks %q:\n%s", want, ascii)
		}
	}
}
