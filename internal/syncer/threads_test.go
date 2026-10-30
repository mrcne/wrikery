package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestPullCommentsWritesCachedTasksAndMovesTheCursor(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "cached")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	var asked []wrike.CommentParams
	fc := &fakeClient{comments: func(p wrike.CommentParams) ([]wrike.Comment, error) {
		asked = append(asked, p)
		return []wrike.Comment{
			{ID: "C1", TaskID: "T1", AuthorID: "U2", Text: "new on a cached task", CreatedDate: now.Add(-time.Hour)},
			{ID: "C2", TaskID: "T9", AuthorID: "U2", Text: "on a task the cache lacks", CreatedDate: now.Add(-time.Hour)},
			{ID: "C3", AuthorID: "U2", Text: "on a folder", CreatedDate: now.Add(-time.Hour)},
		}, nil
	}}
	changed, err := pullComments(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, now)
	if err != nil || !changed {
		t.Fatalf("pullComments = %v, %v, want a change", changed, err)
	}
	if len(asked) != 1 || !asked[0].CreatedFrom.Equal(now.Add(-7*24*time.Hour)) || !asked[0].CreatedTo.Equal(now) || asked[0].Limit != 1000 {
		t.Errorf("asked = %+v, want one read of the thread window up to now with the limit", asked)
	}
	if got, _ := st.Comments().ListForTask(ctx, "T1"); len(got) != 1 || got[0].ID != "C1" {
		t.Errorf("comments of T1 = %+v", got)
	}
	if cursor, _ := st.GetMeta(ctx, store.MetaKeyCommentsPulledTo); cursor != "2026-09-10T12:00:00Z" {
		t.Errorf("cursor = %q", cursor)
	}
	// The next cycle reads from ten minutes before the cursor, and nothing new is no change.
	asked = nil
	later := now.Add(time.Minute)
	changed, err = pullComments(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, later)
	if err != nil || changed {
		t.Errorf("second pull = %v, %v, want no change for the same rows", changed, err)
	}
	if len(asked) != 1 || !asked[0].CreatedFrom.Equal(now.Add(-10*time.Minute)) || !asked[0].CreatedTo.Equal(later) {
		t.Errorf("second read = %+v, want from ten minutes before the cursor", asked)
	}
}

