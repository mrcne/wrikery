package ui

import "github.com/charmbracelet/bubbles/key"

// KeyGroup is one part of the interface with the bindings the ? overlay shows for it, for the manual.
type KeyGroup struct {
	Title string        // the manual's heading
	Keys  []key.Binding // help key and text through Help(), the same bindings the overlay draws
}

// KeyGroups is every group of the ? overlay in the order the manual lists them, from the same methods the overlay reads.
func KeyGroups() []KeyGroup {
	k := defaultKeyMap()
	return []KeyGroup{
		{Title: "Everywhere", Keys: k.global()},
		{Title: "The list", Keys: k.list()},
		{Title: "The task", Keys: k.task()},
		{Title: "The board", Keys: k.board()},
		{Title: "The timesheet", Keys: k.timesheet()},
		{Title: "The changes screen", Keys: k.changes()},
		{Title: "The inbox", Keys: k.inbox()},
		{Title: "The settings screen", Keys: k.settings()},
	}
}
