package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

// benchTasks is the size of the generated cache under the query benchmarks, large enough for the costs to show and small enough to seed in seconds.
// The numbers scale with the size, a change is judged against a run of the same size on the branch before it.
const benchTasks = 10000

func extendedStore(b *testing.B, now time.Time) *store.Store {
	b.Helper()
	st, err := store.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := demo.Seed(ctx, st, now); err != nil {
		b.Fatal(err)
	}
	if err := demo.Extend(ctx, st, now, benchTasks); err != nil {
		b.Fatal(err)
	}
	return st
}

// BenchmarkQueries runs the reads the interface and the commands make at a large cache, over one seeded store.
// A name after the slash picks one, go test ./internal/store -run xxx -bench Queries/FindByTitle -benchmem.
func BenchmarkQueries(b *testing.B) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	st := extendedStore(b, now)
	ctx := context.Background()
	since := now.AddDate(0, 0, -store.InboxDays).UTC().Format(time.RFC3339)
	marker, err := st.GetMeta(ctx, store.MetaKeyInboxSeenTo)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("ListInFolder", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := st.Tasks().ListInFolder(ctx, demo.FolderEverything)
			if err != nil || len(rows) != 5000 {
				b.Fatal(err, len(rows))
			}
		}
	})
	b.Run("ListForResponsible", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := st.Tasks().ListForResponsible(ctx, demo.MeID)
			if err != nil || len(rows) < benchTasks/10 {
				b.Fatal(err, len(rows))
			}
		}
	})
	b.Run("CountOpenForResponsible", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if n, err := st.Tasks().CountOpenForResponsible(ctx, demo.MeID); err != nil || n == 0 {
				b.Fatal(err, n)
			}
		}
	})
	b.Run("FindByTitle", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := st.Tasks().FindByTitle(ctx, "retry loop 99", 3)
			if err != nil || len(rows) == 0 {
				b.Fatal(err, len(rows))
			}
		}
	})
	// A command names one task, so the usual lookup has one hit and reads every title.
	b.Run("FindByTitleUnique", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := st.Tasks().FindByTitle(ctx, "error codes 9999", 3)
			if err != nil || len(rows) != 1 {
				b.Fatal(err, len(rows))
			}
		}
	})
	b.Run("Search", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := st.Tasks().Search(ctx, "retr", 30)
			if err != nil || len(rows) != 30 {
				b.Fatal(err, len(rows))
			}
		}
	})
	b.Run("Inbox", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			rows, err := st.Comments().Inbox(ctx, demo.MeID, since, 200)
			if err != nil || len(rows) == 0 {
				b.Fatal(err, len(rows))
			}
		}
	})
	b.Run("InboxUnread", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := st.Comments().InboxUnread(ctx, demo.MeID, since, marker); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// benchCommentsPerTask gives the ten thousand generated tasks two hundred thousand comments over three years,
// with ten discussions of two thousand comments, the reads a long history costs.
const benchCommentsPerTask = 20

// BenchmarkComments runs the comment reads and writes of the interface and the sync over a long history.
// A name after the slash picks one, go test ./internal/store -run xxx -bench Comments/InboxUnread -benchmem.
func BenchmarkComments(b *testing.B) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	st := extendedStore(b, now)
	ctx := context.Background()
	if err := demo.ExtendThreads(ctx, st, now, benchTasks, benchCommentsPerTask); err != nil {
		b.Fatal(err)
	}
	since := now.AddDate(0, 0, -store.InboxDays).UTC().Format(time.RFC3339)
	marker, err := st.GetMeta(ctx, store.MetaKeyInboxSeenTo)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Inbox", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if rows, err := st.Comments().Inbox(ctx, demo.MeID, since, 200); err != nil || len(rows) == 0 {
				b.Fatal(err, len(rows))
			}
		}
	})
	// The count after every sync cycle that changed a comment, with the marker the inbox left behind.
	b.Run("InboxUnread", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := st.Comments().InboxUnread(ctx, demo.MeID, since, marker); err != nil {
				b.Fatal(err)
			}
		}
	})
	// The count of a user who never opened the inbox, every row of the window.
	b.Run("InboxUnreadNoMarker", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := st.Comments().InboxUnread(ctx, demo.MeID, since, ""); err != nil {
				b.Fatal(err)
			}
		}
	})
	thread, err := st.Comments().ListForTask(ctx, demo.LongThread)
	if err != nil || len(thread) < 100*benchCommentsPerTask {
		b.Fatal(err, len(thread))
	}
	b.Run("LongThreadRead", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := st.Comments().ListForTask(ctx, demo.LongThread); err != nil {
				b.Fatal(err)
			}
		}
	})
	// The refresh of an open task whose thread did not change, what a thread read stores.
	b.Run("LongThreadRefresh", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := st.Comments().ReplaceForTask(ctx, demo.LongThread, thread); err != nil {
				b.Fatal(err)
			}
		}
	})
	// One step of the account wide pull at its limit, a thousand new comments on cached tasks.
	step := make([]store.Comment, 1000)
	for j := range step {
		step[j] = store.Comment{ID: fmt.Sprintf("BENCHNEW%04d", j), TaskID: fmt.Sprintf("IEAAGEN%06d", j*7), AuthorID: "KUGEN001",
			Text: "A new comment from the pull.", CreatedDate: now.Add(-time.Duration(j) * time.Minute).Format(time.RFC3339)}
	}
	b.Run("PullStep", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := st.Comments().UpsertCached(ctx, step); err != nil {
				b.Fatal(err)
			}
		}
	})
}
