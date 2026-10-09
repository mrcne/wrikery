package syncer

import (
	"context"
	"database/sql"
	"errors"
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
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "after", Description: "<p>d</p>", Importance: "Low", AddParents: []string{"F2"}}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{
		createComment: func(taskID, text string) (wrike.Comment, error) {
			return wrike.Comment{ID: "C9", TaskID: taskID, AuthorID: "U1", Text: text,
				CreatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}, nil
		},
		updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
			if u.Importance != "Low" {
				t.Errorf("update sent importance %q, want the queued Low", u.Importance)
			}
			if u.Description != "<p>d</p>" {
				t.Errorf("update sent description %q, want the queued HTML", u.Description)
			}
			if len(u.AddParents) != 1 || u.AddParents[0] != "F2" {
				t.Errorf("update sent parents %v, want the queued F2", u.AddParents)
			}
			return wrike.Task{ID: taskID, Title: u.Title, Status: "Active",
				UpdatedDate: time.Date(2026, 9, 3, 10, 5, 0, 0, time.UTC)}, nil
		},
	}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
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

	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
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

	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
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
	if _, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute); err != nil {
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
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
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
	changed, err := drainOutbox(context.Background(), context.Background(), fc, st, 2*time.Second, 5*time.Minute)
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
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
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
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
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

func TestDrainCreatesATaskThenItsDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "New one", Responsibles: []string{"U1"}})
	if err != nil {
		t.Fatal(err)
	}
	localID := store.LocalID(createID)
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, localID, store.TaskUpdatePayload{Importance: "High"}); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{createTask: func(folderID string, tc wrike.TaskCreate) (wrike.Task, error) {
		if tc.Title != "New one" || len(tc.Responsibles) != 1 || tc.Responsibles[0] != "U1" {
			t.Errorf("create sent %+v, want the queued title and responsible", tc)
		}
		return wrike.Task{ID: "T9", Title: tc.Title, Status: "Active", CustomStatusID: "CS1", Importance: "Normal",
			ResponsibleIDs: tc.Responsibles, ParentIDs: []string{folderID}, Permalink: "https://www.wrike.com/open.htm?id=9",
			CreatedDate: time.Date(2026, 10, 1, 9, 0, 5, 0, time.UTC), UpdatedDate: time.Date(2026, 10, 1, 9, 0, 5, 0, time.UTC)}, nil
	}, updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{ID: taskID, Title: "New one", Importance: u.Importance}, nil
	}}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}

	log := fc.callLog()
	if len(log) != 3 || log[0] != "CreateTask F1" || log[1] != "CreateComment T9" || log[2] != "UpdateTask T9" {
		t.Fatalf("calls = %v, want the create first and the dependents against the confirmed id", log)
	}
	if _, err := st.Tasks().Get(ctx, localID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("local task after the drain: %v, want gone", err)
	}
	task, err := st.Tasks().Get(ctx, "T9")
	if err != nil || task.Importance != "High" {
		t.Errorf("task = %+v, %v, want the server copy with the dependent edit applied", task, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %v", pending, failed, err)
	}
}

func TestDrainRejectedCreateKeepsTheLocalTaskAndItsDependents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	createID, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "New one"})
	if err != nil {
		t.Fatal(err)
	}
	localID := store.LocalID(createID)
	if _, err := st.Outbox().EnqueueComment(ctx, localID, "U1", "first"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{createTask: func(folderID string, tc wrike.TaskCreate) (wrike.Task, error) {
		return wrike.Task{}, &wrike.APIError{StatusCode: 403, Code: "access_forbidden", Description: "no"}
	}}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 1 || failed != 1 {
		t.Errorf("counts = %d, %d, %v, want the create failed and the comment still waiting", pending, failed, err)
	}
	if _, err := st.Tasks().Get(ctx, localID); err != nil {
		t.Errorf("local task after the rejection: %v, want kept for the issues screen", err)
	}
	if log := fc.callLog(); len(log) != 1 {
		t.Errorf("calls = %v, the dependent must not be sent", log)
	}
}

func TestDrainCorruptCreateFailsAndContinues(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")

	// The box never queues an empty title, only a damaged row has one, and the client would refuse it with a plain error.
	if _, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "fine"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	changed, err := drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	if err != nil || !changed {
		t.Fatalf("drain = %v, %v", changed, err)
	}
	if log := fc.callLog(); len(log) != 1 || log[0] != "CreateComment T1" {
		t.Errorf("calls = %v, the damaged create must fail without a request and the next row must drain", log)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 0 || failed != 1 {
		t.Errorf("counts = %d, %d, %v, want the create failed for good", pending, failed, err)
	}
}

// The drain stops at the first write Wrike could not take, so the error has to say which row that was:
// a command that queued a later row would otherwise print another task's failure as its own.
func TestDrainNamesTheRowItStoppedAt(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "one")
	seedTask(t, st, "T2", "two")
	first, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Outbox().EnqueueTaskUpdate(ctx, "T2", store.TaskUpdatePayload{Title: "b"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{}, &wrike.APIError{StatusCode: 503, Method: "PUT", Path: "/tasks/" + taskID}
	}}
	_, err = drainOutbox(ctx, ctx, fc, st, 2*time.Second, 5*time.Minute)
	var se *SendError
	if !errors.As(err, &se) || se.RowID != first || se.EntityID != "T1" {
		t.Fatalf("drain error = %#v, want a SendError for row %d of T1", err, first)
	}
	var apiErr *wrike.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
		t.Errorf("the cause should stay reachable through the error: %v", err)
	}
	row, err := st.Outbox().Get(ctx, second)
	if err != nil || row.State != store.StatePending || row.Attempts != 0 {
		t.Errorf("second row = %+v, %v, want untouched and pending", row, err)
	}
}
