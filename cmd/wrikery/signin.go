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
	login, err := auth.StartLogin(app, wrike.OAuth{App: app, HTTPClient: a.hc, UserAgent: userAgent()})
	if err != nil {
		return ui.SignIn{}, err
	}
	if a.loginTokenURL != "" {
		login.TokenURL = a.loginTokenURL
	}
	result := make(chan ui.SignInResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), codeLifetime)
		defer cancel()
		name, err := a.finishSignIn(ctx, login)
		result <- ui.SignInResult{Name: name, Err: err}
	}()
	return ui.SignIn{URL: login.URL(), Result: result, Paste: login.Paste, Cancel: login.Close}, nil
}

// finishSignIn is the tail verifyToken has for a pasted token: the name through Me, the session kept, the engine started.
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
	me, err := newClient(wrike.StaticToken(s.AccessToken), s.Host, a.clientOpts()...).Me(ctx)
	if err != nil {
		return "", fmt.Errorf("checking the new session: %w", err)
	}
	if err := a.tokens.SaveSession(s); err != nil {
		return "", err
	}
	a.startEngine(auth.NewSource(a.tokens, s, a.hc, userAgent()), s.Host)
	return strings.TrimSpace(me.FirstName + " " + me.LastName), nil
}
