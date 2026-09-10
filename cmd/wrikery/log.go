package main

import (
	"errors"
	"os"
	"path/filepath"
)

// openLog opens the log file for this run, truncated.
// A demo run writes demo.log next to the real log and rotates nothing,
// so trying the demo does not throw away the log of the last real run.
func openLog(path string, demo bool) (*os.File, error) {
	if demo {
		path = filepath.Join(filepath.Dir(path), "demo.log")
	} else if err := rotateLog(path); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
}

// rotateLog moves path to path+".1", overwriting any previous copy.
// One previous run is enough to debug a crash, and the log stops growing forever.
func rotateLog(path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.Rename(path, path+".1")
}
