package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// app owns the engine lifecycle. The engine is rebuilt when the token changes, because the client holds the token.
type app struct {
	cfg    config.Config
	st     *store.Store
	tokens auth.Tokens
	prog   *tea.Program

	mu     sync.Mutex
	closed bool
	engine *syncer.Engine
	cancel context.CancelFunc
	done   chan struct{}
}

// startEngine holds the lock for its whole body, the stop of the old engine included.
// verifyToken runs on a UI command goroutine, so without that a token arriving during shutdown could start an engine against a closing store.
// Waiting under the lock is safe because only startEngine and shutdown ever take it, and neither runs while holding an engine.
func (a *app) startEngine(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.stopLocked()
	eng := syncer.New(wrike.New(token), a.st, syncer.Config{PollInterval: a.cfg.PollInterval}, slog.Default())
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

func (a *app) current() *syncer.Engine {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engine
}

func (a *app) hooks() ui.Hooks {
	return ui.Hooks{
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
	}
}

// verifyToken is the first run and the re-auth path: check the token against /contacts?me=true,
// keep it, then (re)start the engine with it.
func (a *app) verifyToken(ctx context.Context, token string) (string, error) {
	me, err := wrike.New(token).Me(ctx)
	if err != nil {
		var apiErr *wrike.APIError
		if errors.As(err, &apiErr) && apiErr.IsAuth() {
			return "", errors.New("token rejected by Wrike")
		}
		return "", fmt.Errorf("could not reach Wrike: %w", err)
	}
	if err := a.tokens.Save(token); err != nil {
		return "", err
	}
	a.startEngine(token)
	return strings.TrimSpace(me.FirstName + " " + me.LastName), nil
}
