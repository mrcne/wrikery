package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// BenchmarkPullComments is one sync cycle's comment read after a week away on a busy account:
// three thousand comments a week on ten thousand cached tasks, more than the read's limit,
// so the week is read again in halves until each answer fits, the comments already stored after the first round.
func BenchmarkPullComments(b *testing.B) {
	const perWeek = 3000
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	st, err := store.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := demo.Seed(ctx, st, now); err != nil {
		b.Fatal(err)
	}
	if err := demo.Extend(ctx, st, now, 10000); err != nil {
		b.Fatal(err)
	}
	// The fake answers the comments of a range at a steady rate, one every 201.6 seconds, cut at the limit like Wrike.
	every := 7 * 24 * time.Hour / perWeek
	reads := 0
	fc := &fakeClient{comments: func(p wrike.CommentParams) ([]wrike.Comment, error) {
		reads++
		var out []wrike.Comment
		first := p.CreatedFrom.Truncate(every)
		if first.Before(p.CreatedFrom) {
			first = first.Add(every)
		}
		for at := first; at.Before(p.CreatedTo) && len(out) < p.Limit; at = at.Add(every) {
			n := int(at.Unix() / int64(every/time.Second))
			out = append(out, wrike.Comment{ID: fmt.Sprintf("BUSY%010d", n), TaskID: fmt.Sprintf("IEAAGEN%06d", n%10000),
				AuthorID: "KUGEN001", Text: "<p>A comment of a busy week.</p>", CreatedDate: at})
		}
		return out, nil
	}}
	log := slog.New(slog.DiscardHandler)
	b.ReportAllocs()
	for b.Loop() {
		if err := st.SetMeta(ctx, store.MetaKeyCommentsPulledTo, rfc3339(now.Add(-7*24*time.Hour))); err != nil {
			b.Fatal(err)
		}
		if _, err := pullComments(ctx, fc, st, log, demo.MeID, 7*24*time.Hour, now); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(reads)/float64(b.N), "requests/op")
}
