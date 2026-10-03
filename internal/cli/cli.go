// Package cli runs one operation from the command line on the same cache and outbox as the TUI.
// It reads the store, queues writes through the outbox and prints text or JSON.
// Main hands it an Env and the arguments.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// Exit codes.
// A script branches on them, so they are part of the interface and documented in the overview.
const (
	exitOK     = 0
	exitError  = 1 // a missing token, a name that matched nothing, a store error, a write Wrike rejected
	exitUsage  = 2
	exitQueued = 3 // the write is in the outbox and the cache, not on Wrike yet
)

// Env is what main prepares for a command: the pieces it builds for the TUI anyway, plus the two output writers.
type Env struct {
	Version  string
	Config   config.Config
	Store    *store.Store
	Token    string                                                  // empty when none is stored
	Client   func(token, host string) *wrike.Client                  // main's constructor, it carries the User-Agent
	Host     func(ctx context.Context, token string) (string, error) // main's config, meta, probe chain
	LockFile string                                                  // the sync lock next to the database
	Theme    ui.Theme
	Width    int           // columns of the terminal stdout is, 0 in a pipe
	Deadline time.Duration // how long a write waits for Wrike before it is left queued
	Stdout   io.Writer
	Stderr   io.Writer
}

const rootUsage = `usage: wrikery <command> [flags] [arguments]

Commands:
  wrikery sync                              run one sync cycle, then exit
  wrikery task list [--folder F] [--me] [--all]
                                            list tasks, your own by default
  wrikery task show T                       print one task with its description and comments
  wrikery task status T STATUS              move a task to a status of its workflow
  wrikery task create --folder F TITLE...   create a task, the words make the title

A task or folder is an id or a part of its title that matches exactly one cached row.
Every command takes --json. Exit codes: 0 done, 1 error, 2 usage, 3 queued but not on Wrike yet.
`

const taskUsage = `usage: wrikery task list [--folder F | --me] [--all]
       wrikery task show T
       wrikery task status T STATUS
       wrikery task create --folder F TITLE...
`

// Run executes args and returns the exit code.
// Everything it prints goes to env.Stdout and env.Stderr.
func Run(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, rootUsage)
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(env.Stdout, rootUsage)
		return exitOK
	case "sync":
		return runSync(ctx, env, args[1:])
	case "task":
		return runTask(ctx, env, args[1:])
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: unknown command %q\n", args[0])
	_, _ = fmt.Fprint(env.Stderr, rootUsage)
	return exitUsage
}

func runTask(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, taskUsage)
		return exitUsage
	}
	switch args[0] {
	case "list":
		return runTaskList(ctx, env, args[1:])
	case "show":
		return runTaskShow(ctx, env, args[1:])
	case "status":
		return runTaskStatus(ctx, env, args[1:])
	case "create":
		return runTaskCreate(ctx, env, args[1:])
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: unknown task command %q\n", args[0])
	_, _ = fmt.Fprint(env.Stderr, taskUsage)
	return exitUsage
}

// parse runs fs over args.
// The second value is true when the caller is done:
// the usage was asked for and printed (exit 0), or a flag was wrong (exit 2).
func parse(env Env, fs *flag.FlagSet, usage string, args []string) (int, bool) {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		printUsage(env.Stdout, fs, usage)
		return exitOK, true
	case err != nil:
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: %v\n", err)
		printUsage(env.Stderr, fs, usage)
		return exitUsage, true
	}
	return exitOK, false
}

func printUsage(w io.Writer, fs *flag.FlagSet, usage string) {
	_, _ = fmt.Fprint(w, usage)
	fs.SetOutput(w)
	fs.PrintDefaults()
}

//nolint:unused // the task commands added next report a wrong argument count through it
func usageError(env Env, fs *flag.FlagSet, usage, msg string) int {
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: %s\n", msg)
	printUsage(env.Stderr, fs, usage)
	return exitUsage
}

func fail(env Env, err error) int {
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: %v\n", err)
	return exitError
}

var errNotBuilt = errors.New("not built yet")

func runSync(_ context.Context, env Env, _ []string) int { return fail(env, errNotBuilt) }

func runTaskList(_ context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	fs.String("folder", "", "a folder, project or space, by id or by a part of its title")
	fs.Bool("me", false, "your own tasks, the default")
	fs.Bool("all", false, "include completed and cancelled tasks")
	fs.Bool("json", false, "print JSON")
	if code, done := parse(env, fs, listUsage, args); done {
		return code
	}
	return fail(env, errNotBuilt)
}

const listUsage = `usage: wrikery task list [--folder F | --me] [--all] [--json]
`

func runTaskShow(_ context.Context, env Env, _ []string) int   { return fail(env, errNotBuilt) }
func runTaskStatus(_ context.Context, env Env, _ []string) int { return fail(env, errNotBuilt) }
func runTaskCreate(_ context.Context, env Env, _ []string) int { return fail(env, errNotBuilt) }
