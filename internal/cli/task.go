package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

var cmdTaskList = Command{
	Name:     "task list",
	Synopsis: []string{"task list [--folder F | --me] [--all]"},
	Short:    "list tasks, your own by default",
	Notes:    "Lists open tasks, your own unless --folder names a folder, project or space.",
	Flags:    func() *flag.FlagSet { return new(listOpts).flagSet() },
}

type listOpts struct {
	folder        string
	me, all, json bool
}

func (o *listOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("task list", &o.json)
	fs.StringVar(&o.folder, "folder", "", "`F`, a folder, project or space, by id or by a part of its title")
	fs.BoolVar(&o.me, "me", false, "your own tasks, the default")
	fs.BoolVar(&o.all, "all", false, "include completed and cancelled tasks")
	return fs
}

func runTaskList(ctx context.Context, env Env, args []string) int {
	var o listOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdTaskList.usage(), args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, cmdTaskList.usage(), "task list takes flags only")
	}
	if o.folder != "" && o.me {
		return usageError(env, fs, cmdTaskList.usage(), "--folder and --me cannot be combined")
	}
	var tasks []store.Task
	var err error
	if o.folder != "" {
		fo, rerr := resolveFolder(ctx, env.Store, o.folder)
		if rerr != nil {
			return fail(env, rerr)
		}
		tasks, err = env.Store.Tasks().ListInFolder(ctx, fo.ID)
	} else {
		me, merr := meID(ctx, env.Store)
		if merr != nil {
			return fail(env, merr)
		}
		tasks, err = env.Store.Tasks().ListForResponsible(ctx, me)
	}
	if err != nil {
		return fail(env, err)
	}
	hidden := 0
	if !o.all {
		kept := tasks[:0]
		for _, t := range tasks {
			if isDone(t) {
				hidden++
				continue
			}
			kept = append(kept, t)
		}
		tasks = kept
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	if o.json {
		rows := make([]taskJSON, 0, len(tasks))
		for _, t := range tasks {
			rows = append(rows, taskRow(ctx, env, t, &ref))
		}
		return printJSON(env, rows)
	}
	printTaskList(env, tasks, &ref, hidden)
	return exitOK
}

var cmdTaskShow = Command{
	Name:     "task show",
	Synopsis: []string{"task show T"},
	Short:    "print one task with its description and comments",
	Notes:    "Prints one task: the header fields, the description and the comments the cache holds.",
	Flags:    func() *flag.FlagSet { return jsonFlagSet("task show", new(bool)) },
}

func runTaskShow(ctx context.Context, env Env, args []string) int {
	var asJSON bool
	fs := jsonFlagSet("task show", &asJSON)
	positional, code, done := parse(env, fs, cmdTaskShow.usage(), args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, cmdTaskShow.usage(), "task show takes one task, an id or a part of its title")
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
	}
	comments, err := env.Store.Comments().ListForTask(ctx, t.ID)
	if err != nil {
		return fail(env, err)
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	rel, err := loadRelations(ctx, env.Store, t)
	if err != nil {
		return fail(env, err)
	}
	if asJSON {
		d := taskDetailJSON{
			taskJSON:            taskRow(ctx, env, t, &ref),
			DescriptionText:     t.DescriptionPlain,
			DescriptionHTML:     t.Description,
			DescriptionMarkdown: ui.EditorText(t.Description),
			AttachmentCount:     t.AttachmentCount,
			Comments:            make([]commentJSON, 0, len(comments)),
		}
		d.SuperTasks, d.Subtasks, d.Predecessors, d.Successors = rel.json(t, &ref)
		for _, c := range comments {
			d.Comments = append(d.Comments, commentJSON{ID: c.ID, AuthorID: c.AuthorID, Author: contactName(ref, c.AuthorID), Created: c.CreatedDate, Text: c.Text})
		}
		return printJSON(env, d)
	}
	printTaskShow(ctx, env, t, comments, rel, &ref)
	return exitOK
}

var cmdTaskStatus = Command{
	Name:     "task status",
	Synopsis: []string{"task status T STATUS"},
	Short:    "move a task to a status of its workflow",
	Notes:    "Moves a task to a status of its own workflow, by name, case does not matter.",
	Flags:    func() *flag.FlagSet { return jsonFlagSet("task status", new(bool)) },
}

