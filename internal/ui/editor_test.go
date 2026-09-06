package ui

import (
	"os"
	"reflect"
	"testing"
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
	if _, err := openEditor("T"); err == nil {
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
