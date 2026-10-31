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
	st, dir, cleanup, err := openDemoStore(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 0)
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
	st, _, cleanup, err := openDemoStore(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 20)
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
}
