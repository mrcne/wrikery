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
	TokenFile  string
	// SessionFile and AuthLockFile sit beside the token file: an OAuth session when there is no keychain,
	// and the lock a renewal holds so two processes never refresh the same pair.
	SessionFile  string
	AuthLockFile string
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
		ConfigFile:   filepath.Join(configDir, "wrikery", "config.toml"),
		DBFile:       filepath.Join(dataDir, "wrikery", "wrike.db"),
		LogFile:      filepath.Join(stateDir, "wrikery", "wrikery.log"),
		TokenFile:    filepath.Join(configDir, "wrikery", "token"),
		SessionFile:  filepath.Join(configDir, "wrikery", "oauth.json"),
		AuthLockFile: filepath.Join(configDir, "wrikery", "auth.lock"),
	}, nil
}

func (p Paths) EnsureDirs() error {
	// The lock file's directory too: with --config elsewhere it is not the config file's, and a lock that cannot be opened runs the renewal unlocked.
	for _, f := range []string{p.ConfigFile, p.DBFile, p.LogFile, p.AuthLockFile} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return err
		}
	}
	return nil
}
