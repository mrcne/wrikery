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

const uiUsage = `usage: wrikery ui filter [--importance L]... [--person P] [--status S]... [--json]
       wrikery ui filter --clear
       wrikery ui select T
       wrikery ui folder F
       wrikery ui list
       wrikery ui board
       wrikery ui open tasks|timesheet|changes|settings
       wrikery ui refresh

Acts on the interface that is running, through its control socket, and prints what it did.
A person is me, unassigned, a contact id or a part of a name. F is a folder, project or space, or me for My tasks.
With --demo the commands reach a running demo, and every value goes as typed, the demo's cache cannot be read.
`

// runUI serves the ui group. Every command resolves what it can against the cache, sends one request and prints the reply.
// Without a store, the demo, the values go as typed.
func runUI(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, uiUsage)
		return exitUsage
	}
	switch args[0] {
	case "filter":
		return runUIFilter(ctx, env, args[1:])
	case "select":
		return runUISelect(ctx, env, args[1:])
	case "folder":
		return runUIFolder(ctx, env, args[1:])
	case "list", "board":
		return runUIPlain(ctx, env, args[1:], control.Request{Op: control.OpShape, Shape: args[0]}, "ui "+args[0])
	case "open":
		return runUIOpen(ctx, env, args[1:])
	case "refresh":
		return runUIPlain(ctx, env, args[1:], control.Request{Op: control.OpRefresh}, "ui refresh")
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: unknown ui command %q\n", args[0])
	_, _ = fmt.Fprint(env.Stderr, uiUsage)
	return exitUsage
}

func runUIFilter(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("ui filter", flag.ContinueOnError)
	var levels, statuses multiFlag
	fs.Var(&levels, "importance", "High, Normal or Low, may repeat")
	person := fs.String("person", "", "me, unassigned, a contact id or a part of a name")
	fs.Var(&statuses, "status", "a status name, may repeat")
	clearAll := fs.Bool("clear", false, "clear the filter, what F does")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, uiUsage, args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, uiUsage, "ui filter takes flags only")
	}
	if given(fs, "person") && strings.TrimSpace(*person) == "" {
		// A blank person must not pass as no person and clear the filter.
		return usageError(env, fs, uiUsage, "the person is empty")
	}
	wanted := len(levels)+len(statuses) > 0 || *person != ""
	if *clearAll && wanted {
		return usageError(env, fs, uiUsage, "--clear goes alone")
	}
	if !*clearAll && !wanted {
		return usageError(env, fs, uiUsage, "ui filter needs --importance, --person, --status or --clear")
	}
	req := control.Request{Op: control.OpFilter}
	for _, l := range levels {
		level, err := importanceLevel(l)
		if err != nil {
			return usageError(env, fs, uiUsage, err.Error())
		}
		if level == "" {
			return usageError(env, fs, uiUsage, "the importance is empty")
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
	switch p := strings.TrimSpace(*person); {
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
	for _, s := range statuses {
		name := strings.TrimSpace(s)
		if name == "" {
			return usageError(env, fs, uiUsage, "a status name is empty")
		}
		if env.Store != nil && !knownStatus(ref.workflows, name) {
			return fail(env, fmt.Errorf("no status named %q in any workflow", name))
		}
		req.Statuses = append(req.Statuses, name)
	}
	return uiCall(ctx, env, req, *asJSON, "")
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
	fs := flag.NewFlagSet("ui select", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, uiUsage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, uiUsage, "ui select takes one task, an id or a part of its title")
	}
	req := control.Request{Op: control.OpSelect, TaskID: positional[0]}
	text := "selected " + positional[0]
	if env.Store != nil {
		t, err := resolveTask(ctx, env.Store, positional[0])
		if err != nil {
			return fail(env, err)
		}
		// Every folder goes along, the interface opens the first one its sidebar has.
		req.TaskID, req.ParentIDs = t.ID, t.ParentIDs
		text = "selected " + t.Title
	}
	return uiCall(ctx, env, req, *asJSON, text)
}

func runUIFolder(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("ui folder", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, uiUsage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, uiUsage, "ui folder takes one folder, project or space, an id or a part of its title, or me")
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
	return uiCall(ctx, env, req, *asJSON, "")
}

func runUIOpen(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("ui open", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, uiUsage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, uiUsage, "ui open takes one screen, tasks, timesheet, changes or settings")
	}
	switch positional[0] {
	case "tasks", "timesheet", "changes", "settings":
	default:
		return usageError(env, fs, uiUsage, fmt.Sprintf("no screen %q, tasks, timesheet, changes or settings", positional[0]))
	}
	return uiCall(ctx, env, control.Request{Op: control.OpScreen, Screen: positional[0]}, *asJSON, "")
}

// runUIPlain serves the commands with no argument of their own, list, board and refresh.
func runUIPlain(ctx context.Context, env Env, args []string, req control.Request, name string) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, uiUsage, args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, uiUsage, name+" takes no arguments")
	}
	return uiCall(ctx, env, req, *asJSON, "")
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
