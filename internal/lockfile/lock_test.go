package lockfile

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireWaitsForAHeldLockAndGivesUpWithTheContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	held, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire err = %v, want ErrLocked", err)
	}
	held.Release()
	got, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	got.Release()
}

func TestAcquireWithoutAPathNeverBlocks(t *testing.T) {
	l, err := Acquire(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
}

func TestAcquireTellsACancelFromAHeldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	held, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	if _, err := Acquire(ctx, path); !errors.Is(err, context.Canceled) || errors.Is(err, ErrLocked) {
		t.Fatalf("cancelled Acquire err = %v, want context.Canceled", err)
	}
}
