package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

// skipWhileProfiling skips a test that starts a CPU profile when go test -cpuprofile runs one, the runtime allows one at a time.
func skipWhileProfiling(t *testing.T) {
	t.Helper()
	if err := pprof.StartCPUProfile(io.Discard); err != nil {
		t.Skip("a cpu profile is running already")
	}
	pprof.StopCPUProfile()
}

func profileFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 0 {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestStartProfileWritesACPUAndAHeapProfileIntoTheDirectory(t *testing.T) {
	skipWhileProfiling(t)
	dir := t.TempDir()
	stop, err := startProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(profileFiles(t, dir), " "); got != "cpu.pprof heap.pprof" {
		t.Errorf("files with content = %q, want cpu.pprof heap.pprof", got)
	}
}

func TestStartProfileWithoutADirectoryDoesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	stop, err := startProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if got := profileFiles(t, dir); len(got) != 0 {
		t.Errorf("files written without a directory: %v", got)
	}
}

func TestStartProfileRefusesADirectoryItCannotWrite(t *testing.T) {
	_, err := startProfile(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), envProfile) {
		t.Errorf("err = %v, want one naming %s", err, envProfile)
	}
}

func TestRunCommandWritesTheProfilesWhenAsked(t *testing.T) {
	skipWhileProfiling(t)
	isolate(t)
	dir := t.TempDir()
	t.Setenv(envProfile, dir)
	fs, o := rootFlags()
	o.noColor = true
	// Whatever the command answers without a sign in, it ran, and the profiles cover that run.
	runCommand([]string{"task", "list"}, fs, o)
	if got := strings.Join(profileFiles(t, dir), " "); got != "cpu.pprof heap.pprof" {
		t.Errorf("files with content = %q, want cpu.pprof heap.pprof", got)
	}
}

func TestRunCommandHelpWritesNoProfile(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv(envProfile, dir)
	fs, o := rootFlags()
	o.noColor = true
	if code := runCommand([]string{"help"}, fs, o); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if got := profileFiles(t, dir); len(got) != 0 {
		t.Errorf("help wrote profiles: %v", got)
	}
}

// The code of a command says what happened to its work, a profile it could not write is a debug switch's failure and goes to stderr only.
func TestRunCommandKeepsItsCodeWhenTheProfileCannotBeWritten(t *testing.T) {
	skipWhileProfiling(t)
	home := isolate(t)
	dbDir := filepath.Join(home, "data", "wrikery")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dbDir, "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta(context.Background(), store.MetaKeyMe, "KUME"); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	dir := t.TempDir()
	// A directory where the heap profile goes makes the stop fail after the command has done its work.
	if err := os.Mkdir(filepath.Join(dir, "heap.pprof"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envProfile, dir)
	fs, o := rootFlags()
	o.noColor = true
	if code := runCommand([]string{"task", "list"}, fs, o); code != 0 {
		t.Errorf("code = %d, want 0, the list was printed", code)
	}
}

func TestRunCommandStartsTheProfileBeforeTheStoreOpens(t *testing.T) {
	home := isolate(t)
	t.Setenv(envProfile, filepath.Join(t.TempDir(), "missing"))
	fs, o := rootFlags()
	o.noColor = true
	if code := runCommand([]string{"task", "list"}, fs, o); code != 1 {
		t.Errorf("code = %d, want 1 for a profile directory that cannot be written", code)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "wrikery", "wrike.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cache was opened before the profile started: %v", err)
	}
}

func TestRunStartsTheProfileBeforeTheStoreOpens(t *testing.T) {
	home := isolate(t)
	t.Setenv(envProfile, filepath.Join(t.TempDir(), "missing"))
	if err := run(false, false, "", true, nil); err == nil || !strings.Contains(err.Error(), envProfile) {
		t.Errorf("err = %v, want one naming %s", err, envProfile)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "wrikery", "wrike.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cache was opened before the profile started: %v", err)
	}
}
