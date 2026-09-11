package wrike

import (
	"context"
	"net/http"
	"testing"
)

func TestWorkflows(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workflows" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"kind":"workflows","data":[
  {"id":"IEAAAAWF1","name":"Default Workflow","standard":true,"hidden":false,
   "customStatuses":[
     {"id":"IEAAAACS1","name":"In Progress","standardName":true,"color":"Blue","standard":true,"group":"Active","hidden":false},
     {"id":"IEAAAACS2","name":"Blocked","standardName":false,"color":"Red","standard":false,"group":"Active","hidden":false}]}]}`))
	}))

	got, err := c.Workflows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Default Workflow" {
		t.Fatalf("workflows = %+v", got)
	}
	cs := got[0].CustomStatuses
	if len(cs) != 2 || cs[1].Name != "Blocked" || cs[1].Group != "Active" || cs[1].Standard {
		t.Errorf("customStatuses = %+v", cs)
	}
}

func TestSpaceWorkflows(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spaces/IEAAAASPACE1/workflows" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"kind":"workflows","data":[
  {"id":"IEAAAAWF2","name":"Task workflow","description":"","standard":false,"hidden":false,"spaceId":"IEAAAASPACE1",
   "customStatuses":[
     {"id":"IEAAAACS7","name":"Planned","standardName":false,"color":"Purple","standard":false,"group":"Active","hidden":false},
     {"id":"IEAAAACS8","name":"In review","standardName":false,"color":"Blue","standard":false,"group":"Active","hidden":false}]}]}`))
	}))

	got, err := c.SpaceWorkflows(context.Background(), "IEAAAASPACE1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "IEAAAAWF2" || got[0].Standard {
		t.Fatalf("workflows = %+v", got)
	}
	cs := got[0].CustomStatuses
	if len(cs) != 2 || cs[1].Name != "In review" || cs[1].Color != "Blue" {
		t.Errorf("customStatuses = %+v", cs)
	}
}

func TestSpaceWorkflowsRejectsEmptyID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must be sent")
	}))

	if _, err := c.SpaceWorkflows(context.Background(), ""); err == nil {
		t.Error("want error for empty space id, got nil")
	}
}
