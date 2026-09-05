package sync

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func mustScope(t *testing.T, st *store.Store, id, kind string) store.Scope {
	t.Helper()
	sc := store.Scope{ID: id, Kind: kind, Title: id, Followed: true}
	if err := st.Scopes().Upsert(context.Background(), sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

func wtask(id, title string, updated time.Time) wrike.Task {
	return wrike.Task{ID: id, Title: title, Status: "Active", UpdatedDate: updated,
		CreatedDate: updated, ParentIDs: []string{"F1"}}
}

func TestPullScopeInitialThenIncremental(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	sc := mustScope(t, st, "F1", store.ScopeKindProject)

	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	var gotParams []wrike.TaskParams
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		gotParams = append(gotParams, p)
		if p.PageToken == "" && p.UpdatedAfter.IsZero() {
			return wrike.TasksPage{Tasks: []wrike.Task{wtask("T1", "one", t1)}, NextPageToken: "page2"}, nil
		}
		if p.PageToken == "page2" {
			return wrike.TasksPage{Tasks: []wrike.Task{wtask("T2", "two", t2)}}, nil
		}
		// The incremental call: only the changed task comes back.
		return wrike.TasksPage{Tasks: []wrike.Task{wtask("T2", "two v2", t2.Add(time.Hour))}}, nil
	}}

	if err := pullScope(ctx, fc, st, sc, "U1"); err != nil {
		t.Fatal(err)
	}
	if gotParams[0].FolderID != "F1" || !gotParams[0].Descendants {
		t.Errorf("params = %+v, want the folder query with descendants", gotParams[0])
	}
	if len(gotParams[0].Fields) != 3 || gotParams[0].Fields[0] != "description" {
		t.Errorf("fields = %v, description and the id lists are optional and must be requested", gotParams[0].Fields)
	}
	if gotParams[0].PageSize != 1000 {
		t.Errorf("page size = %d, want the verified maximum", gotParams[0].PageSize)
	}

	got, err := st.Scopes().Get(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cursor != "2026-09-02T10:00:00Z" {
		t.Errorf("cursor = %q, want the max updated date seen", got.Cursor)
	}

	// Second pull resumes from the cursor.
	if err := pullScope(ctx, fc, st, got, "U1"); err != nil {
		t.Fatal(err)
	}
	last := gotParams[len(gotParams)-1]
	if last.UpdatedAfter.UTC().Format(time.RFC3339) != "2026-09-02T10:00:00Z" {
		t.Errorf("updated after = %v, want the stored cursor", last.UpdatedAfter)
	}
	task, err := st.Tasks().Get(ctx, "T2")
	if err != nil || task.Title != "two v2" {
		t.Fatalf("task = %+v, %v", task, err)
	}
	got, err = st.Scopes().Get(ctx, "F1")
	if err != nil || got.Cursor != "2026-09-02T11:00:00Z" {
		t.Fatalf("cursor after incremental = %+v, %v", got, err)
	}
}

func TestPullScopeMeUsesResponsibles(t *testing.T) {
	st := newTestStore(t)
	sc := mustScope(t, st, "me", store.ScopeKindMe)

	var got wrike.TaskParams
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		got = p
		return wrike.TasksPage{}, nil
	}}
	if err := pullScope(context.Background(), fc, st, sc, "U1"); err != nil {
		t.Fatal(err)
	}
	if got.FolderID != "" || len(got.Responsibles) != 1 || got.Responsibles[0] != "U1" {
		t.Errorf("params = %+v, want the account wide responsibles query", got)
	}
}

func TestPullScopeSpaceUsesSpaceEndpoint(t *testing.T) {
	st := newTestStore(t)
	sc := mustScope(t, st, "S1", store.ScopeKindSpace)

	var got wrike.TaskParams
	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		got = p
		return wrike.TasksPage{}, nil
	}}
	if err := pullScope(context.Background(), fc, st, sc, "U1"); err != nil {
		t.Fatal(err)
	}
	if got.SpaceID != "S1" || got.FolderID != "" || !got.Descendants {
		t.Errorf("params = %+v, a space is not a folder and must go through the space endpoint", got)
	}
}

func TestPullScopeEmptyInitialStampsCursor(t *testing.T) {
	st := newTestStore(t)
	sc := mustScope(t, st, "F1", store.ScopeKindProject)
	if err := pullScope(context.Background(), &fakeClient{}, st, sc, "U1"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Scopes().Get(context.Background(), "F1")
	if err != nil || got.Cursor == "" {
		t.Fatalf("scope = %+v, %v, an empty initial pull must still start the cursor", got, err)
	}
}

func TestSweepPrunesVanishedTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	sc := mustScope(t, st, "F1", store.ScopeKindProject)
	seedTask(t, st, "T1", "keep")
	seedTask(t, st, "T2", "gone remotely")

	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		return wrike.TasksPage{Tasks: []wrike.Task{{ID: "T1"}}}, nil
	}}
	if err := sweep(ctx, fc, st, []store.Scope{sc}, "U1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); err == nil {
		t.Error("T2 survived the sweep")
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Errorf("T1 pruned wrongly: %v", err)
	}
}

