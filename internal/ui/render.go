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
