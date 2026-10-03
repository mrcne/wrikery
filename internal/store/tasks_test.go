package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
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

func TestListInFolderWalksDescendantsAndSorts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Folders().ReplaceTree(ctx, []Folder{
		{ID: "S1", Title: "Space", Space: true, ChildIDs: []string{"F1"}},
		{ID: "F1", Title: "Proj", ChildIDs: []string{"F2"}},
		{ID: "F2", Title: "Sub"},
		{ID: "X", Title: "Other"},
	}); err != nil {
		t.Fatal(err)
	}
	tasks := []Task{
		{ID: "done", Title: "Done", Status: "Completed", ParentIDs: []string{"F1"}, Dates: &TaskDates{Type: "Planned", Due: "2026-09-01"}, UpdatedDate: "2026-09-05T00:00:00Z", Description: "<p>hidden</p>"},
		{ID: "late", Title: "Late", Status: "Active", ParentIDs: []string{"F2"}, Dates: &TaskDates{Type: "Planned", Due: "2026-09-02"}, UpdatedDate: "2026-09-01T00:00:00Z", ResponsibleIDs: []string{"U2", "U1"}},
		{ID: "soon", Title: "Soon", Status: "Active", ParentIDs: []string{"F1"}, Dates: &TaskDates{Type: "Planned", Due: "2026-09-09"}, UpdatedDate: "2026-09-04T00:00:00Z"},
		{ID: "nodate", Title: "No date", Status: "Active", ParentIDs: []string{"F1"}, UpdatedDate: "2026-09-03T00:00:00Z"},
		{ID: "both", Title: "In two folders", Status: "Active", ParentIDs: []string{"F1", "F2"}, UpdatedDate: "2026-09-02T00:00:00Z"},
		{ID: "out", Title: "Elsewhere", Status: "Active", ParentIDs: []string{"X"}},
	}
	if err := st.Tasks().Upsert(ctx, tasks); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().ListInFolder(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range got {
		ids = append(ids, task.ID)
	}
	want := []string{"late", "soon", "nodate", "both", "done"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
	if got[0].ResponsibleIDs == nil || len(got[0].ResponsibleIDs) != 2 {
		t.Errorf("responsibles not loaded: %+v", got[0])
	}
	if got[4].Description != "" {
		t.Error("list query must not load descriptions")
	}
}

// A Backlog task carries a dates block with a type and nothing else, see https://developers.wrike.com/api/v4/tasks/.
// Stored as an empty string it would sort ahead of every real due date, so the empty ends have to reach the column as NULL.
func TestBacklogTaskKeepsItsTypeAndSortsWithTheUndated(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{
		{ID: "backlog", Title: "Backlog", Status: "Active", ParentIDs: []string{"F1"},
			Dates: &TaskDates{Type: "Backlog"}, UpdatedDate: "2026-09-04T00:00:00Z"},
		{ID: "planned", Title: "Planned", Status: "Active", ParentIDs: []string{"F1"},
			Dates: &TaskDates{Type: "Planned", Due: "2026-09-07"}, UpdatedDate: "2026-09-01T00:00:00Z"},
		{ID: "nodates", Title: "No dates block", Status: "Active", ParentIDs: []string{"F1"},
			UpdatedDate: "2026-09-03T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().ListInFolder(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range got {
		ids = append(ids, task.ID)
	}
	want := []string{"planned", "backlog", "nodates"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v, the dated task first and the undated ones after it", ids, want)
	}
	one, err := st.Tasks().Get(ctx, "backlog")
	if err != nil {
		t.Fatal(err)
	}
	if one.Dates == nil || one.Dates.Type != "Backlog" || one.Dates.Start != "" || one.Dates.Due != "" {
		t.Errorf("dates = %+v, want type Backlog with no start and no due", one.Dates)
	}
}

func TestListForResponsible(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{
		{ID: "mine", Title: "Mine", Status: "Active", ResponsibleIDs: []string{"ME"}},
		{ID: "shared", Title: "Shared", Status: "Active", ResponsibleIDs: []string{"ME", "U2"}},
		{ID: "theirs", Title: "Theirs", Status: "Active", ResponsibleIDs: []string{"U2"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().ListForResponsible(ctx, "ME")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %d tasks, %v", len(got), err)
	}
	for _, task := range got {
		if task.ID == "theirs" {
			t.Error("theirs listed")
		}
	}
}

func TestListRowsCarryParentIDs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Folders().ReplaceTree(ctx, []Folder{
		{ID: "S1", Title: "Space", Space: true, ChildIDs: []string{"F1", "F2"}},
		{ID: "F1", Title: "One"},
		{ID: "F2", Title: "Two"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Upsert(ctx, []Task{
		{ID: "both", Title: "In two folders", Status: "Active", ParentIDs: []string{"F2", "F1"}, ResponsibleIDs: []string{"ME"}},
		{ID: "one", Title: "In one", Status: "Active", ParentIDs: []string{"F1"}, ResponsibleIDs: []string{"ME"}},
	}); err != nil {
		t.Fatal(err)
	}
	inFolder, err := st.Tasks().ListInFolder(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := st.Tasks().ListForResponsible(ctx, "ME")
	if err != nil {
		t.Fatal(err)
	}
	for _, rows := range [][]Task{inFolder, mine} {
		for _, task := range rows {
			want := []string{"F1"}
			if task.ID == "both" {
				want = []string{"F1", "F2"}
			}
			if !reflect.DeepEqual(task.ParentIDs, want) {
				t.Errorf("%s: ParentIDs = %v, want %v", task.ID, task.ParentIDs, want)
			}
		}
	}
}

func TestPruneExceptKeepsLocalTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a"), makeTask("T2", "b")}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "new"}, "")
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.Tasks().PruneExcept(ctx, []string{"T1"})
	if err != nil || n != 1 {
		t.Fatalf("pruned = %d, %v, want only T2", n, err)
	}
	if _, err := st.Tasks().Get(ctx, LocalID(id)); err != nil {
		t.Errorf("the unconfirmed task was pruned: %v", err)
	}
}

func TestRecentlyOpenedIDsSkipsLocalTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "a")}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "new"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, LocalID(id), "2026-10-01T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T1", "2026-10-01T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	ids, err := st.Tasks().RecentlyOpenedIDs(ctx, "2026-09-30T00:00:00Z", 10)
	if err != nil || len(ids) != 1 || ids[0] != "T1" {
		t.Errorf("ids = %v, %v, want only T1, the local task has no thread on the server", ids, err)
	}
}

func TestFindByTitleFoldsCaseAndDiacriticsLikeTheSearch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	err := st.Tasks().Upsert(ctx, []Task{
		makeTask("T1", "Plan \u0141\u00f3d\u017a"),
		makeTask("T2", "\u015awi\u0119ty list"),
		makeTask("T3", "Write docs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := func(fragment string) []string {
		t.Helper()
		got, err := st.Tasks().FindByTitle(ctx, fragment, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, task := range got {
			out = append(out, task.ID)
		}
		return out
	}
	for fragment, want := range map[string]string{
		"\u0142\u00f3d\u017a": "T1",
		"\u0142odz":           "T1",
		"\u015aWIETY":         "T2",
		"swiety":              "T2",
		"DOCS":                "T3",
	} {
		if got := ids(fragment); len(got) != 1 || got[0] != want {
			t.Errorf("FindByTitle(%q) = %v, want %s alone", fragment, got, want)
		}
	}
	// The search index does not fold the l with stroke into l, so the lookup must not either.
	got, err := st.Tasks().Search(ctx, "lodz", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || len(ids("lodz")) != 0 {
		t.Errorf("lodz: search found %d, lookup found %v, want both to find nothing", len(got), ids("lodz"))
	}
}

func TestFindByTitleTreatsWildcardsAsCharactersAndKeepsListOrderAndLimit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	old := makeTask("T1", "Login copy 100% done")
	fresh := makeTask("T2", "Fix the login page")
	fresh.UpdatedDate = "2026-09-05T10:00:00Z"
	done := makeTask("T3", "Login archive")
	done.Status = "Completed"
	done.UpdatedDate = "2026-09-09T10:00:00Z"
	if err := st.Tasks().Upsert(ctx, []Task{old, fresh, done}); err != nil {
		t.Fatal(err)
	}
	for fragment, want := range map[string]int{"100%": 1, "l_gin": 0, "%": 1, "_": 0} {
		got, err := st.Tasks().FindByTitle(ctx, fragment, 10)
		if err != nil || len(got) != want {
			t.Errorf("FindByTitle(%q) = %d rows, %v, want %d", fragment, len(got), err, want)
		}
	}
	got, err := st.Tasks().FindByTitle(ctx, "login", 10)
	if err != nil || len(got) != 3 || got[0].ID != "T2" || got[1].ID != "T1" || got[2].ID != "T3" {
		t.Errorf("order = %+v, %v, want T2 T1 T3: open tasks first, newest change first, done last", got, err)
	}
	got, err = st.Tasks().FindByTitle(ctx, "login", 2)
	if err != nil || len(got) != 2 || got[0].ID != "T2" || got[1].ID != "T1" {
		t.Errorf("limit 2 = %+v, %v", got, err)
	}
}

func TestFindByTitleFillsTheDescription(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "Fix the login page")}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().FindByTitle(ctx, "login", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].Description != "<p>body of Fix the login page</p>" {
		t.Errorf("description = %q", got[0].Description)
	}
}

func TestByPermalinkIDMatchesTheNumberOnAnyHost(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	eu, com, long := makeTask("EU", "On the eu host"), makeTask("COM", "On the main host"), makeTask("LONG", "Number with a longer neighbour")
	eu.Permalink = "https://app-eu.wrike.com/open.htm?id=4552825748"
	com.Permalink = "https://www.wrike.com/open.htm?id=77"
	long.Permalink = "https://www.wrike.com/open.htm?id=12"
	if err := st.Tasks().Upsert(ctx, []Task{eu, com, long}); err != nil {
		t.Fatal(err)
	}
	for number, want := range map[string]string{"4552825748": "EU", "77": "COM", "12": "LONG"} {
		got, err := st.Tasks().ByPermalinkID(ctx, number)
		if err != nil || got.ID != want {
			t.Errorf("ByPermalinkID(%s) = %q, %v, want %s", number, got.ID, err, want)
		}
		if err == nil && got.Description == "" {
			t.Errorf("ByPermalinkID(%s) came without the description", number)
		}
	}
	for _, number := range []string{"1", "2", "99", "552825748"} {
		if _, err := st.Tasks().ByPermalinkID(ctx, number); !errors.Is(err, ErrNotFound) {
			t.Errorf("ByPermalinkID(%s) error = %v, want ErrNotFound", number, err)
		}
	}
}
