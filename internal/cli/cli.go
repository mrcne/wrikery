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
	exitError  = 1 // a missing token, a name that matched nothing, a store error, a write Wrike rejected, a token rejected during the send
	exitUsage  = 2
	exitQueued = 3 // the write is in the outbox and the cache, not on Wrike yet
)

// Env is what main prepares for a command: the pieces it builds for the TUI anyway, plus the two output writers.
type Env struct {
	Config   config.Config
	Store    *store.Store
	Token    string                                                  // empty when none is stored
	Client   func(token, host string) *wrike.Client                  // main's constructor, it carries the User-Agent
	Host     func(ctx context.Context, token string) (string, error) // main's config, meta, probe chain
	LockFile string                                                  // the sync lock next to the database
	Theme    ui.Theme
	Width    int           // columns of the terminal stdout is, 0 in a pipe
	Deadline time.Duration // how long a write waits for the lock and to start, a create already sent runs to Wrike's answer within the client's 30 seconds
	Stdout   io.Writer
	Stderr   io.Writer
}

const rootUsage = `usage: wrikery <command> [flags] [arguments]

Commands:
  wrikery sync [--full]                     run one sync cycle, then exit, --full also checks for deleted tasks
  wrikery task list [--folder F] [--me] [--all]
                                            list tasks, your own by default
  wrikery task show T                       print one task with its description and comments
  wrikery task status T STATUS              move a task to a status of its workflow
  wrikery task create --folder F TITLE...   create a task, the words make the title

A task or folder is an id or a part of its title that matches exactly one cached row.
A task may also be the number or the link from the browser, the link in quotes, like "https://app-eu.wrike.com/open.htm?id=4552825748".
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

// parse runs fs over args and returns the arguments that are not flags.
// Flags may stand anywhere, the flag package alone stops at the first argument that is not one,
// and a script writes the task before --json.
// A "--" ends the flags the usual way, everything after it is an argument.
// The last value is true when the caller is done:
// the usage was asked for and printed (exit 0), or a flag was wrong (exit 2).
func parse(env Env, fs *flag.FlagSet, usage string, args []string) ([]string, int, bool) {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var positional []string
	for {
		err := fs.Parse(args)
		switch {
		case errors.Is(err, flag.ErrHelp):
			printUsage(env.Stdout, fs, usage)
			return nil, exitOK, true
		case err != nil:
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: %v\n", err)
			printUsage(env.Stderr, fs, usage)
			return nil, exitUsage, true
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(positional, rest...), exitOK, false
		}
		if len(rest) == 0 {
			return positional, exitOK, false
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func printUsage(w io.Writer, fs *flag.FlagSet, usage string) {
	_, _ = fmt.Fprint(w, usage)
	fs.SetOutput(w)
	fs.PrintDefaults()
}

func usageError(env Env, fs *flag.FlagSet, usage, msg string) int {
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: %s\n", msg)
	printUsage(env.Stderr, fs, usage)
	return exitUsage
}

func fail(env Env, err error) int {
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: %v\n", err)
	return exitError
}
