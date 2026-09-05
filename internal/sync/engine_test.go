package sync

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func startEngine(t *testing.T, fc *fakeClient, st *store.Store, cfg Config) *Engine {
	t.Helper()
	e := New(fc, st, cfg, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = e.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return e
}

func waitFor(t *testing.T, e *Engine, desc string, want func(Event) bool) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-e.Events():
			if want(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", desc)
		}
	}
}

func isState(s SyncState) func(Event) bool {
	return func(ev Event) bool { return ev.Kind == EventStateChanged && ev.State == s }
}

func TestEngineFirstCycleDrainsAndSyncs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject,
		Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	seedTask(t, st, "T1", "seeded")
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "queued before start"); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if p.FolderID == "F1" {
			return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T2", Title: "pulled", Status: "Active",
				UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}}}, nil
		}
		return wrike.TasksPage{}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour})

	waitFor(t, e, "outbox drained", func(ev Event) bool {
		return ev.Kind == EventOutboxChanged && ev.Pending == 0 && ev.Failed == 0
	})
	waitFor(t, e, "idle after first cycle", isState(StateIdle))

	log := fc.callLog()
	if len(log) == 0 || log[0] != "CreateComment T1" {
		t.Fatalf("calls = %v, the drain must run before any pull", log)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); err != nil {
		t.Errorf("pulled task missing: %v", err)
	}
	if id, err := st.GetMeta(ctx, metaKeyMe); err != nil || id != "U1" {
		t.Errorf("me meta = %q, %v", id, err)
	}
	scopes, err := st.Scopes().Followed(ctx)
	if err != nil || len(scopes) != 2 {
		t.Fatalf("scopes = %+v, %v, want F1 plus the me row", scopes, err)
	}
	sc, err := st.Scopes().Get(ctx, "F1")
	if err != nil || sc.Cursor == "" {
		t.Errorf("scope = %+v, %v, want a cursor after the first cycle", sc, err)
	}
}

func TestEngineGoesOfflineAndRecovers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject,
		Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}

	var fails atomic.Int32
	fails.Store(2)
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if fails.Load() > 0 {
			fails.Add(-1)
			return wrike.TasksPage{}, &wrike.APIError{StatusCode: 503, Code: "server_error"}
		}
		return wrike.TasksPage{}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour,
		ReconnectBase: time.Millisecond, ReconnectCeil: 5 * time.Millisecond})

	waitFor(t, e, "offline", isState(StateOffline))
	waitFor(t, e, "recovery to idle", isState(StateIdle))
}

func TestEngine401PausesUntilRefresh(t *testing.T) {
	st := newTestStore(t)
	var broken atomic.Bool
	broken.Store(true)
	fc := &fakeClient{me: func() (wrike.Contact, error) {
		if broken.Load() {
			return wrike.Contact{}, &wrike.APIError{StatusCode: 401, Code: "not_authorized"}
		}
		return wrike.Contact{ID: "U1", Me: true}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: 5 * time.Millisecond,
		ReconnectBase: time.Millisecond, ReconnectCeil: 2 * time.Millisecond})

	waitFor(t, e, "auth required", isState(StateAuthRequired))
	calls := len(fc.callLog())
	time.Sleep(50 * time.Millisecond)
	if now := len(fc.callLog()); now != calls {
		t.Fatalf("calls went from %d to %d while paused, the engine must not poll without a token", calls, now)
	}

	broken.Store(false)
	e.Refresh()
	waitFor(t, e, "idle after refresh", isState(StateIdle))
}

func TestEngineCursorResumeAfterRestart(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject,
		Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	updated := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)

	fc1 := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if p.FolderID == "F1" {
			return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1", Title: "one", Status: "Active",
				UpdatedDate: updated}}}, nil
		}
		return wrike.TasksPage{}, nil
	}}
	e1 := New(fc1, st, Config{PollInterval: time.Hour}, slog.New(slog.DiscardHandler))
	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan struct{})
	go func() { _ = e1.Run(ctx1); close(done1) }()
	waitFor(t, e1, "first engine idle", isState(StateIdle))
	cancel1()
	<-done1

	var mu sync.Mutex
	var after []time.Time
	fc2 := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if p.FolderID == "F1" {
			mu.Lock()
			after = append(after, p.UpdatedAfter)
			mu.Unlock()
		}
		return wrike.TasksPage{}, nil
	}}
	e2 := startEngine(t, fc2, st, Config{PollInterval: time.Hour})
	waitFor(t, e2, "second engine idle", isState(StateIdle))

	mu.Lock()
	defer mu.Unlock()
	if len(after) == 0 || !after[0].Equal(updated) {
		t.Fatalf("updated after = %v, want the cursor %v from the previous run", after, updated)
	}
}

func TestEngineWakeDrainsPromptly(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")
	// The only followed scope is the default "me" scope, and the first cycle sweeps it (manual refresh).
	// Report T1 as still live there so the sweep does not prune the task this test is about to comment on,
	// which is not what this test means to exercise.
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1", Title: "a", Status: "Active",
			UpdatedDate: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)}}}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour})
	waitFor(t, e, "initial idle", isState(StateIdle))

	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello"); err != nil {
		t.Fatal(err)
	}
	e.WakeOutbox()
	waitFor(t, e, "queue drained after wake", func(ev Event) bool {
		return ev.Kind == EventOutboxChanged && ev.Pending == 0
	})
	found := false
	for _, call := range fc.callLog() {
		if call == "CreateComment T1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls = %v, the wake must trigger a drain without waiting for the poll", fc.callLog())
	}
}
