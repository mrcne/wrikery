// Package config loads the TOML config file and resolves XDG paths.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	LogLevel string `toml:"log_level"`
}

var levels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// Load reads the config file. A missing file is fine and yields defaults.
func Load(path string) (Config, error) {
	cfg := Config{LogLevel: "info"}
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	// The decoder drops keys it does not know, so a typo would silently do nothing.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		return Config{}, fmt.Errorf("reading %s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	// The decoder matches key names case-insensitively, so accept values the same way.
	cfg.LogLevel = strings.ToLower(cfg.LogLevel)
	if _, ok := levels[cfg.LogLevel]; !ok {
		return Config{}, fmt.Errorf("reading %s: unknown log_level %q", path, cfg.LogLevel)
	}
	return cfg, nil
}

func (c Config) SlogLevel() slog.Level {
	return levels[c.LogLevel]
}
