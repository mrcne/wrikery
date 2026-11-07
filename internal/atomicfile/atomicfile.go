// Package atomicfile writes a file through a temporary one in the same directory, so a reader never sees half of it
// and a crash leaves either the old file or the new one.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
)

// Write puts data at path with the mode perm.
// The temporary file is synced before the rename: without that a power loss soon after the rename can leave an empty file on APFS and ext4,
// and an empty config or token file reads as "nothing set" instead of an error.
// The directory is synced after the rename where the system allows it, so the new name is on disk too.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if err := errors.Join(werr, tmp.Sync(), tmp.Close(), os.Chmod(tmp.Name(), perm)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // best effort, some file systems refuse a directory sync and the rename is already done
		_ = d.Close()
	}
	return nil
}
