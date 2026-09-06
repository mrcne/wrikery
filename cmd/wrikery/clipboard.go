package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/muesli/termenv"
)

// clipboardTool picks the tool that fits the session instead of the first one installed.
// A desktop box often has both wl-copy and xclip on it, and the one that does not match the session fails,
// which used to report a failed copy even though OSC 52 had already put the text on the clipboard.
func clipboardTool(goos string, env func(string) string) []string {
	if goos == "darwin" {
		return []string{"pbcopy"}
	}
	if env("WAYLAND_DISPLAY") != "" {
		return []string{"wl-copy"}
	}
	if env("DISPLAY") != "" {
		for _, tool := range [][]string{{"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
			if _, err := exec.LookPath(tool[0]); err == nil {
				return tool
			}
		}
	}
	return nil
}

// copyText writes an OSC 52 sequence, which reaches the local clipboard through ssh and through tmux when set-clipboard is on,
// then feeds the session's clipboard tool for the terminals that do not support OSC 52.
// OSC 52 reports nothing back, so a failing tool is not a failed copy and the error says so.
func copyText(text string) error {
	termenv.NewOutput(os.Stdout).Copy(text)
	tool := clipboardTool(runtime.GOOS, os.Getenv)
	if tool == nil {
		return nil
	}
	// A clipboard tool serves the selection until another program takes it over and normally detaches to do that.
	// The timeout is for the one that does not, which would otherwise hold the key press forever.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool[0], tool[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copied through the terminal only, %s failed: %w", tool[0], err)
	}
	return nil
}
