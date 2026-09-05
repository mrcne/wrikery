package sync

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestDrainSendsInOrderAndCompletes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "before")

	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "after"}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{
		createComment: func(taskID, text string) (wrike.Comment, error) {
			return wrike.Comment{ID: "C9", TaskID: taskID, AuthorID: "U1", Text: text,
				CreatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}, nil
		},
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			return wrike.Task{ID: taskID, Title: u.Title, Status: "Active",
				UpdatedDate: time.Date(2026, 9, 3, 10, 5, 0, 0, time.UTC)}, nil
		},
	}
	changed, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, want changed and no error", changed, err)
	}

	log := fc.callLog()
	if len(log) != 2 || log[0] != "CreateComment T1" || log[1] != "UpdateTask T1" {
		t.Fatalf("calls = %v, want comment first by outbox order", log)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v", pending, failed, err)
	}
	comments, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil || len(comments) != 1 || comments[0].ID != "C9" {
		t.Fatalf("comments = %+v, %v, want the confirmed C9", comments, err)
	}
	task, err := st.Tasks().Get(ctx, "T1")
	if err != nil || task.Title != "after" || task.UpdatedDate != "2026-09-03T10:05:00Z" {
		t.Fatalf("task = %+v, %v, the server response must land in the cache", task, err)
	}
}

func TestDrainRejectedWriteFailsRowAndContinues(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "doomed"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "fine"); err != nil {
		t.Fatal(err)
	}

	first := true
	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		if first {
			first = false
			return wrike.Comment{}, &wrike.APIError{StatusCode: 403, Code: "access_forbidden", Description: "no"}
		}
		return wrike.Comment{ID: "C2", TaskID: taskID, Text: text}, nil
	}}

	changed, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 1 {
		t.Fatalf("counts = %d, %d, %v, want the rejection failed and the rest drained", pending, failed, err)
	}
	rows, err := st.Outbox().ListFailed(ctx)
	if err != nil || len(rows) != 1 || rows[0].LastError == "" {
		t.Fatalf("failed rows = %+v, %v, want the error text kept", rows, err)
	}
}

func TestDrainTransientStopsAndReschedules(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	firstID, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "two"); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		return wrike.Comment{}, &wrike.APIError{StatusCode: 429, Code: "rate_limit_exceeded"}
	}}

	changed, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err == nil {
		t.Fatal("want the transient error back so the engine goes to backoff")
	}
	if changed {
		t.Error("changed = true, nothing completed or failed")
	}
	if got := fc.callLog(); len(got) != 1 {
		t.Fatalf("calls = %v, the drain must stop at the first transient failure", got)
	}
	pending, failed, cerr := st.Outbox().Counts(ctx)
	if cerr != nil || pending != 2 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v, a 429 must never fail a row", pending, failed, cerr)
	}
	row, rerr := st.Outbox().NextDue(ctx, "2027-01-01T00:00:00Z")
	if rerr != nil || row.ID != firstID || row.Attempts != 1 || row.NextAttemptAt == "" {
		t.Fatalf("row = %+v, %v, want attempts 1 and a future next attempt", row, rerr)
	}
}

func TestDrainCompletesCreateThenDependentEdit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", store.TimelogCreatePayload{
		Hours: 1, TrackedDate: "2026-09-03",
	})
	if err != nil {
		t.Fatal(err)
	}
	localID := store.LocalIDPrefix + strconv.FormatInt(createID, 10)
	if _, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, store.TimelogUpdatePayload{Hours: 2}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{
		createTimelog: func(taskID string, hours float64, trackedDate, comment string) (wrike.Timelog, error) {
			return wrike.Timelog{ID: "L9", TaskID: taskID, UserID: "U1", TrackedDate: trackedDate, Hours: hours}, nil
		},
		updateTimelog: func(timelogID string, u wrike.TimelogUpdate) (wrike.Timelog, error) {
			return wrike.Timelog{ID: timelogID, TaskID: "T1", UserID: "U1", TrackedDate: "2026-09-03", Hours: u.Hours}, nil
		},
	}
	if _, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	log := fc.callLog()
	if len(log) != 2 || log[0] != "CreateTimelog T1" || log[1] != "UpdateTimelog L9" {
		t.Fatalf("calls = %v, the dependent edit must drain against the confirmed id", log)
	}
	logs, err := st.Timelogs().ListForTask(ctx, "T1")
	if err != nil || len(logs) != 1 || logs[0].ID != "L9" || logs[0].Hours != 2 {
		t.Fatalf("timelogs = %+v, %v", logs, err)
	}
}

func TestDrainDeleteAlreadyGoneCompletes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T1", UserID: "U1",
		TrackedDate: "2026-09-01", Hours: 1,
		CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogDelete(ctx, "L1"); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{deleteTimelog: func(timelogID string) error {
		return &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
	}}
	changed, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, a delete of a vanished timelog is success", changed, err)
	}
	pending, failed, cerr := st.Outbox().Counts(ctx)
	if cerr != nil || pending != 0 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v", pending, failed, cerr)
	}
}

func TestDrainEmptyQueueIsQuiet(t *testing.T) {
	st := newTestStore(t)
	fc := &fakeClient{}
	changed, err := drainOutbox(context.Background(), fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || changed {
		t.Fatalf("drain = %v, %v, want a no-op", changed, err)
	}
	if len(fc.callLog()) != 0 {
		t.Errorf("calls = %v", fc.callLog())
	}
}

func TestDrainCorruptRowFailsAndContinues(t *testing.T) {
	// The path is needed to reach behind the store, so this test opens its own database.
	path := filepath.Join(t.TempDir(), "wrike.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	badID, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "will be corrupted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "fine"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE outbox SET payload = 'not json' WHERE id = ?`, badID); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{}
	changed, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v, a corrupt row must not stop the drain", changed, err)
	}
	if got := fc.callLog(); len(got) != 1 || got[0] != "CreateComment T1" {
		t.Fatalf("calls = %v, want only the good row sent", got)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 1 {
		t.Fatalf("counts = %d, %d, %v, want the corrupt row failed and the rest drained", pending, failed, err)
	}
	rows, err := st.Outbox().ListFailed(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != badID || rows[0].LastError == "" {
		t.Fatalf("failed rows = %+v, %v", rows, err)
	}
}

func TestDrainAuthFailureLeavesRowDue(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "waiting for a token")
	if err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		return wrike.Comment{}, &wrike.APIError{StatusCode: 401, Code: "not_authorized"}
	}}
	changed, err := drainOutbox(ctx, fc, st, 2*time.Second, 5*time.Minute)
	if classify(err) != failAuth || changed {
		t.Fatalf("drain = %v, %v, want the auth error back and nothing changed", changed, err)
	}
	// The row must be pending and due right now, with no backoff and no attempt counted against it.
	row, err := st.Outbox().NextDue(ctx, rfc3339(time.Now()))
	if err != nil || row.ID != id || row.Attempts != 0 || row.State != store.StatePending {
		t.Fatalf("row = %+v, %v, want it back in the queue untouched", row, err)
	}
	if row.NextAttemptAt != "" {
		t.Fatalf("next attempt = %q, want none", row.NextAttemptAt)
	}
}