func runTaskStatus(ctx context.Context, env Env, args []string) int {
	var asJSON bool
	fs := jsonFlagSet("task status", &asJSON)
	positional, code, done := parse(env, fs, cmdTaskStatus.usage(), args)
	if done {
		return code
	}
	if len(positional) != 2 {
		return usageError(env, fs, cmdTaskStatus.usage(), "task status takes a task and a status name")
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	cs, err := findStatus(ref.workflows, t.CustomStatusID, positional[1])
	if err != nil {
		return fail(env, err)
	}
	line := func(_ context.Context, x store.Task) string { return "[" + ref.statusName(x) + "]" }
	return finishUpdate(ctx, env, t, &ref, store.TaskUpdatePayload{CustomStatusID: cs.ID, Status: cs.Group}, asJSON, line, "")
}

// currentTask reads the task back after a write. A local id may have been swapped for the server's by the drain.
func currentTask(ctx context.Context, env Env, id string) (store.Task, error) {
	if store.IsLocalID(id) {
		real, err := env.Store.Outbox().RealID(ctx, id)
		switch {
		case err == nil:
			id = real
		case !errors.Is(err, store.ErrNotFound):
			return store.Task{}, err
		}
	}
	return env.Store.Tasks().Get(ctx, id)
}

var cmdTaskCreate = Command{
	Name:     "task create",
	Synopsis: []string{"task create --folder F [--importance L] [--description FILE|-] TITLE..."},
	Short:    "create a task, the words make the title",
	Notes: `Creates a task in a folder, project or space. The words after the flags make the title, quotes are optional.
Nobody is assigned, the status is the folder's default on Wrike.
The description is markdown, the text the E editor in the interface writes, from a file or from stdin with -.
Put -- before a title with a word that starts with a dash, everything after it is title.`,
	Flags: func() *flag.FlagSet { return new(createOpts).flagSet() },
}

type createOpts struct {
	folder, importance, description string
	json                            bool
}

func (o *createOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("task create", &o.json)
	fs.StringVar(&o.folder, "folder", "", "the folder, project or space `F` for the task, by id or by a part of its title")
	fs.StringVar(&o.importance, "importance", "", "`L` is High, Normal or Low, Normal when left out")
	fs.StringVar(&o.description, "description", "", "a markdown `FILE` with the description, - reads it from stdin")
	return fs
}

func runTaskCreate(ctx context.Context, env Env, args []string) int {
	var o createOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdTaskCreate.usage(), args)
	if done {
		return code
	}
	title := strings.TrimSpace(strings.Join(positional, " "))
	if o.folder == "" {
		return usageError(env, fs, cmdTaskCreate.usage(), "task create needs --folder")
	}
	if title == "" {
		return usageError(env, fs, cmdTaskCreate.usage(), "task create needs a title")
	}
	level, err := importanceLevel(o.importance)
	if err != nil {
		return usageError(env, fs, cmdTaskCreate.usage(), err.Error())
	}
	var html string
	if o.description != "" {
		md, err := readMarkdown(env, o.description)
		if err != nil {
			return fail(env, err)
		}
		// A new task has no description to merge against, so the markdown is converted whole.
		var ok bool
		if html, ok = ui.MergeDescription("", md); !ok {
			return usageError(env, fs, cmdTaskCreate.usage(), "the description is empty")
		}
	}
	host, code, unchecked := preflight(ctx, env)
	if code != exitOK {
		return code
	}
	fo, err := resolveFolder(ctx, env.Store, o.folder)
	if err != nil {
		return fail(env, err)
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	rowID, err := env.Store.Outbox().EnqueueTaskCreate(ctx, fo.ID, store.TaskCreatePayload{Title: title, Description: html, Importance: level})
	if err != nil {
		return fail(env, err)
	}
	out, reason, task, readOK, err := deliver(ctx, env, host, unchecked, rowID, store.Task{ID: store.LocalID(rowID), Title: title}, &ref, o.json)
	if err != nil {
		return fail(env, err)
	}
	// The write is decided, a Ctrl-C from here on must not stop the reads that report it.
	ctx = context.WithoutCancel(ctx)
	if o.json || !readOK {
		return reportWrite(ctx, env, out, reason, o.json, readOK, task, &ref, "")
	}
	word := "created"
	if out == queued {
		word = "queued"
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: queued, not on Wrike yet: %s\n", reason)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s %s  %s  in %s\n", word, task.ID, task.Title, fo.Title)
	if out == queued {
		return exitQueued
	}
	return exitOK
}

// importanceLevel matches High, Normal or Low without case and returns it as Wrike spells it.
// The three values are the TaskImportance enum of https://developers.wrike.com/reference/puttaskssingle .
// An empty argument is the flag left out, which keeps the task as it is.
func importanceLevel(arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return "", nil
	}
	for _, level := range []string{"High", "Normal", "Low"} {
		if strings.EqualFold(strings.TrimSpace(arg), level) {
			return level, nil
		}
	}
	return "", fmt.Errorf("importance is High, Normal or Low, not %q", arg)
}

// readMarkdown reads the description named by --description, a file or stdin for -.
func readMarkdown(env Env, arg string) (string, error) {
	var b []byte
	var err error
	if arg == "-" {
		b, err = io.ReadAll(env.Stdin)
	} else {
		b, err = os.ReadFile(arg)
	}
	if err != nil {
		return "", fmt.Errorf("reading the description: %w", err)
	}
	return string(b), nil
}
