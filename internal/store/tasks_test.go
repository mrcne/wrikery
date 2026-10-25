package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "new"})
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
	id, err := st.Outbox().EnqueueTaskCreate(ctx, "F1", TaskCreatePayload{Title: "new"})
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

func TestFindByTitleMatchesNothingForAFragmentThatFoldsToEmpty(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "Plan")}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().FindByTitle(ctx, "\u0301", 10)
	if err != nil || len(got) != 0 {
		t.Errorf("FindByTitle(combining mark) = %v, %v, want no rows", got, err)
	}
}

func TestUpsertStoresTheRelationsAndReplacesThem(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a := makeTask("T1", "child")
	a.SuperTaskIDs = []string{"P2", "P1"}
	a.DependencyIDs = []string{"X"}
	a.AttachmentCount = 2
	if err := st.Tasks().Upsert(ctx, []Task{a}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().Get(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SuperTaskIDs) != 2 || got.SuperTaskIDs[0] != "P1" || got.SuperTaskIDs[1] != "P2" {
		t.Errorf("super tasks = %v, want [P1 P2]", got.SuperTaskIDs)
	}
	if len(got.DependencyIDs) != 1 || got.DependencyIDs[0] != "X" {
		t.Errorf("dependency ids = %v, want [X]", got.DependencyIDs)
	}
	if got.AttachmentCount != 2 {
		t.Errorf("attachment count = %d, want 2", got.AttachmentCount)
	}

	a.SuperTaskIDs, a.DependencyIDs, a.AttachmentCount = []string{"P2"}, nil, 0
	if err := st.Tasks().Upsert(ctx, []Task{a}); err != nil {
		t.Fatal(err)
	}
	got, err = st.Tasks().Get(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SuperTaskIDs) != 1 || got.SuperTaskIDs[0] != "P2" || len(got.DependencyIDs) != 0 || got.AttachmentCount != 0 {
		t.Errorf("after the second upsert = %+v, want the relations replaced", got)
	}
}

func TestSubtasksListsTheChildrenInListOrder(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	p, q := makeTask("P", "parent"), makeTask("Q", "other parent")
	c1, c2, c3 := makeTask("C1", "done child"), makeTask("C2", "open child"), makeTask("C3", "elsewhere")
	c1.Status = "Completed"
	c1.SuperTaskIDs, c2.SuperTaskIDs, c3.SuperTaskIDs = []string{"P"}, []string{"P"}, []string{"Q"}
	if err := st.Tasks().Upsert(ctx, []Task{p, q, c1, c2, c3}); err != nil {
		t.Fatal(err)
	}
	subs, err := st.Tasks().Subtasks(ctx, "P")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 || subs[0].ID != "C2" || subs[1].ID != "C1" {
		t.Errorf("subtasks of P = %+v, want the open child first then the done one", subs)
	}
	subs, err = st.Tasks().Subtasks(ctx, "C1")
	if err != nil || len(subs) != 0 {
		t.Errorf("subtasks of a leaf = %+v, %v", subs, err)
	}
}

func TestByIDsReturnsTheCachedTasksInListOrder(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a, b := makeTask("A", "done one"), makeTask("B", "open one")
	a.Status = "Completed"
	if err := st.Tasks().Upsert(ctx, []Task{a, b, makeTask("C", "unasked")}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().ByIDs(ctx, []string{"A", "B", "GONE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "B" || got[1].ID != "A" {
		t.Errorf("by ids = %+v, want the open task first and the unknown id left out", got)
	}
	if got, err := st.Tasks().ByIDs(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("by no ids = %+v, %v", got, err)
	}
}

func TestRelatedIDsNamesEveryOtherEndOnce(t *testing.T) {
	task := Task{ID: "T1", SuperTaskIDs: []string{"P1", "P2"}}
	deps := []Dependency{
		{ID: "X", PredecessorID: "T2", SuccessorID: "T1"},
		{ID: "Y", PredecessorID: "T1", SuccessorID: "T3"},
		{ID: "Z", PredecessorID: "T2", SuccessorID: "T1"},
	}
	got := RelatedIDs(task, deps)
	want := []string{"P1", "P2", "T2", "T3"}
	if len(got) != len(want) {
		t.Fatalf("related = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("related = %v, want %v", got, want)
		}
	}
}

func TestUpsertReachedStampsAndTheSweepKeepsAFreshStamp(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	ago := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T1", "fresh")}, ago(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T2", "stale")}, ago(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Tasks().PruneExcept(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Errorf("T1 with a one day old stamp was pruned: %v", err)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); err == nil {
		t.Error("T2 survived with a stamp of thirty one days")
	}
}

func TestApplyPageClearsTheStampAndUpsertKeepsIt(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mustUpsertScope(t, st, "F1", ScopeKindProject)
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T1", "one")}, "2026-10-22T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().ApplyPage(ctx, "F1", []Task{makeTask("T1", "one")}, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || len(got) != 0 {
		t.Errorf("after a scope pull ReachedIDs = %v, %v, want none", got, err)
	}
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T2", "two")}, "2026-10-22T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T2", "two again")}); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || !reflect.DeepEqual(got, []string{"T2"}) {
		t.Errorf("after a plain upsert ReachedIDs = %v, %v, want T2", got, err)
	}
}

func TestMarkOpenedRenewsTheStampOnly(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T1", "reached")}, now.Add(-31*24*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T2", "pulled")}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T1", "T2"} {
		if err := st.Tasks().MarkOpened(ctx, id, now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || !reflect.DeepEqual(got, []string{"T1"}) {
		t.Errorf("ReachedIDs = %v, %v, want T1 alone, an open must not stamp a pulled task", got, err)
	}
	if _, err := st.Tasks().PruneExcept(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Errorf("T1 opened today was pruned, the open did not renew the stamp: %v", err)
	}
}

func TestReachedIDsFiltersByOpenedSince(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T1", "opened"), makeTask("T2", "never opened")}, "2026-10-22T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T1", "2026-10-20T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || !reflect.DeepEqual(got, []string{"T1", "T2"}) {
		t.Errorf("ReachedIDs(all) = %v, %v", got, err)
	}
	if got, err := st.Tasks().ReachedIDs(ctx, "2026-10-19T00:00:00Z"); err != nil || !reflect.DeepEqual(got, []string{"T1"}) {
		t.Errorf("ReachedIDs(opened since) = %v, %v", got, err)
	}
}

func TestMissingReferencedIDsIsOneHopFromUnstampedTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Timelogs().Upsert(ctx, []Timelog{
		{ID: "L1", TaskID: "T9", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1},
		{ID: "L2", TaskID: "local:4", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1},
		{ID: "L3", TaskID: "T10", UserID: "U2", TrackedDate: "2026-10-20", Hours: 1},
		{ID: "L4", TaskID: "T11", UserID: "U1", TrackedDate: "2020-01-01", Hours: 1},
	}); err != nil {
		t.Fatal(err)
	}
	t1 := makeTask("T1", "pulled, with relations")
	t1.DependencyIDs, t1.SuperTaskIDs = []string{"D1"}, []string{"T7"}
	t3 := makeTask("T3", "pulled, referenced by nothing")
	if err := st.Tasks().Upsert(ctx, []Task{t1, t3}); err != nil {
		t.Fatal(err)
	}
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []Dependency{edge("D1", "T1", "T8")}); err != nil {
		t.Fatal(err)
	}
	t2 := makeTask("T2", "reached, its relations stay out")
	t2.DependencyIDs, t2.SuperTaskIDs = []string{"D2"}, []string{"T5"}
	if err := st.Tasks().UpsertReached(ctx, []Task{t2}, "2026-10-22T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Dependencies().ReplaceForTask(ctx, "T2", []Dependency{edge("D2", "T6", "T2")}); err != nil {
		t.Fatal(err)
	}
	// Another person's entry and an own entry older than the synced window name nothing, those rows outlive their tasks.
	if got, err := st.Tasks().MissingReferencedIDs(ctx, "U1", "2026-08-24", 10); err != nil || !reflect.DeepEqual(got, []string{"T7", "T8", "T9"}) {
		t.Errorf("MissingReferencedIDs = %v, %v, want T7 T8 T9", got, err)
	}
	if got, err := st.Tasks().MissingReferencedIDs(ctx, "U1", "2026-08-24", 2); err != nil || len(got) != 2 {
		t.Errorf("MissingReferencedIDs(limit 2) = %v, %v", got, err)
	}
}

