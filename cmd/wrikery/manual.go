package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/mrcne/wrikery/internal/cli"
	"github.com/mrcne/wrikery/internal/ui"
)

// manualDate is the .TH date, the day the text of the page last changed.
// mandoc warns on a page without a date, and a build date would change the committed page on every run.
const manualDate = "2026-10-24"

// troff escapes text for one line of the page.
// A backslash and a hyphen are troff's own, and a line that starts with a dot or a quote is read as a request and vanishes.
func troff(s string) string {
	s = strings.ReplaceAll(s, `\`, `\e`)
	s = strings.ReplaceAll(s, "-", `\-`)
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		s = `\&` + s
	}
	return s
}

// arg makes one macro argument of s: quoted when it holds a space, and a double quote inside becomes the \(dq glyph,
// which groff and mandoc both know, since a bare one would end the argument.
func arg(s string) string {
	s = strings.ReplaceAll(s, `"`, `\(dq`)
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

var descriptionParagraphs = []string{
	`wrikery is a terminal client for Wrike, made for people who prefer the keyboard and the terminal over the web application.
It browses the spaces, projects and folders you follow, shows a task with its description, comments, subtasks and dependencies, adds comments, changes statuses, logs time and shows your timesheet, without leaving the terminal.`,
	`It keeps a local copy of what you follow in one SQLite file, so browsing and search are instant and reading works offline.
A change is applied to that copy at once and waits in a queue, which is sent when Wrike can be reached.
The changes screen, !, lists what went out over the last thirty days with the old and the new values, and the writes that failed, for a retry or a discard.`,
	`Without a command it opens the interface.
With a command it runs one operation on the same local copy and exits, for scripts and agents, with --json for a script.
wrikery --demo runs on built in sample data with no account and no network.
The first run asks for a permanent API token, or signs in through the browser when the config file names an API app, and keeps the credential in the system keychain.`,
}

const keysIntro = `The ? key shows the bindings of the current screen.
Vim keys and the arrow keys both work, and home, end, page up and page down work beside g, G, ctrl+d and ctrl+u.
Tab moves between the panes, enter goes one level deeper and esc goes back.`

// A tagged paragraph of the EXIT STATUS, ENVIRONMENT and FILES sections, the tag in bold or in italics.
type entry struct {
	tag, text string
	italic    bool
}

var exitStatus = []entry{
	{tag: "0", text: "The operation is done, a write is on Wrike and a read printed the cache."},
	{tag: "1", text: `An error: no sign in, a name that matched nothing, a store error, a write Wrike rejected, or a token Wrike refused, which leaves the change queued.`},
	{tag: "2", text: "A usage mistake, the error and the usage went to standard error."},
	{tag: "3", text: `The write is queued and not on Wrike yet, in offline mode or because Wrike could not be reached.
The next sync sends it, from the interface or from wrikery sync.`},
}

var environment = []entry{
	{tag: "WRIKERY_TOKEN", text: `A permanent API token that overrides the stored sign in, keychain and files both.
A command run with it compares its account with the one the cache was built from and refuses another.`},
	{tag: "NO_COLOR", text: "Plain output without colors, what --no-color does."},
	{tag: "VISUAL, EDITOR", text: `The editor E and C open for a description or a longer comment, VISUAL first.
A command that waits for a window, code --wait, works the same way.`},
	{tag: "XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME", text: "Override ~/.config, ~/.local/share and ~/.local/state, where the files below live."},
}

var files = []entry{
	{tag: "~/.config/wrikery/config.toml", italic: true, text: `The config file, --config PATH reads another one.
The settings screen, opened with the comma key, shows the keys of the file with their values, the full list of keys is in docs/overview.md on the project page.`},
	{tag: "~/.local/share/wrikery/wrike.db", italic: true, text: `The local copy and the queue of writes in one SQLite file.
Deleting it resets the cache, and the queued writes go with it.`},
	{tag: "~/.local/state/wrikery/wrikery.log", italic: true, text: `The log of the last run, the previous run's log is kept as wrikery.log.1.
A demo run writes demo.log in the same directory.`},
	{tag: "The API token", text: `In the system keychain, the macOS Keychain or the Secret Service on Linux, falling back to ~/.config/wrikery/token with mode 0600 on a machine without one.`},
	{tag: "The browser session", text: `A second keychain item with the access token, the refresh token, the expiry, the host and the app, falling back to ~/.config/wrikery/oauth.json with mode 0600, and ~/.config/wrikery/auth.lock held while it is renewed.`},
}

// examples are the commands the README shows.
const examples = `wrikery sync                                 # one sync cycle, --full also checks for deleted tasks
wrikery task list --folder "4 Later"         # tasks of a folder, project or space
wrikery task list                            # your own open tasks
wrikery task show "licence file"             # one task with its description and comments
wrikery task show "https://app-eu.wrike.com/open.htm?id=4552825748"
wrikery task status MAAAAAEPXpuT "In Progress"
wrikery task create --folder Sandbox Try the new command
wrikery task create --folder Sandbox --importance High --description notes.md Try it with a description
wrikery task edit MAAAAAEPXpuT --title "A plainer title" --description -
wrikery task comment MAAAAAEPXpuT Pushed the branch
wrikery task assign MAAAAAEPXpuT me
wrikery task move MAAAAAEPXpuT --to "Next release" --from Backlog
wrikery ui filter --importance High --person me # narrow the interface that is running
wrikery ui folder "Next release"                # open a folder there, as from the sidebar`

const seeAlso = `The project page, https://github.com/mrcne/wrikery, with the product description in docs/overview.md and the technical design in docs/architecture.md.`

// manualPage is man/wrikery.1 in the man(7) macros, built from the command table, the root flag set and the key groups,
// so a change to any of them shows up in the committed page.
func manualPage(global *flag.FlagSet) string {
	var b strings.Builder
	line := func(format string, args ...any) { _, _ = fmt.Fprintf(&b, format+"\n", args...) }
	// text writes prose, one sentence per source line, escaped.
	text := func(s string) {
		for _, l := range strings.Split(s, "\n") {
			line("%s", troff(l))
		}
	}
	flags := func(fs *flag.FlagSet) {
		fs.VisitAll(func(f *flag.Flag) {
			placeholder, usage := flag.UnquoteUsage(f)
			line(".TP")
			if placeholder == "" {
				line(`.B \-\-%s`, troff(f.Name))
			} else {
				line(`.BI "\-\-%s " %s`, troff(f.Name), placeholder)
			}
			text(usage)
		})
	}
	entries := func(list []entry) {
		for _, e := range list {
			line(".TP")
			if e.italic {
				line(".I %s", arg(troff(e.tag)))
			} else {
				line(".B %s", arg(troff(e.tag)))
			}
			text(e.text)
		}
	}

	line(`.TH WRIKERY 1 %s wrikery "User Commands"`, manualDate)
	line(".SH NAME")
	line(`wrikery \- an unofficial terminal client for Wrike`)
	line(".SH SYNOPSIS")
	line(".B wrikery")
	line(".RI [ flags ]")
	line(".br")
	line(".B wrikery")
	line(".RI [ flags ]")
	line(".I command")
	line(".RI [ arguments ]")
	line(".br")
	line(".B wrikery help")
	line(".RI [ command ]")
	line(".SH DESCRIPTION")
	for i, p := range descriptionParagraphs {
		if i > 0 {
			line(".PP")
		}
		text(p)
	}
	line(".SH OPTIONS")
	flags(global)

	line(".SH COMMANDS")
	text(strings.TrimRight(cli.CommandNotes, "\n"))
	headed := map[string]bool{}
	for _, c := range cli.Commands() {
		// The group's own heading and notes come before its first command.
		for _, g := range cli.Groups() {
			if !headed[g.Name] && strings.HasPrefix(c.Name, g.Name+" ") {
				line(".SS The %s commands", g.Name)
				text(g.Notes)
				headed[g.Name] = true
			}
		}
		line(".SS %s", c.Name)
		// Filled, not .nf, so a long synopsis wraps at a word on a narrow terminal instead of running off the edge.
		for i, s := range c.Synopsis {
			if i > 0 {
				line(".br")
			}
			line(".B %s", arg(troff("wrikery "+s)))
		}
		if c.Notes != "" {
			line(".PP")
			text(c.Notes)
		}
		line(".RS")
		flags(c.Flags())
		line(".RE")
	}

	line(".SH KEYS")
	text(keysIntro)
	for _, g := range ui.KeyGroups() {
		line(".SS %s", g.Title)
		for _, k := range g.Keys {
			h := k.Help()
			line(".TP")
			line(".B %s", arg(troff(h.Key)))
			text(h.Desc)
		}
	}

	line(".SH EXIT STATUS")
	entries(exitStatus)
	line(".SH ENVIRONMENT")
	entries(environment)
	line(".SH FILES")
	text("The directories follow the XDG base directory convention on macOS and Linux alike.")
	entries(files)
	line(".SH EXAMPLES")
	line(".nf")
	text(examples)
	line(".fi")
	line(".SH SEE ALSO")
	text(seeAlso)
	return b.String()
}
