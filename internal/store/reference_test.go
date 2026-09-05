package store

import (
	"context"
	"reflect"
	"testing"
)

func TestFolderTreeRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	tree := []Folder{
		{ID: "F1", Title: "Root", Scope: "WsFolder", ChildIDs: []string{"F2", "F3"}},
		{ID: "F2", Title: "Api", Scope: "WsFolder", Project: &Project{
			Status: "Green", CustomStatusID: "CS1", StartDate: "2026-09-01", EndDate: "2026-12-01"}},
		{ID: "F3", Title: "Backlog", Scope: "WsFolder"},
	}
	if err := st.Folders().ReplaceTree(ctx, tree); err != nil {
		t.Fatal(err)
	}

	got, err := st.Folders().Get(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ChildIDs) != 2 || got.Project != nil {
		t.Errorf("root = %+v, want 2 children and no project", got)
	}
	api, err := st.Folders().Get(ctx, "F2")
	if err != nil {
		t.Fatal(err)
	}
	if api.Project == nil || api.Project.Status != "Green" {
		t.Errorf("project folder = %+v", api)
	}
	kids, err := st.Folders().Children(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 || kids[0].Title != "Api" || kids[1].Title != "Backlog" {
		t.Errorf("children = %+v, want Api then Backlog by title", kids)
	}

	// A replace drops folders that are gone from the tree.
	if err := st.Folders().ReplaceTree(ctx, tree[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Folders().Get(ctx, "F3"); err == nil {
		t.Error("F3 survived a replace that no longer contains it")
	}
}

func TestSpacesAndContactsReplaceAll(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.Spaces().ReplaceAll(ctx, []Space{
		{ID: "S1", Title: "Ops", AccessType: "Public"},
		{ID: "S2", Title: "Dev", AccessType: "Private", Archived: true},
	}); err != nil {
		t.Fatal(err)
	}
	spaces, err := st.Spaces().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 2 || spaces[0].Title != "Dev" {
		t.Errorf("spaces = %+v, want Dev first by title", spaces)
	}

	if err := st.Contacts().ReplaceAll(ctx, []Contact{
		{ID: "U1", FirstName: "Anna", LastName: "Nowak", Me: true},
		{ID: "U2", FirstName: "Piotr", LastName: "Kowalski"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Contacts().ReplaceAll(ctx, []Contact{
		{ID: "U1", FirstName: "Anna", LastName: "Nowak", Me: true},
	}); err != nil {
		t.Fatal(err)
	}
	contacts, err := st.Contacts().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 || contacts[0].ID != "U1" || !contacts[0].Me {
		t.Errorf("contacts = %+v, want only U1 after replace", contacts)
	}
	got, err := st.Contacts().Get(ctx, "U1")
	if err != nil || got.FirstName != "Anna" {
		t.Errorf("get U1 = %+v, %v", got, err)
	}
}

func TestFolderSubtreeReturnsRootThenDescendants(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	err := st.Folders().ReplaceTree(ctx, []Folder{
		{ID: "S1", Title: "Platform", Space: true, ChildIDs: []string{"F2", "F1"}},
		{ID: "F1", Title: "API", ChildIDs: []string{"F3"}, Project: &Project{Status: "Green"}},
		{ID: "F2", Title: "Web"},
		{ID: "F3", Title: "API v2"},
		{ID: "X", Title: "Elsewhere"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Folders().Subtree(ctx, "S1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	want := []string{"S1", "F1", "F3", "F2"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if !got[0].Space || len(got[0].ChildIDs) != 2 || got[1].Project == nil {
		t.Errorf("fields lost: %+v", got[:2])
	}
}

func TestFolderSubtreeUnknownRoot(t *testing.T) {
	st := newTestStore(t)
	got, err := st.Folders().Subtree(context.Background(), "nope")
	if err != nil || len(got) != 0 {
		t.Fatalf("Subtree(unknown) = %v, %v", got, err)
	}
}

func TestWorkflowsKeepStatusOrder(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	wf := Workflow{ID: "W1", Name: "Default", Standard: true, CustomStatuses: []CustomStatus{
		{ID: "CS1", Name: "New", Color: "Blue", Group: "Active"},
		{ID: "CS2", Name: "In Progress", Color: "Green", Group: "Active"},
		{ID: "CS3", Name: "Done", Color: "Gray", Group: "Completed"},
	}}
	if err := st.Workflows().ReplaceAll(ctx, []Workflow{wf}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Workflows().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].CustomStatuses) != 3 {
		t.Fatalf("workflows = %+v", got)
	}
	for i, want := range []string{"New", "In Progress", "Done"} {
		if got[0].CustomStatuses[i].Name != want {
			t.Errorf("status %d = %q, want %q (API order must survive)", i, got[0].CustomStatuses[i].Name, want)
		}
	}
}
