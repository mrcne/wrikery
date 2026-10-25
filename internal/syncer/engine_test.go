package syncer

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/lockfile"
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

	fc := &fakeClient{
		tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
			if p.FolderID == "F1" {
				return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T2", Title: "pulled", Status: "Active",
					UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}}}, nil
			}
			return wrike.TasksPage{}, nil
		},
		timelogs: func(p wrike.TimelogParams) (wrike.TimelogsPage, error) {
			return wrike.TimelogsPage{Timelogs: []wrike.Timelog{
				{ID: "L1", TaskID: "T2", UserID: "U1", TrackedDate: "2026-09-03", Hours: 1},
			}}, nil
		},
	}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour})

	waitFor(t, e, "outbox drained", func(ev Event) bool {
		return ev.Kind == EventOutboxChanged && ev.Pending == 0 && ev.Failed == 0
	})
	waitFor(t, e, "timelogs synced", func(ev Event) bool {
		return ev.Kind == EventStoreChanged && slices.Contains(ev.Entities, KindTimelogs)
	})
	waitFor(t, e, "idle after first cycle", isState(StateIdle))

	log := fc.callLog()
	// The account check comes first, it reads who the credential is and sends or pulls nothing.
	for len(log) > 0 && (log[0] == "Me" || log[0] == "Account") {
		log = log[1:]
	}
	if len(log) == 0 || log[0] != "CreateComment T1" {
		t.Fatalf("calls = %v, the drain must run before any pull", fc.callLog())
	}
	tasksCalls := 0
	for _, call := range log {
		if strings.HasPrefix(call, "Tasks") {
			tasksCalls++
		}
	}
	if tasksCalls != 2 {
		t.Errorf("Tasks calls = %d, want one per scope, the initial pull already saw every id the sweep needs", tasksCalls)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); err != nil {
		t.Errorf("pulled task missing: %v", err)
	}
	if id, err := st.GetMeta(ctx, store.MetaKeyMe); err != nil || id != "U1" {
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

// A request Wrike rejects is not a lost network: the store is fine and the other pulls went through,
// so the engine reports a failure rather than offline, and clears it on the next clean cycle.
func TestEngineReportsARejectedCycleAsFailed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject,
		Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}

	var fails atomic.Int32
	fails.Store(1)
	fc := &fakeClient{timelogs: func(p wrike.TimelogParams) (wrike.TimelogsPage, error) {
		if fails.Load() > 0 {
			fails.Add(-1)
			return wrike.TimelogsPage{}, &wrike.APIError{StatusCode: 400, Code: "invalid_parameter"}
		}
		return wrike.TimelogsPage{}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour,
		ReconnectBase: time.Millisecond, ReconnectCeil: 5 * time.Millisecond})

	waitFor(t, e, "failed", isState(StateFailed))
	waitFor(t, e, "recovery to idle", isState(StateIdle))
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

// After an outage the oldest write must go first, not at the end of a backoff the newer writes overtake.
func TestEngineSendsTheQueueInOrderAfterAnOutage(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"T1", "T2", "T3"} {
		seedTask(t, st, id, id)
		if _, err := st.Outbox().EnqueueComment(ctx, id, "U1", "queued"); err != nil {
			t.Fatal(err)
		}
	}
	// Wrike is down for the whole first cycle, the write and the pulls alike, and back for the second.
	var down atomic.Bool
	down.Store(true)
	fc := &fakeClient{
		createComment: func(taskID, text string) (wrike.Comment, error) {
			if down.Load() {
				return wrike.Comment{}, &wrike.APIError{StatusCode: 503}
			}
			return wrike.Comment{ID: "C" + taskID, TaskID: taskID, Text: text}, nil
		},
		tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
			if down.Swap(false) {
				return wrike.TasksPage{}, &wrike.APIError{StatusCode: 503}
			}
			return wrike.TasksPage{}, nil
		},
	}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour, BackoffBase: 2 * time.Second,
		ReconnectBase: time.Millisecond, ReconnectCeil: 5 * time.Millisecond})
	waitFor(t, e, "recovery to idle", isState(StateIdle))
	var sent []string
	for _, call := range fc.callLog() {
		if strings.HasPrefix(call, "CreateComment ") {
			sent = append(sent, strings.TrimPrefix(call, "CreateComment "))
		}
	}
	if strings.Join(sent, " ") != "T1 T1 T2 T3" {
		t.Errorf("comments sent in the order %v, want T1 again before the newer ones", sent)
	}
}

