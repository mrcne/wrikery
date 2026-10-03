package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/syncer"
)

const syncUsage = `usage: wrikery sync [--json]

Runs one sync cycle, queued writes first, then the pulls, and exits. Ctrl-C stops it.
`

type syncJSON struct {
	State   string `json:"state"`
	Pending int    `json:"pending"`
	Failed  int    `json:"failed"`
}

func runSync(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, syncUsage, args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, syncUsage, "sync takes no arguments")
	}
	host, code := preflight(ctx, env)
	if code != exitOK {
		return code
	}
	eng := syncer.New(env.Client(env.Token, host), env.Store,
		syncer.Config{PollInterval: env.Config.PollInterval, LockFile: env.LockFile}, slog.Default())
	state, cycleErr := eng.Once(ctx)
	// Ctrl-C may have cancelled ctx during the cycle, the counts are still worth printing.
	pending, failed, err := env.Store.Outbox().Counts(context.WithoutCancel(ctx))
	if err != nil {
		return fail(env, err)
	}
	if *asJSON {
		code := printJSON(env, syncJSON{State: string(state), Pending: pending, Failed: failed})
		if cycleErr != nil {
			return fail(env, cycleErr)
		}
		return code
	}
	th := env.Theme
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	_, _ = fmt.Fprintf(env.Stdout, "sync %s, %s %d pending, %s %d failed\n", state,
		dim.Render(th.Glyphs.Pending), pending, lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed), failed)
	if cycleErr != nil {
		return fail(env, cycleErr)
	}
	return exitOK
}
