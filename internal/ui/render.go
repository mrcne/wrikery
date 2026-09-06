package ui

import (
	"strings"
	"sync"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// renderDescription turns Wrike's HTML into terminal text: html-to-markdown, then glamour at the pane width.
// plain is the store's stripped text, used when the HTML is empty or the conversion fails.
// An empty description renders as an empty string, so the caller can leave the block out instead of drawing a blank box.
func renderDescription(html, plain string, width int, mode string) string {
	out := renderMarkup(html, plain, width, mode)
	if mode == "ascii" {
		// ASCII mode is a promise about every character on screen, and neither glamour nor Wrike's own text keeps it.
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
	md, err := htmltomarkdown.ConvertString(html)
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
		style = glamour.WithStyles(cfg)
	}
	// glamour colors unconditionally, at true color, while the rest of the UI goes through lipgloss.
	// Handing it lipgloss's profile leaves color as one decision for the whole frame.
	return glamour.NewTermRenderer(style, glamour.WithWordWrap(width), glamour.WithColorProfile(lipgloss.ColorProfile()))
}

// asciiFallback maps the box drawing and punctuation glamour and lipgloss reach for onto plain equivalents.
// The runes are escapes because this package keeps its own source ASCII.
// In order: the two rules, the four corners, the five junctions, the bullet and the right arrow.
var asciiFallback = map[rune]rune{
	'\u2500': '-', '\u2502': '|',
	'\u250c': '+', '\u2510': '+', '\u2514': '+', '\u2518': '+',
	'\u253c': '+', '\u251c': '+', '\u2524': '+', '\u252c': '+', '\u2534': '+',
	'\u2022': '*', '\u2192': '-',
}

// toASCII is the invariant behind the ascii setting, whatever a style or a description body turns out to contain.
// A character with no sensible stand-in becomes a question mark, which is what a terminal without the glyph would show anyway.
func toASCII(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 128:
			return r
		case asciiFallback[r] != 0:
			return asciiFallback[r]
		}
		return '?'
	}, s)
}

// lipgloss breaks on word boundaries, which is what the plain text fallback wants.
func wordWrap(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}
