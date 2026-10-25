package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/syncer"
)

var cmdSync = Command{
	Name:     "sync",
	Synopsis: []string{"sync [--full]"},
	Short:    "run one sync cycle, then exit, --full also checks for deleted tasks",
	Notes: `Runs one sync cycle, queued writes first, then the pulls, and exits. Ctrl-C stops it.
--full also checks for tasks deleted on Wrike, which crawls every followed scope and is slower.`,
	Flags: func() *flag.FlagSet { return new(syncOpts).flagSet() },
}

type syncOpts struct{ full, json bool }

func (o *syncOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("sync", &o.json)
	fs.BoolVar(&o.full, "full", false, "also check for tasks deleted on Wrike, slower")
	return fs
}

type syncJSON struct {
	State   string `json:"state"`
	Pending int    `json:"pending"`
	Failed  int    `json:"failed"`
}

func runSync(ctx context.Context, env Env, args []string) int {
	var o syncOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdSync.usage(), args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, cmdSync.usage(), "sync takes no arguments")
	}
	if env.Offline {
		return usageError(env, fs, cmdSync.usage(), "sync talks to Wrike, which offline mode forbids, run wrikery --offline=false sync")
	}
	// An account read Wrike did not answer is left to the engine, its first cycle compares before it sends or pulls.
	host, code, _ := preflight(ctx, env)
	if code != exitOK {
		return code
	}
	eng := syncer.New(env.Client(host), env.Store,
		syncer.Config{PollInterval: env.Config.PollInterval, LockFile: env.LockFile, Host: host, CheckHome: env.TokenFromEnv, PullEffort: env.Config.Budget.Source == "effort"}, slog.Default())
	state, cycleErr := eng.Once(ctx, o.full)
	// The pull changed the cache, a running interface shows it now and not at its own next poll.
	if env.Socket != "" {
		control.Nudge(ctx, env.Socket)
	}
	shown := string(state)
	if errors.Is(cycleErr, context.Canceled) {
		// The engine calls a cancelled cycle offline, which is wrong for a run the user stopped.
		shown = "interrupted"
	}
	// Ctrl-C may have cancelled ctx during the cycle, the counts are still worth printing.
	pending, failed, err := env.Store.Outbox().Counts(context.WithoutCancel(ctx))
	if err != nil {
		return fail(env, err)
	}
	if o.json {
		code := printJSON(env, syncJSON{State: shown, Pending: pending, Failed: failed})
		if cycleErr != nil {
			return fail(env, cycleErr)
		}
		return code
	}
	th := env.Theme
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	_, _ = fmt.Fprintf(env.Stdout, "sync %s, %s %d pending, %s %d failed\n", shown,
		dim.Render(th.Glyphs.Pending), pending, lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed), failed)
	if cycleErr != nil {
		return fail(env, cycleErr)
	}
	return exitOK
}
