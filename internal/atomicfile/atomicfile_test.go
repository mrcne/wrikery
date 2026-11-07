package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrcne/wrikery/internal/atomicfile"
)

func TestWriteReplacesTheFileWholeAndKeepsThePerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := atomicfile.Write(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(path, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "two\n" {
		t.Errorf("file = %q, %v", got, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v, want the perm of the last write", fi.Mode(), err)
	}
	// Nothing but the file itself stays in the directory.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("directory holds %d entries, want the file alone", len(entries))
	}
}

func TestWriteIntoAMissingDirectoryLeavesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gone")
	if err := atomicfile.Write(filepath.Join(dir, "f"), []byte("x"), 0o600); err == nil {
		t.Fatal("Write into a missing directory succeeded")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the directory was created: %v", err)
	}
}
