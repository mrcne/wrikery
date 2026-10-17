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

// Lock is an advisory lock on a file, shared by the processes of one user.
// The sync engine holds one next to the database for the outbox drain, the auth source one beside the token file for a renewal.
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
