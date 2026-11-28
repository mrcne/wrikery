package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/store"
)

var (
	cmdUIFilter = Command{
		Name:     "ui filter",
		Synopsis: []string{"ui filter [--importance L]... [--person P] [--status S]... | --clear"},
		Short:    "narrow the running interface, or clear its filter",
		Notes: `Narrows the interface that is running to importance levels, a person and statuses, in any combination, or clears its filter with --clear.
A person is me, unassigned, a contact id or a part of a name, a status is a name from any cached workflow.`,
		Flags: func() *flag.FlagSet { return new(filterOpts).flagSet() },
	}
	cmdUISelect = Command{
		Name:     "ui select",
		Synopsis: []string{"ui select T"},
		Short:    "show a task in the running interface",
		Notes: `Shows a task in the interface that is running, the way a jump from the search does.
T is an id, a part of its title, the number or the link from the browser.`,
		Flags: func() *flag.FlagSet { return jsonFlagSet("ui select", new(bool)) },
	}
	cmdUIFolder = Command{
		Name:     "ui folder",
		Synopsis: []string{"ui folder F"},
		Short:    "open a space, project or folder there, me for My tasks",
		Notes: `Opens a space, project or folder in the interface that is running, as from the sidebar.
F is an id or a part of its title, me is My tasks.`,
		Flags: func() *flag.FlagSet { return jsonFlagSet("ui folder", new(bool)) },
	}
	cmdUIList = Command{
		Name:     "ui list",
		Synopsis: []string{"ui list"},
		Short:    "the list shape of the running interface",
		Notes:    "Switches the interface that is running to the list.",
		Flags:    func() *flag.FlagSet { return jsonFlagSet("ui list", new(bool)) },
	}
	cmdUIBoard = Command{
		Name:     "ui board",
		Synopsis: []string{"ui board"},
		Short:    "the board shape of the running interface",
		Notes:    "Switches the interface that is running to the board over workflow statuses.",
		Flags:    func() *flag.FlagSet { return jsonFlagSet("ui board", new(bool)) },
	}
	cmdUIOpen = Command{
		Name:     "ui open",
		Synopsis: []string{"ui open tasks|timesheet|changes|inbox|settings"},
		Short:    "open one screen of the running interface",
		Notes:    "Opens one screen of the interface that is running: tasks, timesheet, changes, inbox or settings.",
		Flags:    func() *flag.FlagSet { return jsonFlagSet("ui open", new(bool)) },
	}
	cmdUIShow = Command{
		Name:     "ui show",
		Synopsis: []string{"ui show"},
		Short:    "what the running interface has in view, the task and the folder",
		Notes: `Prints what the interface that is running has in view: the screen, the shape, the folder the list is on, the task the keys act on and the filter.
The task is the one a key pressed now would change, none on a screen without one and none for the moment the list or the detail pane is still reading what it was sent to.
It answers with a box open as well and says busy, every other ui command is refused then.
While the external editor is open the interface answers nothing and the command exits with 1.
In JSON the folder and the task have their ids, null when there is none, and the filter has the values ui filter takes.`,
		Flags: func() *flag.FlagSet { return jsonFlagSet("ui show", new(bool)) },
	}
	cmdUIRefresh = Command{
		Name:     "ui refresh",
		Synopsis: []string{"ui refresh"},
		Short:    "a sync cycle now, what R does",
		Notes:    "Runs a sync cycle in the interface that is running, what R does there.",
		Flags:    func() *flag.FlagSet { return jsonFlagSet("ui refresh", new(bool)) },
	}
)

// uiJSON is the answer of every ui command, the interface's own line.
type uiJSON struct {
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

type filterOpts struct {
	levels, statuses multiFlag
	person           string
	clear, json      bool
}

func (o *filterOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("ui filter", &o.json)
	fs.Var(&o.levels, "importance", "`L` is High, Normal or Low, may repeat")
	fs.StringVar(&o.person, "person", "", "`P` is me, unassigned, a contact id or a part of a name")
	fs.Var(&o.statuses, "status", "a status name `S`, may repeat")
	fs.BoolVar(&o.clear, "clear", false, "clear the filter, what F does")
	return fs
}

// runUI serves the ui group. Every command resolves what it can against the cache, sends one request and prints the reply.
// Without a store, the demo, the values go as typed.
func runUI(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, groupUsage(groupUI))
		return exitUsage
	}
	if isHelpWord(args[0]) {
		_, _ = fmt.Fprint(env.Stdout, groupUsage(groupUI))
		return exitOK
	}
	switch args[0] {
	case "filter":
		return runUIFilter(ctx, env, args[1:])
	case "select":
		return runUISelect(ctx, env, args[1:])
	case "folder":
		return runUIFolder(ctx, env, args[1:])
	case "list":
		return runUIPlain(ctx, env, args[1:], control.Request{Op: control.OpShape, Shape: "list"}, cmdUIList)
	case "board":
		return runUIPlain(ctx, env, args[1:], control.Request{Op: control.OpShape, Shape: "board"}, cmdUIBoard)
	case "open":
		return runUIOpen(ctx, env, args[1:])
	case "refresh":
		return runUIPlain(ctx, env, args[1:], control.Request{Op: control.OpRefresh}, cmdUIRefresh)
	case "show":
		return runUIShow(ctx, env, args[1:])
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: unknown ui command %q\n", args[0])
	_, _ = fmt.Fprint(env.Stderr, groupUsage(groupUI))
	return exitUsage
}

