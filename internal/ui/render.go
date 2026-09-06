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
func renderDescription(html, plain string, width int, mode string) string {
	if strings.TrimSpace(html) == "" {
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
		// glamour's ascii style prefixes list items with a bullet, the one character an ASCII terminal cannot draw.
		cfg.Item.BlockPrefix = "- "
		style = glamour.WithStyles(cfg)
	}
	// glamour colors unconditionally, at true color, while the rest of the UI goes through lipgloss.
	// Handing it lipgloss's profile leaves color as one decision for the whole frame.
	return glamour.NewTermRenderer(style, glamour.WithWordWrap(width), glamour.WithColorProfile(lipgloss.ColorProfile()))
}

// lipgloss breaks on word boundaries, which is what the plain text fallback wants.
func wordWrap(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}
