package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

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
		Synopsis: []string{"ui open tasks|timesheet|changes|settings"},
		Short:    "open one screen of the running interface",
		Notes:    "Opens one screen of the interface that is running: tasks, timesheet, changes or settings.",
		Flags:    func() *flag.FlagSet { return jsonFlagSet("ui open", new(bool)) },
	}
	cmdUIRefresh = Command{
		Name:     "ui refresh",
		Synopsis: []string{"ui refresh"},
		Short:    "a sync cycle now, what R does",
		Notes:    "Runs a sync cycle in the interface that is running, what R does there.",
		Flags:    func() *flag.FlagSet { return jsonFlagSet("ui refresh", new(bool)) },
	}
)

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
		return usageError(env, fs, cmdUIOpen.usage(), "ui open takes one screen, tasks, timesheet, changes or settings")
	}
	switch positional[0] {
	case "tasks", "timesheet", "changes", "settings":
	default:
		return usageError(env, fs, cmdUIOpen.usage(), fmt.Sprintf("no screen %q, tasks, timesheet, changes or settings", positional[0]))
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

// uiCall sends the request and prints the interface's line, or text when the command has a better one.
// No interface, a refusal and a name that matched nothing are all exit 1, the command did not do what it was asked.
func uiCall(ctx context.Context, env Env, req control.Request, asJSON bool, text string) int {
	reply, err := control.Call(ctx, env.Socket, req)
	if errors.Is(err, control.ErrNotRunning) {
		return fail(env, errors.New("no wrikery interface is running"))
	}
	if err != nil {
		return fail(env, err)
	}
	if !reply.OK {
		if reply.Error == "" {
			reply.Error = "the interface refused the request"
		}
		return fail(env, errors.New(reply.Error))
	}
	if text == "" {
		text = reply.Text
	}
	if asJSON {
		type result struct {
			OK   bool   `json:"ok"`
			Text string `json:"text"`
		}
		return printJSON(env, result{OK: true, Text: text})
	}
	_, _ = fmt.Fprintln(env.Stdout, text)
	return exitOK
}
