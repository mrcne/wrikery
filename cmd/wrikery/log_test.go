package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrikery.log")
	rotated := path + ".1"

	if err := rotateLog(path); err != nil {
		t.Fatalf("rotateLog on a missing file returned an error: %v", err)
	}
	if _, err := os.Stat(rotated); !os.IsNotExist(err) {
		t.Fatalf("rotating a missing file created %s", rotated)
	}

	if err := os.WriteFile(path, []byte("first run"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := rotateLog(path); err != nil {
		t.Fatalf("rotateLog: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original log still exists after rotation")
	}
	got, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", rotated, err)
	}
	if string(got) != "first run" {
		t.Errorf("rotated content = %q, want %q", got, "first run")
	}

	if err := os.WriteFile(path, []byte("second run"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := rotateLog(path); err != nil {
		t.Fatalf("rotateLog: %v", err)
	}
	got, err = os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", rotated, err)
	}
	if string(got) != "second run" {
		t.Errorf("rotated content after second run = %q, want %q", got, "second run")
	}
}
