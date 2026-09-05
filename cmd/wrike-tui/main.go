package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

// @TODO: version will be set at build time through ldflags, see the Makefile.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("wrike-tui " + version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wrike-tui: "+err.Error())
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

	st, err := store.Open(context.Background(), paths.DBFile)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	slog.Info("started", "version", version, "db", paths.DBFile)

	p := tea.NewProgram(ui.New(version), tea.WithAltScreen())
	_, err = p.Run()
	return err
}
