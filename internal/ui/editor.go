package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

type editorDoneMsg struct {
	taskID string
	path   string
	err    error
	edit   *descriptionEdit // nil when the editor was opened for a comment
}

// descriptionEdit is what the description editor needs on the way back:
// the HTML the task had when the editor opened, so the merge knows which blocks were left alone, and the markdown the file started with.
type descriptionEdit struct {
	html string
	text string
}

type descriptionReadyMsg struct{ task store.Task }

type submitDescriptionMsg struct{ taskID, html string }

// editorArgv splits editor on whitespace,
// so a path to the editor binary itself that contains a space needs a small wrapper script on PATH instead.
func editorArgv(editor, path string) []string {
	return append(strings.Fields(editor), path)
}

// editorFile creates the temp file the editor opens: the description's markdown, or an empty file for a comment.
func editorFile(edit *descriptionEdit) (string, error) {
	name := "wrikery-comment-*.md"
	if edit != nil {
		name = "wrikery-description-*.md"
	}
	f, err := os.CreateTemp("", name)
	if err != nil {
		return "", err
	}
	if edit != nil {
		if _, err := f.WriteString(edit.text); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", err
		}
	}
	return f.Name(), f.Close()
}

// openEditor hands the terminal to $VISUAL or $EDITOR on a temp file.
// bubbletea restores the screen when it returns.
func openEditor(taskID string, edit *descriptionEdit) (tea.Cmd, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return nil, errors.New("set $EDITOR to write in an editor")
	}
	path, err := editorFile(edit)
	if err != nil {
		return nil, err
	}
	argv := editorArgv(editor, path)
	c := exec.Command(argv[0], argv[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{taskID: taskID, path: path, err: err, edit: edit}
	}), nil
}

// loadDescription reads the task again, the list rows carry no description.
func (m Model) loadDescription(id string) tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		task, err := st.Tasks().Get(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return descriptionReadyMsg{task: task}
	}
}

// finishDescriptionEdit turns the saved file into a task update, or into a toast when there is nothing to send.
func (m *Model) finishDescriptionEdit(taskID string, edit *descriptionEdit, text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return m.status.show("empty file, description unchanged", false)
	}
	html, changed := mergeDescription(edit.html, text)
	if !changed {
		return m.status.show("description unchanged", false)
	}
	return intent(submitDescriptionMsg{taskID: taskID, html: html})
}
