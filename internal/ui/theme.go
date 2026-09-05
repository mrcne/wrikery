package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

type Theme struct {
	Accent, Text, Muted, Border, Error, Warn, Success lipgloss.TerminalColor
	ASCII                                             bool
	Glyphs                                            Glyphs
	mode                                              string
}

type Glyphs struct {
	Active, Completed, Deferred, Cancelled string
	Expanded, Collapsed                    string
	Pending, Failed                        string
	Synced, Syncing, Offline               string
	Cursor                                 string
}

// The only two lines in the package allowed to hold non ASCII characters.
var unicodeGlyphs = Glyphs{Active: "○", Completed: "✓", Deferred: "◌", Cancelled: "✕", Expanded: "▾", Collapsed: "▸", Pending: "~", Failed: "!", Synced: "●", Syncing: "◐", Offline: "○", Cursor: ">"}
var asciiGlyphs = Glyphs{Active: "o", Completed: "v", Deferred: "z", Cancelled: "x", Expanded: "v", Collapsed: ">", Pending: "~", Failed: "!", Synced: "*", Syncing: "~", Offline: "o", Cursor: ">"}

var asciiBorder = lipgloss.Border{Top: "-", Bottom: "-", Left: "|", Right: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+"}

type palette struct{ light, dark string }

var basePalette = map[string]palette{
	"accent":  {"#2f6fdb", "#7aa2f7"},
	"text":    {"#1f2328", "#e6e6e6"},
	"muted":   {"#6e7781", "#8b8b8b"},
	"border":  {"#c8ccd0", "#3b4048"},
	"error":   {"#c62828", "#f7768e"},
	"warn":    {"#b26a00", "#e0af68"},
	"success": {"#1b7f3b", "#9ece6a"},
}

// Wrike status color names, from the customStatuses color field. Unknown names fall back to the group.
var wrikeColors = map[string]palette{
	"Blue": {"#2f6fdb", "#7aa2f7"}, "DarkBlue": {"#1e3a8a", "#5a7bd6"}, "Indigo": {"#4b3fbf", "#9d8cff"},
	"Turquoise": {"#0e8a8a", "#4fd1c5"}, "DarkCyan": {"#0b6e6e", "#3aa7a7"}, "Green": {"#1b7f3b", "#9ece6a"},
	"YellowGreen": {"#5e8f1a", "#b8d55c"}, "Yellow": {"#b26a00", "#e0af68"}, "Orange": {"#c2410c", "#ff9e64"},
	"Red": {"#c62828", "#f7768e"}, "Pink": {"#b3286e", "#f59ad0"}, "Purple": {"#7c3aed", "#bb9af7"},
	"Violet": {"#6d28d9", "#a78bfa"}, "Brown": {"#7a4b1f", "#c48a5a"}, "Gray": {"#6e7781", "#8b8b8b"},
}

var ansiNames = map[string]string{"black": "0", "red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7"}

// fromPalette resolves a palette entry against the configured theme mode, and falls back to an adaptive color when the mode is unset (auto).
func (t Theme) fromPalette(p palette) lipgloss.TerminalColor {
	switch t.mode {
	case "dark":
		return lipgloss.Color(p.dark)
	case "light":
		return lipgloss.Color(p.light)
	}
	return lipgloss.AdaptiveColor{Light: p.light, Dark: p.dark}
}

func NewTheme(cfg config.UIConfig) Theme {
	t := Theme{mode: cfg.Theme, ASCII: cfg.ASCII, Glyphs: unicodeGlyphs}
	if cfg.ASCII {
		t.Glyphs = asciiGlyphs
	}
	t.Accent = t.fromPalette(basePalette["accent"])
	t.Text = t.fromPalette(basePalette["text"])
	t.Muted = t.fromPalette(basePalette["muted"])
	t.Border = t.fromPalette(basePalette["border"])
	t.Error = t.fromPalette(basePalette["error"])
	t.Warn = t.fromPalette(basePalette["warn"])
	t.Success = t.fromPalette(basePalette["success"])
	if cfg.Accent != "" {
		if code, ok := ansiNames[strings.ToLower(cfg.Accent)]; ok {
			t.Accent = lipgloss.Color(code)
		} else {
			t.Accent = lipgloss.Color(cfg.Accent)
		}
	}
	return t
}

func (t Theme) StatusColor(cs store.CustomStatus) lipgloss.TerminalColor {
	if p, ok := wrikeColors[cs.Color]; ok {
		return t.fromPalette(p)
	}
	switch cs.Group {
	case "Completed":
		return t.Success
	case "Deferred":
		return t.Warn
	case "Cancelled":
		return t.Muted
	}
	return t.Accent
}

func (t Theme) StatusGlyph(group string) string {
	switch group {
	case "Completed":
		return t.Glyphs.Completed
	case "Deferred":
		return t.Glyphs.Deferred
	case "Cancelled":
		return t.Glyphs.Cancelled
	}
	return t.Glyphs.Active
}

func (t Theme) border() lipgloss.Border {
	if t.ASCII {
		return asciiBorder
	}
	return lipgloss.RoundedBorder()
}

// box draws a pane: rounded border, the title in the top edge, exact width and height.
// lipgloss v1 has no border titles, so the top line is assembled by hand and the style draws the other three sides.
func (t Theme) box(title, body string, width, height int, focused bool) string {
	if width < 4 || height < 2 {
		return ""
	}
	b := t.border()
	color := t.Border
	if focused {
		color = t.Accent
	}
	edge := lipgloss.NewStyle().Foreground(color)
	titleStyle := lipgloss.NewStyle().Foreground(t.Muted)
	if focused {
		titleStyle = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	}
	inner := width - 2
	title = ansi.Truncate(title, inner-4, "...")
	rest := inner - ansi.StringWidth(title) - 3
	if rest < 0 {
		rest = 0
	}
	top := edge.Render(b.TopLeft+b.Top+" ") + titleStyle.Render(title) + edge.Render(" "+strings.Repeat(b.Top, rest)+b.TopRight)

	// The body is padded or trimmed by hand: lipgloss's Height and MaxHeight alone did not pin the exact line count the box tests require for every input shape.
	lines := strings.Split(body, "\n")
	bodyHeight := height - 2
	if len(lines) > bodyHeight {
		lines = lines[:bodyHeight]
	}
	for len(lines) < bodyHeight {
		lines = append(lines, "")
	}
	content := lipgloss.NewStyle().Width(inner).MaxWidth(inner).Render(strings.Join(lines, "\n"))
	sides := lipgloss.NewStyle().
		Border(b, false, true, true, true).BorderForeground(color).
		Render(content)
	return top + "\n" + sides
}
