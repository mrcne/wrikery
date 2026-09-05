// Package auth stores the Wrike API token: system keychain first, a 0600 file when there is none (ADR 004).
package auth

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	EnvToken = "WRIKERY_TOKEN"
	service  = "wrikery"
	account  = "api-token"
)

var ErrNoToken = errors.New("auth: no token stored")

type Tokens struct {
	FallbackFile string
}

// Load checks the environment, then the keychain, then the fallback file.
// A keychain error other than not found is treated like an empty keychain: on a headless Linux box
// go-keyring returns a D-Bus error rather than a typed one, and the file is where Save put the token.
func (t Tokens) Load() (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvToken)); v != "" {
		return v, nil
	}
	v, err := keyring.Get(service, account)
	if err == nil && v != "" {
		return v, nil
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		slog.Debug("keyring read failed, trying the file", "error", err)
	}
	raw, err := os.ReadFile(t.FallbackFile)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoToken
	}
	if err != nil {
		return "", err
	}
	v = strings.TrimSpace(string(raw))
	if v == "" {
		return "", ErrNoToken
	}
	return v, nil
}

func (t Tokens) Save(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("auth: empty token")
	}
	err := keyring.Set(service, account, token)
	if err == nil {
		return nil
	}
	slog.Info("keyring unavailable, storing the token in a file", "path", t.FallbackFile, "error", err)
	if err := os.MkdirAll(filepath.Dir(t.FallbackFile), 0o700); err != nil {
		return err
	}
	return os.WriteFile(t.FallbackFile, []byte(token+"\n"), 0o600)
}

// Delete removes the token from both places. Not found in either is success.
func (t Tokens) Delete() error {
	if err := keyring.Delete(service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		slog.Debug("keyring delete failed", "error", err)
	}
	if err := os.Remove(t.FallbackFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
