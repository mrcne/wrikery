package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/cli"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// version and commit are set through ldflags by the release build.
// A go install build has no ldflags,
// so buildVersion and buildCommit fall back to the build info the toolchain recorded.
var (
	version = "dev"
	commit  = ""
)

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func buildCommit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return ""
}

// versionLine formats the --version output:
// it shortens a full commit hash to seven characters and drops the parentheses when no commit is known.
// A version from git describe on an untagged build already contains the short hash, so the parentheses go too.
func versionLine(version, commit, goVersion string) string {
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if commit == "" || strings.Contains(version, commit) {
		return fmt.Sprintf("wrikery %s %s", version, goVersion)
	}
	return fmt.Sprintf("wrikery %s (%s) %s", version, commit, goVersion)
}

const usage = `usage: wrikery [flags]
       wrikery [flags] <command> [arguments]

An unofficial terminal client for Wrike.
Without a command it opens the interface. With one it runs that operation and exits,
see "wrikery help" for the commands.

Flags:
`

func main() {
	fs := flag.NewFlagSet("wrikery", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	showVersion := fs.Bool("version", false, "print the version and exit")
	demoMode := fs.Bool("demo", false, "run on built in sample data, no token and no network")
	logout := fs.Bool("logout", false, "sign out, remove the stored token or session, and exit")
	configPath := fs.String("config", "", "read this config file instead of the default")
	noColor := fs.Bool("no-color", false, "plain output without colors, NO_COLOR in the environment does the same")
	offline := fs.Bool("offline", false, "start without syncing, nothing is pulled or sent, R in the interface goes online, wins over offline in the config")

	switch err := fs.Parse(os.Args[1:]); {
	case errors.Is(err, flag.ErrHelp):
		return
	case err != nil:
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		os.Exit(runCommand(fs.Args(), *configPath, *noColor, *demoMode, *showVersion, *logout, offlineArg(fs, offline)))
	}
	if *showVersion {
		fmt.Println(versionLine(buildVersion(), buildCommit(), runtime.Version()))
		return
	}
	if err := run(*demoMode, *logout, *configPath, *noColor, offlineArg(fs, offline)); err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		os.Exit(1)
	}
}

// offlineArg is --offline as the command line gave it, nil when the line did not name it and the config decides.
// A plain bool cannot tell --offline=false from no flag, and the false must win over offline = true in the file.
func offlineArg(fs *flag.FlagSet, value *bool) *bool {
	var given *bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "offline" {
			given = value
		}
	})
	return given
}

// offlineMode is the flag when the command line gave it, the config key otherwise.
func offlineMode(cfg config.Config, arg *bool) bool {
	if arg != nil {
		return *arg
	}
	return cfg.Offline
}

// setup resolves the paths and loads the config file, the part of a start every mode shares.
func setup(configPath string) (config.Paths, config.Config, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	if configPath != "" {
		// The default path may be missing and then the defaults apply, a path given by hand is a typo when it is missing.
		if _, err := os.Stat(configPath); err != nil {
			return config.Paths{}, config.Config{}, fmt.Errorf("reading the config file: %w", err)
		}
		paths.ConfigFile = configPath
	}
	if err := paths.EnsureDirs(); err != nil {
		return config.Paths{}, config.Config{}, err
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	return paths, cfg, nil
}

func run(demoMode, logout bool, configPath string, noColor bool, offlineFlag *bool) error {
	if noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	paths, cfg, err := setup(configPath)
	if err != nil {
		return err
	}
	tokens := auth.Tokens{TokenFile: paths.TokenFile, SessionFile: paths.SessionFile, LockFile: paths.AuthLockFile}
	if logout {
		if err := tokens.Delete(); err != nil {
			return err
		}
		fmt.Println("signed out")
		return nil
	}

	logFile, err := openLog(paths.LogFile, demoMode)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile,
		&slog.HandlerOptions{Level: cfg.SlogLevel()})))

	theme := cfg.UI.Theme
	cfg.UI = resolveTheme(cfg.UI, true)

	if demoMode {
		st, _, cleanup, err := openDemoStore(time.Now())
		if err != nil {
			return err
		}
		defer cleanup()
		p := tea.NewProgram(ui.New(ui.Options{
			Version: buildVersion(), Store: st, Config: cfg, ThemeSetting: theme, ConfigFile: paths.ConfigFile, Demo: true,
			Hooks: ui.Hooks{Refresh: func() {}, WakeOutbox: func() {}, OpenURL: openURL, Copy: copyText},
		}), tea.WithAltScreen())
		if l := startControl(demoSocketPath(paths), controlHandler(p.Send, outboxCounts(st))); l != nil {
			defer func() { _ = l.Close() }()
		}
		_, err = p.Run()
		return err
	}

	st, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	st.Source = store.SourceTUI
	slog.Info("started", "version", buildVersion(), "db", paths.DBFile)

	session, err := tokens.Load()
	firstRun := errors.Is(err, auth.ErrNoToken)
	if err != nil && !firstRun {
		return err
	}
	offline := offlineMode(cfg, offlineFlag)
	if offline && firstRun {
		// --offline=false is the way out in both cases, the flag on the line or offline = true in the file.
		return errors.New("no sign in stored yet, the first run needs Wrike, start with --offline=false")
	}
	a := &app{cfg: cfg, st: st, tokens: tokens, lock: lockPath(paths), session: session}
	a.prog = tea.NewProgram(ui.New(ui.Options{
		Version: buildVersion(), Store: st, Config: cfg, ThemeSetting: theme, ConfigFile: paths.ConfigFile, Offline: offline,
		FirstRun: firstRun, SignedInWith: signedInWith(session, firstRun), SignedInApp: session.ClientID, Hooks: a.hooks(),
	}), tea.WithAltScreen())
	if l := startControl(socketPath(paths), controlHandler(a.prog.Send, outboxCounts(st))); l != nil {
		defer func() { _ = l.Close() }()
	}
	if offline {
		// No drain runs, so the rows a process that died mid-send left in flight come back to the queue here,
		// or the changes screen would show them as being sent and refuse a retry or a discard.
		if err := syncer.ResetInflight(context.Background(), st, a.lock, 2*time.Second, slog.Default()); err != nil {
			slog.Warn("could not put back the writes left in flight", "error", err)
		}
	} else if !firstRun {
		host, err := resolveHost(context.Background(), cfg, st, session)
		if err != nil {
			slog.Warn("could not detect the Wrike data center", "error", err)
			host = wrike.DefaultHost
		}
		a.startEngine(auth.NewSource(tokens, session, nil, userAgent()), host)
	}
	_, err = a.prog.Run()
	a.shutdown()
	return err
}

