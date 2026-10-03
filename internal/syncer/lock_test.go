package syncer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireWaitsForAHeldLockAndGivesUpWithTheContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	held, err := acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if _, err := acquire(ctx, path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire err = %v, want ErrLocked", err)
	}
	held.release()
	got, err := acquire(context.Background(), path)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	got.release()
}

func TestAcquireWithoutAPathNeverBlocks(t *testing.T) {
	l, err := acquire(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	l.release()
}

func TestAcquireTellsACancelFromAHeldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	held, err := acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	if _, err := acquire(ctx, path); !errors.Is(err, context.Canceled) || errors.Is(err, ErrLocked) {
		t.Fatalf("cancelled acquire err = %v, want context.Canceled", err)
	}
}
