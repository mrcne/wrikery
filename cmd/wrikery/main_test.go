package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/zalando/go-keyring"

	"github.com/mrcne/wrikery/internal/config"
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
	if code := runCommand([]string{"help"}, "", true, true, false, false); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRunCommandRefusesVersionAndLogout(t *testing.T) {
	for name, c := range map[string][2]bool{"version": {true, false}, "logout": {false, true}} {
		if code := runCommand([]string{"help"}, "", true, false, c[0], c[1]); code != 2 {
			t.Errorf("--%s: code = %d, want 2", name, code)
		}
	}
}

// isolate points every directory the app resolves at a temp dir and keeps the keychain out of the test.
func isolate(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("WRIKERY_TOKEN", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	// runCommand points the default logger at a file that is closed when it returns.
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	return dir
}

func TestRunCommandHelpNeedsNoTokenAndNoTerminal(t *testing.T) {
	dir := isolate(t)
	for _, arg := range []string{"help", "-h", "--help"} {
		if code := runCommand([]string{arg}, "", true, false, false, false); code != 0 {
			t.Errorf("%s: code = %d, want 0", arg, code)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Errorf("help created %v under the temp dir, err %v", entries, err)
	}
}

func TestRunCommandUsageErrorStillExitsWithTwo(t *testing.T) {
	isolate(t)
	if code := runCommand([]string{"task"}, "", true, false, false, false); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestResolveThemeGivesDarkInAPipeAndKeepsAnExplicitTheme(t *testing.T) {
	if got := resolveTheme(config.UIConfig{Theme: "auto", ASCII: true}, false); got.Theme != "dark" || !got.ASCII {
		t.Errorf("auto in a pipe = %+v, want dark with the other fields kept", got)
	}
	// The background answer is pinned, or the test would send the query to the real terminal and accept either answer.
	lipgloss.SetHasDarkBackground(false)
	t.Cleanup(func() { lipgloss.SetHasDarkBackground(true) })
	if got := resolveTheme(config.UIConfig{Theme: "auto", ASCII: true}, true); got.Theme != "light" || !got.ASCII {
		t.Errorf("auto on a light terminal = %+v, want light with the other fields kept", got)
	}
	for _, theme := range []string{"light", "dark", "mono"} {
		for _, terminal := range []bool{false, true} {
			if got := resolveTheme(config.UIConfig{Theme: theme}, terminal); got.Theme != theme {
				t.Errorf("theme %q, terminal %v resolved to %q", theme, terminal, got.Theme)
			}
		}
	}
}
