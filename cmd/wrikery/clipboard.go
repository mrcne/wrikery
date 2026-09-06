package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/muesli/termenv"
)

// copyText writes an OSC 52 sequence, which reaches the local clipboard through ssh and through tmux when set-clipboard is on, then also feeds a system tool when one is installed.
// Neither reports failure, so the only error is a missing tool and no terminal support, which cannot be detected here.
func copyText(text string) error {
	termenv.NewOutput(os.Stdout).Copy(text)
	for _, tool := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return nil
}
