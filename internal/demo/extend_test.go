package demo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

func seededForExtend(t *testing.T) (*store.Store, time.Time) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	if err := Seed(context.Background(), st, now); err != nil {
		t.Fatal(err)
	}
	return st, now
}

func TestExtendAddsGeneratedTasksOnTopOfTheSeed(t *testing.T) {
	st, now := seededForExtend(t)
	ctx := context.Background()
	mineBefore, err := st.Tasks().ListForResponsible(ctx, MeID)
	if err != nil {
		t.Fatal(err)
	}
	since := now.AddDate(0, 0, -store.InboxDays).UTC().Format(time.RFC3339)
	marker, err := st.GetMeta(ctx, store.MetaKeyInboxSeenTo)
	if err != nil {
		t.Fatal(err)
	}
	inboxBefore, err := st.Comments().Inbox(ctx, MeID, since, 500)
	if err != nil {
		t.Fatal(err)
	}
	clockBefore := st.Now()

	if err := Extend(ctx, st, now, 200); err != nil {
		t.Fatal(err)
	}

	if rows, err := st.Tasks().ListInFolder(ctx, FolderEverything); err != nil || len(rows) != 200 {
		t.Errorf("the big folder holds %d tasks, %v, want all 200, it takes the first five thousand", len(rows), err)
	}
	if rows, err := st.Tasks().ListInFolder(ctx, SpaceWork); err != nil || len(rows) != 200 {
		t.Errorf("the space holds %d tasks, %v, want 200", len(rows), err)
	}
	if tree, err := st.Folders().Subtree(ctx, SpaceWork); err != nil || len(tree) != 1+60+300+1 || !tree[0].Space {
		t.Errorf("the space subtree has %d folders, %v, want the root, 60 projects, 300 folders and the big one", len(tree), err)
	}
	if mine, err := st.Tasks().ListForResponsible(ctx, MeID); err != nil || len(mine) != len(mineBefore)+20 {
		t.Errorf("my tasks = %d, %v, want %d, every tenth generated task", len(mine), err, len(mineBefore)+20)
	}
	if spaces, err := st.Spaces().List(ctx); err != nil || len(spaces) != 3 {
		t.Errorf("spaces = %d, %v, want 3", len(spaces), err)
	}
	followed, err := st.Scopes().Followed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var work *store.Scope
	for i := range followed {
		if followed[i].ID == SpaceWork {
			work = &followed[i]
		}
	}
	if work == nil || work.Cursor == "" {
		t.Errorf("the generated space is not a followed scope with a cursor: %+v", work)
	}
	first, err := st.Tasks().Get(ctx, "IEAAGEN000000")
	if err != nil {
		t.Fatal(err)
	}
	seventh, err := st.Tasks().Get(ctx, "IEAAGEN000007")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Description) < 1000 || len(seventh.Description) < 1000 {
		t.Errorf("every seventh task carries the long description, got %d and %d bytes", len(first.Description), len(seventh.Description))
	}
	if second, err := st.Tasks().Get(ctx, "IEAAGEN000001"); err != nil || len(second.Description) >= 1000 {
		t.Errorf("the other tasks carry the sample descriptions, got %d bytes, %v", len(second.Description), err)
	}
	if rows, err := st.Tasks().ListInFolder(ctx, ProjectAPI); err != nil || len(rows) == 0 {
		t.Errorf("the seed's tasks are gone: %d in the sample project, %v", len(rows), err)
	}
	if unread, err := st.Comments().InboxUnread(ctx, MeID, since, marker); err != nil || unread != 2 {
		t.Errorf("unread = %d, %v, want the seed's 2, the generated comments arrived before the marker", unread, err)
	}
	if rows, err := st.Comments().Inbox(ctx, MeID, since, 500); err != nil || len(rows) <= len(inboxBefore) {
		t.Errorf("inbox rows = %d, %v, want more than the seed's %d", len(rows), err, len(inboxBefore))
	}
	if !st.Now().Equal(clockBefore) {
		t.Errorf("the store clock was not put back, it reads %v, want %v", st.Now(), clockBefore)
	}
}

func TestExtendSpreadsTheRestOverTheProjectFolders(t *testing.T) {
	st, now := seededForExtend(t)
	ctx := context.Background()
	if err := Extend(ctx, st, now, bigFolderTasks+10); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.Tasks().ListInFolder(ctx, FolderEverything); err != nil || len(rows) != bigFolderTasks {
		t.Errorf("the big folder holds %d tasks, %v, want %d", len(rows), err, bigFolderTasks)
	}
	// The ten past the big folder land on the first ten project folders, five in the first project and five in the second.
	if rows, err := st.Tasks().ListInFolder(ctx, "IEAAPRJ000"); err != nil || len(rows) != foldersPerProject {
		t.Errorf("the first project holds %d tasks, %v, want %d", len(rows), err, foldersPerProject)
	}
}

func TestExtendPutsEverythingInTheBigFolderWhenNIsSmall(t *testing.T) {
	st, now := seededForExtend(t)
	ctx := context.Background()
	if err := Extend(ctx, st, now, 3); err != nil {
		t.Fatal(err)
	}
	if rows, err := st.Tasks().ListInFolder(ctx, FolderEverything); err != nil || len(rows) != 3 {
		t.Errorf("the big folder holds %d tasks, %v, want all 3", len(rows), err)
	}
	if rows, err := st.Tasks().ListInFolder(ctx, "IEAAPRJ000"); err != nil || len(rows) != 0 {
		t.Errorf("the first project holds %d tasks, %v, want none", len(rows), err)
	}
}

func TestExtendWithZeroChangesNothing(t *testing.T) {
	st, now := seededForExtend(t)
	ctx := context.Background()
	if err := Extend(ctx, st, now, 0); err != nil {
		t.Fatal(err)
	}
	if spaces, err := st.Spaces().List(ctx); err != nil || len(spaces) != 2 {
		t.Errorf("spaces = %d, %v, want the seed's 2", len(spaces), err)
	}
}
