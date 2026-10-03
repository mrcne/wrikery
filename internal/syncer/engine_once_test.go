package syncer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestOnceRunsOneCycleAndLeavesInflightRowsAlone(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	// A row another process is sending right now. Run would reset it at startup, Once must not.
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().MarkInflight(ctx, id); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	e := New(fc, st, Config{}, nil)
	state, err := e.Once(ctx)
	if err != nil || state != StateIdle {
		t.Fatalf("Once = %q, %v, want idle and no error", state, err)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != store.StateInflight {
		t.Errorf("row state = %q, want inflight untouched", row.State)
	}
	if me, err := st.GetMeta(ctx, store.MetaKeyMe); err != nil || me != "U1" {
		t.Errorf("me = %q, %v, want U1 from the cycle", me, err)
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
	if err := e.Drain(ctx); err != nil {
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
	if err := e.Drain(dctx); !errors.Is(err, ErrLocked) {
		t.Fatalf("Drain err = %v, want ErrLocked", err)
	}
	if calls := fc.callLog(); len(calls) != 0 {
		t.Errorf("calls = %v, want none while locked", calls)
	}
	row, err := st.Outbox().Get(ctx, id)
	if err != nil || row.State != store.StatePending {
		t.Errorf("row = %+v, %v, want still pending", row, err)
	}
}

func TestDrainReschedulesTheRowWhenTheContextEndsMidSend(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "One")
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	// The send outlives the deadline, the way a stalled request does when a command's wait runs out.
	fc := &fakeClient{updateTask: func(taskID string, u wrike.TaskUpdate) (wrike.Task, error) {
		time.Sleep(150 * time.Millisecond)
		return wrike.Task{}, context.DeadlineExceeded
	}}
	e := New(fc, st, Config{}, nil)
	dctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := e.Drain(dctx); err == nil {
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

func TestRunWaitsForTheLockBeforeResettingInflightRows(t *testing.T) {
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
