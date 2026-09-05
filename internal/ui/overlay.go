package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// composite draws fg over bg at column x, row y. The cuts are ANSI aware so colors in the background survive.
func composite(bg, fg string, x, y int) string {
	bgLines := strings.Split(bg, "\n")
	for i, fl := range strings.Split(fg, "\n") {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		line := bgLines[row]
		fw := ansi.StringWidth(fl)
		left := ansi.Truncate(line, x, "")
		pad := x - ansi.StringWidth(left)
		if pad < 0 {
			pad = 0
		}
		right := ansi.TruncateLeft(line, x+fw, "")
		bgLines[row] = left + strings.Repeat(" ", pad) + fl + right
	}
	return strings.Join(bgLines, "\n")
}

func centered(bg, fg string, width, height int) string {
	x := (width - lipgloss.Width(fg)) / 2
	y := (height - lipgloss.Height(fg)) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return composite(bg, fg, x, y)
}
