package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"

	tea "github.com/charmbracelet/bubbletea"
)

// envProfile names a directory the run writes its profiles into, a switch for measuring, see docs/architecture.md.
const envProfile = "WRIKERY_PROFILE"

// startProfile begins a CPU profile of the run into dir and gives back the stop that ends it and writes the heap profile.
// An empty dir, the variable unset, costs nothing and the stop does nothing.
// A directory that cannot be written is an error, a switch that silently does nothing wastes the measurement.
func startProfile(dir string) (func() error, error) {
	if dir == "" {
		return func() error { return nil }, nil
	}
	cpu, err := os.Create(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envProfile, err)
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		_ = cpu.Close()
		return nil, fmt.Errorf("%s: %w", envProfile, err)
	}
	return func() error {
		pprof.StopCPUProfile()
		cpuErr := cpu.Close()
		heap, err := os.Create(filepath.Join(dir, "heap.pprof"))
		if err != nil {
			return fmt.Errorf("%s: %w", envProfile, err)
		}
		// The heap profile reports the state at the last collection, see https://pkg.go.dev/runtime/pprof#WriteHeapProfile,
		// so one runs now, or the profile would show whatever the last cycle happened to leave.
		runtime.GC()
		if err := pprof.WriteHeapProfile(heap); err != nil {
			_ = heap.Close()
			return fmt.Errorf("%s: %w", envProfile, err)
		}
		return errors.Join(heap.Close(), cpuErr)
	}, nil
}

// runProgram runs the interface and ends the profile with the final model still reachable,
// so a heap profile written at exit shows what the last screen held and not an empty heap.
func runProgram(p *tea.Program, stop func() error) error {
	final, err := p.Run()
	stopErr := stop()
	runtime.KeepAlive(final)
	return errors.Join(err, stopErr)
}
