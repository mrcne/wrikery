package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// userAgent names the app and its version, so Wrike support can tell wrikery traffic apart from other API clients.
func userAgent() string {
	return "wrikery/" + buildVersion() + " (+https://github.com/mrcne/wrikery)"
}

// newClient builds a Wrike client for host that takes its token from src.
func newClient(src wrike.TokenSource, host string, opts ...wrike.Option) *wrike.Client {
	all := []wrike.Option{
		wrike.WithBaseURL(wrike.BaseURL(host)),
		wrike.WithUserAgent(userAgent()),
		wrike.WithTokenSource(src),
	}
	all = append(all, opts...)
	return wrike.New("", all...)
}

// app owns the engine lifecycle. The engine is rebuilt when the sign in changes, because the client holds the token source.
type app struct {
	cfg    config.Config
	st     *store.Store
	tokens auth.Tokens
	prog   *tea.Program
	lock   string
	// hc and loginTokenURL are set by the tests, which route every host to a fake server. Nil and empty otherwise.
	hc            *http.Client
	loginTokenURL string

	mu     sync.Mutex
	closed bool
	engine *syncer.Engine
	cancel context.CancelFunc
	done   chan struct{}
}

// startEngine holds the lock for its whole body, the stop of the old engine included.
// verifyToken runs on a UI command goroutine, so without that a token arriving during shutdown could start an engine against a closing store.
// Waiting under the lock is safe because only startEngine and shutdown ever take it, and neither runs while holding an engine.
func (a *app) startEngine(src wrike.TokenSource, host string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.stopLocked()
	client := newClient(src, host, a.clientOpts()...)
	eng := syncer.New(client, a.st, syncer.Config{PollInterval: a.cfg.PollInterval, LockFile: a.lock}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.engine, a.cancel, a.done = eng, cancel, done
	// The forwarder ends with the engine context, and prog.Send after the program exited is a documented no-op, so nothing joins it.
	go forwardEvents(ctx, eng.Events(), a.prog.Send)
	go func() {
		defer close(done)
		if err := eng.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("sync engine stopped", "error", err)
		}
	}()
}

// shutdown stops the engine for good, so a token verified after the program returned does not start another one.
func (a *app) shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.stopLocked()
}

func (a *app) stopLocked() {
	cancel, done := a.cancel, a.done
	a.engine, a.cancel, a.done = nil, nil, nil
	if cancel != nil {
		cancel()
		<-done
	}
}

// clientOpts routes the clients to the fake server of a test, nothing in production.
func (a *app) clientOpts() []wrike.Option {
	if a.hc == nil {
		return nil
	}
	return []wrike.Option{wrike.WithHTTPClient(a.hc)}
}

func (a *app) current() *syncer.Engine {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engine
}

func (a *app) hooks() ui.Hooks {
	h := ui.Hooks{
		Refresh: func() {
			if e := a.current(); e != nil {
				e.Refresh()
			}
		},
		WakeOutbox: func() {
			if e := a.current(); e != nil {
				e.WakeOutbox()
			}
		},
		VerifyToken: a.verifyToken,
		OpenURL:     openURL,
		Copy:        copyText,
		Reach: func(ctx context.Context, ref string) (store.Task, error) {
			e := a.current()
			if e == nil {
				return store.Task{}, errors.New("not signed in")
			}
			t, err := e.Reach(ctx, ref)
			if errors.Is(err, syncer.ErrNoSuchTask) {
				return t, ui.ErrNoSuchTask
			}
			return t, err
		},
	}
	// Without an app in the config the browser sign in has nothing to open, so the screen does not offer it.
	if a.cfg.OAuth.ClientID != "" {
		h.SignIn = a.signIn
	}
	return h
}

// verifyToken is the first run and the re-auth path: check the token against /contacts?me=true,
// keep it, then (re)start the engine with it.
// A configured host is probed alone, an empty one tries every known data center.
// The answer is remembered in the store so later runs do not probe again.
func (a *app) verifyToken(ctx context.Context, token string) (string, error) {
	hosts := apiHosts
	if a.cfg.Host != "" {
		hosts = []string{a.cfg.Host}
	}
	host, me, err := probeHost(ctx, token, hosts, a.hc)
	if err != nil {
		var apiErr *wrike.APIError
		switch {
		case errors.As(err, &apiErr) && apiErr.IsAuth():
			return "", errors.New("token rejected by Wrike")
		case errors.Is(err, errNoDataCenter) && a.cfg.Host != "":
			return "", fmt.Errorf("host %s from the config file does not serve this account", a.cfg.Host)
		case errors.Is(err, errNoDataCenter):
			return "", errors.New("no Wrike data center accepted this token, set host in the config file")
		default:
			return "", fmt.Errorf("could not reach Wrike: %w", err)
		}
	}
	return a.finish(ctx, wrike.StaticToken(token), host, me, func() error { return a.tokens.SaveToken(token) })
}

// finish is the tail every sign in shares once Wrike named the user: the host remembered for the settings screen,
// the credential stored, the engine started.
// A context that ended on the way, esc on the screen, stores nothing and starts nothing.
func (a *app) finish(ctx context.Context, src wrike.TokenSource, host string, me wrike.Contact, save func() error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if a.cfg.Host == "" {
		// A failed write here only means the next start probes again,
		// the credential Wrike already accepted must not be thrown away over it.
		if err := a.st.SetMeta(ctx, store.MetaKeyHost, host); err != nil {
			slog.Warn("could not store the detected Wrike host", "error", err)
		}
	}
	if err := save(); err != nil {
		return "", err
	}
	a.startEngine(src, host)
	return strings.TrimSpace(me.FirstName + " " + me.LastName), nil
}

// resolveHost is the chain run uses before starting the engine:
// the config wins, then the host a browser sign in got from the token answer, then the host remembered from a previous probe,
// and only when all are empty is the network touched, once, under a short deadline.
// The app must open on the cache when offline, so the probe cannot have the client's full retry budget:
// a GET retries a network failure three times with backoff on top of the 30s client timeout.
func resolveHost(ctx context.Context, cfg config.Config, st *store.Store, s auth.Session) (string, error) {
	if cfg.Host != "" {
		return cfg.Host, nil
	}
	if s.Host != "" {
		return s.Host, nil
	}
	if host, _ := st.GetMeta(ctx, store.MetaKeyHost); host != "" {
		return host, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	host, _, err := probeHost(probeCtx, s.AccessToken, apiHosts, nil)
	if err != nil {
		return "", err
	}
	if err := st.SetMeta(ctx, store.MetaKeyHost, host); err != nil {
		slog.Warn("could not store the detected Wrike host", "error", err)
	}
	return host, nil
}

// lockPath is the sync lock file, next to the database so two data directories never share one.
func lockPath(p config.Paths) string {
	return filepath.Join(filepath.Dir(p.DBFile), "sync.lock")
}
