package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionLine(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		goVer   string
		want    string
	}{
		{
			name:    "full commit is shortened to seven characters",
			version: "v0.1.0",
			commit:  "abc1234def",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0 (abc1234) go1.26",
		},
		{
			name:    "empty commit drops the parentheses",
			version: "v0.1.0",
			commit:  "",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0 go1.26",
		},
		{
			name:    "a commit shorter than seven characters is printed whole",
			version: "v0.1.0",
			commit:  "abc12",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0 (abc12) go1.26",
		},
		{
			name:    "a git describe version that already holds the short hash drops the parentheses",
			version: "v0.1.0-3-gabc1234-dirty",
			commit:  "abc1234def",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0-3-gabc1234-dirty go1.26",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionLine(tt.version, tt.commit, tt.goVer); got != tt.want {
				t.Errorf("versionLine(%q, %q, %q) = %q, want %q",
					tt.version, tt.commit, tt.goVer, got, tt.want)
			}
		})
	}
}

func TestRunCommandRefusesDemoMode(t *testing.T) {
	if code := runCommand([]string{"help"}, "", true, true); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRunCommandHelpNeedsNoTokenAndNoTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WRIKERY_TOKEN", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	if code := runCommand([]string{"help"}, "", true, false); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "wrikery", "wrike.db")); err != nil {
		t.Errorf("the command did not open the store: %v", err)
	}
}
