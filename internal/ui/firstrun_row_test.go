package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The failure text names the host that was checked, so the row must not move to another one while the check runs.
func TestDataCenterRowKeepsThePickWhileACheckRuns(t *testing.T) {
	f := newFirstRun(stepSignIn, "", defaultKeyMap(), false, false, "")
	f.hosts = []string{"www.wrike.com", "app-eu.wrike.com"}
	f.onRow, f.verifying = true, true
	for _, k := range []tea.KeyMsg{{Type: tea.KeySpace}, {Type: tea.KeyRight}, {Type: tea.KeyLeft}} {
		f, _ = f.Update(k)
	}
	if f.pick != 0 {
		t.Errorf("pick = %d, the row moved while the token was being checked", f.pick)
	}
}