// A write whose own request Wrike never answers, a 504 for that one payload, while the pulls of the same cycle go through:
// Wrike is up and the failure is the write's own, so it gets its own wait and the rest of the queue and the pulls go on,
// instead of the whole sync stopping at it and the bar calling a working network offline.
func TestAWriteWrikeKeepsNotAnsweringGetsItsOwnWait(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "heavy")
	seedTask(t, st, "T2", "light")
	stuck, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "never answered")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T2", "U1", "fine"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{createComment: func(taskID, text string) (wrike.Comment, error) {
		if taskID == "T1" {
			return wrike.Comment{}, &wrike.APIError{StatusCode: 504}
		}
		return wrike.Comment{ID: "C" + taskID, TaskID: taskID, Text: text}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour, BackoffBase: time.Hour,
		ReconnectBase: time.Millisecond, ReconnectCeil: 5 * time.Millisecond})
	waitFor(t, e, "a cycle that went through", isState(StateIdle))
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Contains(fc.callLog(), "CreateComment T2") && time.Now().Before(deadline) {
		e.WakeOutbox()
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.Contains(fc.callLog(), "CreateComment T2") {
		t.Errorf("calls = %v, the write after the stuck one never went out", fc.callLog())
	}
	row, err := st.Outbox().Get(ctx, stuck)
	if err != nil || row.State != store.StatePending || row.Attempts == 0 || row.NextAttemptAt == "" {
		t.Errorf("stuck row = %+v, %v, want pending with a wait of its own", row, err)
	}
}

// An offline start sends nothing, but a row a dead process left in flight must still come back to the queue,
// or the changes screen shows it as being sent and refuses to retry or discard it.
func TestResetInflightPutsTheRowsBackUnderTheLock(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "a")
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "stranded")
	if err != nil {
		t.Fatal(err)
	}
	inflight := func() {
		t.Helper()
		if err := st.Outbox().MarkInflight(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	state := func() store.OutboxState {
		t.Helper()
		row, err := st.Outbox().Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return row.State
	}
	lockPath := filepath.Join(t.TempDir(), "sync.lock")
	log := slog.New(slog.DiscardHandler)

	inflight()
	if err := ResetInflight(ctx, st, lockPath, 2*time.Second, log); err != nil || state() != store.StatePending {
		t.Fatalf("reset = %v, row %s, want it pending again", err, state())
	}

	// Another process holds the lock and may be sending the row right now, so it is left alone.
	held, err := lockfile.Acquire(ctx, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	inflight()
	start := time.Now()
	if err := ResetInflight(ctx, st, lockPath, 200*time.Millisecond, log); err != nil || state() != store.StateInflight {
		t.Errorf("reset under a held lock = %v, row %s, want nil and the row left in flight", err, state())
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the reset waited %s, past its budget", time.Since(start))
	}
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

func TestEngineSkipsRejectedScope(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"F1", "F2"} {
		if err := st.Scopes().Upsert(ctx, store.Scope{ID: id, Kind: store.ScopeKindProject,
			Title: id, Followed: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Not returned by any scope. The sweep must not prune it while one scope is unreadable.
	seedTask(t, st, "T9", "cached before access was lost")

	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		switch p.FolderID {
		case "F1":
			return wrike.TasksPage{}, &wrike.APIError{StatusCode: 403, Code: "access_forbidden"}
		case "F2":
			return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T2", Title: "still readable", Status: "Active",
				UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}}}, nil
		}
		return wrike.TasksPage{}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour})

	waitFor(t, e, "idle despite the rejected scope", func(ev Event) bool {
		if ev.Kind == EventStateChanged && ev.State == StateOffline {
			t.Fatal("the engine went offline over one rejected scope")
		}
		return ev.Kind == EventStateChanged && ev.State == StateIdle
	})
	if _, err := st.Tasks().Get(ctx, "T2"); err != nil {
		t.Errorf("the readable scope was not pulled: %v", err)
	}
	if sc, err := st.Scopes().Get(ctx, "F2"); err != nil || sc.Cursor == "" {
		t.Errorf("scope F2 = %+v, %v, want a cursor", sc, err)
	}
	if _, err := st.Tasks().Get(ctx, "T9"); err != nil {
		t.Errorf("T9 was pruned from a partial union: %v", err)
	}
}

