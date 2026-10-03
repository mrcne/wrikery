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
	logout := fs.Bool("logout", false, "remove the stored token and exit")
	configPath := fs.String("config", "", "read this config file instead of the default")
	noColor := fs.Bool("no-color", false, "plain output without colors, NO_COLOR in the environment does the same")

	switch err := fs.Parse(os.Args[1:]); {
	case errors.Is(err, flag.ErrHelp):
		return
	case err != nil:
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		os.Exit(runCommand(fs.Args(), *configPath, *noColor, *demoMode, *showVersion, *logout))
	}
	if *showVersion {
		fmt.Println(versionLine(buildVersion(), buildCommit(), runtime.Version()))
		return
	}
	if err := run(*demoMode, *logout, *configPath, *noColor); err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		os.Exit(1)
	}
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

func run(demoMode, logout bool, configPath string, noColor bool) error {
	if noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	paths, cfg, err := setup(configPath)
	if err != nil {
		return err
	}
	tokens := auth.Tokens{FallbackFile: paths.TokenFile}
	if logout {
		if err := tokens.Delete(); err != nil {
			return err
		}
		fmt.Println("token removed")
		return nil
	}

	logFile, err := openLog(paths.LogFile, demoMode)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile,
		&slog.HandlerOptions{Level: cfg.SlogLevel()})))

	cfg.UI = resolveTheme(cfg.UI, true)

	if demoMode {
		st, _, cleanup, err := openDemoStore(time.Now())
		if err != nil {
			return err
		}
		defer cleanup()
		p := tea.NewProgram(ui.New(ui.Options{
			Version: buildVersion(), Store: st, Config: cfg.UI, Demo: true,
			Hooks: ui.Hooks{Refresh: func() {}, WakeOutbox: func() {}, OpenURL: openURL, Copy: copyText},
		}), tea.WithAltScreen())
		_, err = p.Run()
		return err
	}

	st, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	slog.Info("started", "version", buildVersion(), "db", paths.DBFile)

	a := &app{cfg: cfg, st: st, tokens: tokens, lock: lockPath(paths)}
	token, err := tokens.Load()
	firstRun := errors.Is(err, auth.ErrNoToken)
	if err != nil && !firstRun {
		return err
	}
	a.prog = tea.NewProgram(ui.New(ui.Options{
		Version: buildVersion(), Store: st, Config: cfg.UI, FirstRun: firstRun, Hooks: a.hooks(),
	}), tea.WithAltScreen())
	if !firstRun {
		host, err := resolveHost(context.Background(), cfg, st, token)
		if err != nil {
			slog.Warn("could not detect the Wrike data center", "error", err)
			host = wrike.DefaultHost
		}
		a.startEngine(token, host)
	}
	_, err = a.prog.Run()
	a.shutdown()
	return err
}

// runCommand prepares what a command needs, the first half of run without the program, and hands over to internal/cli.
func runCommand(args []string, configPath string, noColor, demo, showVersion, logout bool) int {
	if demo {
		fmt.Fprintln(os.Stderr, "wrikery: --demo runs the interface on sample data, it cannot run a command")
		return 2
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
	tokens := auth.Tokens{FallbackFile: paths.TokenFile}
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM}
	// signal.Notify un-ignores an ignored SIGHUP, which would undo nohup, see https://pkg.go.dev/os/signal.
	if !signal.Ignored(syscall.SIGHUP) {
		signals = append(signals, syscall.SIGHUP)
	}
	ctx, stop := signal.NotifyContext(context.Background(), signals...)
	defer stop()
	// NotifyContext keeps catching signals until stop runs, so without this a second Ctrl-C would not end the process while a sent create is waited for.
	go func() { <-ctx.Done(); stop() }()
	return cli.Run(ctx, cli.Env{
		Config: cfg,
		Store:  st,
		Token: func() (string, error) {
			token, err := tokens.Load()
			if errors.Is(err, auth.ErrNoToken) {
				return "", nil
			}
			return token, err
		},
		Client:   func(token, host string) *wrike.Client { return newClient(token, host) },
		Host:     func(ctx context.Context, token string) (string, error) { return resolveHost(ctx, cfg, st, token) },
		LockFile: lockPath(paths),
		Theme:    ui.NewTheme(cfg.UI),
		Width:    width,
		Deadline: 15 * time.Second,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	}, args)
}

// resolveTheme turns the auto theme into dark or light.
// It queries the terminal, which is too slow for the render path, so it happens once and only when there is a terminal to ask.
// A pipe gets no color anyway, dark is the default there.
func resolveTheme(ui config.UIConfig, terminal bool) config.UIConfig {
	if ui.Theme != "auto" {
		return ui
	}
	ui.Theme = "dark"
	if terminal && !lipgloss.HasDarkBackground() {
		ui.Theme = "light"
	}
	return ui
}