func TestPullCommentsStepsThroughALongGapAndSplitsAFullWindow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "cached")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if err := st.SetMeta(ctx, store.MetaKeyCommentsPulledTo, rfc3339(now.Add(-20*24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	var asked []wrike.CommentParams
	fc := &fakeClient{comments: func(p wrike.CommentParams) ([]wrike.Comment, error) {
		asked = append(asked, p)
		// The first step answers a full page, the halves answer one comment each.
		if len(asked) == 1 {
			full := make([]wrike.Comment, 1000)
			for i := range full {
				full[i] = wrike.Comment{ID: fmt.Sprintf("F%d", i), TaskID: "T1", CreatedDate: p.CreatedFrom}
			}
			return full, nil
		}
		return []wrike.Comment{{ID: "S" + strconv.Itoa(len(asked)), TaskID: "T1", CreatedDate: p.CreatedFrom}}, nil
	}}
	if _, err := pullComments(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	// 20 days and 10 minutes in steps of 7 days is 3 steps, the full one is read again as 2 halves.
	if len(asked) != 5 {
		t.Fatalf("reads = %d, want 5:\n%+v", len(asked), asked)
	}
	for i, p := range asked {
		if p.CreatedTo.Sub(p.CreatedFrom) > 7*24*time.Hour {
			t.Errorf("read %d spans %s, more than the cap", i, p.CreatedTo.Sub(p.CreatedFrom))
		}
	}
	if !asked[0].CreatedFrom.Equal(now.Add(-20*24*time.Hour - 10*time.Minute)) {
		t.Errorf("first read starts %s, want ten minutes before the cursor", asked[0].CreatedFrom)
	}
	if !asked[1].CreatedFrom.Equal(asked[0].CreatedFrom) || !asked[2].CreatedTo.Equal(asked[0].CreatedTo) || !asked[1].CreatedTo.Equal(asked[2].CreatedFrom) {
		t.Errorf("the halves %+v and %+v do not cover the full step %+v", asked[1], asked[2], asked[0])
	}
	if !asked[len(asked)-1].CreatedTo.Equal(now) {
		t.Errorf("last read ends %s, want now", asked[len(asked)-1].CreatedTo)
	}
	if got, _ := st.Comments().ListForTask(ctx, "T1"); len(got) != 4 {
		t.Errorf("comments written = %d, want the one of each half and each later step", len(got))
	}
}

func TestPullCommentsCapsTheCatchUp(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if err := st.SetMeta(ctx, store.MetaKeyCommentsPulledTo, rfc3339(now.Add(-60*24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	var asked []wrike.CommentParams
	fc := &fakeClient{comments: func(p wrike.CommentParams) ([]wrike.Comment, error) {
		asked = append(asked, p)
		return nil, nil
	}}
	if _, err := pullComments(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 4 || !asked[0].CreatedFrom.Equal(now.Add(-28*24*time.Hour)) || !asked[3].CreatedTo.Equal(now) {
		t.Errorf("reads = %+v, want 4 steps from 28 days back up to now", asked)
	}
}

func TestPullCommentsSkipsARejectedReadAndMovesTheCursor(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fc := &fakeClient{comments: func(p wrike.CommentParams) ([]wrike.Comment, error) {
		return nil, &wrike.APIError{StatusCode: 500, Code: "server_error"}
	}}
	if _, err := pullComments(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, now); err == nil {
		t.Error("a 500 must fail the pull, the next cycle tries again")
	}
	if cursor, _ := st.GetMeta(ctx, store.MetaKeyCommentsPulledTo); cursor != "" {
		t.Errorf("cursor after a 500 = %q, want none", cursor)
	}
	fc.comments = func(p wrike.CommentParams) ([]wrike.Comment, error) {
		return nil, &wrike.APIError{StatusCode: 400, Code: "invalid_parameter"}
	}
	if changed, err := pullComments(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, now); err != nil || changed {
		t.Errorf("a 400 = %v, %v, want no error and no change", changed, err)
	}
	if cursor, _ := st.GetMeta(ctx, store.MetaKeyCommentsPulledTo); cursor != rfc3339(now) {
		t.Errorf("cursor after a 400 = %q, want now so the next cycle asks a fresh range", cursor)
	}
}

func TestPullTimelogChangesWritesCachedTasksAndWalksThePages(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "cached")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	var asked []wrike.TimelogParams
	fc := &fakeClient{timelogs: func(p wrike.TimelogParams) (wrike.TimelogsPage, error) {
		asked = append(asked, p)
		if p.PageToken == "" {
			return wrike.TimelogsPage{NextPageToken: "P2", Timelogs: []wrike.Timelog{
				{ID: "L1", TaskID: "T1", UserID: "U2", TrackedDate: "2026-09-09", Hours: 2, CreatedDate: now.Add(-time.Hour), UpdatedDate: now.Add(-time.Hour)},
				{ID: "L2", TaskID: "T9", UserID: "U2", TrackedDate: "2026-09-09", Hours: 1, CreatedDate: now.Add(-time.Hour), UpdatedDate: now.Add(-time.Hour)},
			}}, nil
		}
		// An empty page with a token, the answer Wrike gives at the end, see pullMyTimelogs.
		return wrike.TimelogsPage{NextPageToken: "P3"}, nil
	}}
	changed, err := pullTimelogChanges(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, now)
	if err != nil || !changed {
		t.Fatalf("pullTimelogChanges = %v, %v, want a change", changed, err)
	}
	if len(asked) != 2 || asked[1].PageToken != "P2" {
		t.Fatalf("asked = %+v, want two pages", asked)
	}
	first := asked[0]
	if !first.UpdatedFrom.Equal(now.Add(-7*24*time.Hour)) || !first.UpdatedTo.Equal(now) || first.PageSize != 1000 || first.Me || first.TrackedFrom != "" {
		t.Errorf("first read = %+v, want the updated window of the first run alone", first)
	}
	if got, _ := st.Timelogs().ListForTask(ctx, "T1"); len(got) != 1 || got[0].ID != "L1" {
		t.Errorf("entries of T1 = %+v", got)
	}
	if got, _ := st.Timelogs().ListForTask(ctx, "T9"); len(got) != 0 {
		t.Errorf("an entry of an unknown task was written: %+v", got)
	}
	if cursor, _ := st.GetMeta(ctx, store.MetaKeyTimelogChangesPulledTo); cursor != rfc3339(now) {
		t.Errorf("cursor = %q", cursor)
	}
}

// The point of the pulls: a comment posted on a task nobody opened is in the cache after one cycle, without a per task read.
func TestCycleBringsANewCommentOnATaskNobodyOpened(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "never opened")
	fc := &fakeClient{comments: func(p wrike.CommentParams) ([]wrike.Comment, error) {
		return []wrike.Comment{{ID: "C1", TaskID: "T1", AuthorID: "U2", Text: "posted while nobody looked", CreatedDate: time.Now().Add(-time.Minute)}}, nil
	}}
	e := New(fc, st, Config{PollInterval: time.Hour}, slog.New(slog.DiscardHandler))
	if _, err := e.Once(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Comments().ListForTask(ctx, "T1"); len(got) != 1 || got[0].Text != "posted while nobody looked" {
		t.Errorf("comments = %+v, want the new comment without an open", got)
	}
	if calls := fc.callLog(); slices.ContainsFunc(calls, func(s string) bool { return strings.HasPrefix(s, "TaskComments") }) {
		t.Errorf("the per task read ran for a task nobody opened: %v", calls)
	}
}
