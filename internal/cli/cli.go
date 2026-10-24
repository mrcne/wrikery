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
	"regexp"
	"strings"
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
	exitError  = 1 // no sign in, a name that matched nothing, a store error, a write Wrike rejected, a sign in rejected during the send
	exitUsage  = 2
	exitQueued = 3 // the write is in the outbox and the cache, not on Wrike yet
)

// Env is what main prepares for a command: the pieces it builds for the TUI anyway, plus the two output writers.
type Env struct {
	Config       config.Config
	Store        *store.Store
	SignedIn     func() (bool, error)                      // read only by the commands that send, false when nothing is stored
	Client       func(host string) *wrike.Client           // main's constructor, it carries the User-Agent and the token source
	Host         func(ctx context.Context) (string, error) // main's config, session, meta, probe chain
	LockFile     string                                    // the sync lock next to the database
	Socket       string                                    // the control socket of a running interface, next to the database as well
	Theme        ui.Theme
	Width        int           // columns of the terminal stdout is, 0 in a pipe
	Deadline     time.Duration // how long a write waits for the lock and to start, a create already sent waits for Wrike's answer, up to a minute and a half when Wrike rate limits
	Offline      bool          // offline mode: a write is queued and not sent, nothing reads the sign in or talks to Wrike
	TokenFromEnv bool          // the credential is WRIKERY_TOKEN, which no sign in checked, so every command compares its account with the cache's
	Stdin        io.Reader     // a description given as - is read from here
	Stdout       io.Writer
	Stderr       io.Writer
	GlobalFlags  *flag.FlagSet // the root flag set, which main owns, the help page lists it
}

// Run executes args and returns the exit code.
// Everything it prints goes to env.Stdout and env.Stderr.
func Run(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, HelpPage(env.GlobalFlags))
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		// help help is the page as well, it must not run help on itself, and help --json or help -x is the page too,
		// a word that starts with a dash names no command.
		if len(args) == 1 || isHelpWord(args[1]) || strings.HasPrefix(args[1], "-") {
			_, _ = fmt.Fprint(env.Stdout, HelpPage(env.GlobalFlags))
			return exitOK
		}
		return Run(ctx, env, helpArgs(args[1:]))
	case "sync":
		return runSync(ctx, env, args[1:])
	case "task":
		return runTask(ctx, env, args[1:])
	case "ui":
		return runUI(ctx, env, args[1:])
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: unknown command %q\n", args[0])
	_, _ = fmt.Fprint(env.Stderr, HelpPage(env.GlobalFlags))
	return exitUsage
}

func runTask(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, groupUsage(groupTask))
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help":
		_, _ = fmt.Fprint(env.Stdout, groupUsage(groupTask))
		return exitOK
	case "list":
		return runTaskList(ctx, env, args[1:])
	case "show":
		return runTaskShow(ctx, env, args[1:])
	case "status":
		return runTaskStatus(ctx, env, args[1:])
	case "create":
		return runTaskCreate(ctx, env, args[1:])
	case "edit":
		return runTaskEdit(ctx, env, args[1:])
	case "comment":
		return runTaskComment(ctx, env, args[1:])
	case "assign":
		return runTaskAssign(ctx, env, args[1:], false)
	case "unassign":
		return runTaskAssign(ctx, env, args[1:], true)
	case "move":
		return runTaskMove(ctx, env, args[1:])
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: unknown task command %q\n", args[0])
	_, _ = fmt.Fprint(env.Stderr, groupUsage(groupTask))
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
	var tail []string
	if end := terminator(fs, args); end >= 0 {
		args, tail = args[:end], args[end+1:]
	}
	var positional []string
	for {
		err := fs.Parse(args)
		switch {
		case errors.Is(err, flag.ErrHelp):
			printUsage(env.Stdout, fs, usage)
			return nil, exitOK, true
		case err != nil:
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: %s\n", FlagError(err))
			printUsage(env.Stderr, fs, usage)
			return nil, exitUsage, true
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return append(positional, tail...), exitOK, false
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// terminator is the index of the bare "--" that ends the flags, or -1.
// A "--" that is the value of a flag, "--folder --", is skipped, flag.Parse would take it as the value as well.
func terminator(fs *flag.FlagSet, args []string) int {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return i
		}
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		name, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		f := fs.Lookup(name)
		if f == nil || hasValue {
			continue
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue
		}
		i++
	}
	return -1
}

func printUsage(w io.Writer, fs *flag.FlagSet, usage string) {
	_, _ = fmt.Fprint(w, usage)
	printFlags(w, fs)
}

// oneDashFlag matches the flag name in the messages of the flag package, "flag provided but not defined: -x",
// "flag needs an argument: -x", "invalid value "v" for flag -x" and "invalid boolean value "v" for -x".
var oneDashFlag = regexp.MustCompile(`(defined: |argument: |flag |for )-(\w)`)

// FlagError is the text of a parse error with the flag spelled with two dashes, as every help page spells it.
// The flag package writes one dash whatever the user typed.
func FlagError(err error) string {
	return oneDashFlag.ReplaceAllString(err.Error(), "${1}--${2}")
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
