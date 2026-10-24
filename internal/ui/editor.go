package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

// editorWindowReturn is how fast a command that opens the file in a window of its own comes back:
// code and cursor hand the file to the running app and exit well inside a second, even when the app has to start first,
// and nobody writes and saves a description in that time.
const editorWindowReturn = 2 * time.Second

type editorDoneMsg struct {
	taskID  string
	path    string
	err     error
	edit    *descriptionEdit // nil when the editor was opened for a comment
	elapsed time.Duration    // how long the editor ran, see editorWindowReturn
}

// untouched reports whether the file is still what editorFile wrote, the description's markdown or nothing for a comment.
func untouched(edit *descriptionEdit, text []byte) bool {
	if edit == nil {
		return len(text) == 0
	}
	return string(text) == edit.text
}

// editorWindowDialog holds the temp file an editor left untouched when it returned at once,
// which is what the code and cursor commands do when they hand the file to a window:
// the edit happens there, and enter here sends what was saved.
// The root answers enter and esc itself, see sendEditorBox and dropEditorFile, so the box has no key handling of its own:
// a message on the way back would let a second enter or an esc land on a file the first one already sent and removed.
type editorWindowDialog struct {
	taskID    string
	path      string
	edit      *descriptionEdit
	saveFirst bool // enter found the file still untouched
}

func (d editorWindowDialog) Update(tea.KeyMsg) (dialog, tea.Cmd) { return d, nil }

func (d editorWindowDialog) View(th Theme, width, height int) string {
	w := min(width, 60)
	what := "comment"
	if d.edit != nil {
		what = "description"
	}
	lines := []string{
		"The editor returned at once and left the file untouched.",
		"If it opened the file in another window, write the " + what + " there, save it, and press enter here to send it.",
	}
	if d.saveFirst {
		lines = append(lines, "", "The file is still unchanged, save it in the editor first.")
	}
	prompt := lipgloss.NewStyle().Width(w - 2).Render(strings.Join(lines, "\n"))
	body := prompt + "\n\n" + lipgloss.NewStyle().Foreground(th.Muted).Render("enter send   esc drop")
	return th.box("Editor", body, w, lipgloss.Height(body)+2, true)
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
		return nil, errors.New("set $EDITOR (or $VISUAL) to write in an editor")
	}
	path, err := editorFile(edit)
	if err != nil {
		return nil, err
	}
	argv := editorArgv(editor, path)
	c := exec.Command(argv[0], argv[1:]...)
	started := time.Now()
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{taskID: taskID, path: path, err: err, edit: edit, elapsed: time.Since(started)}
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

// finishEditorFile turns what the editor saved into a write, or into a toast when there is nothing to send.
func (m *Model) finishEditorFile(taskID string, edit *descriptionEdit, text []byte) tea.Cmd {
	if edit != nil {
		return m.finishDescriptionEdit(taskID, edit, string(text))
	}
	trimmed := strings.TrimSpace(string(text))
	if trimmed == "" {
		return m.status.show("empty comment, nothing sent", false)
	}
	return intent(submitCommentMsg{taskID: taskID, text: trimmed})
}

// sendEditorBox reads the file the editor box holds, at once and in the root, so nothing is in flight when the next key comes.
// A file still untouched keeps the box, the window may still have it open, anything else ends the edit the way an editor's return does.
func (m *Model) sendEditorBox(d editorWindowDialog) tea.Cmd {
	text, _ := os.ReadFile(d.path)
	if untouched(d.edit, text) {
		d.saveFirst = true
		m.dialog = d
		return nil
	}
	m.overlay, m.dialog = overlayNone, nil
	_ = os.Remove(d.path)
	return m.finishEditorFile(d.taskID, d.edit, text)
}

// dropEditorFile gives up the edit an editor box holds, the file goes whatever it holds by now.
func (m *Model) dropEditorFile() tea.Cmd {
	d, ok := m.dialog.(editorWindowDialog)
	if !ok {
		return nil
	}
	_ = os.Remove(d.path)
	if d.edit != nil {
		return m.status.show("description edit dropped", false)
	}
	return m.status.show("comment dropped, nothing sent", false)
}

// finishDescriptionEdit turns the saved file into a task update, or into a toast when there is nothing to send.
func (m *Model) finishDescriptionEdit(taskID string, edit *descriptionEdit, text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return m.status.show("empty file, description unchanged", false)
	}
	html, changed := MergeDescription(edit.html, text)
	if !changed {
		return m.status.show("description unchanged", false)
	}
	return intent(submitDescriptionMsg{taskID: taskID, html: html})
}
