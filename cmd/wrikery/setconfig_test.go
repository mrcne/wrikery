package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/config"
)

func TestSetConfigWritesTheFileAndAppliesTheKey(t *testing.T) {
	a := &app{cfg: config.Config{PollInterval: time.Minute, LogLevel: "info"}, configFile: filepath.Join(t.TempDir(), "config.toml"), logLevel: new(slog.LevelVar)}
	cfg, err := a.setConfig("poll_interval", "2m")
	if err != nil || cfg.PollInterval != 2*time.Minute || a.cfg.PollInterval != 2*time.Minute {
		t.Fatalf("setConfig = %+v, %v, a.cfg.PollInterval = %v, want two minutes in both", cfg, err, a.cfg.PollInterval)
	}
	if _, err := a.setConfig("log_level", "debug"); err != nil {
		t.Fatal(err)
	}
	if a.logLevel.Level() != slog.LevelDebug || a.cfg.LogLevel != "debug" {
		t.Errorf("after the save the level is %v and the config says %q, want debug", a.logLevel.Level(), a.cfg.LogLevel)
	}
	got, _ := os.ReadFile(a.configFile)
	if string(got) != "poll_interval = \"2m\"\nlog_level = \"debug\"\n" {
		t.Errorf("file = %q", got)
	}
}

// The save applies the one key, a host or an app changed by hand since the start waits for the next start and its sign in check.
func TestSetConfigDoesNotTakeOverAKeyChangedByHand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("host = \"app-eu.wrike.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config.Config{PollInterval: time.Minute, LogLevel: "info"}, configFile: path, logLevel: new(slog.LevelVar)}
	cfg, err := a.setConfig("ui.theme", "dark")
	if err != nil || cfg.UI.Theme != "dark" {
		t.Fatalf("setConfig = %+v, %v", cfg, err)
	}
	if a.cfg.Host != "" || cfg.Host != "" {
		t.Errorf("the host from the file was taken over: running %q, returned %q", a.cfg.Host, cfg.Host)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "host = \"app-eu.wrike.com\"") {
		t.Errorf("the host line is gone from the file: %q", got)
	}
}

func TestSetConfigReturnsTheLoaderError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("poll_interval = \"60s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config.Config{PollInterval: time.Minute, LogLevel: "info"}, configFile: path, logLevel: new(slog.LevelVar)}
	_, err := a.setConfig("poll_interval", "5s")
	if err == nil || !strings.Contains(err.Error(), "poll_interval 5s is below 10s") {
		t.Fatalf("setConfig = %v, want the loader's refusal", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "poll_interval = \"60s\"\n" || a.cfg.PollInterval != time.Minute {
		t.Errorf("a refused save changed something: file %q, running %v", got, a.cfg.PollInterval)
	}
}
