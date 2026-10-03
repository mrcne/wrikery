package syncer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestOnceResendsARowADeadProcessLeftInFlight(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{ID: taskID, Title: u.Title, Status: "Active"}, nil
	}}
	e := New(fc, st, Config{}, nil)
	state, err := e.Once(ctx)
	if err != nil || state != StateIdle {
		t.Fatalf("Once = %q, %v, want idle and no error", state, err)
	}
	if _, err := st.Outbox().Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("row after Once: err = %v, want ErrNotFound, the row was sent", err)
	}
	if me, err := st.GetMeta(ctx, store.MetaKeyMe); err != nil || me != "U1" {
		t.Errorf("me = %q, %v, want U1 from the cycle", me, err)
	}
}

func TestOnceCreatesTheMeScopeOnAFreshStore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	e := New(&fakeClient{}, st, Config{}, nil)
	if _, err := e.Once(ctx); err != nil {
		t.Fatal(err)
	}
	scopes, err := st.Scopes().Followed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].ID != store.ScopeKindMe {
		t.Errorf("followed scopes = %+v, want the me scope", scopes)
	}
}

func TestOnceReportsOfflineWhenWrikeIsUnreachable(t *testing.T) {
	st := newTestStore(t)
	fc := &fakeClient{me: func() (wrike.Contact, error) { return wrike.Contact{}, errors.New("dial tcp: connection refused") }}
	e := New(fc, st, Config{}, nil)
	state, err := e.Once(context.Background())
	if err == nil || state != StateOffline {
		t.Errorf("Once = %q, %v, want offline with the error", state, err)
	}
}

func TestDrainSendsThePendingRowsAndStops(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	if _, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{ID: taskID, Title: u.Title, Status: "Active"}, nil
	}}
	e := New(fc, st, Config{}, nil)
	if err := e.Drain(ctx, 0); err != nil {
		t.Fatal(err)
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 || failed != 0 {
		t.Errorf("counts after drain = %d pending, %d failed", pending, failed)
	}
	if calls := fc.callLog(); len(calls) != 1 || calls[0] != "UpdateTask T1" {
		t.Errorf("calls = %v, want one UpdateTask and no pull", calls)
	}
}

func TestDrainGivesUpWhenTheLockIsHeld(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(t.TempDir(), "sync.lock")
	held, err := acquire(ctx, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	fc := &fakeClient{}
	e := New(fc, st, Config{LockFile: lockPath}, nil)
	dctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if err := e.Drain(dctx, 0); !errors.Is(err, ErrLocked) {
		t.Fatalf("Drain err = %v, want ErrLocked", err)
	}
	if calls := fc.callLog(); len(calls) != 0 {
		t.Errorf("calls = %v, want none while locked", calls)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil || row.State != store.StateInflight {
		t.Errorf("row = %+v, %v, want still inflight, the reset needs the lock", row, err)
	}
}

func TestDrainCutsAStartedUpdateAtTheBudget(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	// The send outlives the budget, the way a stalled request does when a command's wait runs out.
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		time.Sleep(300 * time.Millisecond)
		return wrike.Task{}, context.DeadlineExceeded
	}}
	e := New(fc, st, Config{}, nil)
	if err := e.Drain(ctx, 100*time.Millisecond); err == nil {
		t.Fatal("want the context error back")
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != store.StatePending || row.Attempts != 1 || row.NextAttemptAt == "" {
		t.Errorf("row = %+v, want pending with one attempt and a next time, not stranded in flight", row)
	}
}

func TestDrainLetsAStartedCreateFinishPastTheBudget(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", store.TaskCreatePayload{Title: "New"}, "")
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{createTask: func(folderID string, tc wrike.TaskCreate) (wrike.Task, error) {
		time.Sleep(300 * time.Millisecond)
		return wrike.Task{ID: "TNEW", Title: "New", Status: "Active"}, nil
	}}
	e := New(fc, st, Config{}, nil)
	// The loop then reads the next row on the ended budget, so Drain may return the context error.
	// The commit is what counts here.
	_ = e.Drain(ctx, 100*time.Millisecond)
	if _, err := st.Outbox().Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("row after the drain: err = %v, want ErrNotFound", err)
	}
	if task, err := st.Tasks().Get(ctx, "TNEW"); err != nil || task.Title != "New" {
		t.Errorf("task = %+v, %v, want the created task in the cache", task, err)
	}
}

func TestDrainContinuesWhenTheLockFileCannotBeOpened(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	var logs bytes.Buffer
	lockPath := filepath.Join(t.TempDir(), "missing", "sync.lock")
	e := New(&fakeClient{}, st, Config{LockFile: lockPath}, slog.New(slog.NewTextHandler(&logs, nil)))
	for i := 0; i < 2; i++ {
		id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Drain(ctx, 0); err != nil {
			t.Fatalf("Drain %d: %v", i, err)
		}
		if _, err := st.Outbox().Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("row %d after the drain: err = %v, want ErrNotFound, it was sent", i, err)
		}
	}
	if n := strings.Count(logs.String(), "sync lock unavailable"); n != 1 {
		t.Errorf("warning logged %d times, want once\n%s", n, logs.String())
	}
}

func TestDrainStopsStartingRowsAtTheBudget(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	seedTask(t, st, "T2", "Two")
	first, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Outbox().EnqueueTaskUpdate(ctx, "T2", store.TaskUpdatePayload{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		time.Sleep(150 * time.Millisecond)
		return wrike.Task{ID: taskID, Title: u.Title, Status: "Active"}, nil
	}}
	e := New(fc, st, Config{}, nil)
	if err := e.Drain(ctx, 100*time.Millisecond); err == nil {
		t.Fatal("want the context error once the budget ended")
	}
	if _, err := st.Outbox().Get(ctx, first); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("first row: err = %v, want ErrNotFound, it was sent", err)
	}
	row, err := st.Outbox().Get(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != store.StatePending || row.Attempts != 0 {
		t.Errorf("second row = %+v, want pending and never started", row)
	}
}

func TestRunWaitsForTheLockBeforeTheFirstDrainPass(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(t.TempDir(), "sync.lock")
	held, err := acquire(ctx, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		return wrike.Task{ID: taskID, Title: u.Title, Status: "Active"}, nil
	}}
	e := New(fc, st, Config{LockFile: lockPath, PollInterval: time.Hour}, nil)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- e.Run(runCtx) }()
	time.Sleep(300 * time.Millisecond)
	row, err := st.Outbox().Get(ctx, id)
	if err != nil || row.State != store.StateInflight {
		t.Fatalf("row under a held lock = %+v, %v, want still inflight", row, err)
	}
	held.release()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := st.Outbox().Get(ctx, id); errors.Is(err, store.ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the row was never reset and sent after the lock was released")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v", err)
	}
}

func TestDrainCommitsASendThatOutlivedTheContext(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	dctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Wrike answers and the command's context ends before the local commit.
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		cancel()
		return wrike.Task{ID: taskID, Title: u.Title, Status: "Active"}, nil
	}}
	e := New(fc, st, Config{}, nil)
	// The loop then reads the next row on the ended context, so Drain may return the context error.
	// The commit is what counts here.
	_ = e.Drain(dctx, 0)
	if _, err := st.Outbox().Get(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("row after the drain: err = %v, want ErrNotFound", err)
	}
	task, err := st.Tasks().Get(ctx, "T1")
	if err != nil || task.Title != "Two" {
		t.Errorf("task = %+v, %v, want the new title", task, err)
	}
}
