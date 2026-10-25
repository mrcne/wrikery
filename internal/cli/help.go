package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Command is one command as the help page, its own --help and the manual describe it.
// The run function and the help read the same entry, so the two can never disagree.
type Command struct {
	Name     string               // the words after wrikery, "task list"
	Synopsis []string             // the usage lines after "wrikery ", one per form, --json left out since every command takes it
	Short    string               // the one line next to the first form on the help page
	Notes    string               // the sentences --help prints after the usage lines, may be empty
	Flags    func() *flag.FlagSet // the command's flags defined on a fresh set, nothing parsed
}

// Group is a command group, task or ui, with the notes its page prints after the usage lines of its commands.
type Group struct {
	Name, Notes string
}

var (
	groupTask = Group{Name: "task", Notes: "The task commands read the local cache and queue their writes through the same outbox as the interface."}
	groupUI   = Group{Name: "ui", Notes: `The ui commands act on the interface that is running, through its control socket, and print what it did.
With --demo they reach a running demo, and every value goes as typed, the demo's cache cannot be read.`}
)

// Commands is every command in the order the help page and the manual list them.
func Commands() []Command {
	return []Command{
		cmdSync,
		cmdTaskList, cmdTaskShow, cmdTaskStatus, cmdTaskCreate, cmdTaskEdit, cmdTaskComment, cmdTaskAssign, cmdTaskUnassign, cmdTaskMove,
		cmdUIFilter, cmdUISelect, cmdUIFolder, cmdUIList, cmdUIBoard, cmdUIOpen, cmdUIRefresh,
	}
}

// Groups is the two command groups in the order of the help page.
func Groups() []Group {
	return []Group{groupTask, groupUI}
}

// Has says whether c is one of the group's commands, task list is in task.
func (g Group) Has(c Command) bool {
	return strings.HasPrefix(c.Name, g.Name+" ")
}

// usage is the text the command's --help prints before its flags, the command's notes and then its group's,
// so a ui command's help says on its own that it reaches the interface that is running.
func (c Command) usage() string {
	notes := c.Notes
	for _, g := range Groups() {
		if g.Has(c) {
			notes = strings.TrimSpace(notes + "\n" + g.Notes)
		}
	}
	return usageText(c.Synopsis, notes)
}

// groupUsage is the page of a group: the synopsis of every command in it, then the group's notes.
func groupUsage(g Group) string {
	var synopsis []string
	for _, c := range Commands() {
		if g.Has(c) {
			synopsis = append(synopsis, c.Synopsis...)
		}
	}
	return usageText(synopsis, g.Notes)
}

func usageText(synopsis []string, notes string) string {
	var b strings.Builder
	for i, s := range synopsis {
		if i == 0 {
			b.WriteString("usage: ")
		} else {
			b.WriteString("       ")
		}
		b.WriteString("wrikery " + s + "\n")
	}
	if notes != "" {
		b.WriteString("\n" + notes + "\n")
	}
	return b.String()
}

// printFlags lists the flags of fs with two dashes and the value's placeholder, --folder F, which flag.PrintDefaults cannot print.
// The placeholder is the backquoted word of the usage text, the way flag.UnquoteUsage reads it, a bool flag has none.
// One line per flag, so the texts have to stay short enough to go unwrapped.
func printFlags(w io.Writer, fs *flag.FlagSet) {
	type row struct{ head, text string }
	var rows []row
	width := 0
	fs.VisitAll(func(f *flag.Flag) {
		placeholder, text := flag.UnquoteUsage(f)
		head := "--" + f.Name
		if placeholder != "" {
			head += " " + placeholder
		}
		// The zero values of the kinds of flag the commands define, the standard printer's own check is not exported.
		if d := f.DefValue; d != "" && d != "false" && d != "0" {
			text += " (default " + d + ")"
		}
		rows = append(rows, row{head, text})
		width = max(width, len(head))
	})
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, r.head, r.text)
	}
}

// jsonFlagSet is the start of every command's flag set, --json is the one flag they all take.
func jsonFlagSet(name string, json *bool) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.BoolVar(json, "json", false, "print JSON")
	return fs
}

const helpHead = `usage: wrikery [flags]
       wrikery [flags] <command> [arguments]
       wrikery help [<command>]

An unofficial terminal client for Wrike.
Without a command it opens the interface. With one it runs that operation and exits.

Commands:
`

// CommandNotes is the paragraph on arguments every command shares, printed on the help page and in the manual.
const CommandNotes = `A task or folder is an id or a part of its title that matches exactly one cached row.
A task may also be the number or the link from the browser, a bare number is always the number from the link, the link in quotes, like "https://app-eu.wrike.com/open.htm?id=4552825748".
A person is me, a contact id or a part of a name that matches exactly one contact.
A description is markdown, the text the E editor in the interface writes.
The ui commands act on the interface that is running and print what it did, wrikery --demo ui ... drives a demo.
Every command takes --json. Exit codes: 0 done, 1 error, 2 usage, 3 queued but not on Wrike yet.
`

const helpTail = `See "wrikery help <command>" for one command and "man wrikery" for the manual.
`

// HelpPage is the one page wrikery --help, -h and help print.
// global is the root flag set, which main owns, nil lists no flags.
func HelpPage(global *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString(helpHead)
	for _, c := range Commands() {
		row := "  wrikery " + c.Synopsis[0]
		if len(row) > 42 {
			// A long form pushes its line to the next row, the column stays readable.
			b.WriteString(row + "\n" + strings.Repeat(" ", 44) + c.Short + "\n")
			continue
		}
		_, _ = fmt.Fprintf(&b, "%-44s%s\n", row, c.Short)
	}
	b.WriteString("\n" + CommandNotes)
	if global != nil {
		b.WriteString("\nFlags:\n")
		printFlags(&b, global)
	}
	b.WriteString("\n" + helpTail)
	return b.String()
}

// helpArgs turns the words after help into the command with its --help flag, wrikery help task status is wrikery task status --help.
// The flag goes right after the command's name, before any -- the line carries, and a task or ui command has a second word.
func helpArgs(words []string) []string {
	n := 1
	for _, g := range Groups() {
		// A dash word after the group names no command, the group page answers it.
		if words[0] == g.Name && len(words) > 1 && !strings.HasPrefix(words[1], "-") {
			n = 2
		}
	}
	out := append([]string{}, words[:n]...)
	out = append(out, "--help")
	return append(out, words[n:]...)
}

// isHelpWord matches the word help and the three spellings of the flag the flag package answers with ErrHelp.
func isHelpWord(s string) bool {
	return s == "help" || s == "-h" || s == "-help" || s == "--help"
}

// IsHelp says whether args ask for help at some level, so main can skip the setup a command needs:
// a help word first, a group followed by one, or a help flag among a command's arguments before any --.
// A --help that is the value of another flag, --folder --help, is a value, the way the flag package reads it.
func IsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if isHelpWord(args[0]) {
		return true
	}
	name, rest := args[0], args[1:]
	for _, g := range Groups() {
		if args[0] != g.Name {
			continue
		}
		if len(args) == 1 {
			return false
		}
		if isHelpWord(args[1]) {
			return true
		}
		name, rest = args[0]+" "+args[1], args[2:]
	}
	var fs *flag.FlagSet
	for _, c := range Commands() {
		if c.Name == name {
			fs = c.Flags()
		}
	}
	if fs == nil {
		return false
	}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			return false
		}
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		flagName, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if flagName == "h" || flagName == "help" {
			return true
		}
		if !hasValue && valueFlag(fs, flagName) {
			i++
		}
	}
	return false
}
