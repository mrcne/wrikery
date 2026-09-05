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
