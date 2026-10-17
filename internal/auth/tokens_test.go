package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func testTokens(t *testing.T) Tokens {
	t.Helper()
	// The app creates the config directory at every start, the lock file needs it to exist.
	dir := filepath.Join(t.TempDir(), "wrikery")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Tokens{TokenFile: filepath.Join(dir, "token"), SessionFile: filepath.Join(dir, "oauth.json"), LockFile: filepath.Join(dir, "auth.lock")}
}

func oauthSession(expiry time.Time) Session {
	return Session{AccessToken: "at1", RefreshToken: "rt1", Expiry: expiry, Host: "app-eu.wrike.com", ClientID: "id1", ClientSecret: "s3cret"}
}

func TestFileFallbackWhenKeyringUnavailable(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	tk := testTokens(t)

	if _, err := tk.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load on empty = %v, want ErrNoToken", err)
	}
	if err := tk.SaveToken("  abc123\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tk.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
	got, err := tk.Load()
	if err != nil || got.AccessToken != "abc123" || got.OAuth() {
		t.Fatalf("Load = %+v, %v, want the token abc123", got, err)
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
	tk := testTokens(t)
	if err := tk.SaveToken("fromkeyring"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tk.TokenFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file written although the keyring worked: %v", err)
	}
	got, err := tk.Load()
	if err != nil || got.AccessToken != "fromkeyring" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

func TestLoadPrefersTheEnvThenTheSessionThenTheToken(t *testing.T) {
	keyring.MockInit()
	tk := testTokens(t)
	// Both kinds at once never happen through Save, the items are written directly to see the order.
	if err := keyring.Set(service, tokenAccount, "perm"); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(service, sessionAccount, `{"access_token":"at1","refresh_token":"rt1","host":"www.wrike.com"}`); err != nil {
		t.Fatal(err)
	}
	got, err := tk.Load()
	if err != nil || got.AccessToken != "at1" || !got.OAuth() {
		t.Fatalf("Load = %+v, %v, want the session", got, err)
	}
	if err := keyring.Delete(service, sessionAccount); err != nil {
		t.Fatal(err)
	}
	if got, err = tk.Load(); err != nil || got.AccessToken != "perm" {
		t.Fatalf("Load without a session = %+v, %v, want the token", got, err)
	}
	t.Setenv(EnvToken, "fromenv")
	if got, err = tk.Load(); err != nil || got.AccessToken != "fromenv" || got.OAuth() {
		t.Fatalf("Load with the env = %+v, %v", got, err)
	}
}

func TestSessionRoundTripThroughTheFile(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	tk := testTokens(t)
	want := oauthSession(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	if err := tk.SaveSession(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tk.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file mode = %o, want 600", perm)
	}
	got, err := tk.Load()
	if err != nil || got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.Expiry.Equal(want.Expiry) ||
		got.Host != want.Host || got.ClientID != want.ClientID || got.ClientSecret != want.ClientSecret {
		t.Fatalf("Load = %+v, %v, want %+v", got, err, want)
	}
	if err := tk.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := tk.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load after Delete = %v, want ErrNoToken", err)
	}
}

func TestSessionRoundTripThroughTheKeyring(t *testing.T) {
	keyring.MockInit()
	tk := testTokens(t)
	want := oauthSession(time.Now().Add(time.Hour))
	if err := tk.SaveSession(want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tk.SessionFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("session file written although the keyring worked: %v", err)
	}
	got, err := tk.Load()
	if err != nil || got.AccessToken != "at1" || got.RefreshToken != "rt1" || !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

func TestSavingOneKindRemovesTheOther(t *testing.T) {
	for name, init := range map[string]func(){
		"keyring": keyring.MockInit,
		"files":   func() { keyring.MockInitWithError(errors.New("no secret service")) },
	} {
		t.Run(name, func(t *testing.T) {
			init()
			tk := testTokens(t)
			if err := tk.SaveToken("perm"); err != nil {
				t.Fatal(err)
			}
			if err := tk.SaveSession(oauthSession(time.Now().Add(time.Hour))); err != nil {
				t.Fatal(err)
			}
			if got, err := tk.Load(); err != nil || !got.OAuth() {
				t.Fatalf("Load after SaveSession = %+v, %v, want the session", got, err)
			}
			if _, err := keyring.Get(service, tokenAccount); !errors.Is(err, keyring.ErrNotFound) && name == "keyring" {
				t.Errorf("the token item survived SaveSession: %v", err)
			}
			if _, err := os.Stat(tk.TokenFile); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the token file survived SaveSession: %v", err)
			}
			if err := tk.SaveToken("perm2"); err != nil {
				t.Fatal(err)
			}
			if got, err := tk.Load(); err != nil || got.OAuth() || got.AccessToken != "perm2" {
				t.Fatalf("Load after SaveToken = %+v, %v, want the token", got, err)
			}
			if _, err := os.Stat(tk.SessionFile); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the session file survived SaveToken: %v", err)
			}
		})
	}
}

func TestSaveRejectsEmpty(t *testing.T) {
	keyring.MockInit()
	tk := testTokens(t)
	if err := tk.SaveToken("   "); err == nil {
		t.Fatal("SaveToken of a blank token succeeded")
	}
	if err := tk.SaveSession(Session{AccessToken: "at1"}); err == nil {
		t.Fatal("SaveSession without a refresh token succeeded")
	}
}

func TestSaveRemovesStaleFileWhenKeychainWorks(t *testing.T) {
	tk := testTokens(t)
	keyring.MockInitWithError(errors.New("no secret service"))
	if err := tk.SaveToken("old"); err != nil {
		t.Fatal(err)
	}
	keyring.MockInit()
	if err := tk.SaveToken("new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tk.TokenFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale token file still there: %v", err)
	}
	if got, err := tk.Load(); err != nil || got.AccessToken != "new" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}
