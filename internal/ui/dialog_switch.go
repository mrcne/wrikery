package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SwitchNeeded is the answer of a sign in, or of Hooks.SwitchHome, when the credential belongs to another account,
// data center or person than the cache was built from. Nothing has been stored or sent:
// the box asks, and Confirm does the move after the user's yes.
type SwitchNeeded struct {
	From, To        string // "Acme on www.wrike.com", or "Ada Nowak in Acme on www.wrike.com" when the person differs
	Pending, Failed int    // the queue the move would send, the counts of the status bar
	DBFile          string // the cache, which the answer no names
	// Confirm is the yes: the cache moves, the credential is stored and the engine starts.
	// It runs on a context of its own, the sign in's has ended by then, and answers the user's name for the first run screen.
	Confirm func() (string, error)
}

func (s *SwitchNeeded) Error() string {
	return "this sign in is for " + s.To + ", the cache belongs to " + s.From
}

// Declined is what the first run screen says after the answer no.
func (s *SwitchNeeded) Declined() string {
	return "This cache belongs to " + s.From + ". Sign in to that account, or move " + s.DBFile +
		" away and start wrikery again for a fresh cache, the queued writes go with the file, x on the changes screen drops one first."
}

// switchDialog asks whether the account a credential belongs to is the new home of the cache.
// signIn tells the first run's answer from the one R asked for in the another account state, browser and app ride along for the former.
type switchDialog struct {
	sw       *SwitchNeeded
	signIn   bool
	browser  bool
	app      string
	answered bool
}

type confirmSwitchMsg struct{ d switchDialog }  // the yes
type switchDeclinedMsg struct{ d switchDialog } // the no, or esc
type switchLoadedMsg struct {                   // the answer of Hooks.SwitchHome, a nil box when the account matches again
	sw  *SwitchNeeded
	err error
}
type switchDoneMsg struct{ err error } // the yes asked from the another account state ran through

func (d switchDialog) Update(msg tea.KeyMsg) (dialog, tea.Cmd) {
	if d.answered {
		return d, nil
	}
	switch msg.String() {
	case "y":
		d.answered = true
		return d, tea.Batch(intent(confirmSwitchMsg{d: d}), intent(closeDialogMsg{}))
	case "n":
		d.answered = true
		return d, tea.Batch(intent(switchDeclinedMsg{d: d}), intent(closeDialogMsg{}))
	}
	return d, nil
}

func (d switchDialog) View(th Theme, width, height int) string {
	w := min(width, 72)
	text := lipgloss.NewStyle().Width(w - 2)
	body := text.Render(fmt.Sprintf("This cache, %d queued and %d failed writes belong to %s.", d.sw.Pending, d.sw.Failed, d.sw.From)) + "\n" +
		text.Render("This sign in is for "+d.sw.To+".") + "\n\n" +
		text.Render("Is this the new home of this cache? y sends the queued writes there and rebuilds the cache from it, n leaves everything as it is.") + "\n\n" +
		lipgloss.NewStyle().Foreground(th.Muted).Render("y yes   n or esc no")
	return th.box("Another account", body, w, lipgloss.Height(body)+2, true)
}
