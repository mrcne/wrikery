package main

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestClipboardToolBySession(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}
	// A Wayland session on a box that also runs Xwayland has DISPLAY set as well, so the order of the checks matters.
	wayland := map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}
	if got := clipboardTool("darwin", env(nil)); !reflect.DeepEqual(got, []string{"pbcopy"}) {
		t.Errorf("darwin: got %v, want [pbcopy]", got)
	}
	if got := clipboardTool("linux", env(wayland)); !reflect.DeepEqual(got, []string{"wl-copy"}) {
		t.Errorf("wayland: got %v, want [wl-copy]", got)
	}
	if got := clipboardTool("linux", env(nil)); got != nil {
		t.Errorf("no graphical session: got %v, want no tool", got)
	}
	// The X11 branch takes the first tool that is installed, so what to expect is read off this machine too.
	var wantX11 []string
	switch {
	case lookPathOK("xclip"):
		wantX11 = []string{"xclip", "-selection", "clipboard"}
	case lookPathOK("xsel"):
		wantX11 = []string{"xsel", "--clipboard", "--input"}
	}
	if got := clipboardTool("linux", env(map[string]string{"DISPLAY": ":0"})); !reflect.DeepEqual(got, wantX11) {
		t.Errorf("x11: got %v, want %v", got, wantX11)
	}
}

func lookPathOK(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
