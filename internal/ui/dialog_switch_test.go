package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

var moveToEU = &SwitchNeeded{From: "Acme on www.wrike.com", To: "Acme on app-eu.wrike.com", Pending: 5, Failed: 2, DBFile: "/home/ada/.local/share/wrikery/wrike.db"}

func TestSwitchBoxSaysWhatGoesWhere(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	view := squeeze(ansi.Strip(switchDialog{sw: moveToEU}.View(th, 120, 30)))
	for _, want := range []string{"5 queued and 2 failed writes", "belong to Acme on", "www.wrike.com.", "This sign in is for Acme on", "app-eu.wrike.com.",
		"Is this the new home of this cache?", "y sends the queued writes there"} {
		if !strings.Contains(view, want) {
			t.Errorf("box lacks %q:\n%s", want, view)
		}
	}
}

func TestSwitchBoxNamesTheFileOnNo(t *testing.T) {
	want := "This cache belongs to Acme on www.wrike.com. Sign in to that account, or move /home/ada/.local/share/wrikery/wrike.db away and start wrikery again for a fresh cache, the queued writes go with the file, x on the changes screen drops one first."
	if got := moveToEU.Declined(); got != want {
		t.Errorf("notice = %q", got)
	}
}

func TestAnotherAccountStateOpensTheBoxOnR(t *testing.T) {
	opts := rootTestOptions(t)
	asked := 0
	opts.Hooks.SwitchHome = func() (*SwitchNeeded, error) { asked++; return moveToEU, nil }
	m := New(opts)
	m.width, m.height = 120, 40
	next, _ := m.Update(SyncStateMsg{State: "another_account"})
	m = next.(Model)
	if bar := ansi.Strip(m.status.View(m.theme, 120, "", m.opts.Now())); !strings.Contains(bar, "another account, R decides") {
		t.Errorf("bar = %q", bar)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = next.(Model)
	for _, msg := range collect(cmd) {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if _, ok := m.dialog.(switchDialog); !ok || asked != 1 {
		t.Errorf("dialog = %T, asked %d times, want the switch box after one ask", m.dialog, asked)
	}
}

// The engine says after the first full cycle of a move that the followed spaces are not in the new account,
// the follow box opens on the settings screen for the user to pick them again.
func TestScopesLostOpensTheFollowBoxOnTheSettingsScreen(t *testing.T) {
	m := New(rootTestOptions(t))
	m.width, m.height = 120, 40
	updated, cmd := m.Update(ScopesLostMsg{})
	m = updated.(Model)
	told := false
	// The messages are fed back the way the program would, until the picker's answer has landed.
	queue := collect(cmd)
	for i := 0; i < len(queue) && i < 20; i++ {
		if toast, ok := queue[i].(toastMsg); ok {
			told = told || toast.text == "the followed spaces and projects are not in this account, pick them again"
			continue
		}
		updated, c := m.Update(queue[i])
		m = updated.(Model)
		if _, isPicker := queue[i].(openScopesMsg); isPicker && c != nil {
			queue = append(queue, c())
		}
	}
	if _, open := m.dialog.(scopesDialog); !open || !told || m.screen != screenSettings {
		t.Errorf("dialog %T, told %v, screen %v, want the follow box open on the settings screen", m.dialog, told, m.screen)
	}
}

// On the first run screen the picker is on screen already, with the new account's spaces.
func TestScopesLostLeavesTheFirstRunAlone(t *testing.T) {
	opts := rootTestOptions(t)
	opts.FirstRun = true
	m := New(opts)
	updated, cmd := m.Update(ScopesLostMsg{})
	if updated.(Model).screen != screenFirstRun || cmd != nil {
		t.Errorf("screen %v, cmd %v, want the first run left as it is", updated.(Model).screen, cmd != nil)
	}
}

// The yes runs the move, the keychain write and the engine start, the spinner keeps turning meanwhile.
func TestTheYesKeepsTheSpinnerTurning(t *testing.T) {
	opts := rootTestOptions(t)
	opts.FirstRun = true
	m := New(opts)
	sw := &SwitchNeeded{From: "Acme on www.wrike.com", To: "Acme on app-eu.wrike.com", Confirm: func() (string, error) { return "Ann", nil }}
	_, cmd := m.Update(confirmSwitchMsg{d: switchDialog{sw: sw, signIn: true}})
	ticks := false
	for _, msg := range collect(cmd) {
		if _, ok := msg.(spinner.TickMsg); ok {
			ticks = true
		}
	}
	if !ticks {
		t.Error("the yes started no spinner tick")
	}
}

// The hook refreshes the engine itself on a match, a second refresh would run a second full cycle at once.
func TestAMatchOnRRefreshesOnce(t *testing.T) {
	opts := rootTestOptions(t)
	refreshed := 0
	opts.Hooks.Refresh = func() { refreshed++ }
	m := New(opts)
	if _, cmd := m.Update(switchLoadedMsg{}); cmd == nil || refreshed != 0 {
		t.Errorf("refreshed %d times from the interface, the hook did it already", refreshed)
	}
}

func TestReachErrorTextBeforeTheAccountCheck(t *testing.T) {
	if got := reachErrorText(store.TaskRef{Kind: store.RefID, Value: "T9"}, ErrAccountUnchecked); got != "checking the account first, try again in a moment" {
		t.Errorf("text = %q", got)
	}
}

// Nothing is pulled while the engine waits for the answer, a fetch from the search or the timesheet included.
func TestReachWaitsForTheAnswerInTheAnotherAccountState(t *testing.T) {
	opts := rootTestOptions(t)
	asked := 0
	opts.Hooks.Reach = func(ctx context.Context, ref store.TaskRef) (store.Task, error) { asked++; return store.Task{}, nil }
	m := New(opts)
	next, _ := m.Update(SyncStateMsg{State: "another_account"})
	m = next.(Model)
	answer, ok := m.reach(reachMsg{ref: store.TaskRef{Kind: store.RefID, Value: "T9"}, text: "T9"})().(reachedMsg)
	if !ok || !errors.Is(answer.err, ErrAnotherAccount) || asked != 0 {
		t.Fatalf("answer = %+v, hook asked %d times, want ErrAnotherAccount and no fetch", answer, asked)
	}
	if got := reachErrorText(answer.ref, answer.err); got != "another account, R decides" {
		t.Errorf("text = %q", got)
	}
}
