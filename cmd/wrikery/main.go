package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/mrcne/wrikery/internal/auth"
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
func versionLine(version, commit, goVersion string) string {
	if commit == "" {
		return fmt.Sprintf("wrikery %s %s", version, goVersion)
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	return fmt.Sprintf("wrikery %s (%s) %s", version, commit, goVersion)
}

const usage = `usage: wrikery [flags]

An unofficial terminal client for Wrike.

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
		fmt.Fprintf(os.Stderr, "wrikery: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		os.Exit(2)
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

func run(demoMode, logout bool, configPath string, noColor bool) error {
	if noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if configPath != "" {
		paths.ConfigFile = configPath
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	cfg, err := config.Load(paths.ConfigFile)
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

	logFile, err := os.OpenFile(paths.LogFile,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile,
		&slog.HandlerOptions{Level: cfg.SlogLevel()})))

	// Resolving the auto theme queries the terminal, which is too slow for the render path, so it happens here and once.
	if cfg.UI.Theme == "auto" {
		if lipgloss.HasDarkBackground() {
			cfg.UI.Theme = "dark"
		} else {
			cfg.UI.Theme = "light"
		}
	}

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

	a := &app{cfg: cfg, st: st, tokens: tokens}
	token, err := tokens.Load()
	firstRun := errors.Is(err, auth.ErrNoToken)
	if err != nil && !firstRun {
		return err
	}
	a.prog = tea.NewProgram(ui.New(ui.Options{
		Version: buildVersion(), Store: st, Config: cfg.UI, FirstRun: firstRun, Hooks: a.hooks(),
	}), tea.WithAltScreen())
	if !firstRun {
		// The config wins, then the host remembered from a previous probe.
		// Only when both are empty is the network touched, once, before the program starts.
		host := cfg.Host
		if host == "" {
			host, _ = st.GetMeta(context.Background(), store.MetaKeyHost)
		}
		if host == "" {
			// The app must open on the cache when offline, the engine reports the network trouble later,
			// so the probe gets a hard deadline instead of the client's full retry budget.
			// A GET retries a network failure three times with backoff, on top of the 30s client timeout,
			// which could otherwise hold the TUI from painting for over a minute.
			probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			probed, _, err := probeHost(probeCtx, token, apiHosts, nil)
			cancel()
			if err != nil {
				slog.Warn("could not detect the Wrike data center", "error", err)
				host = wrike.DefaultHost
			} else {
				host = probed
				if err := st.SetMeta(context.Background(), store.MetaKeyHost, host); err != nil {
					slog.Warn("could not store the detected Wrike host", "error", err)
				}
			}
		}
		a.startEngine(token, host)
	}
	_, err = a.prog.Run()
	a.shutdown()
	return err
}
