package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mrcne/wrikery/internal/lockfile"
	"github.com/mrcne/wrikery/pkg/wrike"
)

const (
	// renewAhead is how long before its expiry an access token is renewed, so a request never goes out with one about to expire.
	renewAhead = 5 * time.Minute
	// lockWait bounds the wait for the lock file, longer than the refresh request another process may be in.
	lockWait = 45 * time.Second
)

// Source hands the client the token of a session, it is the wrike.TokenSource of the app.
// A permanent token comes back as it is.
// An OAuth access token is renewed before it expires and after Wrike rejected it, through the refresh token.
type Source struct {
	tokens     Tokens
	oauth      wrike.OAuth
	now        func() time.Time
	mu         sync.Mutex
	session    Session
	lockWarned bool
}

func NewSource(tokens Tokens, s Session, hc *http.Client, userAgent string) *Source {
	return &Source{
		tokens:  tokens,
		session: s,
		now:     time.Now,
		oauth:   wrike.OAuth{App: wrike.OAuthApp{ClientID: s.ClientID, ClientSecret: s.ClientSecret}, HTTPClient: hc, UserAgent: userAgent},
	}
}

// Session is the pair the source holds now, the host in it is where the client talks to.
func (s *Source) Session() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session
}

// Token implements wrike.TokenSource.
// A renewal that fails while nothing was rejected keeps the held token:
// an access token stays valid until its expiry whatever happened to the refresh token,
// and a network failure fails the request the same way a moment later.
// A renewal that fails after a rejection is the error, the client wraps the 401 with it and sync pauses.
func (s *Source) Token(ctx context.Context, rejected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.session.OAuth() {
		return s.session.AccessToken, nil
	}
	// A rejected token that is not the held one was replaced already, by this source or by the other process.
	if rejected != s.session.AccessToken && s.fresh(s.session) {
		return s.session.AccessToken, nil
	}
	token, err := s.renew(ctx, rejected)
	if err != nil {
		if rejected != "" {
			return "", err
		}
		slog.Warn("could not renew the session, keeping the token until it expires", "error", err)
		return s.session.AccessToken, nil
	}
	return token, nil
}

func (s *Source) fresh(sess Session) bool {
	return s.now().Add(renewAhead).Before(sess.Expiry)
}

// renew runs under the lock file.
// The interface and a command are separate processes, and a refresh makes the refresh token the other one holds invalid,
// so the stored session is read first and a pair the other process renewed is adopted instead of a second refresh.
func (s *Source) renew(ctx context.Context, rejected string) (string, error) {
	lctx, cancel := context.WithTimeout(ctx, lockWait)
	defer cancel()
	lock, err := lockfile.Acquire(lctx, s.tokens.LockFile)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !s.lockWarned {
			s.lockWarned = true
			slog.Warn("auth lock unavailable, renewing without it", "path", s.tokens.LockFile, "error", err)
		}
		lock = &lockfile.Lock{}
	}
	defer lock.Release()
	if stored, err := s.tokens.Load(); err == nil && stored.OAuth() &&
		stored.AccessToken != s.session.AccessToken && stored.AccessToken != rejected && s.fresh(stored) {
		s.adopt(stored)
		return stored.AccessToken, nil
	}
	resp, err := s.oauth.Refresh(ctx, wrike.TokenURL(s.session.Host), s.session.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("renewing the session: %w", err)
	}
	next := s.session
	next.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		next.RefreshToken = resp.RefreshToken
	}
	if resp.Host != "" {
		next.Host = resp.Host
	}
	next.Expiry = s.now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	if err := s.tokens.SaveSession(next); err != nil {
		return "", fmt.Errorf("storing the renewed session: %w", err)
	}
	s.adopt(next)
	return next.AccessToken, nil
}

func (s *Source) adopt(sess Session) {
	s.session = sess
	s.oauth.App = wrike.OAuthApp{ClientID: sess.ClientID, ClientSecret: sess.ClientSecret}
}
