package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func makeTask(id, title string) Task {
	return Task{
		ID:          id,
		Title:       title,
		Description: "<p>body of " + title + "</p>",
		Status:      "Active",
		CreatedDate: "2026-09-01T10:00:00Z",
		UpdatedDate: "2026-09-01T10:00:00Z",
	}
}

func mustUpsertScope(t *testing.T, st *Store, id, kind string) {
	t.Helper()
	if err := st.Scopes().Upsert(context.Background(),
		Scope{ID: id, Kind: kind, Title: id, Followed: true}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPageUpsertsAndAdvancesCursor(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mustUpsertScope(t, st, "F1", ScopeKindProject)

	a := makeTask("T1", "first")
	a.ResponsibleIDs = []string{"U2", "U1"}
	a.ParentIDs = []string{"F1"}
	a.Dates = &TaskDates{Type: "Planned", Duration: 480, Start: "2026-09-01T09:00:00", Due: "2026-09-02T17:00:00"}

	// Intermediate page, cursor must stay empty.
	if err := st.Tasks().ApplyPage(ctx, "F1", []Task{a}, ""); err != nil {
		t.Fatal(err)
	}
	s, err := st.Scopes().Get(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Cursor != "" || s.LastSyncedAt != "" {
		t.Errorf("scope after intermediate page = %+v, want empty cursor and last synced", s)
	}

	// Final page advances the cursor and stamps last_synced_at.
	if err := st.Tasks().ApplyPage(ctx, "F1", []Task{makeTask("T2", "second")}, "2026-09-01T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	s, err = st.Scopes().Get(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Cursor != "2026-09-01T10:00:00Z" || s.LastSyncedAt == "" {
		t.Errorf("scope after final page = %+v, want cursor set and last synced stamped", s)
	}

	got, err := st.Tasks().Get(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "first" || got.Description != "<p>body of first</p>" {
		t.Errorf("task = %+v", got)
	}
	if got.DescriptionPlain != "body of first" {
		t.Errorf("description_plain = %q, want stripped text", got.DescriptionPlain)
	}
	if len(got.ResponsibleIDs) != 2 || got.ResponsibleIDs[0] != "U1" {
		t.Errorf("responsibles = %v, want sorted [U1 U2]", got.ResponsibleIDs)
	}
	if got.Dates == nil || got.Dates.Due != "2026-09-02T17:00:00" {
		t.Errorf("dates = %+v", got.Dates)
	}
}

func TestApplyPageUnknownScope(t *testing.T) {
	st := newTestStore(t)
	err := st.Tasks().ApplyPage(context.Background(), "ghost", []Task{makeTask("T1", "x")}, "2026-09-01T10:00:00Z")
	if err == nil {
		t.Fatal("want error for unknown scope")
	}
}

func TestUpsertReplacesJoinRowsAndKeepsLastOpened(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	a := makeTask("T1", "first")
	a.ResponsibleIDs = []string{"U1", "U2"}
	a.ParentIDs = []string{"F1", "F2"}
	if err := st.Tasks().Upsert(ctx, []Task{a}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T1", "2026-09-03T08:00:00Z"); err != nil {
		t.Fatal(err)
	}

	a.ResponsibleIDs = []string{"U2", "U3"}
	a.ParentIDs = []string{"F2"}
	a.Title = "renamed"
	if err := st.Tasks().Upsert(ctx, []Task{a}); err != nil {
		t.Fatal(err)
	}

	got, err := st.Tasks().Get(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "renamed" {
		t.Errorf("title = %q", got.Title)
	}
	if len(got.ResponsibleIDs) != 2 || got.ResponsibleIDs[0] != "U2" || got.ResponsibleIDs[1] != "U3" {
		t.Errorf("responsibles = %v, want [U2 U3]", got.ResponsibleIDs)
	}
	if len(got.ParentIDs) != 1 || got.ParentIDs[0] != "F2" {
		t.Errorf("parents = %v, want [F2]", got.ParentIDs)
	}
	if got.LastOpenedAt != "2026-09-03T08:00:00Z" {
		t.Errorf("last opened = %q, a sync upsert must not clear it", got.LastOpenedAt)
	}
	if got.Dates != nil {
		t.Errorf("dates = %+v, want nil for a task without dates", got.Dates)
	}
}

func TestGetMissingTask(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Tasks().Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestPruneExceptDeletesTheRest(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a"), makeTask("T2", "b"), makeTask("T3", "c")}); err != nil {
		t.Fatal(err)
	}
	n, err := st.Tasks().PruneExcept(ctx, []string{"T1", "T3"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned = %d, want 1", n)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("T2 still there after prune: %v", err)
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Errorf("T1 gone after prune: %v", err)
	}
}

func TestRecentlyOpenedIDs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a"), makeTask("T2", "b"), makeTask("T3", "c")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T1", "2026-09-01T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T2", "2026-09-03T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	ids, err := st.Tasks().RecentlyOpenedIDs(ctx, "2026-08-30T00:00:00Z", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "T2" || ids[1] != "T1" {
		t.Errorf("ids = %v, want [T2 T1] newest first", ids)
	}
	ids, err = st.Tasks().RecentlyOpenedIDs(ctx, "2026-09-02T00:00:00Z", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "T2" {
		t.Errorf("ids = %v, want [T2] after the since filter", ids)
	}
}
