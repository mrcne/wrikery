package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/config"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
}

func TestLoadReadsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`log_level = "debug"`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestLoadReadsOffline(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil || cfg.Offline {
		t.Fatalf("defaults = %v, %v, want online", cfg.Offline, err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("offline = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil || !cfg.Offline {
		t.Errorf("offline = %v, %v, want true from the file", cfg.Offline, err)
	}
}

func TestLoadRejectsUnknownLogLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`log_level = "loud"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("want error for unknown log_level, got nil")
	}
}

func TestLoadRejectsMalformedToml(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`log_level = `), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("want error for malformed toml, got nil")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("loglevel = \"debug\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("want error for a misspelled key, got nil")
	}
}

func TestLoadAcceptsUppercaseLogLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`log_level = "DEBUG"`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestDefaultPathsHonorXDGEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != "/tmp/xdg-config/wrikery/config.toml" {
		t.Errorf("ConfigFile = %q", p.ConfigFile)
	}
	if p.DBFile != "/tmp/xdg-data/wrikery/wrike.db" {
		t.Errorf("DBFile = %q", p.DBFile)
	}
	if p.LogFile != "/tmp/xdg-state/wrikery/wrikery.log" {
		t.Errorf("LogFile = %q", p.LogFile)
	}
	if p.SessionFile != "/tmp/xdg-config/wrikery/oauth.json" || p.AuthLockFile != "/tmp/xdg-config/wrikery/auth.lock" {
		t.Errorf("SessionFile = %q, AuthLockFile = %q, want both beside the token file", p.SessionFile, p.AuthLockFile)
	}
}

func TestEnsureDirsCreatesTheCredentialDirectory(t *testing.T) {
	base := t.TempDir()
	// --config points elsewhere, so the config file's directory is not the one the credentials and the lock live in.
	p := config.Paths{
		ConfigFile:   filepath.Join(base, "elsewhere", "config.toml"),
		DBFile:       filepath.Join(base, "data", "wrike.db"),
		LogFile:      filepath.Join(base, "state", "wrikery.log"),
		TokenFile:    filepath.Join(base, "config", "wrikery", "token"),
		SessionFile:  filepath.Join(base, "config", "wrikery", "oauth.json"),
		AuthLockFile: filepath.Join(base, "config", "wrikery", "auth.lock"),
	}
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "config", "wrikery")); err != nil {
		t.Fatalf("the credential directory is missing: %v", err)
	}
}

func TestEnsureDirsCreatesParents(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "c"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "d"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "s"))
	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "d", "wrikery")); err != nil {
		t.Errorf("data dir not created: %v", err)
	}
}

// Every XDG variable is set, so nothing needs the home directory. A hardened service unit can run that way.
func TestDefaultPathsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() = %v, want no error", err)
	}
	if p.ConfigFile != "/tmp/xdg-config/wrikery/config.toml" {
		t.Errorf("ConfigFile = %q", p.ConfigFile)
	}
}

func writeConfig(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsUITableAndPollInterval(t *testing.T) {
	path := writeConfig(t, `
poll_interval = "2m"
[ui]
theme = "dark"
accent = "#7aa2f7"
ascii = true
branch_template = "feat/{id}-{slug}"
hide_prefixes = ["(MX)", "(TX)"]
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 2*time.Minute {
		t.Errorf("PollInterval = %v", cfg.PollInterval)
	}
	if cfg.UI.Theme != "dark" || cfg.UI.Accent != "#7aa2f7" || !cfg.UI.ASCII || cfg.UI.BranchTemplate != "feat/{id}-{slug}" {
		t.Errorf("UI = %+v", cfg.UI)
	}
	if len(cfg.UI.HidePrefixes) != 2 || cfg.UI.HidePrefixes[0] != "(MX)" || cfg.UI.HidePrefixes[1] != "(TX)" {
		t.Errorf("HidePrefixes = %v, want the two codes", cfg.UI.HidePrefixes)
	}
}

func TestLoadDefaultsForUI(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 60*time.Second || cfg.UI.Theme != "auto" || cfg.UI.BranchTemplate != "{id}-{slug}" {
		t.Errorf("defaults = %+v", cfg)
	}
}

func TestLoadRejectsBadThemeAndShortPoll(t *testing.T) {
	for _, body := range []string{"[ui]\ntheme = \"solarized\"\n", "poll_interval = \"2s\"\n"} {
		if _, err := config.Load(writeConfig(t, body)); err == nil {
			t.Errorf("Load(%q) succeeded, want error", body)
		}
	}
}

func TestLoadAcceptsHost(t *testing.T) {
	path := writeConfig(t, `host = "app-eu.wrike.com"`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "app-eu.wrike.com" {
		t.Errorf("Host = %q, want app-eu.wrike.com", cfg.Host)
	}
}

func TestLoadRejectsHostWithSchemeOrSlash(t *testing.T) {
	for _, host := range []string{"https://app-eu.wrike.com", "app-eu.wrike.com/", "app-eu.wrike.com/api/v4"} {
		if _, err := config.Load(writeConfig(t, `host = "`+host+`"`)); err == nil {
			t.Errorf("Load(host=%q) succeeded, want error", host)
		}
	}
}

func TestDefaultPathsHasTokenFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.TokenFile != "/tmp/xdg-config/wrikery/token" {
		t.Errorf("TokenFile = %q", p.TokenFile)
	}
}

func TestLoadReadsTheOAuthTable(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "[oauth]\nclient_id = \" id1 \"\nclient_secret = \"s3cret\"\nredirect_uri = \"http://localhost:9000/cb\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := config.OAuthConfig{ClientID: "id1", ClientSecret: "s3cret", RedirectURI: "http://localhost:9000/cb"}
	if cfg.OAuth != want {
		t.Errorf("OAuth = %+v, want %+v", cfg.OAuth, want)
	}
}

func TestLoadDefaultsTheRedirectURI(t *testing.T) {
	for name, body := range map[string]string{"with an app": "[oauth]\nclient_id = \"id1\"\n", "without": ""} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Load(writeConfig(t, body))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OAuth.RedirectURI != "http://localhost:8731/callback" {
				t.Errorf("RedirectURI = %q", cfg.OAuth.RedirectURI)
			}
		})
	}
}

