package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/auth"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

// TODO: version will be set at build time through ldflags, see the Makefile.
var version = "dev"

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
		fmt.Println("wrikery " + version)
		return
	}
	if err := run(*demoMode, *logout); err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		os.Exit(1)
	}
}

func run(demoMode, logout bool) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
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
			Version: version, Store: st, Config: cfg.UI, Demo: true,
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
	slog.Info("started", "version", version, "db", paths.DBFile)

	a := &app{cfg: cfg, st: st, tokens: tokens}
	token, err := tokens.Load()
	firstRun := errors.Is(err, auth.ErrNoToken)
	if err != nil && !firstRun {
		return err
	}
	a.prog = tea.NewProgram(ui.New(ui.Options{
		Version: version, Store: st, Config: cfg.UI, FirstRun: firstRun, Hooks: a.hooks(),
	}), tea.WithAltScreen())
	if !firstRun {
		a.startEngine(token)
	}
	_, err = a.prog.Run()
	a.shutdown()
	return err
}
