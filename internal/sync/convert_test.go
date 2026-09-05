package sync

import (
	"testing"
	"time"

	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestTaskFromWrike(t *testing.T) {
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	in := wrike.Task{
		ID: "T1", Title: "a", Description: "<p>b</p>", Status: "Active",
		CustomStatusID: "CS1", Importance: "High", Permalink: "https://x",
		ResponsibleIDs: []string{"U1"}, ParentIDs: []string{"F1"},
		Dates:       &wrike.TaskDates{Type: "Planned", Duration: 480, Start: "2026-09-01T09:00:00", Due: "2026-09-02T17:00:00"},
		CreatedDate: created, UpdatedDate: created.Add(time.Hour),
	}
	got := taskFromWrike(in)
	if got.CreatedDate != "2026-09-01T09:00:00Z" {
		t.Errorf("created = %q, want UTC RFC3339", got.CreatedDate)
	}
	if got.UpdatedDate != "2026-09-01T10:00:00Z" {
		t.Errorf("updated = %q", got.UpdatedDate)
	}
	if got.Dates == nil || got.Dates.Due != "2026-09-02T17:00:00" {
		t.Errorf("dates = %+v, the zone-less strings must pass through untouched", got.Dates)
	}
	if got.Title != "a" || got.CustomStatusID != "CS1" || len(got.ResponsibleIDs) != 1 {
		t.Errorf("task = %+v", got)
	}

	in.Dates = nil
	if got := taskFromWrike(in); got.Dates != nil {
		t.Errorf("dates = %+v, want nil", got.Dates)
	}
}

func TestFolderAndWorkflowFromWrike(t *testing.T) {
	f := folderFromWrike(wrike.Folder{ID: "F1", Title: "Root", ChildIDs: []string{"F2"}})
	if f.Project != nil {
		t.Errorf("plain folder got project %+v", f.Project)
	}
	p := folderFromWrike(wrike.Folder{ID: "F2", Title: "Api", Project: &wrike.Project{Status: "Green"}})
	if p.Project == nil || p.Project.Status != "Green" {
		t.Errorf("project folder = %+v", p)
	}

	ws := workflowsFromWrike([]wrike.Workflow{{ID: "W1", Name: "Default", CustomStatuses: []wrike.CustomStatus{
		{ID: "CS1", Name: "New", Group: "Active"},
		{ID: "CS2", Name: "Done", Group: "Completed"},
	}}})
	if len(ws) != 1 || len(ws[0].CustomStatuses) != 2 || ws[0].CustomStatuses[0].Name != "New" {
		t.Errorf("workflows = %+v, slice order must survive", ws)
	}
}