func TestEngineQuietCyclesEmitNoStoreHints(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject,
		Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if p.FolderID == "F1" && p.UpdatedAfter.IsZero() {
			return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1", Title: "one", Status: "Active",
				UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}}}, nil
		}
		return wrike.TasksPage{}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: 2 * time.Millisecond})
	waitFor(t, e, "first idle", isState(StateIdle))

	// The polls after that find nothing new, so the UI must not be told to re-read anything.
	deadline := time.After(50 * time.Millisecond)
	for {
		select {
		case ev := <-e.Events():
			if ev.Kind == EventStoreChanged {
				t.Fatalf("store hint for %v on a cycle that pulled nothing", ev.Entities)
			}
		case <-deadline:
			return
		}
	}
}

// The start of the interface is a manual cycle too, but only a refresh the user asked for, R or sync --full, asks for the Effort field again.
func TestEffortIsAskedAgainOnARefreshAlone(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject, Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta(ctx, store.MetaKeyEffortUnavailable, "1"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var asked [][]string
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		mu.Lock()
		asked = append(asked, p.Fields)
		mu.Unlock()
		return wrike.TasksPage{}, nil
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour, PullEffort: true})
	waitFor(t, e, "first idle", isState(StateIdle))
	mu.Lock()
	for _, f := range asked {
		if slices.Contains(f, effortField) {
			t.Errorf("the start asked for the effort although the row says the account has none: %q", asked)
		}
	}
	before := len(asked)
	mu.Unlock()
	e.Refresh()
	waitFor(t, e, "syncing after the refresh", isState(StateSyncing))
	waitFor(t, e, "idle after the refresh", isState(StateIdle))
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, f := range asked[before:] {
		found = found || slices.Contains(f, effortField)
	}
	if !found {
		t.Errorf("the refresh did not ask for the effort again: %q", asked[before:])
	}
}

// A new task field, the Effort field turned on in the config for example, is read for every cached task once: the cursors are cleared
// when the set of fields the pulls ask for differs from the one the last run remembered.
func TestPullStartsOverWhenTheTaskFieldsChange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject, Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	// Every pull answers one task, so the scope gets a cursor and the next pull is incremental unless something cleared it.
	var since []time.Time
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if p.FolderID == "F1" {
			since = append(since, p.UpdatedAfter)
		}
		return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1", Title: "t", Status: "Active", UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)}}}, nil
	}}
	if _, err := New(fc, st, Config{}, slog.New(slog.DiscardHandler)).Once(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(since) != 1 {
		t.Fatalf("the first run pulled %d times, want once", len(since))
	}
	if _, err := New(fc, st, Config{PullEffort: true}, slog.New(slog.DiscardHandler)).Once(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(since) != 2 || !since[1].IsZero() {
		t.Fatalf("a run with a new field must pull from the start, since = %v", since)
	}
	if _, err := New(fc, st, Config{PullEffort: true}, slog.New(slog.DiscardHandler)).Once(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(since) != 3 || since[2].IsZero() {
		t.Errorf("the same set again must stay incremental, since = %v", since)
	}
}
