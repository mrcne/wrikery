package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func seedOutboxTask(t *testing.T, st *Store) {
	t.Helper()
	if err := st.Tasks().Upsert(context.Background(), []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}
}

func TestNextDueOrderAndBackoff(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedOutboxTask(t, st)

	first, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "two")
	if err != nil {
		t.Fatal(err)
	}

	row, err := st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != first || row.Kind != KindCommentCreate || row.EntityID != "T1" {
		t.Fatalf("row = %+v, want the oldest comment", row)
	}

	// A transient failure pushes the row past now, the next one surfaces.
	if err := st.Outbox().Reschedule(ctx, first, "boom", "2026-09-03T10:05:00Z"); err != nil {
		t.Fatal(err)
	}
	row, err = st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != second {
		t.Fatalf("row = %+v, want the second while the first backs off", row)
	}
	// Time passes, the first is due again and still wins on age.
	row, err = st.Outbox().NextDue(ctx, "2026-09-03T10:06:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != first || row.Attempts != 1 || row.LastError != "boom" {
		t.Fatalf("row = %+v, want first with attempts 1", row)
	}
}

func TestNextDueEmpty(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Outbox().NextDue(context.Background(), "2026-09-03T10:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestCompleteCommentSwapsLocalRow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedOutboxTask(t, st)

	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "queued")
	if err != nil {
		t.Fatal(err)
	}
	real := Comment{ID: "C9", TaskID: "T1", AuthorID: "U1", Text: "queued",
		CreatedDate: "2026-09-03T10:00:00Z"}
	if err := st.Outbox().CompleteComment(ctx, id, real); err != nil {
		t.Fatal(err)
	}

	got, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "C9" {
		t.Fatalf("comments = %+v, want only the confirmed C9", got)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 || failed != 0 {
		t.Errorf("counts after complete = %d, %d, want 0, 0", pending, failed)
	}
}

func TestCompleteTimelogRemapsQueuedEdits(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", TimelogCreatePayload{
		Hours: 1, TrackedDate: "2026-09-03",
	})
	if err != nil {
		t.Fatal(err)
	}
	localID := "local:" + strconv.FormatInt(createID, 10)
	editID, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, TimelogUpdatePayload{Hours: 2})
	if err != nil {
		t.Fatal(err)
	}

	real := Timelog{ID: "L9", TaskID: "T1", UserID: "U1", TrackedDate: "2026-09-03",
		Hours: 1, CreatedDate: "2026-09-03T10:00:00Z", UpdatedDate: "2026-09-03T10:00:00Z"}
	if err := st.Outbox().CompleteTimelog(ctx, createID, real); err != nil {
		t.Fatal(err)
	}

	row, err := st.Outbox().NextDue(ctx, "2026-09-03T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != editID || row.EntityID != "L9" {
		t.Fatalf("row = %+v, want the queued edit remapped to L9", row)
	}
}

func TestFailRetryDiscard(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedOutboxTask(t, st)

	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "doomed")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, id, "403 access forbidden"); err != nil {
		t.Fatal(err)
	}
	failedRows, err := st.Outbox().ListFailed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(failedRows) != 1 || failedRows[0].LastError != "403 access forbidden" {
		t.Fatalf("failed = %+v", failedRows)
	}

	if err := st.Outbox().Retry(ctx, id); err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != id || row.Attempts != 0 || row.LastError != "" {
		t.Fatalf("retried row = %+v, want clean pending", row)
	}

	if err := st.Outbox().Fail(ctx, id, "still 403"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, id); err != nil {
		t.Fatal(err)
	}
	comments, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 0 {
		t.Errorf("comments after discard = %+v, the optimistic row must go", comments)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 || failed != 0 {
		t.Errorf("counts after discard = %d, %d", pending, failed)
	}
}

func TestNextDueSkipsLocalTargets(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", TimelogCreatePayload{
		Hours: 1, TrackedDate: "2026-09-03",
	})
	if err != nil {
		t.Fatal(err)
	}
	localID := "local:" + strconv.FormatInt(createID, 10)
	editID, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, TimelogUpdatePayload{Hours: 2})
	if err != nil {
		t.Fatal(err)
	}

	// Push the create into the future, the dependent edit must not surface
	// in its place even though it is due by time alone.
	if err := st.Outbox().Reschedule(ctx, createID, "boom", "2026-09-03T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound while the create backs off", err)
	}

	real := Timelog{ID: "L9", TaskID: "T1", UserID: "U1", TrackedDate: "2026-09-03",
		Hours: 1, CreatedDate: "2026-09-03T10:00:00Z", UpdatedDate: "2026-09-03T10:00:00Z"}
	if err := st.Outbox().CompleteTimelog(ctx, createID, real); err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != editID || row.EntityID != "L9" {
		t.Fatalf("row = %+v, want the edit remapped to L9", row)
	}
}

func TestDiscardCascadesToDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", TimelogCreatePayload{
		Hours: 1, TrackedDate: "2026-09-03",
	})
	if err != nil {
		t.Fatal(err)
	}
	localID := "local:" + strconv.FormatInt(createID, 10)
	if _, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, TimelogUpdatePayload{Hours: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogDelete(ctx, localID); err != nil {
		t.Fatal(err)
	}

	if err := st.Outbox().Fail(ctx, createID, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, createID); err != nil {
		t.Fatal(err)
	}

	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 || failed != 0 {
		t.Errorf("counts after discard = %d, %d, want 0, 0", pending, failed)
	}
	logs, err := st.Timelogs().ListForTask(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Errorf("timelogs after discard = %+v, the optimistic row must go", logs)
	}
}

func TestDiscardRefusesInflight(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedOutboxTask(t, st)

	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound for an inflight row", err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 || failed != 0 {
		t.Errorf("counts after refused discard = %d, %d, want 1, 0", pending, failed)
	}
}

func TestMarkInflightAndReset(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedOutboxTask(t, st)

	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("second mark = %v, want ErrNotFound", err)
	}
	if _, err := st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Errorf("inflight row surfaced in NextDue: %v", err)
	}
	n, err := st.Outbox().ResetInflight(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("reset = %d, want 1", n)
	}
	if _, err := st.Outbox().NextDue(ctx, "2026-09-03T10:00:00Z"); err != nil {
		t.Errorf("row not pending after reset: %v", err)
	}
}

func TestCompleteTaskSwapsInServerVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedOutboxTask(t, st)

	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", TaskUpdatePayload{Title: "local"})
	if err != nil {
		t.Fatal(err)
	}
	server := makeTask("T1", "server")
	server.UpdatedDate = "2026-09-03T12:00:00Z"
	if err := st.Outbox().CompleteTask(ctx, id, server); err != nil {
		t.Fatal(err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Fatalf("counts = %d, %d, %v, want the row gone", pending, failed, err)
	}
	task, err := st.Tasks().Get(ctx, "T1")
	if err != nil || task.Title != "server" || task.UpdatedDate != "2026-09-03T12:00:00Z" {
		t.Fatalf("task = %+v, %v, want the server version in the cache", task, err)
	}
}

func TestCompleteTaskCreateSwapsTheRowAndRepointsDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"}, "CS1")
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	commentID, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogCreate(ctx, localID, "U1", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, localID, TaskUpdatePayload{Importance: "High"}); err != nil {
		t.Fatal(err)
	}
	if row, err := st.Outbox().NextDue(ctx, "2030-01-01T00:00:00Z"); err != nil || row.ID != createID {
		t.Fatalf("next due = %+v, %v, the dependents must wait for the create", row, err)
	}

	if err := st.Tasks().MarkOpened(ctx, localID, "2026-10-01T09:00:00Z"); err != nil {
		t.Fatal(err)
	}

	real := Task{ID: "T9", Title: "New one", Status: "Active", CustomStatusID: "CS1", Importance: "Normal",
		ParentIDs: []string{"F1"}, Permalink: "https://www.wrike.com/open.htm?id=9",
		CreatedDate: "2026-10-01T09:00:05Z", UpdatedDate: "2026-10-01T09:00:05Z"}
	if err := st.Outbox().CompleteTaskCreate(ctx, createID, real); err != nil {
		t.Fatal(err)
	}

	if _, err := st.Tasks().Get(ctx, localID); !errors.Is(err, ErrNotFound) {
		t.Errorf("local task after the swap: %v, want gone", err)
	}
	if got, err := st.Tasks().Get(ctx, "T9"); err != nil || got.Permalink == "" {
		t.Errorf("server task = %+v, %v", got, err)
	}
	if opened, err := st.Tasks().RecentlyOpenedIDs(ctx, "2026-10-01T00:00:00Z", 10); err != nil || len(opened) != 1 || opened[0] != "T9" {
		t.Errorf("recently opened after the swap = %v, %v, want T9", opened, err)
	}
	comments, err := st.Comments().ListForTask(ctx, "T9")
	if err != nil || len(comments) != 1 || comments[0].ID != LocalID(commentID) {
		t.Errorf("comments on T9 = %+v, %v, want the queued one re-pointed", comments, err)
	}
	logs, err := st.Timelogs().ListForTask(ctx, "T9")
	if err != nil || len(logs) != 1 {
		t.Errorf("timelogs on T9 = %+v, %v, want the queued one re-pointed", logs, err)
	}
	row, err := st.Outbox().NextDue(ctx, "2030-01-01T00:00:00Z")
	if err != nil || row.ID != commentID || row.EntityID != "T9" {
		t.Errorf("next due = %+v, %v, want the comment against T9", row, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 3 || failed != 0 {
		t.Errorf("counts = %d, %d, %v, want the three dependents pending", pending, failed, err)
	}
}

func TestDiscardTaskCreateDropsTheTaskAndItsDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, createID, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, createID); err != nil {
		t.Fatal(err)
	}

	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %v, want the dependent gone with the create", pending, failed, err)
	}
	if _, err := st.Tasks().Get(ctx, localID); !errors.Is(err, ErrNotFound) {
		t.Errorf("local task after discard: %v, want gone", err)
	}
	if comments, _ := st.Comments().ListForTask(ctx, localID); len(comments) != 0 {
		t.Errorf("comments after discard = %+v, want none", comments)
	}
}

