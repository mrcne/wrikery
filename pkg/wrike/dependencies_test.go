package wrike

import (
	"context"
	"net/http"
	"testing"
)

// The fixture is shaped like the live answer for a task on either end of the edge.
const dependenciesFixture = `{"kind":"dependencies","data":[
  {"id":"MgAAAAEPLXpEMwAAAAEPLXpF","predecessorId":"MAAAAAEPLXpE","successorId":"MAAAAAEPLXpF","relationType":"FinishToStart","lagTime":960}
]}`

func TestTaskDependenciesDecodesTheEdge(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/MAAAAAEPLXpF/dependencies" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(dependenciesFixture))
	}))
	deps, err := c.TaskDependencies(context.Background(), "MAAAAAEPLXpF")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("deps = %+v", deps)
	}
	d := deps[0]
	if d.ID != "MgAAAAEPLXpEMwAAAAEPLXpF" || d.PredecessorID != "MAAAAAEPLXpE" || d.SuccessorID != "MAAAAAEPLXpF" {
		t.Errorf("edge = %+v", d)
	}
	if d.RelationType != "FinishToStart" || d.LagTime != 960 {
		t.Errorf("relation = %q lag = %d", d.RelationType, d.LagTime)
	}
}

func TestTaskDependenciesRejectsAnEmptyID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request expected")
	}))
	if _, err := c.TaskDependencies(context.Background(), ""); err == nil {
		t.Error("want an error for an empty id")
	}
}