func runUIFilter(ctx context.Context, env Env, args []string) int {
	var o filterOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdUIFilter.usage(), args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, cmdUIFilter.usage(), "ui filter takes flags only")
	}
	if given(fs, "person") && strings.TrimSpace(o.person) == "" {
		// A blank person must not pass as no person and clear the filter.
		return usageError(env, fs, cmdUIFilter.usage(), "the person is empty")
	}
	wanted := len(o.levels)+len(o.statuses) > 0 || o.person != ""
	if o.clear && wanted {
		return usageError(env, fs, cmdUIFilter.usage(), "--clear goes alone")
	}
	if !o.clear && !wanted {
		return usageError(env, fs, cmdUIFilter.usage(), "ui filter needs --importance, --person, --status or --clear")
	}
	req := control.Request{Op: control.OpFilter}
	for _, l := range o.levels {
		level, err := importanceLevel(l)
		if err != nil {
			return usageError(env, fs, cmdUIFilter.usage(), err.Error())
		}
		if level == "" {
			return usageError(env, fs, cmdUIFilter.usage(), "the importance is empty")
		}
		req.Importance = append(req.Importance, level)
	}
	var ref refData
	if env.Store != nil {
		var err error
		if ref, err = loadRef(ctx, env.Store); err != nil {
			return fail(env, err)
		}
	}
	switch p := strings.TrimSpace(o.person); {
	case p == "":
	case strings.EqualFold(p, "me") || strings.EqualFold(p, "unassigned"):
		// The interface knows its own id, and unassigned is its word.
		req.Person = strings.ToLower(p)
	case env.Store == nil:
		req.Person = p
	default:
		id, err := resolveFilterPerson(ref, p)
		if err != nil {
			return fail(env, err)
		}
		req.Person = id
	}
	for _, s := range o.statuses {
		name := strings.TrimSpace(s)
		if name == "" {
			return usageError(env, fs, cmdUIFilter.usage(), "a status name is empty")
		}
		if env.Store != nil && !knownStatus(ref.workflows, name) {
			return fail(env, fmt.Errorf("no status named %q in any workflow", name))
		}
		req.Statuses = append(req.Statuses, name)
	}
	return uiCall(ctx, env, req, o.json, "")
}

// resolveFilterPerson is the person resolver for the filter: a contact id or a part of a name, groups left out,
// but someone who left the account kept, the filter box offers whoever is on a task in view and finding their tasks is the point.
func resolveFilterPerson(ref refData, arg string) (string, error) {
	if c, ok := ref.contacts[arg]; ok && c.Type != "Group" {
		return c.ID, nil
	}
	var people []store.Contact
	for _, c := range ref.contacts {
		if c.Type != "Group" {
			people = append(people, c)
		}
	}
	return matchContact(arg, people)
}

// knownStatus says whether some cached workflow spells a status so, case aside. The interface keys the filter by name.
func knownStatus(workflows []store.Workflow, name string) bool {
	for _, wf := range workflows {
		for _, cs := range wf.CustomStatuses {
			if strings.EqualFold(cs.Name, name) {
				return true
			}
		}
	}
	return false
}

func runUISelect(ctx context.Context, env Env, args []string) int {
	var asJSON bool
	fs := jsonFlagSet("ui select", &asJSON)
	positional, code, done := parse(env, fs, cmdUISelect.usage(), args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, cmdUISelect.usage(), "ui select takes one task, an id or a part of its title")
	}
	req := control.Request{Op: control.OpSelect, TaskID: positional[0]}
	text := "selected " + positional[0]
	if env.Store != nil {
		t, code := resolveOrReach(ctx, env, positional[0])
		if code != exitOK {
			return code
		}
		// Every folder goes along, the interface opens the first one its sidebar has,
		// and the people on the task, so the user's own task can land in My tasks when no folder is there.
		req.TaskID, req.ParentIDs, req.ResponsibleIDs = t.ID, t.ParentIDs, t.ResponsibleIDs
		text = "selected " + t.Title
	}
	return uiCall(ctx, env, req, asJSON, text)
}

func runUIFolder(ctx context.Context, env Env, args []string) int {
	var asJSON bool
	fs := jsonFlagSet("ui folder", &asJSON)
	positional, code, done := parse(env, fs, cmdUIFolder.usage(), args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, cmdUIFolder.usage(), "ui folder takes one folder, project or space, an id or a part of its title, or me")
	}
	req := control.Request{Op: control.OpFolder, FolderID: positional[0]}
	switch {
	case strings.EqualFold(positional[0], "me"):
		req.FolderID = store.ScopeKindMe
	case env.Store != nil:
		fo, err := resolveFolder(ctx, env.Store, positional[0])
		if err != nil {
			return fail(env, err)
		}
		req.FolderID = fo.ID
	}
	return uiCall(ctx, env, req, asJSON, "")
}

