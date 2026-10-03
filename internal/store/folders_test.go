package store

import (
	"context"
	"testing"
)

func TestFoldersFindByTitleMatchesAFragment(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	err := st.Folders().ReplaceTree(ctx, []Folder{
		{ID: "S1", Title: "Wrikery", Space: true, ChildIDs: []string{"P1", "P2"}},
		{ID: "P1", Title: "4 Later", Project: &Project{Status: "Green"}},
		{ID: "P2", Title: "3 Release", Project: &Project{Status: "Green"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Folders().FindByTitle(ctx, "later", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "P1" {
		t.Fatalf("later matched %+v, want P1", got)
	}
	got, err = st.Folders().FindByTitle(ctx, "e", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("e matched %d folders, want 3", len(got))
	}
	if got[0].Title != "3 Release" {
		t.Errorf("first hit = %q, want the titles in order", got[0].Title)
	}
}
