package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

// seedBoard puts a small board in the store: a space with two projects, four tasks, a workflow and two people.
// The me id is U1. Task ids are chosen so none is a substring of a title.
func seedBoard(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.Folders().ReplaceTree(ctx, []store.Folder{
		{ID: "SPACE1", Title: "Wrikery", Space: true, ChildIDs: []string{"PROJ1", "PROJ2"}},
		{ID: "PROJ1", Title: "4 Later", Project: &store.Project{Status: "Green"}},
		{ID: "PROJ2", Title: "3 Release", Project: &store.Project{Status: "Green"}},
	}))
	must(st.Workflows().ReplaceAll(ctx, []store.Workflow{{
		ID: "WF1", Name: "Default Workflow", Standard: true,
		CustomStatuses: []store.CustomStatus{
			{ID: "ST_NEW", Name: "New", Group: "Active", Color: "Blue", Standard: true},
			{ID: "ST_PROG", Name: "In Progress", Group: "Active", Color: "Green"},
			{ID: "ST_HOLD", Name: "On Hold", Group: "Deferred", Color: "Yellow"},
			{ID: "ST_DONE", Name: "Completed", Group: "Completed", Color: "Green", Standard: true},
			{ID: "ST_GONE", Name: "Old", Group: "Cancelled", Hidden: true},
		},
	}}))
	must(st.Contacts().ReplaceAll(ctx, []store.Contact{
		{ID: "U1", FirstName: "Marcin", LastName: "Tester", Me: true},
		{ID: "U2", FirstName: "Anna", LastName: "Nowak"},
	}))
	must(st.SetMeta(ctx, store.MetaKeyMe, "U1"))
	must(st.Tasks().Upsert(ctx, []store.Task{
		{ID: "TASK1", Title: "Headless commands for scripts", Status: "Active", CustomStatusID: "ST_NEW", Importance: "High",
			Permalink: "https://www.wrike.com/open.htm?id=1", ResponsibleIDs: []string{"U1"}, ParentIDs: []string{"PROJ1"},
			Description: "<p>Three flags, <b>no</b> subcommands.</p>", CreatedDate: "2026-09-08T10:00:00Z", UpdatedDate: "2026-10-01T12:00:00Z",
			Dates: &store.TaskDates{Type: "Planned", Start: "2026-10-01", Due: "2026-10-03", Duration: 1440}},
		{ID: "TASK2", Title: "Reach tasks outside the followed scopes", Status: "Active", CustomStatusID: "ST_PROG", Importance: "Normal",
			Permalink: "https://www.wrike.com/open.htm?id=2", ParentIDs: []string{"PROJ1"},
			CreatedDate: "2026-09-08T10:00:00Z", UpdatedDate: "2026-09-20T12:00:00Z"},
		{ID: "TASK3", Title: "Custom fields", Status: "Deferred", CustomStatusID: "ST_HOLD", Importance: "Low",
			Permalink: "https://www.wrike.com/open.htm?id=3", ParentIDs: []string{"PROJ1"},
			CreatedDate: "2026-09-08T10:00:00Z", UpdatedDate: "2026-09-10T12:00:00Z"},
		{ID: "TASK4", Title: "Add the licence file", Status: "Completed", CustomStatusID: "ST_DONE", Importance: "High",
			Permalink: "https://www.wrike.com/open.htm?id=4", ResponsibleIDs: []string{"U1", "U2"}, ParentIDs: []string{"PROJ2"},
			CreatedDate: "2026-09-08T10:00:00Z", UpdatedDate: "2026-10-02T12:00:00Z"},
	}))
}

func TestResolveTaskTakesAnIdFirst(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	got, err := resolveTask(context.Background(), env.Store, "TASK2")
	if err != nil || got.ID != "TASK2" {
		t.Errorf("resolveTask(TASK2) = %+v, %v", got, err)
	}
}

func TestResolveTaskTakesAUniqueFragmentCaseInsensitively(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	got, err := resolveTask(context.Background(), env.Store, "LICENCE")
	if err != nil || got.ID != "TASK4" {
		t.Errorf("resolveTask(LICENCE) = %+v, %v", got, err)
	}
}