func runUIOpen(ctx context.Context, env Env, args []string) int {
	var asJSON bool
	fs := jsonFlagSet("ui open", &asJSON)
	positional, code, done := parse(env, fs, cmdUIOpen.usage(), args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, cmdUIOpen.usage(), "ui open takes one screen, tasks, timesheet, changes, inbox or settings")
	}
	switch positional[0] {
	case "tasks", "timesheet", "changes", "inbox", "settings":
	default:
		return usageError(env, fs, cmdUIOpen.usage(), fmt.Sprintf("no screen %q, tasks, timesheet, changes, inbox or settings", positional[0]))
	}
	return uiCall(ctx, env, control.Request{Op: control.OpScreen, Screen: positional[0]}, asJSON, "")
}

// runUIPlain serves the commands with no argument of their own, list, board and refresh.
func runUIPlain(ctx context.Context, env Env, args []string, req control.Request, cmd Command) int {
	var asJSON bool
	fs := jsonFlagSet(cmd.Name, &asJSON)
	positional, code, done := parse(env, fs, cmd.usage(), args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, cmd.usage(), cmd.Name+" takes no arguments")
	}
	return uiCall(ctx, env, req, asJSON, "")
}

// runUIShow asks the interface what it has in view and prints it, the one ui command that reads.
func runUIShow(ctx context.Context, env Env, args []string) int {
	var asJSON bool
	fs := jsonFlagSet("ui show", &asJSON)
	positional, code, done := parse(env, fs, cmdUIShow.usage(), args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, cmdUIShow.usage(), "ui show takes no arguments")
	}
	reply, code := uiAsk(ctx, env, control.Request{Op: control.OpShow})
	if code != exitOK {
		return code
	}
	if reply.State == nil {
		return fail(env, errors.New("the running interface answered without its state, restart it"))
	}
	state := *reply.State
	// A list is never null in the JSON a script reads, whatever the other end sent.
	if state.Filter.Importance == nil {
		state.Filter.Importance = []string{}
	}
	if state.Filter.Statuses == nil {
		state.Filter.Statuses = []string{}
	}
	if asJSON {
		return printJSON(env, state)
	}
	printUIState(env, state)
	return exitOK
}

// printUIState writes one line per part of the state, the label first as task show writes its header,
// with the filter worded as the line under the rows of the interface.
func printUIState(env Env, s control.State) {
	label := lipgloss.NewStyle().Foreground(env.Theme.Muted)
	dim := lipgloss.NewStyle().Foreground(env.Theme.Dim)
	line := func(name, value string) {
		_, _ = fmt.Fprintf(env.Stdout, "%s%s\n", label.Render(padRight(name, 8)), value)
	}
	item := func(it *control.StateItem) string {
		if it == nil {
			return "none"
		}
		return it.Title + "  " + dim.Render(it.ID)
	}
	line("screen", s.Screen)
	line("shape", s.Shape)
	line("folder", item(s.Folder))
	line("task", item(s.Task))
	var parts []string
	if len(s.Filter.Importance) > 0 {
		parts = append(parts, strings.Join(s.Filter.Importance, ", "))
	}
	if s.Filter.Person != nil {
		parts = append(parts, s.Filter.Person.Name)
	}
	if len(s.Filter.Statuses) > 0 {
		parts = append(parts, strings.Join(s.Filter.Statuses, ", "))
	}
	if len(parts) == 0 {
		parts = []string{"none"}
	}
	line("filter", strings.Join(parts, " | "))
	if s.Busy {
		line("busy", "a box, the search, the help, a filter field or the sign in screen has the keys")
	}
}

// uiAsk sends the request and returns the interface's reply, or the exit code after the reason was printed.
// No interface and a refusal are both exit 1, the command did not do what it was asked.
func uiAsk(ctx context.Context, env Env, req control.Request) (control.Reply, int) {
	reply, err := control.Call(ctx, env.Socket, req)
	if errors.Is(err, control.ErrNotRunning) {
		return reply, fail(env, errors.New("no wrikery interface is running"))
	}
	if err != nil {
		return reply, fail(env, err)
	}
	if !reply.OK {
		if reply.Error == "" {
			reply.Error = "the interface refused the request"
		}
		return reply, fail(env, errors.New(reply.Error))
	}
	return reply, exitOK
}

// uiCall sends the request and prints the interface's line, or text when the command has a better one.
// A name that matched nothing is exit 1 like no interface and a refusal.
func uiCall(ctx context.Context, env Env, req control.Request, asJSON bool, text string) int {
	reply, code := uiAsk(ctx, env, req)
	if code != exitOK {
		return code
	}
	if text == "" {
		text = reply.Text
	}
	if asJSON {
		return printJSON(env, uiJSON{OK: true, Text: text})
	}
	_, _ = fmt.Fprintln(env.Stdout, text)
	return exitOK
}
