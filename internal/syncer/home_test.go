package syncer

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

var acme = store.Home{AccountID: "ACC1", AccountName: "Acme", Host: "app-eu.wrike.com", ContactID: "U1"}

func TestMeetHomeAdoptsALegacyCache(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.SetMeta(ctx, store.MetaKeyMe, "U1"); err != nil {
		t.Fatal(err)
	}
	met, m, err := MeetHome(ctx, &fakeClient{}, st, "www.wrike.com")
	if err != nil || m != store.MeetAdopt {
		t.Fatalf("meet = %v, %v, want adopt", m, err)
	}
	if h, _ := st.Home(ctx); h != met || h.AccountID != "ACC1" || h.Host != "www.wrike.com" || h.ContactID != "U1" {
		t.Errorf("record = %+v, met %+v", h, met)
	}
}

func TestMeetHomeAsksOnAnotherHost(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.AdoptHome(ctx, store.Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
	_, m, err := MeetHome(ctx, &fakeClient{}, st, "app-eu.wrike.com")
	if err != nil || m != store.MeetAsk {
		t.Fatalf("meet = %v, %v, want ask", m, err)
	}
	if h, _ := st.Home(ctx); h.Host != "www.wrike.com" {
		t.Errorf("a question wrote the record: %+v", h)
	}
}

// moved puts the cache on acme with the marker set, the state right after the user's yes.
func moved(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.MoveHome(context.Background(), acme); err != nil {
		t.Fatal(err)
	}
}

func answering(tasks ...wrike.Task) func(ids, fields []string) ([]wrike.Task, error) {
	return func(ids, fields []string) ([]wrike.Task, error) {
		var out []wrike.Task
		for _, task := range tasks {
			if slices.Contains(ids, task.ID) {
				out = append(out, task)
			}
		}
		return out, nil
	}
}

func rowState(t *testing.T, st *store.Store, id int64) store.OutboxRow {
	t.Helper()
	row, err := st.Outbox().Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestCheckQueueHoldsAMissingTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "Release notes")
	seedTask(t, st, "T2", "Budget")
	first, _ := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one")
	second, _ := st.Outbox().EnqueueComment(ctx, "T2", "U1", "two")
	fc := &fakeClient{tasksByIDs: answering(wrike.Task{ID: "T1", Title: "Release notes"})}
	if err := checkQueue(ctx, fc, st, acme); err != nil {
		t.Fatal(err)
	}
	if row := rowState(t, st, first); row.State != store.StatePending {
		t.Errorf("T1's comment = %s, want pending", row.State)
	}
	if row := rowState(t, st, second); row.State != store.StateFailed || row.LastError != "not found in Acme on app-eu.wrike.com" {
		t.Errorf("T2's comment = %s %q, want held as not found", row.State, row.LastError)
	}
}

func TestCheckQueueHoldsADifferentTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "Release notes")
	id, _ := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one")
	fc := &fakeClient{tasksByIDs: answering(wrike.Task{ID: "T1", Title: "Budget"})}
	if err := checkQueue(ctx, fc, st, acme); err != nil {
		t.Fatal(err)
	}
	if row := rowState(t, st, id); row.State != store.StateFailed || row.LastError != "a different task in Acme on app-eu.wrike.com" {
		t.Errorf("row = %s %q, want held as a different task", row.State, row.LastError)
	}
}

// The comment's own task title is the new name, Wrike holds the old one until the rename goes out.
func TestCheckQueueTakesARenamedTasksComment(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "Old")
	rename, _ := st.Outbox().EnqueueTaskUpdate(ctx, "T1", store.TaskUpdatePayload{Title: "New"})
	comment, _ := st.Outbox().EnqueueComment(ctx, "T1", "U1", "after the rename")
	fc := &fakeClient{tasksByIDs: answering(wrike.Task{ID: "T1", Title: "Old"})}
	if err := checkQueue(ctx, fc, st, acme); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{rename, comment} {
		if row := rowState(t, st, id); row.State != store.StatePending {
			t.Errorf("row %d = %s %q, want pending", id, row.State, row.LastError)
		}
	}
}

func TestCheckQueueHoldsAnEntryOfAnotherUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "Release notes")
	if err := st.Timelogs().Upsert(ctx, []store.Timelog{{ID: "L1", TaskID: "T1", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1}}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTimelogUpdate(ctx, "L1", store.TimelogUpdatePayload{Hours: 2})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{taskTimelogs: func(taskID string) ([]wrike.Timelog, error) {
		return []wrike.Timelog{{ID: "L1", TaskID: "T1", UserID: "U9"}}, nil
	}}
	if err := checkQueue(ctx, fc, st, acme); err != nil {
		t.Fatal(err)
	}
	if row := rowState(t, st, id); row.State != store.StateFailed || row.LastError != "not found in Acme on app-eu.wrike.com" {
		t.Errorf("row = %s %q, want held, the entry is somebody else's", row.State, row.LastError)
	}
}

