package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

// openDemoStore seeds a throwaway database. The directory goes away with cleanup, so a demo run leaves nothing behind.
func openDemoStore(now time.Time) (*store.Store, string, func(), error) {
	dir, err := os.MkdirTemp("", "wrikery-demo-*")
	if err != nil {
		return nil, "", nil, err
	}
	st, err := store.Open(filepath.Join(dir, "demo.db"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, "", nil, err
	}
	if err := demo.Seed(context.Background(), st, now); err != nil {
		_ = st.Close()
		_ = os.RemoveAll(dir)
		return nil, "", nil, err
	}
	cleanup := func() {
		_ = st.Close()
		_ = os.RemoveAll(dir)
	}
	return st, dir, cleanup, nil
}
