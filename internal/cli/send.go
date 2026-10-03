package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
)

type outcome int

const (
	sent     outcome = iota // the row completed, the change is on Wrike
	queued                  // the row is still pending, Wrike was not reached inside the deadline
	rejected                // Wrike refused the write, the row is failed and listed under sync issues
)

// preflight checks the token and resolves the host before anything is queued,
// so a failure here never hides a row behind an error exit.
func preflight(ctx context.Context, env Env) (string, int) {
	if env.Token == "" {
		return "", fail(env, errors.New("no token stored, run wrikery once to sign in"))
	}
	host, err := env.Host(ctx, env.Token)
	if err != nil {
		return "", fail(env, fmt.Errorf("could not reach Wrike: %w", err))
	}
	return host, exitOK
}

// send runs one drain pass for the row just queued and reads its fate back from the outbox.
// The pass sends every due row older than this one first, the order the engine keeps.
// The drain error is only the reason behind a queued outcome, the row's state is the truth.
func send(ctx context.Context, env Env, host string, rowID int64) (outcome, string, error) {
	eng := syncer.New(env.Client(env.Token, host), env.Store,
		syncer.Config{PollInterval: env.Config.PollInterval, LockFile: env.LockFile}, slog.Default())
	dctx, cancel := context.WithTimeout(ctx, env.Deadline)
	defer cancel()
	drainErr := eng.Drain(dctx)
	row, err := env.Store.Outbox().Get(ctx, rowID)
	if errors.Is(err, store.ErrNotFound) {
		return sent, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	if row.State == store.StateFailed {
		return rejected, row.LastError, nil
	}
	reason := "Wrike could not be reached"
	switch {
	case errors.Is(drainErr, syncer.ErrLocked):
		reason = "another wrikery is sending"
	case drainErr != nil:
		slog.Warn("drain after a command write", "error", drainErr)
	}
	return queued, reason, nil
}

// reportWrite is the common ending of a write command. text is the line for the sent and queued cases
// without its leading word, the function adds "now:" or "queued:".
func reportWrite(ctx context.Context, env Env, out outcome, reason string, asJSON bool, t store.Task, ref *refData, text string) int {
	switch out {
	case rejected:
		return fail(env, fmt.Errorf("the change was rejected by Wrike: %s, it is listed under sync issues in wrikery", reason))
	case queued:
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: queued, not on Wrike yet: %s\n", reason)
	}
	if asJSON {
		type result struct {
			taskJSON
			Sent bool `json:"sent"`
		}
		code := printJSON(env, result{taskJSON: taskRow(ctx, env, t, ref), Sent: out == sent})
		if out == queued {
			return exitQueued
		}
		return code
	}
	if out == queued {
		_, _ = fmt.Fprintln(env.Stdout, "queued: "+text)
		return exitQueued
	}
	_, _ = fmt.Fprintln(env.Stdout, "now: "+text)
	return exitOK
}
