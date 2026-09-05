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

	// Home is only a fallback. Some setups have no HOME but do set these three.
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
		ConfigFile: filepath.Join(configDir, "wrikery", "config.toml"),
		DBFile:     filepath.Join(dataDir, "wrikery", "wrike.db"),
		LogFile:    filepath.Join(stateDir, "wrikery", "wrikery.log"),
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
