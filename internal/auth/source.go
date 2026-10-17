package auth

import (
	"context"
	"errors"
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
	// renewRetry is how long a failed proactive renewal is left alone while the held token is still valid,
	// so a token endpoint that is down is not asked again by every request of a pull.
	renewRetry = time.Minute
	// refreshTimeout bounds a refresh, which runs on past the request's context, see renew.
	refreshTimeout = 45 * time.Second
)

// ErrSignedOut is the renewal's answer when the store holds nothing or a permanent token:
// the user signed out or signed in another way while this process ran, and a renewal would undo that.
// It counts as a refusal, so the 401 stands and the sign in screen follows.
var ErrSignedOut = fmt.Errorf("%w: signed out, nothing to renew", wrike.ErrRenewalRefused)

// Source hands the client the token of a session, it is the wrike.TokenSource of the app.
// A permanent token comes back as it is.
// An OAuth access token is renewed before it expires and after Wrike rejected it, through the refresh token.
type Source struct {
	tokens     Tokens
	oauth      wrike.OAuth
	now        func() time.Time
	mu         sync.Mutex
	session    Session
	failedAt   time.Time // the last proactive renewal that failed, zero after a success
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

// Token implements wrike.TokenSource.
// A renewal that fails while nothing was rejected keeps the held token:
// an access token stays valid until its expiry whatever happened to the refresh token,
// and a network failure fails the request the same way a moment later.
// A renewal that fails after a rejection is the error, the client decides from its kind whether the 401 stands.
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
	if rejected == "" && !s.failedAt.IsZero() && s.now().Sub(s.failedAt) < renewRetry {
		return s.session.AccessToken, nil
	}
	token, err := s.renew(ctx, rejected)
	if err != nil {
		if rejected != "" {
			return "", err
		}
		s.failedAt = s.now()
		// A request cancelled on the way out, a quit, is no news.
		if ctx.Err() == nil {
			slog.Warn("could not renew the session, keeping the token until it expires", "error", err)
		}
		return s.session.AccessToken, nil
	}
	s.failedAt = time.Time{}
	return token, nil
}

func (s *Source) fresh(sess Session) bool {
	return s.now().Add(renewAhead).Before(sess.Expiry)
}

// renew runs under the lock file.
// The interface and a command are separate processes, and a refresh makes the refresh token the other one holds invalid,
// so the stored session is read first and a pair the other process renewed is adopted instead of a second refresh.
// The store is also the authority on whether there is a session at all: after a sign out nothing may be written back.
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
	stored, err := s.tokens.Load()
	switch {
	case errors.Is(err, ErrNoToken) || (err == nil && !stored.OAuth()):
		return "", ErrSignedOut
	case err == nil && s.adoptable(stored, rejected):
		// The other process renewed the pair, and its refresh token is the live one even when the access token is about to expire.
		s.adopt(stored)
		if s.fresh(stored) {
			return stored.AccessToken, nil
		}
	}
	// Wrike rotates the pair as it answers, so a refresh cut short by a quit would lose the only live refresh token.
	// It runs on past the request's context, under its own deadline.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	resp, err := s.oauth.Refresh(rctx, wrike.TokenURL(s.session.Host), s.session.RefreshToken)
	if err != nil {
		// Without the lock the other process may have renewed first, then its stored pair is the live one and this refresh token the dead one.
		if again, lerr := s.tokens.Load(); lerr == nil && s.adoptable(again, rejected) && s.fresh(again) {
			s.adopt(again)
			return again.AccessToken, nil
		}
		return "", refusal(err)
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
	if resp.AssumedExpiry {
		slog.Warn("the token answer carried no usable expires_in, assuming an hour")
	}
	// The pair in memory first, it is the only live one, so a store that cannot be written does not lose it.
	s.adopt(next)
	if err := s.tokens.SaveSession(next); err != nil {
		slog.Warn("could not store the renewed session, the other process will have to sign in again", "error", err)
	}
	return next.AccessToken, nil
}

// adoptable is true for a stored pair that is not the one held and not the one Wrike rejected.
func (s *Source) adoptable(stored Session, rejected string) bool {
	return stored.OAuth() && stored.AccessToken != s.session.AccessToken && stored.AccessToken != rejected
}

func (s *Source) adopt(sess Session) {
	s.session = sess
	s.oauth.App = wrike.OAuthApp{ClientID: sess.ClientID, ClientSecret: sess.ClientSecret}
}

// refusal marks the answers of the token endpoint that mean the session is dead: a 400 or a 401,
// invalid_grant for a rotated or revoked refresh token, invalid_client for a secret that changed, see RFC 6749 section 5.2.
// Everything else, the network, a 5xx or a 429, is a failure the request can retry later with the session intact.
func refusal(err error) error {
	var apiErr *wrike.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusUnauthorized) {
		return fmt.Errorf("%w: %w", wrike.ErrRenewalRefused, err)
	}
	return fmt.Errorf("renewing the session: %w", err)
}
