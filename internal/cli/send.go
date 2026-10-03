package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/pkg/wrike"
)

type outcome int

const (
	queued   outcome = iota // the row is still pending, Wrike was not reached inside the deadline
	sent                    // the row completed, the change is on Wrike
	rejected                // Wrike refused the write, the row is failed and listed under sync issues
	blocked                 // the token was rejected, the row is pending but only a new sign in in the interface sends it
)

func (o outcome) String() string {
	return [...]string{"queued", "sent", "rejected", "blocked"}[o]
}

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
// The deadline bounds the wait for the lock and the start of a write, a create already sent runs to Wrike's answer.
// The drain error is the reason behind a queued outcome or the sign of a rejected token, otherwise the row's state is the truth.
func send(ctx context.Context, env Env, host string, rowID int64) (outcome, string, error) {
	eng := syncer.New(env.Client(env.Token, host), env.Store,
		syncer.Config{PollInterval: env.Config.PollInterval, LockFile: env.LockFile}, slog.Default())
	drainErr := eng.Drain(ctx, env.Deadline)
	// Ctrl-C cancels ctx while the drain runs, the engine puts its row back on purpose.
	// A read on the cancelled ctx would fail at once, so the outcome is read on one that cannot be cancelled.
	row, err := env.Store.Outbox().Get(context.WithoutCancel(ctx), rowID)
	if errors.Is(err, store.ErrNotFound) {
		return sent, "", nil
	}
	if err != nil {
		return queued, "", err
	}
	if row.State == store.StateFailed {
		return rejected, row.LastError, nil
	}
	var apiErr *wrike.APIError
	if errors.As(drainErr, &apiErr) && (apiErr.IsAuth() || apiErr.IsWrongHost()) {
		// Nothing sends the row until a person signs in again, so a script must not read this as "goes out with the next sync".
		return blocked, "", nil
	}
	var reason string
	switch {
	case errors.Is(drainErr, syncer.ErrLocked):
		reason = "another wrikery is sending"
	case errors.Is(drainErr, context.DeadlineExceeded):
		reason = "Wrike did not answer in time"
	case errors.Is(drainErr, context.Canceled):
		reason = "interrupted"
	case drainErr != nil:
		reason = drainErr.Error()
	default:
		reason = "waiting for an earlier write"
	}
	return queued, reason, nil
}

func reportBlocked(env Env) int {
	_, _ = fmt.Fprintln(env.Stderr, "wrikery: token rejected, run wrikery to sign in again, the change stays queued")
	return exitError
}

// reportWrite is the common ending of a write command. text is the line for the sent and queued cases
// without its leading word, the function adds "now:" or "queued:".
func reportWrite(ctx context.Context, env Env, out outcome, reason string, asJSON bool, t store.Task, ref *refData, text string) int {
	switch out {
	case blocked:
		return reportBlocked(env)
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
		if code := printJSON(env, result{taskJSON: taskRow(ctx, env, t, ref), Sent: out == sent}); code != exitOK {
			return code
		}
		if out == queued {
			return exitQueued
		}
		return exitOK
	}
	if out == queued {
		_, _ = fmt.Fprintln(env.Stdout, "queued: "+text)
		return exitQueued
	}
	_, _ = fmt.Fprintln(env.Stdout, "now: "+text)
	return exitOK
}
