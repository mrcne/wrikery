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

const listUsage = `usage: wrikery task list [--folder F | --me] [--all] [--json]

Lists open tasks, your own unless --folder names a folder, project or space.
`

func runTaskList(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	folder := fs.String("folder", "", "a folder, project or space, by id or by a part of its title")
	me := fs.Bool("me", false, "your own tasks, the default")
	all := fs.Bool("all", false, "include completed and cancelled tasks")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, listUsage, args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, listUsage, "task list takes flags only")
	}
	if *folder != "" && *me {
		return usageError(env, fs, listUsage, "--folder and --me cannot be combined")
	}
	var tasks []store.Task
	var err error
	if *folder != "" {
		fo, rerr := resolveFolder(ctx, env.Store, *folder)
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
	if !*all {
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
	if *asJSON {
		rows := make([]taskJSON, 0, len(tasks))
		for _, t := range tasks {
			rows = append(rows, taskRow(ctx, env, t, &ref))
		}
		return printJSON(env, rows)
	}
	printTaskList(env, tasks, &ref, hidden)
	return exitOK
}

const showUsage = `usage: wrikery task show T [--json]

Prints one task: the header fields, the description and the comments the cache holds.
`

func runTaskShow(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task show", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, showUsage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, showUsage, "task show takes one task, an id or a part of its title")
	}
	t, err := resolveTask(ctx, env.Store, positional[0])
	if err != nil {
		return fail(env, err)
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
	if *asJSON {
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

const statusUsage = `usage: wrikery task status T STATUS [--json]

Moves a task to a status of its own workflow, by name, case does not matter.
`

func runTaskStatus(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, statusUsage, args)
	if done {
		return code
	}
	if len(positional) != 2 {
		return usageError(env, fs, statusUsage, "task status takes a task and a status name")
	}
	t, err := resolveTask(ctx, env.Store, positional[0])
	if err != nil {
		return fail(env, err)
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
	return finishUpdate(ctx, env, t, &ref, store.TaskUpdatePayload{CustomStatusID: cs.ID, Status: cs.Group}, *asJSON, line, "")
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

const createUsage = `usage: wrikery task create --folder F [--importance L] [--description FILE|-] [--json] TITLE...

Creates a task in a folder, project or space. The words after the flags make the title, quotes are optional.
Nobody is assigned, the status is the folder's default on Wrike.
The description is markdown, the text the E editor in the interface writes, from a file or from stdin with -.
Put -- before a title with a word that starts with a dash, everything after it is title.
`

func runTaskCreate(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	folder := fs.String("folder", "", "the folder, project or space, by id or by a part of its title")
	importance := fs.String("importance", "", "High, Normal or Low, Normal when left out")
	description := fs.String("description", "", "a markdown file with the description, - reads it from stdin")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, createUsage, args)
	if done {
		return code
	}
	title := strings.TrimSpace(strings.Join(positional, " "))
	if *folder == "" {
		return usageError(env, fs, createUsage, "task create needs --folder")
	}
	if title == "" {
		return usageError(env, fs, createUsage, "task create needs a title")
	}
	level, err := importanceLevel(*importance)
	if err != nil {
		return usageError(env, fs, createUsage, err.Error())
	}
	var html string
	if *description != "" {
		md, err := readMarkdown(env, *description)
		if err != nil {
			return fail(env, err)
		}
		// A new task has no description to merge against, so the markdown is converted whole.
		var ok bool
		if html, ok = ui.MergeDescription("", md); !ok {
			return usageError(env, fs, createUsage, "the description is empty")
		}
	}
	host, code := preflight(ctx, env)
	if code != exitOK {
		return code
	}
	fo, err := resolveFolder(ctx, env.Store, *folder)
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
	out, reason, task, readOK, err := deliver(ctx, env, host, rowID, store.Task{ID: store.LocalID(rowID), Title: title}, &ref, *asJSON)
	if err != nil {
		return fail(env, err)
	}
	// The write is decided, a Ctrl-C from here on must not stop the reads that report it.
	ctx = context.WithoutCancel(ctx)
	if *asJSON || !readOK {
		return reportWrite(ctx, env, out, reason, *asJSON, readOK, task, &ref, "")
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
