package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/config"
)

// Every case asserts the whole file, the edit touches one line and nothing else.
func TestSetEditsOneLineAndKeepsTheRest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		missing bool
		key     string
		value   any
		want    string
		wantErr string
	}{
		{name: "replace in ui", body: "# look\n[ui]\n# the theme\ntheme = \"dark\"\naccent = \"red\"\n", key: "ui.theme", value: "light",
			want: "# look\n[ui]\n# the theme\ntheme = \"light\"\naccent = \"red\"\n"},
		{name: "replace at the top level", body: "poll_interval = \"60s\"\n\n[ui]\ntheme = \"dark\"\n", key: "poll_interval", value: "2m",
			want: "poll_interval = \"2m\"\n\n[ui]\ntheme = \"dark\"\n"},
		{name: "trailing comment kept", body: "[ui]\ntheme = \"dark\" # the look\n", key: "ui.theme", value: "light",
			want: "[ui]\ntheme = \"light\" # the look\n"},
		{name: "hash inside the string", body: "[ui]\naccent = \"#7aa2f7\"\n", key: "ui.accent", value: "#ff0000",
			want: "[ui]\naccent = \"#ff0000\"\n"},
		{name: "add to an existing table before the next header's comments", body: "[ui]\ntheme = \"dark\"\n\n# the budget\n[budget]\nsource = \"effort\"\n", key: "ui.accent", value: "red",
			want: "[ui]\ntheme = \"dark\"\naccent = \"red\"\n\n# the budget\n[budget]\nsource = \"effort\"\n"},
		{name: "add a top level key before the first header", body: "log_level = \"info\"\n\n[ui]\ntheme = \"dark\"\n", key: "poll_interval", value: "90s",
			want: "log_level = \"info\"\npoll_interval = \"90s\"\n\n[ui]\ntheme = \"dark\"\n"},
		{name: "add a top level key to a file with no header", body: "log_level = \"info\"\n", key: "offline", value: true,
			want: "log_level = \"info\"\noffline = true\n"},
		{name: "add a top level key to a file that starts with a header", body: "[ui]\ntheme = \"dark\"\n", key: "poll_interval", value: "90s",
			want: "poll_interval = \"90s\"\n[ui]\ntheme = \"dark\"\n"},
		{name: "create the table at the end", body: "poll_interval = \"60s\"\n", key: "ui.theme", value: "light",
			want: "poll_interval = \"60s\"\n\n[ui]\ntheme = \"light\"\n"},
		{name: "missing file", missing: true, key: "ui.theme", value: "dark", want: "[ui]\ntheme = \"dark\"\n"},
		{name: "list value", body: "[ui]\n", key: "ui.hide_prefixes", value: []string{"(MX)", "(TX)"},
			want: "[ui]\nhide_prefixes = [\"(MX)\", \"(TX)\"]\n"},
		{name: "empty list", body: "[ui]\nhide_prefixes = [\"(MX)\"]\n", key: "ui.hide_prefixes", value: []string{},
			want: "[ui]\nhide_prefixes = []\n"},
		{name: "case of the header and the key kept", body: "[UI]\nTheme = \"dark\"\n", key: "ui.theme", value: "light",
			want: "[UI]\nTheme = \"light\"\n"},
		{name: "windows line ends", body: "[ui]\r\ntheme = \"dark\"\r\n", key: "ui.theme", value: "light",
			want: "[ui]\r\ntheme = \"light\"\n"},
		{name: "duplicate key refused", body: "[ui]\ntheme = \"dark\"\ntheme = \"light\"\n", key: "ui.theme", value: "auto", wantErr: "set ui.theme in "},
		{name: "two spellings refused", body: "[ui]\ntheme = \"dark\"\nTheme = \"light\"\n", key: "ui.theme", value: "auto", wantErr: "by hand"},
		{name: "dotted key refused", body: "ui.theme = \"dark\"\n", key: "ui.theme", value: "light", wantErr: "by hand"},
		{name: "inline table refused", body: "ui = { theme = \"dark\" }\n", key: "ui.theme", value: "light", wantErr: "by hand"},
		{name: "multi line array refused", body: "[ui]\nhide_prefixes = [\n  \"(MX)\",\n]\n", key: "ui.hide_prefixes", value: []string{"(TX)"}, wantErr: "by hand"},
		{name: "array of tables refused", body: "[[ui]]\ntheme = \"dark\"\n", key: "ui.theme", value: "light", wantErr: "by hand"},
		{name: "loader refuses the value", body: "poll_interval = \"60s\"\n", key: "poll_interval", value: "5s", wantErr: "poll_interval 5s is below 10s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			if tc.missing {
				path = filepath.Join(t.TempDir(), "config.toml")
			} else {
				path = writeConfig(t, tc.body)
			}
			_, err := config.Set(path, tc.key, tc.value)
			got, readErr := os.ReadFile(path)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Set() error = %v, want one containing %q", err, tc.wantErr)
				}
				if tc.name == "loader refuses the value" && !strings.Contains(err.Error(), "reading "+path+":") {
					t.Errorf("the error names %q, want the real file %s", err, path)
				}
				if readErr != nil || string(got) != tc.body {
					t.Errorf("file after a refused edit:\n%q\nwant it unchanged:\n%q", got, tc.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("Set() = %v", err)
			}
			if readErr != nil || string(got) != tc.want {
				t.Errorf("file:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestSetReturnsWhatTheNewFileLoads(t *testing.T) {
	path := writeConfig(t, "[ui]\ntheme = \"dark\"\n")
	cfg, err := config.Set(path, "ui.theme", "Light")
	if err != nil {
		t.Fatal(err)
	}
	// The loader lowercases the theme, so the value handed back is the file's as read, not as typed.
	if cfg.UI.Theme != "light" {
		t.Errorf("returned Theme = %q, want light", cfg.UI.Theme)
	}
	loaded, err := config.Load(path)
	if err != nil || loaded.UI.Theme != "light" {
		t.Errorf("Load after Set = %+v, %v", loaded.UI, err)
	}
}

func TestSetKeepsTheModeAndCreatesWith0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Set(path, "ui.theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("mode after Set = %v, %v, want 0644 kept", fi.Mode(), err)
	}
	fresh := filepath.Join(t.TempDir(), "config.toml")
	if _, err := config.Set(fresh, "ui.theme", "dark"); err != nil {
		t.Fatal(err)
	}
	// The file may hold a client secret, so a new one is private.
	if fi, err := os.Stat(fresh); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode of a new file = %v, %v, want 0600", fi.Mode(), err)
	}
}

// A config kept in a dotfiles directory is a link, the edit goes to the target and the link stays.
func TestSetFollowsASymbolicLink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "real.toml")
	if err := os.WriteFile(target, []byte("[ui]\ntheme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Set(link, "ui.theme", "light"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "[ui]\ntheme = \"light\"\n" {
		t.Errorf("target after Set = %q, %v", got, err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", fi, err)
	}
}
