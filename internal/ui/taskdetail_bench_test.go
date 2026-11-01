package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// longThread is n comments of one to four sentences, an hour apart, the newest an hour before now.
func longThread(n int, now time.Time) []store.Comment {
	sentence := "Checked the change on staging, the numbers look right but the export still rounds the hours. "
	out := make([]store.Comment, n)
	for j := range out {
		out[j] = store.Comment{ID: fmt.Sprintf("C%05d", j), TaskID: "T1", AuthorID: fmt.Sprintf("U%02d", j%50),
			Text:        fmt.Sprintf("Comment %d. %s", j, strings.TrimSpace(strings.Repeat(sentence, 1+j%4))),
			CreatedDate: now.Add(-time.Duration(n-j) * time.Hour).Format(time.RFC3339)}
	}
	return out
}

func threadDetail(n int, now time.Time) taskDetailModel {
	return taskDetailModel{loaded: true, comments: longThread(n, now),
		task: store.Task{ID: "T1", Title: "A task", Status: "Active", UpdatedDate: "2026-10-29T10:00:00Z", ThreadReadAt: "2026-10-29T10:00:00Z"}}
}

// BenchmarkThreadLayout is the detail pane laid out with a thread of each size:
// opened is the first layout of a task, redrawn a layout of the same task again, which toasts, sync events and resizes cause.
func BenchmarkThreadLayout(b *testing.B) {
	th := NewTheme(config.UIConfig{Theme: "dark"})
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, n := range []int{15, 200, 2000} {
		b.Run(fmt.Sprintf("opened/comments=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				d := threadDetail(n, now)
				d.layout(th, refData{}, now, 60, 40, "dark", false)
			}
		})
		b.Run(fmt.Sprintf("redrawn/comments=%d", n), func(b *testing.B) {
			d := threadDetail(n, now)
			d.layout(th, refData{}, now, 60, 40, "dark", false)
			b.ReportAllocs()
			for b.Loop() {
				d.layout(th, refData{}, now, 60, 40, "dark", false)
			}
		})
	}
}