func TestLoadRefusesASecretWithoutAnID(t *testing.T) {
	_, err := config.Load(writeConfig(t, "[oauth]\nclient_secret = \"s3cret\"\n"))
	if err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("err = %v, want a refusal naming client_id", err)
	}
}

func TestLoadRefusesABadRedirectURI(t *testing.T) {
	for _, uri := range []string{"ftp://localhost/cb", "localhost:8731/cb", "http://", "://x"} {
		_, err := config.Load(writeConfig(t, "[oauth]\nclient_id = \"id1\"\nredirect_uri = \""+uri+"\"\n"))
		if err == nil || !strings.Contains(err.Error(), "redirect_uri") {
			t.Errorf("redirect_uri %q: err = %v, want a refusal", uri, err)
		}
	}
}

func TestLoadReadsAndChecksTheBudgetTable(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		want    config.BudgetConfig
		wantErr string
	}{
		"field":          {body: "[budget]\nsource = \"field\"\nfield = \"Budget\"\n", want: config.BudgetConfig{Source: "field", Field: "Budget"}},
		"effort":         {body: "[budget]\nsource = \"Effort\"\n", want: config.BudgetConfig{Source: "effort"}},
		"none":           {body: "", want: config.BudgetConfig{}},
		"unknown source": {body: "[budget]\nsource = \"money\"\n", wantErr: "budget.source"},
		"field missing":  {body: "[budget]\nsource = \"field\"\n", wantErr: "budget.field"},
		"field alone":    {body: "[budget]\nfield = \"Budget\"\n", wantErr: "budget.source"},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want it to name %s", name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || cfg.Budget != tc.want {
			t.Errorf("%s: budget = %+v, %v, want %+v", name, cfg.Budget, err, tc.want)
		}
	}
}
