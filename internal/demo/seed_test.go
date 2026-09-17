package demo

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

func TestSeedFillsEveryTable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC) // a Thursday

	if err := Seed(ctx, st, now); err != nil {
		t.Fatal(err)
	}
	if me, _ := st.GetMeta(ctx, store.MetaKeyMe); me != MeID {
		t.Errorf("meta me = %q", me)
	}
	// The demo has to look synced, or the timesheet would mark every week as not synced.
	if from, _ := st.GetMeta(ctx, store.MetaKeyTimelogFrom); from != "2026-07-06" {
		t.Errorf("meta timelog window start = %q, want 2026-07-06", from)
	}
	if spaces, _ := st.Spaces().List(ctx); len(spaces) != 2 {
		t.Errorf("spaces = %d", len(spaces))
	}
	tree, err := st.Folders().Subtree(ctx, SpacePlatform)
	if err != nil || len(tree) != 6 || !tree[0].Space {
		t.Errorf("platform subtree = %d folders, %v", len(tree), err)
	}
	if tasks, err := st.Tasks().ListInFolder(ctx, folderWishlist); err != nil || len(tasks) != 0 {
		t.Errorf("Wishlist should stay empty, it holds %d tasks, %v", len(tasks), err)
	}
	if hits, _ := st.Tasks().Search(ctx, "retry", 100); len(hits) == 0 {
		t.Error("search finds nothing, descriptions or titles not seeded")
	}
	pending, failed, err := st.Outbox().Counts(ctx)
	if err != nil || pending != 1 || failed != 2 {
		t.Errorf("outbox counts = %d pending %d failed, %v", pending, failed, err)
	}
	scopes, _ := st.Scopes().Followed(ctx)
	if len(scopes) != 3 {
		t.Fatalf("followed scopes = %d", len(scopes))
	}
	for _, sc := range scopes {
		if sc.Cursor == "" {
			t.Errorf("scope %s has no cursor, first run would show it as syncing", sc.ID)
		}
	}
	logs, _ := st.Timelogs().ListForTask(ctx, "IEAATASK00")
	if len(logs) == 0 {
		t.Error("no timelogs on the first task")
	}
	if ws, _ := st.Workflows().List(ctx); len(ws) != 2 || len(ws[0].CustomStatuses) == 0 {
		t.Errorf("workflows = %+v", ws)
	}
}

func TestSeedIsInternallyConsistent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	if err := Seed(ctx, st, now); err != nil {
		t.Fatal(err)
	}

	wfs, err := st.Workflows().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statusGroup := map[string]string{}
	for _, w := range wfs {
		for _, cs := range w.CustomStatuses {
			statusGroup[cs.ID] = cs.Group
		}
	}

	byFolder := map[string][]store.Task{}
	var all []store.Task
	for i := 0; i < taskCount; i++ {
		task, err := st.Tasks().Get(ctx, fmt.Sprintf("IEAATASK%02d", i))
		if err != nil {
			t.Fatalf("get task %d: %v", i, err)
		}
		if len(task.ParentIDs) == 0 {
			t.Fatalf("task %s has no parent", task.ID)
		}
		if _, err := st.Folders().Get(ctx, task.ParentIDs[0]); err != nil {
			t.Errorf("task %s parent %s does not resolve: %v", task.ID, task.ParentIDs[0], err)
		}
		for _, rid := range task.ResponsibleIDs {
			if _, err := st.Contacts().Get(ctx, rid); err != nil {
				t.Errorf("task %s responsible %s does not resolve: %v", task.ID, rid, err)
			}
		}
		if group, ok := statusGroup[task.CustomStatusID]; !ok {
			t.Errorf("task %s custom status %s is not in any seeded workflow", task.ID, task.CustomStatusID)
		} else if group != task.Status {
			t.Errorf("task %s status %s does not match its custom status group %s", task.ID, task.Status, group)
		}
		if task.Dates != nil && task.Dates.Due < task.Dates.Start {
			t.Errorf("task %s due %s is before start %s", task.ID, task.Dates.Due, task.Dates.Start)
		}
		byFolder[task.ParentIDs[0]] = append(byFolder[task.ParentIDs[0]], task)
		all = append(all, task)
	}

	for _, f := range taskFolders {
		folderTasks := byFolder[f]
		statuses := map[string]bool{}
		undated := false
		for _, task := range folderTasks {
			statuses[task.CustomStatusID] = true
			if task.Dates == nil {
				undated = true
			}
		}
		if len(statuses) < 4 {
			t.Errorf("folder %s has only %d distinct custom statuses", f, len(statuses))
		}
		if !undated {
			t.Errorf("folder %s has no undated task", f)
		}
	}

	for _, task := range all {
		logs, err := st.Timelogs().ListForTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("timelogs for %s: %v", task.ID, err)
		}
		for _, lg := range logs {
			if lg.UserID != MeID {
				t.Errorf("timelog %s user = %s, want %s", lg.ID, lg.UserID, MeID)
			}
			responsible := false
			for _, r := range task.ResponsibleIDs {
				if r == MeID {
					responsible = true
				}
			}
			if !responsible {
				t.Errorf("task %s carries a timelog but %s is not a responsible", task.ID, MeID)
			}
		}
	}

	states, err := st.Outbox().StatesByEntity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states["IEAATASK00"] != store.StatePending {
		t.Errorf("IEAATASK00 outbox state = %s, want %s", states["IEAATASK00"], store.StatePending)
	}
	failed, err := st.Outbox().ListFailed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 2 || failed[0].Kind != store.KindTaskUpdate || failed[1].Kind != store.KindTimelogCreate {
		t.Errorf("failed outbox rows = %+v", failed)
	}
}
