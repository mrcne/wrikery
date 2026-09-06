package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type editorDoneMsg struct {
	taskID string
	path   string
	err    error
}

// editorArgv splits editor on whitespace,
// so a path to the editor binary itself that contains a space needs a small wrapper script on PATH instead.
func editorArgv(editor, path string) []string {
	return append(strings.Fields(editor), path)
}

// openEditor hands the terminal to $VISUAL or $EDITOR on a temp file.
// bubbletea restores the screen when it returns.
func openEditor(taskID string) (tea.Cmd, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return nil, errors.New("set $EDITOR to write comments in an editor")
	}
	f, err := os.CreateTemp("", "wrikery-comment-*.md")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	_ = f.Close()
	argv := editorArgv(editor, path)
	c := exec.Command(argv[0], argv[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{taskID: taskID, path: path, err: err}
	}), nil
}