func TestDiscardTaskCreateDropsEditsQueuedOnItsTimelog(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	logID, err := st.Outbox().EnqueueTimelogCreate(ctx, localID, "U1", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogUpdate(ctx, LocalID(logID), TimelogUpdatePayload{Hours: 2}); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, createID, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, createID); err != nil {
		t.Fatal(err)
	}

	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %v, want nothing left queued", pending, failed, err)
	}
	if _, err := st.Tasks().Get(ctx, localID); !errors.Is(err, ErrNotFound) {
		t.Errorf("local task after discard: %v, want gone", err)
	}
	if logs, _ := st.Timelogs().ListForTask(ctx, localID); len(logs) != 0 {
		t.Errorf("timelogs after discard = %+v, want none", logs)
	}
}

// A dialog opened on a new task keeps its local id, and the create usually lands while the user types.
func TestEnqueueAfterTheSwapLandsOnTheRealTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	real := Task{ID: "T9", Title: "New one", Status: "Active", ParentIDs: []string{"F1"},
		CreatedDate: "2026-10-01T09:00:05Z", UpdatedDate: "2026-10-01T09:00:05Z"}
	if err := st.Outbox().CompleteTaskCreate(ctx, createID, real); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Outbox().RealID(ctx, localID); err != nil || got != "T9" {
		t.Fatalf("RealID = %q, %v, want T9", got, err)
	}

	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, localID, TaskUpdatePayload{Title: "after"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "late"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogCreate(ctx, localID, "U1", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}

	task, err := st.Tasks().Get(ctx, "T9")
	if err != nil || task.Title != "after" {
		t.Errorf("task = %+v, %v, want the late edit applied to the server row", task, err)
	}
	if comments, err := st.Comments().ListForTask(ctx, "T9"); err != nil || len(comments) != 1 {
		t.Errorf("comments on T9 = %+v, %v, want the late comment", comments, err)
	}
	if logs, err := st.Timelogs().ListForTask(ctx, "T9"); err != nil || len(logs) != 1 {
		t.Errorf("timelogs on T9 = %+v, %v, want the late entry", logs, err)
	}
	for i := 0; i < 3; i++ {
		row, err := st.Outbox().NextDue(ctx, "2030-01-01T00:00:00Z")
		if err != nil || row.EntityID != "T9" {
			t.Fatalf("due row %d = %+v, %v, want every late write against T9", i, row, err)
		}
		if err := st.Outbox().MarkInflight(ctx, row.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnqueueAgainstADiscardedCreateIsRefused(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	if err := st.Outbox().Discard(ctx, createID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, localID, TaskUpdatePayload{Title: "after"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update against a discarded create: %v, want ErrNotFound", err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "late"); !errors.Is(err, ErrNotFound) {
		t.Errorf("comment against a discarded create: %v, want ErrNotFound", err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %v, want nothing queued for a task that is gone", pending, failed, err)
	}
}

func TestEnqueueAfterATimelogSwapLandsOnTheRealEntry(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTimelogCreate(ctx, "T1", "U1", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	real := Timelog{ID: "L9", TaskID: "T1", UserID: "U1", TrackedDate: "2026-10-01", Hours: 1,
		CreatedDate: "2026-10-01T10:00:00Z", UpdatedDate: "2026-10-01T10:00:00Z"}
	if err := st.Outbox().CompleteTimelog(ctx, createID, real); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTimelogUpdate(ctx, localID, TimelogUpdatePayload{Hours: 2}); err != nil {
		t.Fatal(err)
	}
	row, err := st.Outbox().NextDue(ctx, "2030-01-01T00:00:00Z")
	if err != nil || row.Kind != KindTimelogUpdate || row.EntityID != "L9" {
		t.Fatalf("due row = %+v, %v, want the late edit against L9", row, err)
	}
	if logs, err := st.Timelogs().ListForTask(ctx, "T1"); err != nil || len(logs) != 1 || logs[0].Hours != 2 {
		t.Errorf("timelogs = %+v, %v, want the edit applied to L9", logs, err)
	}
}

func TestDiscardTaskCreateDropsADeleteQueuedOnItsTimelog(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "New one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	localID := LocalID(createID)
	logID, err := st.Outbox().EnqueueTimelogCreate(ctx, localID, "U1", TimelogCreatePayload{Hours: 1, TrackedDate: "2026-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	// The delete takes the entry's row out of the timelogs table at once, only the queue still names it.
	if _, err := st.Outbox().EnqueueTimelogDelete(ctx, LocalID(logID)); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, createID, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Discard(ctx, createID); err != nil {
		t.Fatal(err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %v, want the queued delete gone with the create", pending, failed, err)
	}
}
