package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type statusModel struct {
	state        string // idle, syncing, offline, auth_required
	lastSynced   time.Time
	offlineSince time.Time
	pending      int
	failed       int
	toast        string
	toastErr     bool
	toastSeq     int
	demo         bool
}

type toastExpiredMsg struct{ seq int }

// show replaces the key hints with text for three seconds.
// The sequence number keeps an old timer from clearing a newer toast.
func (s *statusModel) show(text string, isErr bool) tea.Cmd {
	s.toastSeq++
	s.toast, s.toastErr = text, isErr
	seq := s.toastSeq
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} })
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d d ago", int(d.Hours()/24))
}

func (s statusModel) View(th Theme, width int, hints string, now time.Time) string {
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	var left strings.Builder
	if s.demo {
		left.WriteString(lipgloss.NewStyle().Foreground(th.Warn).Bold(true).Render("demo") + "  ")
	}
	switch s.state {
	case "syncing":
		left.WriteString(lipgloss.NewStyle().Foreground(th.Accent).Render(th.Glyphs.Syncing + " syncing"))
	case "offline":
		left.WriteString(lipgloss.NewStyle().Foreground(th.Warn).Render(th.Glyphs.Offline + " offline since " + s.offlineSince.Format("15:04")))
	case "auth_required":
		left.WriteString(lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed + " token rejected"))
	default:
		when := "never"
		if !s.lastSynced.IsZero() {
			when = ago(now.Sub(s.lastSynced))
		}
		left.WriteString(lipgloss.NewStyle().Foreground(th.Success).Render(th.Glyphs.Synced) + muted.Render(" synced "+when))
	}
	if s.pending > 0 {
		left.WriteString(muted.Render(fmt.Sprintf("  %d pending", s.pending)))
	}
	if s.failed > 0 {
		left.WriteString(lipgloss.NewStyle().Foreground(th.Error).Render(fmt.Sprintf("  %d failed", s.failed)))
	}
	// What is left after the state, the leading space and a one cell gap. A hint cut in half reads as a glitch, so hints are all or nothing.
	leftText := left.String()
	available := width - lipgloss.Width(leftText) - 3
	right := ""
	switch {
	case s.toast != "" && available > 0:
		style := lipgloss.NewStyle().Foreground(th.Text)
		if s.toastErr {
			style = style.Foreground(th.Error)
		}
		// A toast answers something the user just did, so it is cut down rather than dropped.
		right = style.Render(ansi.Truncate(s.toast, available, "..."))
	case s.toast == "" && hints != "" && lipgloss.Width(hints) <= available:
		right = muted.Render(hints)
	}
	gap := width - lipgloss.Width(leftText) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	line := " " + leftText + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().MaxWidth(width).Render(line) + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
}
