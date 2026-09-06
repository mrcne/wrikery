package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClipboardToolBySession(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}
	// A Wayland session on a box that also runs Xwayland has DISPLAY set as well, so the order of the checks matters.
	wayland := map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}
	x11 := map[string]string{"DISPLAY": ":0"}
	// The tools are stubbed on PATH, or the expectations would depend on what this machine happens to have installed.
	t.Setenv("PATH", stubTools(t, "pbcopy", "wl-copy", "xclip", "xsel"))
	if got := clipboardTool("darwin", env(nil)); !reflect.DeepEqual(got, []string{"pbcopy"}) {
		t.Errorf("darwin: got %v, want [pbcopy]", got)
	}
	if got := clipboardTool("linux", env(wayland)); !reflect.DeepEqual(got, []string{"wl-copy"}) {
		t.Errorf("wayland: got %v, want [wl-copy]", got)
	}
	if got := clipboardTool("linux", env(x11)); !reflect.DeepEqual(got, []string{"xclip", "-selection", "clipboard"}) {
		t.Errorf("x11: got %v, want xclip", got)
	}
	if got := clipboardTool("linux", env(nil)); got != nil {
		t.Errorf("no graphical session: got %v, want no tool", got)
	}
	t.Run("only xsel installed", func(t *testing.T) {
		t.Setenv("PATH", stubTools(t, "xsel"))
		if got := clipboardTool("linux", env(x11)); !reflect.DeepEqual(got, []string{"xsel", "--clipboard", "--input"}) {
			t.Errorf("x11 without xclip: got %v, want xsel", got)
		}
	})
	// A session whose tool is not installed gets nothing, so the copy goes through the terminal alone and reports no failure.
	t.Run("nothing installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		for _, tc := range []struct {
			name, goos string
			vars       map[string]string
		}{
			{"darwin", "darwin", nil},
			{"wayland", "linux", wayland},
			{"x11", "linux", x11},
		} {
			if got := clipboardTool(tc.goos, env(tc.vars)); got != nil {
				t.Errorf("%s without a tool on PATH: got %v, want no tool", tc.name, got)
			}
		}
	})
}

// stubTools writes an empty executable per name into a fresh directory and returns it as a PATH.
func stubTools(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
