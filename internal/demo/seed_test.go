package demo

import (
	"context"
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
	if spaces, _ := st.Spaces().List(ctx); len(spaces) != 2 {
		t.Errorf("spaces = %d", len(spaces))
	}
	tree, err := st.Folders().Subtree(ctx, SpacePlatform)
	if err != nil || len(tree) != 6 || !tree[0].Space {
		t.Errorf("platform subtree = %d folders, %v", len(tree), err)
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
