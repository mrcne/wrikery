package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/mrcne/wrikery/internal/demo"
	"github.com/mrcne/wrikery/internal/store"
)

// openDemoStore seeds a throwaway database, with extra generated tasks on top when asked for.
// The directory goes away with cleanup, so a demo run leaves nothing behind.
func openDemoStore(now time.Time, extra int) (*store.Store, string, func(), error) {
	dir, err := os.MkdirTemp("", "wrikery-demo-*")
	if err != nil {
		return nil, "", nil, err
	}
	st, err := store.Open(filepath.Join(dir, "demo.db"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, "", nil, err
	}
	st.Source = store.SourceTUI
	if err := demo.Seed(context.Background(), st, now); err != nil {
		_ = st.Close()
		_ = os.RemoveAll(dir)
		return nil, "", nil, err
	}
	if err := demo.Extend(context.Background(), st, now, extra); err != nil {
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

// startDemo opens the demo's store and starts the profile when one is asked for.
// The profile starts first, so the CPU profile covers the seeding,
// and a directory it cannot write is reported before the seconds a large demo takes to seed.
func startDemo(now time.Time) (*store.Store, func(), func() error, error) {
	extra, err := demoSize()
	if err != nil {
		return nil, nil, nil, err
	}
	stop, err := startProfile(os.Getenv(envProfile))
	if err != nil {
		return nil, nil, nil, err
	}
	st, _, cleanup, err := openDemoStore(now, extra)
	if err != nil {
		return nil, nil, nil, errors.Join(err, stop())
	}
	return st, cleanup, stop, nil
}
