// Package auth stores what signs the user in: a permanent API token, or an OAuth session renewed through its refresh token.
// System keychain first, a 0600 file when there is none (ADR 004 and ADR 005).
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	EnvToken       = "WRIKERY_TOKEN"
	service        = "wrikery"
	tokenAccount   = "api-token"
	sessionAccount = "oauth-session"
)

var ErrNoToken = errors.New("auth: not signed in")

// Session is what one sign in left behind.
// A permanent token is a session with the access token alone.
// A browser sign in also has the refresh token, the expiry, the host of the user's data center and the app it was made with,
// so a renewal needs nothing from the config.
type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Host         string    `json:"host,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
}

// OAuth is true for a session the browser sign in made, one with a refresh token.
func (s Session) OAuth() bool { return s.RefreshToken != "" }

// Tokens is the credential store: two keychain items, one per kind, each with a file next to the config as the fallback.
type Tokens struct {
	TokenFile   string // the permanent token when there is no keychain
	SessionFile string // the OAuth session when there is no keychain
	LockFile    string // held while a session is renewed, so two processes never refresh the same pair
}

// Load checks the environment, then the OAuth session, then the permanent token.
// A keychain error other than not found is treated like an empty keychain.
// On a headless Linux box go-keyring returns a D-Bus error rather than a typed one, and the file is where the save put the value.
func (t Tokens) Load() (Session, error) {
	if v := strings.TrimSpace(os.Getenv(EnvToken)); v != "" {
		return Session{AccessToken: v}, nil
	}
	raw, err := read(sessionAccount, t.SessionFile)
	if err != nil {
		return Session{}, err
	}
	if raw != "" {
		var s Session
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return Session{}, fmt.Errorf("auth: reading the stored session: %w", err)
		}
		if s.AccessToken != "" {
			return s, nil
		}
	}
	raw, err = read(tokenAccount, t.TokenFile)
	if err != nil {
		return Session{}, err
	}
	if raw == "" {
		return Session{}, ErrNoToken
	}
	return Session{AccessToken: raw}, nil
}

// SaveToken stores a permanent token and removes a browser session, one sign in replaces the previous one.
func (t Tokens) SaveToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("auth: empty token")
	}
	if err := write(tokenAccount, t.TokenFile, token); err != nil {
		return err
	}
	if err := remove(sessionAccount, t.SessionFile); err != nil {
		slog.Debug("removing the session after a token save failed", "error", err)
	}
	return nil
}

// SaveSession stores a browser session and removes a permanent token.
// A renewal calls it before the new pair is used, so another process finds the pair that is valid now.
func (t Tokens) SaveSession(s Session) error {
	if s.AccessToken == "" || s.RefreshToken == "" {
		return errors.New("auth: a session needs an access token and a refresh token")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := write(sessionAccount, t.SessionFile, string(raw)); err != nil {
		return err
	}
	if err := remove(tokenAccount, t.TokenFile); err != nil {
		slog.Debug("removing the token after a session save failed", "error", err)
	}
	return nil
}

// Delete removes both kinds from both places. Not found anywhere is success.
func (t Tokens) Delete() error {
	return errors.Join(remove(tokenAccount, t.TokenFile), remove(sessionAccount, t.SessionFile))
}

// read returns the stored value of one kind, empty when there is none.
func read(account, file string) (string, error) {
	v, err := keyring.Get(service, account)
	if err == nil && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), nil
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		slog.Debug("keyring read failed, trying the file", "account", account, "error", err)
	}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// write puts the value into the keychain, or into the file when there is no keychain.
func write(account, file, value string) error {
	err := keyring.Set(service, account, value)
	if err == nil {
		// A stale fallback file would otherwise be read by a later headless run that has no keychain,
		// handing it a revoked value instead of ErrNoToken.
		if rmErr := os.Remove(file); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			slog.Debug("removing a stale credential file failed", "path", file, "error", rmErr)
		}
		return nil
	}
	slog.Info("keyring unavailable, storing the credential in a file", "path", file, "error", err)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(value+"\n"), 0o600)
}

// remove takes one kind out of the keychain and off the disk, a missing item or file is fine.
func remove(account, file string) error {
	if err := keyring.Delete(service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		slog.Debug("keyring delete failed", "account", account, "error", err)
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
