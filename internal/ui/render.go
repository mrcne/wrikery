package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderDescription turns Wrike's HTML into terminal text: html-to-markdown, then glamour at the pane width.
// plain is the store's stripped text, used when the HTML is empty or the conversion fails.
// An empty description renders as an empty string, so the caller can leave the block out instead of drawing a blank box.
func renderDescription(html, plain string, width int, mode string) string {
	out := renderMarkup(html, plain, width, mode)
	if mode == "ascii" {
		// glamour's ascii style still draws a bullet and an arrow of its own, and a description body can carry drawing characters too.
		out = toASCII(out)
	}
	return out
}

func renderMarkup(html, plain string, width int, mode string) string {
	if strings.TrimSpace(html) == "" {
		if strings.TrimSpace(plain) == "" {
			return ""
		}
		return wordWrap(plain, width)
	}
	md, err := newDisplayConverter().ConvertString(html)
	if err != nil {
		return wordWrap(plain, width)
	}
	r, err := newRenderer(width, mode)
	if err != nil {
		return wordWrap(plain, width)
	}
	out, err := r.Render(md)
	if err != nil {
		return wordWrap(plain, width)
	}
	out = underlineMarks(out)
	return strings.Trim(out, "\n")
}

var rendererMu sync.Mutex

// newRenderer builds a glamour renderer for the width.
// Next to the theme modes it takes ascii, for a terminal that cannot draw what the other styles use.
// glamour keeps no global state, but its style detection queries the terminal in auto mode, so the calls are serialized.
func newRenderer(width int, mode string) (*glamour.TermRenderer, error) {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	style := glamour.WithAutoStyle()
	switch mode {
	case "dark", "light":
		style = glamour.WithStandardStyle(mode)
	case "ascii":
		cfg := styles.ASCIIStyleConfig
		// glamour's ascii style is not quite ascii: list items carry a bullet and the image format ends in an arrow.
		cfg.Item.BlockPrefix = "- "
		cfg.ImageText.Format = "Image: {{.text}} ->"
		// It also writes struck text between tildes, the crossed out attribute is what the other styles use and it needs no glyph.
		crossed := true
		cfg.Strikethrough.BlockPrefix, cfg.Strikethrough.BlockSuffix, cfg.Strikethrough.CrossedOut = "", "", &crossed
		style = glamour.WithStyles(cfg)
	}
	// glamour colors unconditionally, at true color, while the rest of the UI goes through lipgloss.
	// Handing it lipgloss's profile leaves color as one decision for the whole frame.
	return glamour.NewTermRenderer(style, glamour.WithWordWrap(width), glamour.WithColorProfile(lipgloss.ColorProfile()))
}

// asciiDrawing swaps the drawing characters glamour and lipgloss reach for, and touches nothing else.
// The runes are escapes because this package keeps its own source ASCII.
// In order: the two rules, the four corners and the five junctions,
// then the bullet, the right arrow, the ellipsis, the curly quotes and the long dashes.
var asciiDrawing = strings.NewReplacer(
	"\u2500", "-", "\u2502", "|",
	"\u250c", "+", "\u2510", "+", "\u2514", "+", "\u2518", "+",
	"\u253c", "+", "\u251c", "+", "\u2524", "+", "\u252c", "+", "\u2534", "+",
	"\u2022", "*", "\u2192", "-", "\u2026", "...",
	"\u2018", "'", "\u2019", "'", "\u201c", `"`, "\u201d", `"`,
	"\u2013", "-", "\u2014", "-",
)

// toASCII replaces the symbols a font without box drawing cannot show, and leaves every letter where it is.
// The ascii setting is about the frame, a description written in Polish still has to read as Polish.
func toASCII(s string) string { return asciiDrawing.Replace(s) }

// lipgloss breaks on word boundaries, which is what the plain text fallback wants.
func wordWrap(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}

// divider draws a section line: two dashes, the title, dashes to the width, in the muted color.
func divider(th Theme, title string, width int) string {
	return lipgloss.NewStyle().Foreground(th.Muted).Render("-- " + title + " " + strings.Repeat("-", max(0, width-ansi.StringWidth(title)-4)))
}

// Markdown has no underline, so the display converter wraps underlined text in two private use characters that glamour passes through as text.
const underlineOn, underlineOff = "\uE000", "\uE001"

// underlineMarks turns the markers into the underline escape.
// It is on whatever the color profile says, the same as the bold and crossed out attributes glamour writes on its own.
// glamour styles every word on its own and resets after it, so the underline is armed again in front of each run of visible text up to the closing marker.
// That keeps a span underlined across bold words and wrapped lines, and leaves glamour's indent and padding spaces alone.
func underlineMarks(s string) string {
	var b strings.Builder
	in, armed := false, false
	for len(s) > 0 {
		i := strings.IndexAny(s, underlineOn+underlineOff+"\x1b")
		if i < 0 {
			i = len(s)
		}
		if text := s[:i]; text != "" {
			if in && !armed && strings.TrimSpace(text) != "" {
				b.WriteString("\x1b[4m")
				armed = true
			}
			b.WriteString(text)
		}
		s = s[i:]
		switch {
		case s == "":
		case strings.HasPrefix(s, underlineOn):
			in = true
			s = s[len(underlineOn):]
		case strings.HasPrefix(s, underlineOff):
			if armed {
				b.WriteString("\x1b[24m")
			}
			in, armed = false, false
			s = s[len(underlineOff):]
		default:
			n := escapeLen(s)
			seq := s[:n]
			b.WriteString(seq)
			if seq == "\x1b[0m" {
				armed = false
			}
			s = s[n:]
		}
	}
	return b.String()
}

// escapeLen is the length of the control sequence at the start of s: the CSI, its parameter bytes and one final byte.
// Anything else that starts with an escape is passed on one byte at a time.
func escapeLen(s string) int {
	if !strings.HasPrefix(s, "\x1b[") {
		return 1
	}
	for i := 2; i < len(s); i++ {
		if c := s[i]; c >= 0x40 && c <= 0x7e {
			return i + 1
		}
	}
	return len(s)
}
