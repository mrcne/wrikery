package syncer

import (
	"context"
	"errors"
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

func TestFetchThreadReadsANeverReadThreadOnce(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "never read")
	seedTask(t, st, "T2", "read before")
	if err := st.Tasks().MarkThreadRead(ctx, "T2", rfc3339(time.Now().Add(-48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{taskComments: func(taskID string) ([]wrike.Comment, error) {
		return []wrike.Comment{{ID: "C-" + taskID, TaskID: taskID, AuthorID: "U2", Text: "hi", CreatedDate: time.Now()}}, nil
	}}
	e := newReachEngine(t, fc, st, slog.New(slog.DiscardHandler))
	for _, tc := range []struct {
		id   string
		want bool
	}{{"T1", true}, {"T1", false}, {"T2", false}, {"local:4", false}} {
		read, err := e.FetchThread(ctx, tc.id)
		if err != nil || read != tc.want {
			t.Errorf("FetchThread(%s) = %v, %v, want %v", tc.id, read, err, tc.want)
		}
	}
	calls := fc.callLog()
	if n := strings.Count(strings.Join(calls, "\n"), "TaskComments T1"); n != 1 {
		t.Errorf("T1 read %d times, want once: %v", n, calls)
	}
	for _, id := range []string{"T2", "local:4"} {
		if slices.Contains(calls, "TaskComments "+id) {
			t.Errorf("%s was read: %v", id, calls)
		}
	}
	if got, _ := st.Tasks().Get(ctx, "T1"); got.ThreadReadAt == "" {
		t.Error("T1 must carry the stamp after the read")
	}
	if got, _ := st.Comments().ListForTask(ctx, "T1"); len(got) != 1 {
		t.Errorf("comments of T1 = %+v", got)
	}
	// The read reports the caches it touched for the interface to read again.
	select {
	case ev := <-e.Events():
		if ev.Kind != EventStoreChanged || !slices.Contains(ev.Entities, KindComments) {
			t.Errorf("event = %+v, want a store change naming the comments", ev)
		}
	default:
		t.Error("no event after the read")
	}
}

func TestFetchThreadDropsAGoneTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "deleted on the server")
	fc := &fakeClient{taskComments: func(taskID string) ([]wrike.Comment, error) {
		return nil, &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
	}}
	e := newReachEngine(t, fc, st, slog.New(slog.DiscardHandler))
	if read, err := e.FetchThread(ctx, "T1"); !errors.Is(err, ErrNoSuchTask) || read {
		t.Errorf("FetchThread = %v, %v, want ErrNoSuchTask", read, err)
	}
	if _, err := st.Tasks().Get(ctx, "T1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("T1 after the 404: %v, want gone", err)
	}
	select {
	case ev := <-e.Events():
		if !slices.Contains(ev.Entities, KindTasks) {
			t.Errorf("event = %+v, want the tasks kind", ev)
		}
	default:
		t.Error("no event after the drop")
	}
}

func TestFetchThreadLogsARejectionAndReadsNothing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "not shared")
	fc := &fakeClient{taskComments: func(taskID string) ([]wrike.Comment, error) {
		return nil, &wrike.APIError{StatusCode: 403, Code: "access_forbidden"}
	}}
	e := newReachEngine(t, fc, st, slog.New(slog.DiscardHandler))
	if read, err := e.FetchThread(ctx, "T1"); err != nil || read {
		t.Errorf("FetchThread = %v, %v, want false and no error", read, err)
	}
	if got, err := st.Tasks().Get(ctx, "T1"); err != nil || got.ThreadReadAt != "" {
		t.Errorf("T1 after the 403 = %+v, %v, want the task kept without a stamp", got, err)
	}
}

func TestRereadStaleReadsChangedStampedTasksOnly(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for _, tc := range []struct{ id, updated, stamp string }{
		{"T1", "2026-09-03T12:00:00Z", "2026-09-03T10:00:00Z"},
		{"T2", "2026-09-03T09:00:00Z", "2026-09-03T10:00:00Z"},
		{"T3", "2026-09-03T12:00:00Z", ""},
	} {
		if err := st.Tasks().Upsert(ctx, []store.Task{{ID: tc.id, Title: tc.id, Status: "Active", CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: tc.updated}}); err != nil {
			t.Fatal(err)
		}
		if tc.stamp != "" {
			if err := st.Tasks().MarkThreadRead(ctx, tc.id, tc.stamp); err != nil {
				t.Fatal(err)
			}
		}
	}
	fc := &fakeClient{taskComments: func(taskID string) ([]wrike.Comment, error) {
		return []wrike.Comment{{ID: "C-" + taskID, TaskID: taskID, AuthorID: "U2", Text: "edited", CreatedDate: time.Now()}}, nil
	}}
	touched, err := rereadStale(ctx, fc, st, slog.New(slog.DiscardHandler), 50)
	if err != nil || !slices.Contains(touched, KindComments) {
		t.Fatalf("rereadStale = %v, %v, want the comments touched", touched, err)
	}
	calls := fc.callLog()
	if !slices.Contains(calls, "TaskComments T1") || slices.Contains(calls, "TaskComments T2") || slices.Contains(calls, "TaskComments T3") {
		t.Errorf("calls = %v, want T1 alone: changed since its stamp", calls)
	}
	if got, _ := st.Tasks().Get(ctx, "T1"); got.ThreadReadAt <= got.UpdatedDate {
		t.Errorf("T1 stamp %q must be later than its change %q after the read", got.ThreadReadAt, got.UpdatedDate)
	}
	fc.calls = nil
	if touched, err := rereadStale(ctx, fc, st, slog.New(slog.DiscardHandler), 50); err != nil || len(touched) != 0 || len(fc.callLog()) != 0 {
		t.Errorf("a second pass = %v, %v, calls %v, want nothing to read", touched, err, fc.callLog())
	}
}

// A stamped task the poll lists as changed has its thread read again in the same cycle.
func TestCycleReadsAgainTheThreadOfAChangedStampedTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject, Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	seedTask(t, st, "T1", "read before")
	if err := st.Tasks().MarkThreadRead(ctx, "T1", "2026-09-02T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{
		tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
			return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1", Title: "read before", Status: "Active", UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}}}, nil
		},
		taskComments: func(taskID string) ([]wrike.Comment, error) {
			return []wrike.Comment{{ID: "C1", TaskID: taskID, AuthorID: "U2", Text: "edited on the server", CreatedDate: time.Now()}}, nil
		},
	}
	e := New(fc, st, Config{PollInterval: time.Hour}, slog.New(slog.DiscardHandler))
	if _, err := e.Once(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Comments().ListForTask(ctx, "T1"); len(got) != 1 || got[0].Text != "edited on the server" {
		t.Errorf("comments = %+v, want the thread read again after the change", got)
	}
}
