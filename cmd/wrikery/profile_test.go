package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
