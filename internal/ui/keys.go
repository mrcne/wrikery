package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Quit, Help, Search, Timesheet, Issues, Refresh, NextPane, PrevPane, Back        key.Binding
	Up, Down, Top, Bottom, HalfDown, HalfUp, Filter, Enter, Left, Right, ToggleDone key.Binding
	Comment, CommentEditor, LogTime, Status, Assignee, Dates, EditTitle, Open       key.Binding
	CopyLink, CopyBranch, CopyID                                                    key.Binding
	Retry, Discard                                                                  key.Binding
	DayLeft, DayRight, WeekPrev, WeekNext, ThisWeek, Add, Edit, Delete              key.Binding
	Board, GroupBy, PrevGroup, NextGroup, StatusPrev, StatusNext, ColPrev, ColNext  key.Binding
}

func b(help, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Quit: b("q", "quit", "q", "ctrl+c"), Help: b("?", "help", "?"), Search: b("ctrl+f", "search", "ctrl+f"),
		Timesheet: b("T", "timesheet", "T"), Issues: b("!", "sync issues", "!"), Refresh: b("R", "refresh", "R"),
		NextPane: b("tab", "next pane", "tab"), PrevPane: b("shift+tab", "previous pane", "shift+tab"), Back: b("esc", "back", "esc"),

		Up: b("k", "up", "k", "up"), Down: b("j", "down", "j", "down"), Top: b("g", "first", "g", "home"), Bottom: b("G", "last", "G", "end"),
		HalfDown: b("ctrl+d", "half page down", "ctrl+d", "pgdown"), HalfUp: b("ctrl+u", "half page up", "ctrl+u", "pgup"),
		Filter: b("/", "filter", "/"), Enter: b("enter", "open", "enter"), Left: b("h", "collapse / left", "h", "left"), Right: b("l", "expand / right", "l", "right"),
		ToggleDone: b("z", "show completed", "z"),
		Board:      b("b", "board / list", "b"), GroupBy: b("v", "group by", "v"),
		PrevGroup: b("{", "previous group", "{"), NextGroup: b("}", "next group", "}"),
		StatusPrev: b("H", "previous status", "H"), StatusNext: b("L", "next status", "L"),
		ColPrev: b("h", "previous column", "h", "left"), ColNext: b("l", "next column", "l", "right"),

		Comment: b("c", "comment", "c"), CommentEditor: b("C", "comment in $EDITOR", "C"), LogTime: b("t", "log time", "t"),
		Status: b("s", "status", "s"), Assignee: b("a", "assignee", "a"), Dates: b("d", "dates", "d"), EditTitle: b("e", "title", "e"),
		Open: b("o", "open in browser", "o"), CopyLink: b("y", "copy permalink", "y"), CopyBranch: b("Y", "copy branch name", "Y"), CopyID: b("i", "copy task id", "i"),

		Retry: b("r", "retry", "r"), Discard: b("x", "discard", "x"),

		DayLeft: b("h", "previous day", "h", "left"), DayRight: b("l", "next day", "l", "right"),
		WeekPrev: b("[", "previous week", "["), WeekNext: b("]", "next week", "]"), ThisWeek: b(".", "this week", "."),
		Add: b("n", "add entry", "n"), Edit: b("e", "edit entry", "e"), Delete: b("x", "delete entry", "x"),
	}
}

func (k KeyMap) global() []key.Binding {
	return []key.Binding{k.NextPane, k.Help, k.Search, k.Timesheet, k.Issues, k.Refresh, k.Back, k.Quit}
}

func (k KeyMap) list() []key.Binding {
	return []key.Binding{k.Down, k.Up, k.Top, k.Bottom, k.HalfDown, k.HalfUp, k.Filter, k.Enter, k.Left, k.Right, k.ToggleDone, k.GroupBy, k.PrevGroup, k.NextGroup, k.Board}
}

func (k KeyMap) task() []key.Binding {
	return []key.Binding{k.Comment, k.CommentEditor, k.LogTime, k.Status, k.StatusPrev, k.StatusNext, k.Assignee, k.Dates, k.EditTitle, k.Open, k.CopyLink, k.CopyBranch, k.CopyID}
}

func (k KeyMap) board() []key.Binding {
	return []key.Binding{k.ColPrev, k.ColNext, k.Down, k.Up, k.Top, k.Bottom, k.PrevGroup, k.NextGroup, k.Filter, k.ToggleDone, k.GroupBy, k.Board, k.Enter}
}

func (k KeyMap) timesheet() []key.Binding {
	return []key.Binding{k.DayLeft, k.DayRight, k.Down, k.Up, k.WeekPrev, k.WeekNext, k.ThisWeek, k.Add, k.Edit, k.Delete, k.Enter}
}

func (k KeyMap) issues() []key.Binding {
	return []key.Binding{k.Down, k.Up, k.Retry, k.Discard, k.Enter}
}
