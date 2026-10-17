package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// Scope is the one scope the sign in asks for.
// Every endpoint the client calls lists it, the reads next to wsReadOnly and the writes alone,
// for example https://developers.wrike.com/reference/gettasksempty.md and https://developers.wrike.com/reference/puttaskssingle.md .
const Scope = "wsReadWrite"

var (
	// ErrCancelled is what Wait returns after Close.
	ErrCancelled  = errors.New("auth: sign in cancelled")
	errOtherState = errors.New("the answer belongs to another sign in attempt")
)

// Login is one browser sign in: the state, a listener for the redirect when the redirect URI allows one, and the exchange of the code.
type Login struct {
	// TokenURL is where the code is exchanged, wrike.LoginTokenURL unless a test points elsewhere.
	TokenURL string

	oauth  wrike.OAuth
	state  string
	url    string
	path   string
	srv    *http.Server
	once   sync.Once
	result chan outcome
}

type outcome struct {
	code string
	err  error
}

// StartLogin draws the state and starts the listener.
// The listener runs only for an http redirect URI on localhost with a port, on the loopback interface,
// the shape RFC 8252 section 7.3 describes for a native app, https://www.rfc-editor.org/rfc/rfc8252#section-7.3 .
// A port it cannot bind leaves the paste as the way in, the screen text is the same.
func StartLogin(oauth wrike.OAuth) (*Login, error) {
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(oauth.App.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("the redirect URI: %w", err)
	}
	l := &Login{
		TokenURL: wrike.LoginTokenURL,
		oauth:    oauth,
		state:    state,
		url:      wrike.AuthorizeURL(oauth.App, state, Scope),
		path:     u.Path,
		result:   make(chan outcome, 1),
	}
	if l.path == "" {
		// A redirect URI that is a bare origin brings the browser to the root.
		l.path = "/"
	}
	host := u.Hostname()
	if u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1") && u.Port() != "" {
		ln, err := net.Listen("tcp", "127.0.0.1:"+u.Port())
		if err != nil {
			slog.Warn("cannot listen for the browser, the address has to be pasted", "port", u.Port(), "error", err)
		} else {
			l.srv = &http.Server{Handler: http.HandlerFunc(l.handle), ReadHeaderTimeout: 10 * time.Second}
			go func() { _ = l.srv.Serve(ln) }()
			// A resolver may give the browser only ::1 for localhost, so the same port answers there too when it is free.
			if ln6, err := net.Listen("tcp", "[::1]:"+u.Port()); err == nil {
				go func() { _ = l.srv.Serve(ln6) }()
			}
		}
	}
	return l, nil
}

// URL is the address the browser opens, shown on the screen for a copy by hand as well.
func (l *Login) URL() string { return l.url }

// Listening is false when the redirect URI gets no listener, or its port is taken, and only a paste can finish the sign in.
func (l *Login) Listening() bool { return l.srv != nil }

// Wait returns the session once the browser came back, through the listener or through Paste, and the code was exchanged.
// It ends with the context, the caller gives it the ten minutes a code lives, and with ErrCancelled after Close.
func (l *Login) Wait(ctx context.Context) (Session, error) {
	defer l.stop()
	var o outcome
	select {
	case o = <-l.result:
	case <-ctx.Done():
		return Session{}, ctx.Err()
	}
	if o.err != nil {
		return Session{}, o.err
	}
	resp, err := l.oauth.Exchange(ctx, l.TokenURL, o.code)
	if err != nil {
		return Session{}, fmt.Errorf("exchanging the code: %w", err)
	}
	if resp.AssumedExpiry {
		slog.Warn("the token answer carried no usable expires_in, assuming an hour")
	}
	return Session{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		Host:         resp.Host,
		ClientID:     l.oauth.App.ClientID,
		ClientSecret: l.oauth.App.ClientSecret,
	}, nil
}

// Paste takes the address the browser landed on, for a redirect URI without a listener or a browser on another machine.
// An address that does not pass is the error, and the attempt goes on, the way the listener ignores a wrong request.
func (l *Login) Paste(address string) error {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("that is not an address: %w", err)
	}
	q := u.Query()
	if q.Get("code") == "" && q.Get("state") == "" {
		return errors.New("paste the whole address the browser landed on, it carries the code and the state")
	}
	code, err := l.check(q)
	if err != nil {
		return err
	}
	l.deliver(code, nil)
	return nil
}

// Close stops the listener and ends Wait with ErrCancelled.
func (l *Login) Close() {
	l.deliver("", ErrCancelled)
	if l.srv != nil {
		_ = l.srv.Close()
	}
}

// stop lets a handler finish its page before the listener goes, the browser would show a connection error otherwise.
func (l *Login) stop() {
	if l.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.srv.Shutdown(ctx)
}

func (l *Login) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != l.path {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	code, err := l.check(r.URL.Query())
	switch {
	case errors.Is(err, errOtherState):
		// Not this attempt's answer, the wait goes on.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintln(w, "wrikery: this answer belongs to another sign in attempt.")
		return
	case err != nil:
		_, _ = fmt.Fprintf(w, "wrikery: sign in failed: %v\n", err)
	default:
		// The code is exchanged after this page, and the secret is checked only then, so the page does not claim more than it knows.
		_, _ = fmt.Fprintln(w, "wrikery got the answer, go back to the terminal.")
	}
	l.deliver(code, err)
}

// check reads the redirect's query, a code with the state or an error, as RFC 6749 section 4.1.2 lays it out.
func (l *Login) check(q url.Values) (string, error) {
	if q.Get("state") != l.state {
		return "", errOtherState
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return "", errors.New("the sign in was refused in the browser")
		}
		return "", fmt.Errorf("the sign in was refused by Wrike: %s", strings.TrimSpace(e+" "+q.Get("error_description")))
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("the answer carries no code")
	}
	return code, nil
}

func (l *Login) deliver(code string, err error) {
	l.once.Do(func() { l.result <- outcome{code: code, err: err} })
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