// A reach decides "not cached" before the request, and a scope pull can land in between.
// The stamp then goes to the rows the reach created, a task a scope lists stays a plain task.
func TestUpsertReachedLeavesAnExistingRowUnstamped(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "pulled meanwhile")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().UpsertReached(ctx, []Task{makeTask("T1", "pulled, read again"), makeTask("T2", "new")}, "2026-10-22T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Tasks().ReachedIDs(ctx, ""); err != nil || !reflect.DeepEqual(got, []string{"T2"}) {
		t.Errorf("ReachedIDs = %v, %v, want T2 alone", got, err)
	}
	if task, err := st.Tasks().Get(ctx, "T1"); err != nil || task.Title != "pulled, read again" {
		t.Errorf("T1 = %+v, %v, want the fresh title", task, err)
	}
}

func TestByRefLooksUpAnIdOrANumber(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "linked")
	task.Permalink = "https://www.wrike.com/open.htm?id=4552825749"
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []TaskRef{{Kind: RefID, Value: "T1"}, {Kind: RefNumber, Value: "4552825749"}} {
		if got, err := st.Tasks().ByRef(ctx, ref); err != nil || got.ID != "T1" {
			t.Errorf("ByRef(%+v) = %+v, %v", ref, got, err)
		}
	}
	if _, err := st.Tasks().ByRef(ctx, TaskRef{Kind: RefNumber, Value: "1"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown number gave %v, want ErrNotFound", err)
	}
}