func TestSweepAbortsOnPartialUnion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	scA := mustScope(t, st, "F1", store.ScopeKindProject)
	scB := mustScope(t, st, "F2", store.ScopeKindProject)
	seedTask(t, st, "T1", "in F2")

	fc := &fakeClient{tasks: func(p wrike.TaskParams) (wrike.TasksPage, error) {
		if p.FolderID == "F2" {
			return wrike.TasksPage{}, &wrike.APIError{StatusCode: 500, Code: "server_error"}
		}
		return wrike.TasksPage{}, nil
	}}
	if err := sweep(ctx, fc, st, []store.Scope{scA, scB}, "U1"); err == nil {
		t.Fatal("want the error back")
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Errorf("T1 pruned from a partial union: %v", err)
	}
}

func TestRefreshThreadsReplacesAndDeletesGone(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedTask(t, st, "T1", "alive")
	seedTask(t, st, "T2", "deleted remotely")
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.Tasks().MarkOpened(ctx, "T1", now); err != nil {
		t.Fatal(err)
	}
	if err := st.Tasks().MarkOpened(ctx, "T2", now); err != nil {
		t.Fatal(err)
	}

	fc := &fakeClient{
		taskComments: func(taskID string) ([]wrike.Comment, error) {
			if taskID == "T2" {
				return nil, &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
			}
			return []wrike.Comment{{ID: "C1", TaskID: taskID, AuthorID: "U1", Text: "hi",
				CreatedDate: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)}}, nil
		},
		taskTimelogs: func(taskID string) ([]wrike.Timelog, error) {
			return []wrike.Timelog{{ID: "L1", TaskID: taskID, UserID: "U1", TrackedDate: "2026-09-03", Hours: 1,
				CreatedDate: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
				UpdatedDate: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)}}, nil
		},
	}
	if err := refreshThreads(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, 50); err != nil {
		t.Fatal(err)
	}

	comments, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil || len(comments) != 1 || comments[0].ID != "C1" {
		t.Fatalf("comments = %+v, %v", comments, err)
	}
	logs, err := st.Timelogs().ListForTask(ctx, "T1")
	if err != nil || len(logs) != 1 {
		t.Fatalf("timelogs = %+v, %v", logs, err)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); err == nil {
		t.Error("T2 must be deleted after the 404")
	}
}

func TestPullReferenceReplacesAll(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	fc := &fakeClient{
		folderTree: func() ([]wrike.Folder, error) {
			return []wrike.Folder{{ID: "F1", Title: "Root", ChildIDs: []string{"F2"}},
				{ID: "F2", Title: "Api", Project: &wrike.Project{Status: "Green"}}}, nil
		},
		contacts: func() ([]wrike.Contact, error) {
			return []wrike.Contact{{ID: "U1", FirstName: "Anna", Me: true}}, nil
		},
		spaces: func() ([]wrike.Space, error) {
			return []wrike.Space{{ID: "S1", Title: "Dev"}}, nil
		},
		workflows: func() ([]wrike.Workflow, error) {
			return []wrike.Workflow{{ID: "W1", Name: "Default",
				CustomStatuses: []wrike.CustomStatus{{ID: "CS1", Name: "New"}}}}, nil
		},
	}
	if err := pullReference(ctx, fc, st); err != nil {
		t.Fatal(err)
	}
	if f, err := st.Folders().Get(ctx, "F2"); err != nil || f.Project == nil {
		t.Errorf("folder = %+v, %v", f, err)
	}
	if cs, err := st.Contacts().List(ctx); err != nil || len(cs) != 1 || !cs[0].Me {
		t.Errorf("contacts = %+v, %v", cs, err)
	}
	if ss, err := st.Spaces().List(ctx); err != nil || len(ss) != 1 {
		t.Errorf("spaces = %+v, %v", ss, err)
	}
	if ws, err := st.Workflows().List(ctx); err != nil || len(ws) != 1 || len(ws[0].CustomStatuses) != 1 {
		t.Errorf("workflows = %+v, %v", ws, err)
	}
}

func TestRefreshThreadsSkipsRejectedTaskAndDropsGone(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"T1", "T2", "T3"} {
		seedTask(t, st, id, id)
		if err := st.Tasks().MarkOpened(ctx, id, now); err != nil {
			t.Fatal(err)
		}
	}

	fc := &fakeClient{
		taskComments: func(taskID string) ([]wrike.Comment, error) {
			if taskID == "T1" {
				return nil, &wrike.APIError{StatusCode: 403, Code: "access_forbidden"}
			}
			return []wrike.Comment{{ID: "C-" + taskID, TaskID: taskID, AuthorID: "U1", Text: "hi",
				CreatedDate: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)}}, nil
		},
		taskTimelogs: func(taskID string) ([]wrike.Timelog, error) {
			if taskID == "T2" {
				// Deleted between the two calls.
				return nil, &wrike.APIError{StatusCode: 404, Code: "resource_not_found"}
			}
			return nil, nil
		},
	}
	if err := refreshThreads(ctx, fc, st, slog.New(slog.DiscardHandler), 7*24*time.Hour, 50); err != nil {
		t.Fatalf("one rejected thread must not fail the refresh: %v", err)
	}
	if _, err := st.Tasks().Get(ctx, "T1"); err != nil {
		t.Errorf("T1 must survive a 403, it may still be readable later: %v", err)
	}
	if _, err := st.Tasks().Get(ctx, "T2"); err == nil {
		t.Error("T2 must be deleted after the 404 on its timelogs")
	}
	if comments, err := st.Comments().ListForTask(ctx, "T3"); err != nil || len(comments) != 1 {
		t.Errorf("comments for T3 = %+v, %v, the task after the rejected one must still refresh", comments, err)
	}
}
