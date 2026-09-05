package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestFileFallbackWhenKeyringUnavailable(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	file := filepath.Join(t.TempDir(), "wrikery", "token")
	tk := Tokens{FallbackFile: file}

	if _, err := tk.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load on empty = %v, want ErrNoToken", err)
	}
	if err := tk.Save("  abc123\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
	got, err := tk.Load()
	if err != nil || got != "abc123" {
		t.Fatalf("Load = %q, %v, want abc123", got, err)
	}
	if err := tk.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := tk.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load after Delete = %v, want ErrNoToken", err)
	}
}

func TestKeyringPreferredOverFile(t *testing.T) {
	keyring.MockInit()
	file := filepath.Join(t.TempDir(), "token")
	tk := Tokens{FallbackFile: file}
	if err := tk.Save("fromkeyring"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file written although the keyring worked: %v", err)
	}
	got, err := tk.Load()
	if err != nil || got != "fromkeyring" {
		t.Fatalf("Load = %q, %v", got, err)
	}
}

func TestEnvOverridesEverything(t *testing.T) {
	keyring.MockInit()
	t.Setenv(EnvToken, "fromenv")
	tk := Tokens{FallbackFile: filepath.Join(t.TempDir(), "token")}
	got, err := tk.Load()
	if err != nil || got != "fromenv" {
		t.Fatalf("Load = %q, %v", got, err)
	}
}

func TestSaveRejectsEmpty(t *testing.T) {
	keyring.MockInit()
	tk := Tokens{FallbackFile: filepath.Join(t.TempDir(), "token")}
	if err := tk.Save("   "); err == nil {
		t.Fatal("Save of blank token succeeded")
	}
}
