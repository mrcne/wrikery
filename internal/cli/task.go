package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/mrcne/wrikery/internal/store"
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
		meID, merr := env.Store.GetMeta(ctx, store.MetaKeyMe)
		if errors.Is(merr, store.ErrNotFound) {
			return fail(env, errors.New("run wrikery sync first, the cache does not know who you are yet"))
		}
		if merr != nil {
			return fail(env, merr)
		}
		tasks, err = env.Store.Tasks().ListForResponsible(ctx, meID)
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
	if *asJSON {
		d := taskDetailJSON{
			taskJSON:        taskRow(ctx, env, t, &ref),
			DescriptionText: t.DescriptionPlain,
			DescriptionHTML: t.Description,
			Comments:        make([]commentJSON, 0, len(comments)),
		}
		for _, c := range comments {
			d.Comments = append(d.Comments, commentJSON{ID: c.ID, AuthorID: c.AuthorID, Author: contactName(ref, c.AuthorID), Created: c.CreatedDate, Text: c.Text})
		}
		return printJSON(env, d)
	}
	printTaskShow(ctx, env, t, comments, &ref)
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
	host, code := preflight(ctx, env)
	if code != exitOK {
		return code
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
	before := ref.statusName(t)
	rowID, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, t.ID, store.TaskUpdatePayload{CustomStatusID: cs.ID, Status: cs.Group})
	if err != nil {
		return fail(env, err)
	}
	out, reason, err := send(ctx, env, host, rowID)
	if err != nil {
		return fail(env, err)
	}
	after, err := currentTask(ctx, env, t.ID)
	if err != nil {
		return fail(env, err)
	}
	// The pending marks changed under the drain, read them again for the JSON.
	if ref.pending, err = env.Store.Outbox().StatesByEntity(ctx); err != nil {
		return fail(env, err)
	}
	if !*asJSON {
		_, _ = fmt.Fprintf(env.Stdout, "was: %s  [%s]\n", t.Title, before)
	}
	return reportWrite(ctx, env, out, reason, *asJSON, after, &ref, fmt.Sprintf("%s  [%s]", after.Title, ref.statusName(after)))
}

// currentTask reads the task back after a write. A local id may have been swapped for the server's by the drain.
func currentTask(ctx context.Context, env Env, id string) (store.Task, error) {
	if store.IsLocalID(id) {
		if real, err := env.Store.Outbox().RealID(ctx, id); err == nil {
			id = real
		}
	}
	return env.Store.Tasks().Get(ctx, id)
}
