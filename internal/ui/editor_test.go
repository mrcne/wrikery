package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditorArgv(t *testing.T) {
	got := editorArgv("vim -u NONE", "/tmp/x.md")
	if !reflect.DeepEqual(got, []string{"vim", "-u", "NONE", "/tmp/x.md"}) {
		t.Errorf("got %v", got)
	}
}

func TestOpenEditorWithoutEditorSet(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if _, err := openEditor("T", nil); err == nil {
		t.Error("expected an error with no editor set")
	}
}

// writeTempComment writes text to a temp file and returns its path, standing in for what an
// editor would have left behind.
func writeTempComment(t *testing.T, text string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "wrikery-comment-*.md")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEditorDoneSubmitsTheTrimmedComment(t *testing.T) {
	path := writeTempComment(t, "  # heading stays\nhello from the editor  \n")
	m := New(Options{})
	_, cmd := m.Update(editorDoneMsg{taskID: "T1", path: path})
	msgs := collect(cmd)
	if len(msgs) != 1 || msgs[0] != (submitCommentMsg{taskID: "T1", text: "# heading stays\nhello from the editor"}) {
		t.Errorf("msgs = %#v", msgs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after editorDoneMsg", path)
	}
}

// status.show sets the toast text on the model directly and returns a timer command that clears
// it later, so these two tests read the toast off the returned model instead of running that
// command (which sleeps for the toast duration).
func TestEditorDoneOnEmptyFileToastsAndSendsNothing(t *testing.T) {
	path := writeTempComment(t, "   \n\n")
	m := New(Options{})
	next, cmd := m.Update(editorDoneMsg{taskID: "T1", path: path})
	if cmd == nil {
		t.Fatal("expected a command to clear the toast later")
	}
	got := next.(Model)
	if got.status.toast != "empty comment, nothing sent" || got.status.toastErr {
		t.Errorf("toast = %q, isErr = %v", got.status.toast, got.status.toastErr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after editorDoneMsg", path)
	}
}

func TestEditorDoneOnEditorErrorToasts(t *testing.T) {
	path := writeTempComment(t, "whatever was typed before the editor failed")
	m := New(Options{})
	next, cmd := m.Update(editorDoneMsg{taskID: "T1", path: path, err: os.ErrPermission})
	if cmd == nil {
		t.Fatal("expected a command to clear the toast later")
	}
	got := next.(Model)
	want := "editor failed: " + os.ErrPermission.Error()
	if got.status.toast != want || !got.status.toastErr {
		t.Errorf("toast = %q, isErr = %v, want %q, true", got.status.toast, got.status.toastErr, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after editorDoneMsg", path)
	}
}

func TestEditorFileStartsWithTheDescription(t *testing.T) {
	path, err := editorFile(&descriptionEdit{html: "<p>a</p>", text: "a\n"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\n" || !strings.Contains(filepath.Base(path), "wrikery-description-") {
		t.Errorf("file %s holds %q", path, got)
	}
	empty, err := editorFile(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(empty) }()
	if got, _ := os.ReadFile(empty); len(got) != 0 || !strings.Contains(filepath.Base(empty), "wrikery-comment-") {
		t.Errorf("comment file %s holds %q", empty, got)
	}
}

func TestEditorDoneWithADescriptionQueuesTheMergedHTML(t *testing.T) {
	edit := &descriptionEdit{html: "<p>a</p>", text: EditorText("<p>a</p>")}
	path := writeTempComment(t, "a\n\nb\n")
	m := New(Options{})
	_, cmd := m.Update(editorDoneMsg{taskID: "T1", path: path, edit: edit})
	msgs := collect(cmd)
	if len(msgs) != 1 || msgs[0] != (submitDescriptionMsg{taskID: "T1", html: "<p>a</p><p>b</p>"}) {
		t.Errorf("msgs = %#v", msgs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after editorDoneMsg", path)
	}
}

// A minute in the editor is a person who looked at the description and quit, not an editor that opened a window.
func TestEditorDoneWithAnUntouchedDescriptionSendsNothing(t *testing.T) {
	edit := &descriptionEdit{html: "<p>a</p>", text: EditorText("<p>a</p>")}
	for text, want := range map[string]string{"a\n": "description unchanged", "  \n": "empty file, description unchanged"} {
		path := writeTempComment(t, text)
		m := New(Options{})
		next, cmd := m.Update(editorDoneMsg{taskID: "T1", path: path, edit: edit, elapsed: time.Minute})
		if cmd == nil {
			t.Fatal("expected a command to clear the toast later")
		}
		if got := next.(Model); got.status.toast != want || got.status.toastErr {
			t.Errorf("file %q: toast = %q, isErr = %v, want %q", text, got.status.toast, got.status.toastErr, want)
		}
	}
}

// windowPopup opens the editor popup the way a windowed editor does:
// the command returned at once and the file is still what wrikery wrote, the description's markdown or nothing for a comment.
func windowPopup(t *testing.T, edit *descriptionEdit) (Model, string) {
	t.Helper()
	text := ""
	if edit != nil {
		text = edit.text
	}
	path := writeTempComment(t, text)
	next, _ := New(Options{}).Update(editorDoneMsg{taskID: "T1", path: path, edit: edit, elapsed: 200 * time.Millisecond})
	m := next.(Model)
	if d, ok := m.dialog.(editorWindowDialog); !ok || m.overlay != overlayDialog || d.path != path {
		t.Fatalf("dialog = %#v, overlay = %v", m.dialog, m.overlay)
	}
	return m, path
}

// enterInPopup presses enter in the popup and feeds back any editor message it produced, as the program would,
// and returns the rest of what came out.
func enterInPopup(t *testing.T, m Model) (Model, []tea.Msg) {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	var out []tea.Msg
	for _, msg := range collect(cmd) {
		done, ok := msg.(editorDoneMsg)
		if !ok {
			out = append(out, msg)
			continue
		}
		next, cmd = m.Update(done)
		m = next.(Model)
		out = append(out, collect(cmd)...)
	}
	return m, out
}

// A second enter, typed before the first one landed, must not send the comment twice or bring the box back on a file that is gone.
func TestEditorWindowSecondEnterDoesNotReopenTheBox(t *testing.T) {
	m, path := windowPopup(t, nil)
	if err := os.WriteFile(path, []byte("once\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	next, first := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, second := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	sent := 0
	for _, msg := range append(collect(first), collect(second)...) {
		if done, ok := msg.(editorDoneMsg); ok {
			next, cmd := m.Update(done)
			m = next.(Model)
			msg = nil
			if msgs := collect(cmd); len(msgs) == 1 {
				msg = msgs[0]
			}
		}
		if _, ok := msg.(submitCommentMsg); ok {
			sent++
		}
	}
	if sent != 1 || m.dialog != nil {
		t.Errorf("sent %d comments, dialog %#v, want one send and no box", sent, m.dialog)
	}
}

func TestEditorDoneAtOnceWithAnUntouchedFileKeepsItAndAsks(t *testing.T) {
	edit := &descriptionEdit{html: "<p>a</p>", text: EditorText("<p>a</p>")}
	for name, edit := range map[string]*descriptionEdit{"description": edit, "comment": nil} {
		m, path := windowPopup(t, edit)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: temp file %s is gone: %v", name, path, err)
		}
		view := squeeze(m.dialog.View(m.theme, 80, 40))
		if !strings.Contains(view, "returned at once") || !strings.Contains(view, "enter send") || !strings.Contains(view, name) {
			t.Errorf("%s: view = %q", name, view)
		}
	}
}

func TestEditorWindowEnterSendsTheSavedDescription(t *testing.T) {
	edit := &descriptionEdit{html: "<p>a</p>", text: EditorText("<p>a</p>")}
	m, path := windowPopup(t, edit)
	if err := os.WriteFile(path, []byte("a\n\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, msgs := enterInPopup(t, m)
	if len(msgs) != 1 || msgs[0] != (submitDescriptionMsg{taskID: "T1", html: "<p>a</p><p>b</p>"}) {
		t.Errorf("msgs = %#v", msgs)
	}
	if m.overlay != overlayNone || m.dialog != nil {
		t.Errorf("popup still open: %#v", m.dialog)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after the send", path)
	}
}

func TestEditorWindowEnterSendsTheSavedComment(t *testing.T) {
	m, path := windowPopup(t, nil)
	if err := os.WriteFile(path, []byte("hello from the window\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, msgs := enterInPopup(t, m)
	if len(msgs) != 1 || msgs[0] != (submitCommentMsg{taskID: "T1", text: "hello from the window"}) {
		t.Errorf("msgs = %#v", msgs)
	}
	if m.overlay != overlayNone || m.dialog != nil {
		t.Errorf("popup still open: %#v", m.dialog)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after the send", path)
	}
}

// An enter before the save must not take the file away from the window that still has it open.
func TestEditorWindowEnterOnAnUntouchedFileKeepsThePopup(t *testing.T) {
	edit := &descriptionEdit{html: "<p>a</p>", text: EditorText("<p>a</p>")}
	m, path := windowPopup(t, edit)
	m, msgs := enterInPopup(t, m)
	if len(msgs) != 0 {
		t.Errorf("msgs = %#v", msgs)
	}
	if _, ok := m.dialog.(editorWindowDialog); !ok || m.overlay != overlayDialog {
		t.Fatalf("popup closed: %#v", m.dialog)
	}
	if view := squeeze(m.dialog.View(m.theme, 80, 40)); !strings.Contains(view, "still unchanged") {
		t.Errorf("view = %q", view)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("temp file %s is gone: %v", path, err)
	}
}

func TestEditorWindowEscDropsTheFile(t *testing.T) {
	edit := &descriptionEdit{html: "<p>a</p>", text: EditorText("<p>a</p>")}
	for name, c := range map[string]struct {
		edit *descriptionEdit
		want string
	}{"description": {edit, "description edit dropped"}, "comment": {nil, "comment dropped, nothing sent"}} {
		m, path := windowPopup(t, c.edit)
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		got := next.(Model)
		if cmd == nil {
			t.Fatalf("%s: expected a command to clear the toast later", name)
		}
		if got.status.toast != c.want || got.status.toastErr {
			t.Errorf("%s: toast = %q, isErr = %v", name, got.status.toast, got.status.toastErr)
		}
		if got.overlay != overlayNone || got.dialog != nil {
			t.Errorf("%s: popup still open: %#v", name, got.dialog)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: temp file %s still exists after esc", name, path)
		}
	}
}

func TestEditorWindowQuitRemovesTheFile(t *testing.T) {
	m, path := windowPopup(t, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %#v", msgs)
	}
	if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Errorf("msgs = %#v", msgs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s still exists after the quit", path)
	}
}
