package main

import (
	"errors"
	"os/exec"
	"runtime"
)

func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return errors.New("xdg-open not found")
		}
		return exec.Command("xdg-open", url).Start()
	}
	return errors.New("opening a browser is not supported on " + runtime.GOOS)
}
