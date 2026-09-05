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
