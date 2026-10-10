package syncer

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func seedTaskWithDependency(t *testing.T, st *store.Store, id, depID string) {
	t.Helper()
	err := st.Tasks().Upsert(context.Background(), []store.Task{{
		ID: id, Title: id, Status: "Active", DependencyIDs: []string{depID},
		CreatedDate: "2026-09-01T10:00:00Z", UpdatedDate: "2026-09-01T10:00:00Z",
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func wedge(id, pred, succ string) wrike.Dependency {
	return wrike.Dependency{ID: id, PredecessorID: pred, SuccessorID: succ, RelationType: "FinishToStart", LagTime: 480}
}

func dependencyCalls(fc *fakeClient) int {
	n := 0
	for _, c := range fc.callLog() {
		if strings.HasPrefix(c, "TaskDependencies ") {
			n++
		}
	}
	return n
}

func TestPullDependenciesFetchesAnEdgeOnceForBothEnds(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// The pull listed the same edge on both tasks, the way Wrike does.
	seedTaskWithDependency(t, st, "T1", "X")
	seedTaskWithDependency(t, st, "T2", "X")
	fc := &fakeClient{taskDependencies: func(taskID string) ([]wrike.Dependency, error) {
		return []wrike.Dependency{wedge("X", "T1", "T2")}, nil
	}}

	changed, err := pullDependencies(ctx, fc, st, slog.New(slog.DiscardHandler))
	if err != nil || !changed {
		t.Fatalf("changed = %v, %v", changed, err)
	}
	if n := dependencyCalls(fc); n != 1 {
		t.Errorf("dependency calls = %d, want one, the answer covers the other end", n)
	}
	for _, id := range []string{"T1", "T2"} {
		deps, err := st.Dependencies().ListForTask(ctx, id)
		if err != nil || len(deps) != 1 || deps[0].LagMinutes != 480 || deps[0].RelationType != "FinishToStart" {
			t.Errorf("deps of %s = %+v, %v", id, deps, err)
		}
	}

	changed, err = pullDependencies(ctx, fc, st, slog.New(slog.DiscardHandler))
	if err != nil || changed {
		t.Errorf("second pass changed = %v, %v, want nothing to fetch", changed, err)
	}
	if n := dependencyCalls(fc); n != 1 {
		t.Errorf("dependency calls after the second pass = %d, a fetched edge is not asked for again", n)
	}
}

func TestPullDependenciesForgetsATaskTheEndpointRejects(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTaskWithDependency(t, st, "T1", "X")
	seedTaskWithDependency(t, st, "T2", "Y")
	seedTaskWithDependency(t, st, "T3", "Z")
	fc := &fakeClient{taskDependencies: func(taskID string) ([]wrike.Dependency, error) {
		switch taskID {
		case "T1":
			return nil, &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
		case "T2":
			return nil, &wrike.APIError{StatusCode: 403, Code: "access_forbidden"}
		}
		return []wrike.Dependency{wedge("Z", "T3", "T9")}, nil
	}}
	if _, err := pullDependencies(ctx, fc, st, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	// The scope pull listed T1 in this very cycle, so a 404 from this endpoint does not make it a deleted task.
	for _, id := range []string{"T1", "T2"} {
		if _, err := st.Tasks().Get(ctx, id); err != nil {
			t.Errorf("%s must stay, the pull and the sweep decide whether a task exists", id)
		}
	}
	deps, err := st.Dependencies().ListForTask(ctx, "T3")
	if err != nil || len(deps) != 1 {
		t.Errorf("deps of T3 = %+v, %v, the task after a rejected one is still fetched", deps, err)
	}
	if _, err := pullDependencies(ctx, fc, st, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if n := dependencyCalls(fc); n != 3 {
		t.Errorf("dependency calls = %d, want three, a rejected task is not asked again until its next pull", n)
	}
}

func TestPullDependenciesReturnsATransientError(t *testing.T) {
	st := newTestStore(t)
	seedTaskWithDependency(t, st, "T1", "X")
	fc := &fakeClient{taskDependencies: func(taskID string) ([]wrike.Dependency, error) {
		return nil, &wrike.APIError{StatusCode: 503}
	}}
	if _, err := pullDependencies(context.Background(), fc, st, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("an outage must stop the cycle, not be logged away")
	}
}

func TestRefreshThreadsReplacesTheDependenciesOfAnOpenedTask(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTaskWithDependency(t, st, "T1", "X")
	if err := st.Dependencies().ReplaceForTask(ctx, "T1", []store.Dependency{{ID: "X", PredecessorID: "T1", SuccessorID: "T2", RelationType: "FinishToStart"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T1", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	// Wrike dropped the edge. Neither task's updatedDate moves for that, so only this read can notice.
	fc := &fakeClient{taskDependencies: func(taskID string) ([]wrike.Dependency, error) { return nil, nil }}
	if _, err := refreshThreads(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, 50); err != nil {
		t.Fatal(err)
	}
	deps, err := st.Dependencies().ListForTask(ctx, "T1")
	if err != nil || len(deps) != 0 {
		t.Errorf("deps = %+v, %v, want the dropped edge gone", deps, err)
	}
	if n := dependencyCalls(fc); n != 1 {
		t.Errorf("dependency calls = %d, want one for the opened task", n)
	}
}

func TestRefreshThreadsKeepsTheThreadWhenTheDependencyReadFails(t *testing.T) {
	for _, status := range []int{404, 403} {
		st := newTestStore(t)
		ctx := context.Background()
		seedTask(t, st, "T1", "alive")
		if err := st.Tasks().MarkOpened(ctx, "T1", time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		fc := &fakeClient{
			taskComments: func(taskID string) ([]wrike.Comment, error) {
				return []wrike.Comment{{ID: "C1", TaskID: taskID, AuthorID: "U1", Text: "hi", CreatedDate: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)}}, nil
			},
			taskDependencies: func(taskID string) ([]wrike.Dependency, error) {
				return nil, &wrike.APIError{StatusCode: status}
			},
		}
		touched, err := refreshThreads(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, 50)
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if !slices.Contains(touched, KindComments) {
			t.Errorf("status %d: touched = %v, the comments were written and must be announced", status, touched)
		}
		if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
			t.Errorf("status %d: the comment read answered 200, the task is not gone", status)
		}
		comments, err := st.Comments().ListForTask(ctx, "T1")
		if err != nil || len(comments) != 1 {
			t.Errorf("status %d: comments = %+v, %v", status, comments, err)
		}
	}
}

// The edge catch-up runs last in a cycle and announces itself as its own kind:
// the opened task gets its edges from the thread refresh first, and the interface reloads only the detail for the rest.
func TestCycleReadsTheOpenedTaskFirstAndAnnouncesTheEdgesOnTheirOwn(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject, Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	seedTaskWithDependency(t, st, "T1", "X")
	if err := st.Tasks().MarkOpened(ctx, "T1", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	fc := &fakeClient{
		tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
			if p.FolderID == "F1" {
				// T1 is listed too, the first cycle sweeps whatever no scope lists.
				return wrike.TasksPage{Tasks: []wrike.Task{
					{ID: "T1", Title: "opened", Status: "Active", DependencyIDs: []string{"X"}, UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)},
					{ID: "T2", Title: "pulled", Status: "Active", DependencyIDs: []string{"Y"}, UpdatedDate: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)},
				}}, nil
			}
			return wrike.TasksPage{}, nil
		},
		taskDependencies: func(taskID string) ([]wrike.Dependency, error) {
			if taskID == "T1" {
				return []wrike.Dependency{wedge("X", "T1", "T9")}, nil
			}
			return []wrike.Dependency{wedge("Y", "T2", "T9")}, nil
		},
	}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour, ThreadWindow: 7 * 24 * time.Hour, ThreadLimit: 50})
	ev := waitFor(t, e, "edges announced", func(ev Event) bool {
		return ev.Kind == EventStoreChanged && slices.Contains(ev.Entities, KindDependencies) && !slices.Contains(ev.Entities, KindComments)
	})
	if slices.Contains(ev.Entities, KindTasks) {
		t.Errorf("entities = %v, an edge change must not reload the tree and the list", ev.Entities)
	}
	waitFor(t, e, "idle", isState(StateIdle))
	var reads []string
	for _, call := range fc.callLog() {
		if strings.HasPrefix(call, "TaskDependencies ") {
			reads = append(reads, call)
		}
	}
	if len(reads) != 2 || reads[0] != "TaskDependencies T1" || reads[1] != "TaskDependencies T2" {
		t.Errorf("reads = %v, want the opened task through the thread refresh first, then the catch-up", reads)
	}
}
