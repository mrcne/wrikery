// Package config loads the TOML config file and resolves XDG paths.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	LogLevel     string        `toml:"log_level"`
	PollInterval time.Duration `toml:"poll_interval"`
	// Host is the Wrike API host to use, empty means detect it on first run.
	// The answer is remembered, see cmd/wrikery for the probe.
	Host string `toml:"host"`
	// Offline starts the interface and every command without syncing, the --offline flag on the command line wins over it.
	Offline bool `toml:"offline"`
	// OAuth names the API app for the browser sign in, the first run offers it only when ClientID is set.
	OAuth OAuthConfig `toml:"oauth"`
	UI    UIConfig    `toml:"ui"`
}

// OAuthConfig is an API app registered in Wrike's App Console, the bring your own app of the browser sign in.
type OAuthConfig struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"` // optional, the first run asks for it when missing
	RedirectURI  string `toml:"redirect_uri"`  // must match the app's registration, a port on localhost gets a listener
}

const defaultRedirectURI = "http://localhost:8731/callback"

type UIConfig struct {
	Theme          string `toml:"theme"`
	Accent         string `toml:"accent"`
	ASCII          bool   `toml:"ascii"`
	BranchTemplate string `toml:"branch_template"`
	// HidePrefixes are title prefixes the rows and cards leave out, a project code every task starts with for example.
	HidePrefixes []string `toml:"hide_prefixes"`
}

var levels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

const defaultBranchTemplate = "{id}-{slug}"

func defaults() Config {
	return Config{
		LogLevel:     "info",
		PollInterval: 60 * time.Second,
		OAuth:        OAuthConfig{RedirectURI: defaultRedirectURI},
		UI:           UIConfig{Theme: "auto", BranchTemplate: defaultBranchTemplate},
	}
}

// Load reads the config file. A missing file is fine and yields defaults.
func Load(path string) (Config, error) {
	cfg := defaults()
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
	cfg.UI.Theme = strings.ToLower(cfg.UI.Theme)
	switch cfg.UI.Theme {
	case "auto", "dark", "light":
	default:
		return Config{}, fmt.Errorf("reading %s: unknown ui.theme %q", path, cfg.UI.Theme)
	}
	// Below ten seconds the poll alone would eat a large share of the roughly 400 requests per minute Wrike allows (https://developers.wrike.com/faq/).
	if cfg.PollInterval < 10*time.Second {
		return Config{}, fmt.Errorf("reading %s: poll_interval %s is below 10s", path, cfg.PollInterval)
	}
	// An empty template would produce no branch name at all, so it falls back instead of failing the load.
	if cfg.UI.BranchTemplate == "" {
		cfg.UI.BranchTemplate = defaultBranchTemplate
	}
	// A scheme or a path would silently break wrike.BaseURL, which only prefixes https:// and appends /api/v4,
	// so reject anything that is not a bare host name up front.
	if cfg.Host != "" && strings.Contains(cfg.Host, "/") {
		return Config{}, fmt.Errorf("reading %s: host %q must be a host name such as app-eu.wrike.com", path, cfg.Host)
	}
	cfg.OAuth.ClientID = strings.TrimSpace(cfg.OAuth.ClientID)
	cfg.OAuth.ClientSecret = strings.TrimSpace(cfg.OAuth.ClientSecret)
	if cfg.OAuth.ClientSecret != "" && cfg.OAuth.ClientID == "" {
		return Config{}, fmt.Errorf("reading %s: oauth.client_secret needs oauth.client_id, the id of the same API app", path)
	}
	if cfg.OAuth.RedirectURI == "" {
		cfg.OAuth.RedirectURI = defaultRedirectURI
	}
	// The sign in binds a listener to the port of this address and Wrike compares it with the registered one, so a bare host or another scheme would fail late.
	if u, err := url.Parse(cfg.OAuth.RedirectURI); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, fmt.Errorf("reading %s: oauth.redirect_uri %q must be an http or https address such as %s", path, cfg.OAuth.RedirectURI, defaultRedirectURI)
	}
	return cfg, nil
}

func (c Config) SlogLevel() slog.Level {
	return levels[c.LogLevel]
}
