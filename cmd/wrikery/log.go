package main

import (
	"errors"
	"os"
)

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
