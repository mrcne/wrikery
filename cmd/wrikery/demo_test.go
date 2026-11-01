package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

func TestOpenDemoStoreSeedsAndCleansUp(t *testing.T) {
	st, dir, cleanup, err := openDemoStore(context.Background(), time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	me, err := st.GetMeta(context.Background(), store.MetaKeyMe)
	if err != nil || me != demo.MeID {
		t.Errorf("meta = %q, %v", me, err)
	}
	cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("demo dir still exists: %v", err)
	}
}

func TestOpenDemoStoreExtendsWhenAsked(t *testing.T) {
	st, _, cleanup, err := openDemoStore(context.Background(), time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	followed, err := st.Scopes().Followed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(followed))
	for _, sc := range followed {
		ids = append(ids, sc.ID)
	}
	if !slices.Contains(ids, demo.SpaceWork) {
		t.Errorf("followed scopes = %v, want the generated space among them", ids)
	}
	if got, err := st.Comments().ListForTask(context.Background(), "IEAAGEN000001"); err != nil || len(got) != 2 {
		t.Errorf("a generated task has %d comments, %v, want the 2 asked for", len(got), err)
	}
}

func TestOpenDemoStoreStopsAndCleansUpWhenCanceled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := openDemoStore(ctx, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 100, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the cancellation", err)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("the demo left %d entries in the temp directory", len(left))
	}
}
