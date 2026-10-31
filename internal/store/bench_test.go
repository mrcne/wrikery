package store_test

import (
	"context"
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
