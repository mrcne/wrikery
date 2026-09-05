package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"

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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wrikery: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
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

	logFile, err := os.OpenFile(paths.LogFile,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile,
		&slog.HandlerOptions{Level: cfg.SlogLevel()})))

	st, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	slog.Info("started", "version", version, "db", paths.DBFile)

	p := tea.NewProgram(ui.New(version), tea.WithAltScreen())
	_, err = p.Run()
	return err
}