// runCommand prepares what a command needs, the first half of run without the program, and hands over to internal/cli.
func runCommand(args []string, configPath string, noColor, demo, showVersion, logout bool, offlineFlag *bool) int {
	if demo {
		if args[0] != "ui" {
			fmt.Fprintln(os.Stderr, "wrikery: --demo runs the interface on sample data, it cannot run a command, only ui drives a demo")
			return 2
		}
		// The demo's cache is a throwaway in the running process's temp dir, so a ui command talks to its socket and resolves nothing.
		paths, _, err := setup(configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
			return 1
		}
		return cli.Run(context.Background(), cli.Env{Socket: demoSocketPath(paths), Stdout: os.Stdout, Stderr: os.Stderr}, args)
	}
	if showVersion || logout {
		fmt.Fprintln(os.Stderr, "wrikery: --version and --logout take no command")
		return 2
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		// Help prints a constant, so it must not create the data directory, open the store or read the token.
		return cli.Run(context.Background(), cli.Env{Stdout: os.Stdout, Stderr: os.Stderr}, args)
	}
	if noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	paths, cfg, err := setup(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		return 1
	}
	logFile, err := openCommandLog(paths.LogFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		return 1
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: cfg.SlogLevel()})))

	width := 0
	fd := int(os.Stdout.Fd())
	terminal := term.IsTerminal(fd)
	if terminal {
		if w, _, err := term.GetSize(fd); err == nil {
			width = w
		}
	}
	cfg.UI = resolveTheme(cfg.UI, terminal)

	st, err := store.Open(paths.DBFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		return 1
	}
	defer func() { _ = st.Close() }()
	st.Source = store.SourceCLI
	tokens := auth.Tokens{TokenFile: paths.TokenFile, SessionFile: paths.SessionFile, LockFile: paths.AuthLockFile}
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	// signal.Notify un-ignores an ignored SIGHUP, which would undo nohup, see https://pkg.go.dev/os/signal.
	if !signal.Ignored(syscall.SIGHUP) {
		signals = append(signals, syscall.SIGHUP)
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	// NotifyContext keeps catching signals until stop runs, so without this a second Ctrl-C would not end the process while a sent create is waited for.
	go func() { <-ctx.Done(); stop() }()
	creds := &cmdAuth{tokens: tokens, cfg: cfg, st: st}
	return cli.Run(ctx, cli.Env{
		Config:   cfg,
		Store:    st,
		SignedIn: creds.signedIn,
		Client:   creds.client,
		Host:     creds.host,
		LockFile: lockPath(paths),
		Socket:   socketPath(paths),
		Theme:    ui.NewTheme(cfg.UI),
		Width:    width,
		Deadline: 15 * time.Second,
		Offline:  offlineMode(cfg, offlineFlag),
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	}, args)
}

// signedInWith names the stored sign in for the settings screen and the re-auth notice.
func signedInWith(s auth.Session, firstRun bool) string {
	switch {
	case firstRun:
		return ""
	case s.OAuth():
		return ui.SignedInWithBrowser
	}
	return ui.SignedInWithToken
}

// cmdAuth is the sign in of a command. It reads the credentials once, on the first command that sends,
// so a list or a show never touches the keychain.
type cmdAuth struct {
	tokens  auth.Tokens
	cfg     config.Config
	st      *store.Store
	loaded  bool
	session auth.Session
	err     error
}

func (c *cmdAuth) load() (auth.Session, error) {
	if !c.loaded {
		c.loaded = true
		c.session, c.err = c.tokens.Load()
	}
	return c.session, c.err
}

func (c *cmdAuth) signedIn() (bool, error) {
	_, err := c.load()
	if errors.Is(err, auth.ErrNoToken) {
		return false, nil
	}
	return err == nil, err
}

func (c *cmdAuth) client(host string) *wrike.Client {
	s, _ := c.load()
	return newClient(auth.NewSource(c.tokens, s, nil, userAgent()), host)
}

func (c *cmdAuth) host(ctx context.Context) (string, error) {
	s, _ := c.load()
	return resolveHost(ctx, c.cfg, c.st, s)
}

// resolveTheme turns the auto theme into dark or light.
// It queries the terminal, which is too slow for the render path, so it happens once and only when there is a terminal to ask.
// A pipe gets no color anyway, dark is the default there.
func resolveTheme(cfg config.UIConfig, terminal bool) config.UIConfig {
	if cfg.Theme != "auto" {
		return cfg
	}
	cfg.Theme = "dark"
	if terminal && !lipgloss.HasDarkBackground() {
		cfg.Theme = "light"
	}
	return cfg
}

// outboxCounts reads the queue counts for a reload asked for on the control socket.
func outboxCounts(st *store.Store) func() (int, int, error) {
	return func() (int, int, error) { return st.Outbox().Counts(context.Background()) }
}
