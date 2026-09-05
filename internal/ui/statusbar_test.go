package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/config"
)

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{12 * time.Second: "12 s ago", 5 * time.Minute: "5 min ago", 3 * time.Hour: "3 h ago", 49 * time.Hour: "2 d ago"} {
		if got := ago(d); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestStatusBarShowsStateCountsAndToast(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s := statusModel{state: "idle", lastSynced: now.Add(-12 * time.Second), pending: 2, failed: 1}
	out := s.View(th, 80, "? help", now)
	for _, want := range []string{"synced 12 s ago", "2 pending", "1 failed", "? help"} {
		if !strings.Contains(out, want) {
			t.Errorf("status bar %q lacks %q", out, want)
		}
	}
	s.state = "offline"
	s.offlineSince = now.Add(-time.Hour)
	if out := s.View(th, 80, "", now); !strings.Contains(out, "offline since 11:00") {
		t.Errorf("offline bar = %q", out)
	}
	_ = s.show("Comment queued", false)
	if out := s.View(th, 80, "? help", now); !strings.Contains(out, "Comment queued") || strings.Contains(out, "? help") {
		t.Errorf("toast should replace hints: %q", out)
	}
	if lipgloss.Width(s.View(th, 80, "", now)) != 80 {
		t.Error("status bar does not fill the width")
	}
}

func TestStatusBarDropsHintsAndTruncatesToastWhenNarrow(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	hints := "tab next pane  ? help  q quit"
	s := statusModel{state: "idle", lastSynced: now.Add(-12 * time.Second), pending: 2, failed: 1}

	out := s.View(th, 40, hints, now)
	if w := lipgloss.Width(out); w != 40 {
		t.Fatalf("width = %d, want 40: %q", w, out)
	}
	if strings.Contains(out, "help") && !strings.Contains(out, "? help") {
		t.Errorf("a hint was cut mid word: %q", out)
	}
	if strings.Contains(out, "? h") && !strings.Contains(out, "? help") {
		t.Errorf("a hint was cut mid word: %q", out)
	}

	// A toast answers something the user just did, so it is cut down rather than dropped.
	quiet := statusModel{state: "idle", lastSynced: now.Add(-12 * time.Second)}
	_ = quiet.show("the comment was queued and will be sent when the connection is back", false)
	out = quiet.View(th, 40, hints, now)
	if w := lipgloss.Width(out); w != 40 {
		t.Fatalf("toast width = %d, want 40: %q", w, out)
	}
	if !strings.Contains(out, "...") || !strings.Contains(out, "the comment") {
		t.Errorf("a toast too long for the bar should be truncated, not dropped: %q", out)
	}
}
