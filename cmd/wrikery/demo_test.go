package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

func TestOpenDemoStoreSeedsAndCleansUp(t *testing.T) {
	st, dir, cleanup, err := openDemoStore(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
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
