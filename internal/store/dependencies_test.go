package store

import (
	"context"
	"testing"
)

func edge(id, pred, succ string) Dependency {
	return Dependency{ID: id, PredecessorID: pred, SuccessorID: succ, RelationType: "FinishToStart", LagMinutes: 480}
}

func TestReplaceForTaskStoresTheEdgeForBothEnds(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// The pull lists the same dependency id on both ends before any edge is fetched.
	t1, t2 := makeTask("T1", "first"), makeTask("T2", "second")
	t1.DependencyIDs, t2.DependencyIDs = []string{"X"}, []string{"X"}
	if err := st.Tasks().Upsert(ctx, []Task{t1, t2}); err != nil {
		t.Fatal(err)
	}
	missing, err := st.Dependencies().TasksMissingEdges(ctx, 10)
	if err != nil || len(missing) != 2 || len(missing["T1"]) != 1 || missing["T1"][0] != "X" || len(missing["T2"]) != 1 {
		t.Fatalf("missing = %v, %v, want both tasks with the id before the fetch", missing, err)
	}
	if limited, err := st.Dependencies().TasksMissingEdges(ctx, 1); err != nil || len(limited) != 1 {
		t.Fatalf("limited = %v, %v, want one task", limited, err)
	}

	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []Dependency{edge("X", "T1", "T2")}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T1", "T2"} {
		deps, err := st.Dependencies().ListForTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(deps) != 1 || deps[0] != edge("X", "T1", "T2") {
			t.Errorf("deps of %s = %+v, want the one edge with its relation and lag", id, deps)
		}
	}
	missing, err = st.Dependencies().TasksMissingEdges(ctx, 10)
	if err != nil || len(missing) != 0 {
		t.Errorf("missing = %v, %v, want none once the edge is in", missing, err)
	}
}

func TestReplaceForTaskPrunesAnEdgeNoTaskListsAndKeepsAnUnrelatedOne(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t1, t2, t3 := makeTask("T1", "first"), makeTask("T2", "second"), makeTask("T3", "third")
	t2.DependencyIDs, t3.DependencyIDs = []string{"X"}, []string{"Y", "W"}
	if err := st.Tasks().Upsert(ctx, []Task{t1, t2, t3}); err != nil {
		t.Fatal(err)
	}
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []Dependency{edge("X", "T1", "T2"), edge("Y", "T1", "T3"), edge("Z", "T1", "T4")}); err != nil {
		t.Fatal(err)
	}
	// Wrike dropped Y and Z from T1. W never touched T1, so T3 keeps asking for it.
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []Dependency{edge("X", "T1", "T2")}); err != nil {
		t.Fatal(err)
	}
	deps, err := st.Dependencies().ListForTask(ctx, "T1")
	if err != nil || len(deps) != 1 || deps[0].ID != "X" {
		t.Errorf("deps of T1 = %+v, %v, want X alone", deps, err)
	}
	deps, err = st.Dependencies().ListForTask(ctx, "T3")
	if err != nil || len(deps) != 0 {
		t.Errorf("deps of T3 = %+v, %v, Y left T1 so it left T3 too", deps, err)
	}
	missing, err := st.Dependencies().TasksMissingEdges(ctx, 10)
	if err != nil || len(missing) != 1 || len(missing["T3"]) != 1 || missing["T3"][0] != "W" {
		t.Errorf("missing = %v, %v, want T3 still asking for W only", missing, err)
	}
	var n int
	if err := st.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM dependencies WHERE id IN ('Y', 'Z')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("an edge no task lists must be pruned")
	}
}

func TestDeletingATaskDropsItFromTheMissingList(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t1 := makeTask("T1", "first")
	t1.DependencyIDs = []string{"X"}
	if err := st.Tasks().Upsert(ctx, []Task{t1}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().Delete(ctx, "T1"); err != nil {
		t.Fatal(err)
	}
	missing, err := st.Dependencies().TasksMissingEdges(ctx, 10)
	if err != nil || len(missing) != 0 {
		t.Errorf("missing = %v, %v, want none for a deleted task", missing, err)
	}
}

func TestReplaceForTaskDropsARemovedEdgeFromTheOtherEnd(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t1, t2 := makeTask("T1", "first"), makeTask("T2", "second")
	t1.DependencyIDs, t2.DependencyIDs = []string{"X"}, []string{"X"}
	if err := st.Tasks().Upsert(ctx, []Task{t1, t2}); err != nil {
		t.Fatal(err)
	}
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []Dependency{edge("X", "T1", "T2")}); err != nil {
		t.Fatal(err)
	}
	// Wrike dropped X. Only T1 is read, T2's updatedDate never moves for that, so the read has to speak for both ends.
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", nil); err != nil {
		t.Fatal(err)
	}
	deps, err := st.Dependencies().ListForTask(ctx, "T2")
	if err != nil || len(deps) != 0 {
		t.Errorf("deps of T2 = %+v, %v, want the removed edge gone from the other end too", deps, err)
	}
	missing, err := st.Dependencies().TasksMissingEdges(ctx, 10)
	if err != nil || len(missing) != 0 {
		t.Errorf("missing = %v, %v, want T2 not to ask for the edge again", missing, err)
	}
}

func TestReplaceForTaskListsANewEdgeOnTheCachedOtherEnd(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "first"), makeTask("T2", "second")}); err != nil {
		t.Fatal(err)
	}
	// One end is in the cache, the other is a task outside the followed spaces.
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []Dependency{edge("X", "T1", "T2"), edge("Y", "GONE", "T1")}); err != nil {
		t.Fatal(err)
	}
	deps, err := st.Dependencies().ListForTask(ctx, "T2")
	if err != nil || len(deps) != 1 || deps[0].ID != "X" {
		t.Errorf("deps of T2 = %+v, %v, want the edge T1's read brought", deps, err)
	}
	deps, err = st.Dependencies().ListForTask(ctx, "T1")
	if err != nil || len(deps) != 2 {
		t.Errorf("deps of T1 = %+v, %v, want both edges, the uncached end included", deps, err)
	}
}

func TestForgetForTaskDropsOnlyItsOwnRows(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	t1, t2 := makeTask("T1", "first"), makeTask("T2", "second")
	t1.DependencyIDs, t2.DependencyIDs = []string{"X"}, []string{"X"}
	if err := st.Tasks().Upsert(ctx, []Task{t1, t2}); err != nil {
		t.Fatal(err)
	}
	if err := st.Dependencies().ForgetForTask(ctx, "T1"); err != nil {
		t.Fatal(err)
	}
	missing, err := st.Dependencies().TasksMissingEdges(ctx, 10)
	if err != nil || len(missing) != 1 || len(missing["T2"]) != 1 {
		t.Errorf("missing = %v, %v, want T2 alone still asking", missing, err)
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Error("forgetting the rows must keep the task")
	}
}
