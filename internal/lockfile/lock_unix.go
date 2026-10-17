//go:build unix

// Package lockfile is the advisory file lock wrikery processes share, a running interface and a command.
package lockfile

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// ErrLocked is returned when another wrikery process held the lock until the deadline of the wait.
var ErrLocked = errors.New("lockfile: another wrikery process holds the lock")

// Lock is an advisory lock on a file.
// The sync engine holds it on the file next to the database to serialize the outbox drain and the in-flight reset across processes.
// The row claim in the store is atomic on its own, the lock is there for two things it cannot cover:
// a startup reset while another process is mid-send, which would send that row twice,
// and two drainers sending two rows on the same task out of order.
// flock is released by the kernel when the holder dies, so a crash cannot leave it taken.
type Lock struct{ f *os.File }

// Acquire tries the lock every 100ms until it has it or ctx ends.
// A deadline gives ErrLocked, a cancellation gives the context error so a quit is not reported as a held lock.
// An empty path means no lock, which demo mode and most tests use.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	if path == "" {
		return &Lock{}, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lock{f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ctx.Err()
			}
			return nil, ErrLocked
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Release gives the lock up, a zero Lock releases nothing.
func (l *Lock) Release() {
	if l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
