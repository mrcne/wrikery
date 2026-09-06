package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

// Messages main sends from engine events. Strings mirror the syncer constants so this package does not import it.
type SyncStateMsg struct{ State string }
type StoreChangedMsg struct{ Entities []string }
type OutboxChangedMsg struct{ Pending, Failed int }

type errMsg struct{ err error }

type refData struct {
	meID      string
	contacts  map[string]store.Contact
	statuses  map[string]store.CustomStatus
	workflows []store.Workflow
}

type refLoadedMsg struct{ ref refData }
type scopesLoadedMsg struct{ scopes []store.Scope }
type pickerLoadedMsg struct {
	spaces   []store.Space
	projects map[string][]store.Folder // per space id, projects directly under the root
}
type tokenVerifiedMsg struct {
	name string
	err  error
}

// Intents from the first run child. The root turns them into commands.
type firstRunSubmitTokenMsg struct{ token string }
type firstRunConfirmScopesMsg struct{ scopes []store.Scope }
type firstRunFinishedMsg struct{}

func intent(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }
