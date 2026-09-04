package config

import (
	"os"
	"path/filepath"
)

// Paths holds every file location the app uses.
type Paths struct {
	ConfigFile string
	DBFile     string
	LogFile    string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	configDir := envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dataDir := envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	stateDir := envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	return Paths{
		ConfigFile: filepath.Join(configDir, "wrike-tui", "config.toml"),
		DBFile:     filepath.Join(dataDir, "wrike-tui", "wrike.db"),
		LogFile:    filepath.Join(stateDir, "wrike-tui", "wrike-tui.log"),
	}, nil
}

func (p Paths) EnsureDirs() error {
	for _, f := range []string{p.ConfigFile, p.DBFile, p.LogFile} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return err
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
