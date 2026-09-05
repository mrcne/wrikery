package ui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/mrcne/wrikery/internal/ui"
)

func TestAppShowsTitleAndQuitsOnQ(t *testing.T) {
	tm := teatest.NewTestModel(t, ui.New("test"),
		teatest.WithInitialTermSize(80, 24))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "wrikery test")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
