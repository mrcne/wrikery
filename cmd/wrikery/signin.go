package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/ui"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// codeLifetime is how long the browser gets: an authorization code expires ten minutes after it is issued,
// https://developers.wrike.com/docs/oauth-20-authorization.md .
const codeLifetime = 10 * time.Minute

// signIn is the hook behind the browser sign in on the first run screen.
// The app comes from the config, the secret from the config or from the screen.
// The wait, the check of the new session and the engine start run on a goroutine, the screen gets one answer.
func (a *app) signIn(secret string) (ui.SignIn, error) {
	oa := a.cfg.OAuth
	app := wrike.OAuthApp{ClientID: oa.ClientID, ClientSecret: oa.ClientSecret, RedirectURI: oa.RedirectURI}
	if app.ClientSecret == "" {
		app.ClientSecret = strings.TrimSpace(secret)
	}
	if app.ClientID == "" || app.ClientSecret == "" {
		return ui.SignIn{}, errors.New("the browser sign in needs the client id and the secret of an API app")
	}
	login, err := auth.StartLogin(wrike.OAuth{App: app, HTTPClient: a.hc, UserAgent: userAgent()})
	if err != nil {
		return ui.SignIn{}, err
	}
	if a.loginTokenURL != "" {
		login.TokenURL = a.loginTokenURL
	}
	result := make(chan ui.SignInResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), codeLifetime)
	go func() {
		defer cancel()
		name, err := a.finishSignIn(ctx, login)
		result <- ui.SignInResult{Name: name, App: app.ClientID, Err: err}
	}()
	return ui.SignIn{
		URL:       login.URL(),
		Listening: login.Listening(),
		Result:    result,
		Paste:     login.Paste,
		// Esc ends the wait and the work after it, so an answer that came in a moment earlier stores nothing.
		Cancel: func() {
			cancel()
			login.Close()
		},
	}, nil
}

// finishSignIn waits for the browser, checks the new session with Me and hands it to the tail every sign in shares.
func (a *app) finishSignIn(ctx context.Context, login *auth.Login) (string, error) {
	s, err := login.Wait(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return "", errors.New("the sign in timed out, a code lives ten minutes")
	}
	if err != nil {
		return "", err
	}
	if s.Host == "" {
		// The documentation always names the host in the token answer, this only keeps the client off an empty URL.
		s.Host = wrike.DefaultHost
	}
	if a.cfg.Host != "" && a.cfg.Host != s.Host {
		// The config host wins at every later start, and a host that does not serve the account answers 300 to every request.
		return "", fmt.Errorf("host %s from the config file does not serve this account, the sign in named %s", a.cfg.Host, s.Host)
	}
	me, err := newClient(wrike.StaticToken(s.AccessToken), s.Host, a.clientOpts()...).Me(ctx)
	if err != nil {
		return "", fmt.Errorf("checking the new session: %w", err)
	}
	return a.finish(ctx, auth.NewSource(a.tokens, s, a.hc, userAgent()), s.Host, me, func() error { return a.tokens.SaveSession(s) })
}
