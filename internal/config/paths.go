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
	configDir := os.Getenv("XDG_CONFIG_HOME")
	dataDir := os.Getenv("XDG_DATA_HOME")
	stateDir := os.Getenv("XDG_STATE_HOME")

	// The home directory only serves as a fallback, so resolve it only when one of the variables is missing.
	// A hardened service unit can run without HOME but with all three set.
	if configDir == "" || dataDir == "" || stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		if configDir == "" {
			configDir = filepath.Join(home, ".config")
		}
		if dataDir == "" {
			dataDir = filepath.Join(home, ".local", "share")
		}
		if stateDir == "" {
			stateDir = filepath.Join(home, ".local", "state")
		}
	}

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
