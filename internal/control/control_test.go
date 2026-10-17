package control

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// leaveStale closes a listener's socket without unlinking its file, what a crash leaves behind.
func leaveStale(t *testing.T, l *Listener) {
	t.Helper()
	ul, ok := l.listener.(*net.UnixListener)
	if !ok {
		t.Fatal("not a unix listener")
	}
	ul.SetUnlinkOnClose(false)
	_ = ul.Close()
	l.wg.Wait()
}

// socketDir is a short temp dir: a t.TempDir() path can run past the 104 byte limit of a unix socket path on macOS.
func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestCallReachesTheHandlerAndReturnsItsReply(t *testing.T) {
	path := filepath.Join(socketDir(t), "wrikery.sock")
	var got Request
	l, err := Listen(path, func(req Request) Reply {
		got = req
		return Reply{OK: true, Text: "showing High | Ada Nowak (me)"}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	reply, err := Call(path, Request{Op: OpFilter, Importance: []string{"High"}, Person: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if !reply.OK || reply.Text != "showing High | Ada Nowak (me)" {
		t.Errorf("reply = %+v", reply)
	}
	if got.Op != OpFilter || len(got.Importance) != 1 || got.Importance[0] != "High" || got.Person != "me" {
		t.Errorf("handler got %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v, %v, want 0600", info.Mode(), err)
	}
}

func TestListenRemovesAStaleSocket(t *testing.T) {
	path := filepath.Join(socketDir(t), "wrikery.sock")
	// A socket file nobody listens on, what a crash leaves behind.
	l, err := Listen(path, func(Request) Reply { return Reply{OK: true} })
	if err != nil {
		t.Fatal(err)
	}
	leaveStale(t, l)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the stale file is gone before the test started: %v", err)
	}
	again, err := Listen(path, func(Request) Reply { return Reply{OK: true, Text: "fresh"} })
	if err != nil {
		t.Fatalf("a stale socket must be removed and bound again: %v", err)
	}
	defer func() { _ = again.Close() }()
	reply, err := Call(path, Request{Op: OpRefresh})
	if err != nil || reply.Text != "fresh" {
		t.Errorf("reply = %+v, %v", reply, err)
	}
}

func TestListenRefusesASocketInUse(t *testing.T) {
	path := filepath.Join(socketDir(t), "wrikery.sock")
	first, err := Listen(path, func(Request) Reply { return Reply{OK: true, Text: "first"} })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if _, err := Listen(path, func(Request) Reply { return Reply{OK: true, Text: "second"} }); !errors.Is(err, ErrInUse) {
		t.Fatalf("second listen err = %v, want ErrInUse", err)
	}
	reply, err := Call(path, Request{Op: OpRefresh})
	if err != nil || reply.Text != "first" {
		t.Errorf("the first interface must keep the socket: %+v, %v", reply, err)
	}
}

func TestCallWithoutAListenerIsNotRunning(t *testing.T) {
	dir := socketDir(t)
	if _, err := Call(filepath.Join(dir, "missing.sock"), Request{Op: OpRefresh}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("missing file err = %v, want ErrNotRunning", err)
	}
	path := filepath.Join(dir, "stale.sock")
	l, err := Listen(path, func(Request) Reply { return Reply{OK: true} })
	if err != nil {
		t.Fatal(err)
	}
	leaveStale(t, l)
	if _, err := Call(path, Request{Op: OpRefresh}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("stale file err = %v, want ErrNotRunning", err)
	}
}

func TestCloseRemovesTheSocketFile(t *testing.T) {
	path := filepath.Join(socketDir(t), "wrikery.sock")
	l, err := Listen(path, func(Request) Reply { return Reply{OK: true} })
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket file after close: %v, want gone", err)
	}
}

func TestNudgeDropsEveryError(t *testing.T) {
	// No listener, no panic, no wait worth noticing.
	start := time.Now()
	Nudge(filepath.Join(socketDir(t), "none.sock"))
	if time.Since(start) > 2*time.Second {
		t.Errorf("nudge without a listener took %v", time.Since(start))
	}
	path := filepath.Join(socketDir(t), "wrikery.sock")
	var got Request
	l, err := Listen(path, func(req Request) Reply {
		got = req
		return Reply{OK: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	Nudge(path)
	if got.Op != OpReload {
		t.Errorf("nudge sent %+v, want the reload operation", got)
	}
}