func TestUpsertKeepsTheEffortWhenNotToldAndReplacesTheValues(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "budgeted")
	task.CustomFields = []CustomFieldValue{{ID: "CF1", Value: "765"}}
	task.Effort = &TaskEffort{Mode: "Basic", TotalMinutes: 1200}
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().Get(ctx, "T1")
	if err != nil || !reflect.DeepEqual(got.CustomFields, task.CustomFields) || got.Effort == nil || *got.Effort != *task.Effort {
		t.Fatalf("after the first upsert: %+v, %+v, %v", got.CustomFields, got.Effort, err)
	}
	// A write's answer carries the values without being asked and the effort only when asked.
	again := makeTask("T1", "budgeted")
	again.CustomFields = []CustomFieldValue{}
	if err := st.Tasks().Upsert(ctx, []Task{again}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Tasks().Get(ctx, "T1")
	if got.CustomFields != nil || got.Effort == nil || got.Effort.TotalMinutes != 1200 || got.Effort.Mode != "Basic" {
		t.Errorf("after the second upsert: values %+v, effort %+v", got.CustomFields, got.Effort)
	}
	// An absent customFields key is no values either.
	third := makeTask("T1", "budgeted")
	third.CustomFields = []CustomFieldValue{{ID: "CF1", Value: "60"}}
	if err := st.Tasks().Upsert(ctx, []Task{third}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "budgeted")}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Tasks().Get(ctx, "T1")
	if got.CustomFields != nil {
		t.Errorf("an absent key left %+v", got.CustomFields)
	}
	if got.Effort == nil {
		t.Error("the effort was wiped by a task without one")
	}
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T2", "never read")}); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := st.Tasks().Get(ctx, "T2"); fresh.Effort != nil {
		t.Errorf("a task never read with the effort has %+v", fresh.Effort)
	}
}
