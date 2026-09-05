package wrike

import (
	"context"
	"net/http"
	"testing"
)

const foldersFixture = `{"kind":"folderTree","data":[
  {"id":"IEAAAAFD1","title":"Engineering","childIds":["IEAAAAFD2"],"scope":"WsFolder","space":true},
  {"id":"IEAAAAFD2","title":"TUI Rewrite","childIds":[],"scope":"WsFolder",
   "project":{"authorId":"KUAAAA01","ownerIds":["KUAAAA01"],"status":"Green",
              "customStatusId":"IEAAAACS9","startDate":"2026-09-01","endDate":"2026-12-24"}}
]}`

func TestFolderTree(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/folders" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(foldersFixture))
	}))

	got, err := c.FolderTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("folders = %+v", got)
	}
	if got[0].Project != nil {
		t.Errorf("plain folder must have nil Project, got %+v", got[0].Project)
	}
	p := got[1].Project
	if p == nil || p.Status != "Green" || p.OwnerIDs[0] != "KUAAAA01" || p.EndDate != "2026-12-24" {
		t.Errorf("project = %+v", p)
	}
	if got[0].ChildIDs[0] != "IEAAAAFD2" {
		t.Errorf("childIds = %+v", got[0].ChildIDs)
	}
	if !got[0].Space {
		t.Errorf("first folder should have Space == true, got %+v", got[0].Space)
	}
	if got[1].Space {
		t.Errorf("second folder should have Space == false, got %+v", got[1].Space)
	}
}

func TestSpaceFolders(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spaces/IEAAAASPACE1/folders" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(foldersFixture))
	}))

	got, err := c.SpaceFolders(context.Background(), "IEAAAASPACE1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Engineering" {
		t.Errorf("folders = %+v", got)
	}
}

func TestSpaceFoldersRejectsEmptyID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must be sent")
	}))

	if _, err := c.SpaceFolders(context.Background(), ""); err == nil {
		t.Error("want error for empty space id, got nil")
	}
}