func TestCheckQueueCoversFailedRows(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T2", "Budget")
	id, _ := st.Outbox().EnqueueComment(ctx, "T2", "U1", "two")
	if err := st.Outbox().Fail(ctx, id, "wrike: http 400"); err != nil {
		t.Fatal(err)
	}
	if err := checkQueue(ctx, &fakeClient{}, st, acme); err != nil {
		t.Fatal(err)
	}
	if row := rowState(t, st, id); row.LastError != "not found in Acme on app-eu.wrike.com" {
		t.Errorf("failed row = %q, want the not found text, r would send it later", row.LastError)
	}
}

func TestCheckQueueHoldsAMissingFolder(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F9", store.TaskCreatePayload{Title: "New one"})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{folderTree: func() ([]wrike.Folder, error) { return []wrike.Folder{{ID: "F1", Title: "Alpha"}}, nil }}
	if err := checkQueue(ctx, fc, st, acme); err != nil {
		t.Fatal(err)
	}
	if row := rowState(t, st, id); row.State != store.StateFailed || row.LastError != "not found in Acme on app-eu.wrike.com" {
		t.Errorf("row = %s %q, want held, the folder is not in the account", row.State, row.LastError)
	}
}

// A write command drains on an engine of its own, the marker in the store makes it run the check too.
func TestDrainRunsTheCheckOnceUnderTheMarker(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "Release notes")
	moved(t, st)
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "one"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{tasksByIDs: answering(wrike.Task{ID: "T1", Title: "Release notes"})}
	if err := New(fc, st, Config{}, slog.New(slog.DiscardHandler)).Drain(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.callLog(), "TasksByIDs T1") || !slices.Contains(fc.callLog(), "CreateComment T1") {
		t.Errorf("calls = %v, want the check and then the comment", fc.callLog())
	}
	if pending, _ := st.SwitchPending(ctx); pending {
		t.Error("the marker stayed after the check ran through")
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "two"); err != nil {
		t.Fatal(err)
	}
	again := &fakeClient{}
	if err := New(again, st, Config{}, slog.New(slog.DiscardHandler)).Drain(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(again.callLog(), "TasksByIDs T1") {
		t.Errorf("calls = %v, the check ran a second time", again.callLog())
	}
}

// A quit between the yes and the first pass must not lose the check.
func TestTheMarkerSurvivesANewEngine(t *testing.T) {
	st := newTestStore(t)
	seedTask(t, st, "T1", "Release notes")
	moved(t, st)
	if _, err := st.Outbox().EnqueueComment(context.Background(), "T1", "U1", "one"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_ = New(&fakeClient{}, st, Config{}, slog.New(slog.DiscardHandler)).Drain(cancelled, 0)
	fc := &fakeClient{tasksByIDs: answering(wrike.Task{ID: "T1", Title: "Release notes"})}
	if err := New(fc, st, Config{}, slog.New(slog.DiscardHandler)).Drain(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fc.callLog(), "TasksByIDs T1") {
		t.Errorf("calls = %v, the second engine skipped the check", fc.callLog())
	}
}

func TestEngineStopsOnAnotherAccount(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "Release notes")
	if err := st.SetMeta(ctx, store.MetaKeyMe, "U1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "queued offline"); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{me: func() (wrike.Contact, error) { return wrike.Contact{ID: "U2", Me: true}, nil }}
	e := startEngine(t, fc, st, Config{Host: "www.wrike.com", PollInterval: time.Hour})
	ev := waitFor(t, e, "another account", isState(StateAnotherAccount))
	if ev.State != StateAnotherAccount {
		t.Fatal(ev)
	}
	for _, call := range fc.callLog() {
		if call == "CreateComment T1" || call == "Tasks " {
			t.Errorf("calls = %v, nothing may be sent or pulled", fc.callLog())
		}
	}
	var another *AnotherAccountError
	if !errors.As(&AnotherAccountError{Met: acme}, &another) || another.Error() == "" {
		t.Error("AnotherAccountError does not read as itself")
	}
}

func TestEngineChecksEveryStartUnderCheckHome(t *testing.T) {
	st := newTestStore(t)
	if err := st.AdoptHome(context.Background(), store.Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{}
	e := startEngine(t, fc, st, Config{Host: "www.wrike.com", CheckHome: true, PollInterval: time.Hour})
	waitFor(t, e, "a synced first cycle", isState(StateIdle))
	n := 0
	for _, call := range fc.callLog() {
		if call == "Account" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Account read %d times, want once at the start: %v", n, fc.callLog())
	}
}
