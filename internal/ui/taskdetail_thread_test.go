package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// threadText is the whole pane as plain text, the height is large enough for every line of the thread.
func threadText(d *taskDetailModel, th Theme, ref refData, now time.Time, width int, offline bool) string {
	d.layout(th, ref, now, width, 5000, "dark", offline)
	return ansi.Strip(d.vp.View())
}

// Toasts, sync events and resizes lay the pane out again with the same thread, which must not wrap every comment again.
func TestDetailRedrawsAnUnchangedThreadWithoutWrappingItAgain(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark"})
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	comments := longThread(200, now)
	task := store.Task{ID: "T1", Title: "A task", Status: "Active", UpdatedDate: "2026-10-29T10:00:00Z", ThreadReadAt: "2026-10-29T10:00:00Z"}
	opened := testing.AllocsPerRun(5, func() {
		d := taskDetailModel{loaded: true, task: task, comments: comments}
		d.layout(th, refData{}, now, 60, 40, "dark", false)
	})
	d := taskDetailModel{loaded: true, task: task, comments: comments}
	d.layout(th, refData{}, now, 60, 40, "dark", false)
	redrawn := testing.AllocsPerRun(5, func() { d.layout(th, refData{}, now, 60, 40, "dark", false) })
	if redrawn > opened/4 {
		t.Errorf("a redraw allocates %v times, opening the thread %v, want a redraw to reuse what it drew", redrawn, opened)
	}
}

func TestDetailRedrawsWhatChangedInAThread(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark"})
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	d := taskDetailModel{loaded: true, comments: longThread(3, now),
		task: store.Task{ID: "T1", Title: "A task", Status: "Active", UpdatedDate: "2026-10-29T10:00:00Z", ThreadReadAt: "2026-10-29T10:00:00Z"}}
	before := threadText(&d, th, refData{}, now, 60, false)
	if !strings.Contains(before, "U01, 2 h ago") {
		t.Fatalf("the thread does not show the second comment's author and time:\n%s", before)
	}

	// A reload brings the edited text in a new slice, the way a load replaces the thread.
	edited := slices.Clone(d.comments)
	edited[1].Text = "Edited after the review."
	d.comments = edited
	if got := threadText(&d, th, refData{}, now, 60, false); !strings.Contains(got, "Edited after the review.") || strings.Contains(got, "Comment 1.") {
		t.Errorf("the edited comment is not drawn again:\n%s", got)
	}

	if got := threadText(&d, th, refData{}, now.Add(3*time.Hour), 60, false); !strings.Contains(got, "U01, 5 h ago") {
		t.Errorf("the comment's time does not move with the clock:\n%s", got)
	}

	ref := refData{contacts: map[string]store.Contact{"U01": {ID: "U01", FirstName: "Bartek", LastName: "Lis"}}}
	if got := threadText(&d, th, ref, now, 60, false); !strings.Contains(got, "Bartek Lis, 2 h ago") {
		t.Errorf("the author's name that arrived later is not drawn:\n%s", got)
	}

	for _, line := range strings.Split(threadText(&d, th, ref, now, 30, false), "\n") {
		if w := ansi.StringWidth(line); w > 30 {
			t.Errorf("a line is %d cells wide after the pane narrowed to 30: %q", w, line)
		}
	}
}

func TestDetailMarksAQueuedCommentByTheModeEveryRedraw(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark"})
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	d := taskDetailModel{loaded: true, comments: []store.Comment{{ID: store.LocalIDPrefix + "7", TaskID: "T1", AuthorID: "U1", Text: "Written offline."}},
		task: store.Task{ID: "T1", Title: "A task", Status: "Active", UpdatedDate: "2026-10-29T10:00:00Z", ThreadReadAt: "2026-10-29T10:00:00Z"}}
	if got := threadText(&d, th, refData{}, now, 60, false); !strings.Contains(got, "U1 (sending)") {
		t.Errorf("online the queued comment does not read sending:\n%s", got)
	}
	if got := threadText(&d, th, refData{}, now, 60, true); !strings.Contains(got, "U1 (queued)") {
		t.Errorf("offline the queued comment does not read queued:\n%s", got)
	}
}