func TestResolveTaskRefusesAnAmbiguousFragmentWithCandidates(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	_, err := resolveTask(context.Background(), env.Store, "the")
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	for _, want := range []string{"matches 2 tasks", "TASK2", "TASK4", "be more specific"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
}

func TestResolveTaskNamesTheCacheWhenNothingMatches(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	_, err := resolveTask(context.Background(), env.Store, "zzz")
	if err == nil || !strings.Contains(err.Error(), "in the cache") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveTaskAcceptsALocalId(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	id, err := env.Store.Outbox().EnqueueTaskCreate(ctx, "PROJ1", store.TaskCreatePayload{Title: "Fresh"}, "ST_NEW")
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveTask(ctx, env.Store, store.LocalID(id))
	if err != nil || got.Title != "Fresh" {
		t.Errorf("resolveTask(local) = %+v, %v", got, err)
	}
}

func TestResolveFolderMatchesSpacesProjectsAndRefusesAmbiguity(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	if fo, err := resolveFolder(ctx, env.Store, "wrikery"); err != nil || fo.ID != "SPACE1" {
		t.Errorf("resolveFolder(wrikery) = %+v, %v", fo, err)
	}
	if fo, err := resolveFolder(ctx, env.Store, "PROJ2"); err != nil || fo.Title != "3 Release" {
		t.Errorf("resolveFolder(PROJ2) = %+v, %v", fo, err)
	}
	if _, err := resolveFolder(ctx, env.Store, "e"); err == nil || !strings.Contains(err.Error(), "matches 3 folders") {
		t.Errorf("resolveFolder(e) err = %v", err)
	}
}

func TestStatusNameFallsBackToTheGroup(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	ref, err := loadRef(context.Background(), env.Store)
	if err != nil {
		t.Fatal(err)
	}
	if got := ref.statusName(store.Task{CustomStatusID: "ST_HOLD", Status: "Deferred"}); got != "On Hold" {
		t.Errorf("known status = %q", got)
	}
	if got := ref.statusName(store.Task{CustomStatusID: "NOPE", Status: "Active"}); got != "Active" {
		t.Errorf("unknown status = %q", got)
	}
}

func TestTaskRowCarriesNamesAndPrintsJSON(t *testing.T) {
	env, stdout, _ := testEnv(t)
	seedBoard(t, env.Store)
	ctx := context.Background()
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		t.Fatal(err)
	}
	task1, _ := resolveTask(ctx, env.Store, "TASK1")
	task2, _ := resolveTask(ctx, env.Store, "TASK2")
	task4, _ := resolveTask(ctx, env.Store, "TASK4")
	row1 := taskRow(ctx, env, task1, &ref)
	row2 := taskRow(ctx, env, task2, &ref)
	if len(row1.Responsibles) == 0 || row1.Responsibles[0].Name != "Marcin Tester" {
		t.Errorf("TASK1 responsible name = %q, want 'Marcin Tester'", row1.Responsibles[0].Name)
	}
	if len(row1.Folders) == 0 || row1.Folders[0].Title != "4 Later" {
		t.Errorf("TASK1 folder title = %q, want '4 Later'", row1.Folders[0].Title)
	}
	if row1.Pending {
		t.Errorf("TASK1 pending = true, want false")
	}
	if row1.Dates == nil {
		t.Errorf("TASK1 dates = nil, want non-nil")
	}
	if row2.Dates != nil {
		t.Errorf("TASK2 dates = %v, want nil", row2.Dates)
	}
	if row2.Pending {
		t.Errorf("TASK2 pending = true, want false")
	}
	if isDone(task2) {
		t.Errorf("TASK2 isDone = true, want false")
	}
	if !isDone(task4) {
		t.Errorf("TASK4 isDone = false, want true")
	}
	if got := shortDate(task1.CreatedDate); got != "2026-09-08" {
		t.Errorf("shortDate = %q, want '2026-09-08'", got)
	}
	rows := []taskJSON{row1, row2}
	code := printJSON(env, rows)
	if code != exitOK {
		t.Errorf("printJSON exit code = %d, want 0", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "\"responsibles\": []") && !strings.Contains(output, "\"responsibles\":[]") {
		t.Errorf("JSON output missing empty responsibles array for TASK2: %s", output)
	}
}

func TestResolveTaskRefusesAnEmptyArgument(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	for _, arg := range []string{"", "  "} {
		_, err := resolveTask(context.Background(), env.Store, arg)
		if err == nil || !strings.Contains(err.Error(), "a task is needed") {
			t.Errorf("resolveTask(%q) error = %v", arg, err)
		}
	}
}

func TestResolveFolderRefusesAnEmptyArgument(t *testing.T) {
	env, _, _ := testEnv(t)
	seedBoard(t, env.Store)
	for _, arg := range []string{"", "  "} {
		_, err := resolveFolder(context.Background(), env.Store, arg)
		if err == nil || !strings.Contains(err.Error(), "a folder is needed") {
			t.Errorf("resolveFolder(%q) error = %v", arg, err)
		}
	}
}
