package config_test

import (
	"os"
	"path/filepath"
	"testing"

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

func TestDefaultPathsHonorXDGEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != "/tmp/xdg-config/wrike-tui/config.toml" {
		t.Errorf("ConfigFile = %q", p.ConfigFile)
	}
	if p.DBFile != "/tmp/xdg-data/wrike-tui/wrike.db" {
		t.Errorf("DBFile = %q", p.DBFile)
	}
	if p.LogFile != "/tmp/xdg-state/wrike-tui/wrike-tui.log" {
		t.Errorf("LogFile = %q", p.LogFile)
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
	if _, err := os.Stat(filepath.Join(base, "d", "wrike-tui")); err != nil {
		t.Errorf("data dir not created: %v", err)
	}
}
