package main

import (
	"bytes"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/zalando/go-keyring"

	"github.com/mrcne/wrikery/internal/cli"
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
	fs, o := rootFlags()
	o.demo, o.noColor = true, true
	if code := runCommand([]string{"help"}, fs, o); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRunCommandRefusesVersionAndLogout(t *testing.T) {
	for name, c := range map[string][2]bool{"version": {true, false}, "logout": {false, true}} {
		fs, o := rootFlags()
		o.noColor, o.version, o.logout = true, c[0], c[1]
		if code := runCommand([]string{"help"}, fs, o); code != 2 {
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
	fs, o := rootFlags()
	o.noColor = true
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {"help", "task", "status"}} {
		if code := runCommand(args, fs, o); code != 0 {
			t.Errorf("%v: code = %d, want 0", args, code)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Errorf("help created %v under the temp dir, err %v", entries, err)
	}
}

func TestRunCommandUsageErrorStillExitsWithTwo(t *testing.T) {
	isolate(t)
	fs, o := rootFlags()
	o.noColor = true
	if code := runCommand([]string{"task"}, fs, o); code != 2 {
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

// The flag on the command line wins over the config key in both directions, the key alone decides when the flag is absent.
func TestOfflineModeFlagWinsOverTheConfigKey(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		key  bool
		flag *bool
		want bool
	}{
		{false, nil, false},
		{true, nil, true},
		{false, &yes, true},
		{true, &no, false},
	}
	for _, c := range cases {
		if got := offlineMode(config.Config{Offline: c.key}, c.flag); got != c.want {
			t.Errorf("key %v, flag %v: offline = %v, want %v", c.key, c.flag, got, c.want)
		}
	}
}

func TestOfflineArgSeesAnExplicitFalse(t *testing.T) {
	parse := func(args ...string) *bool {
		fs := flag.NewFlagSet("wrikery", flag.ContinueOnError)
		offline := fs.Bool("offline", false, "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return offlineArg(fs, offline)
	}
	if got := parse("--offline=false"); got == nil || *got {
		t.Errorf("--offline=false = %v, want an explicit false", got)
	}
	if got := parse("--offline"); got == nil || !*got {
		t.Errorf("--offline = %v, want true", got)
	}
	if got := parse(); got != nil {
		t.Errorf("no flag = %v, want nil, the config decides", *got)
	}
}

// The first start needs Wrike, and the way out has to work whether the mode came from the flag or from the file.
func TestOfflineFirstRunNamesAWayOutThatWorks(t *testing.T) {
	dir := isolate(t)
	cfgDir := filepath.Join(dir, "config", "wrikery")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("offline = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(false, false, "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "--offline=false") {
		t.Errorf("run = %v, want the refusal naming --offline=false", err)
	}
}

func TestParseRootPrintsTheHelpPageOnStdout(t *testing.T) {
	for _, word := range []string{"--help", "-h"} {
		fs, _ := rootFlags()
		var out, errOut bytes.Buffer
		code, done := parseRoot(fs, []string{word}, &out, &errOut)
		if code != 0 || !done || errOut.Len() != 0 || out.String() != cli.HelpPage(fs) {
			t.Errorf("%s: code %d, done %v, stderr %q, stdout:\n%s", word, code, done, errOut.String(), out.String())
		}
	}
	fs, _ := rootFlags()
	var out, errOut bytes.Buffer
	code, done := parseRoot(fs, []string{"--demmo"}, &out, &errOut)
	if code != 2 || !done || out.Len() != 0 || !strings.Contains(errOut.String(), "wrikery: flag provided but not defined: --demmo\n") || !strings.Contains(errOut.String(), "usage: wrikery [flags]") {
		t.Errorf("--demmo: code %d, done %v, stdout %q, stderr:\n%s", code, done, out.String(), errOut.String())
	}
	fs, o := rootFlags()
	out.Reset()
	errOut.Reset()
	if code, done := parseRoot(fs, []string{"--demo"}, &out, &errOut); done || code != 0 || !o.demo || out.Len()+errOut.Len() != 0 {
		t.Errorf("--demo: code %d, done %v, demo %v, output %q %q", code, done, o.demo, out.String(), errOut.String())
	}
}

func TestHelpPageListsTheGlobalFlags(t *testing.T) {
	fs, _ := rootFlags()
	_, flags, ok := strings.Cut(cli.HelpPage(fs), "Flags:")
	if !ok {
		t.Fatalf("the page has no Flags section:\n%s", cli.HelpPage(fs))
	}
	for _, name := range []string{"config", "demo", "logout", "no-color", "offline", "version"} {
		if !strings.Contains(flags, name) {
			t.Errorf("the flags lack %q:\n%s", name, flags)
		}
	}
}

// A help request must not need the config or the store, a typo in the config file would turn it into exit 1,
// and on a fresh machine it would create the data directory and an empty database.
func TestCommandHelpSkipsTheConfigAndTheStore(t *testing.T) {
	dir := isolate(t)
	confDir := filepath.Join(dir, "config", "wrikery")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.toml"), []byte("poll_interval = [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, o := rootFlags()
	o.noColor = true
	for _, args := range [][]string{{"task", "--help"}, {"ui", "-h"}, {"sync", "--help"}, {"task", "list", "--help"}, {"help", "task", "create"}} {
		if code := runCommand(args, fs, o); code != 0 {
			t.Errorf("%v: code = %d, want 0 with a broken config file", args, code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "data")); !os.IsNotExist(err) {
		t.Errorf("help created the data directory, stat err %v", err)
	}
}
