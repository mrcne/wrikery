package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

const editUsage = `usage: wrikery task edit T [--title TITLE] [--importance L] [--description FILE|-] [--json]

Changes the title, the importance or the description of a task, one write for the flags given.
The description is markdown, the text the E editor in the interface writes, from a file or from stdin with -.
It is merged with the description the cache holds: a paragraph left as description_markdown prints it keeps the bytes Wrike stored.
`

func runTaskEdit(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task edit", flag.ContinueOnError)
	title := fs.String("title", "", "the new title")
	importance := fs.String("importance", "", "High, Normal or Low")
	description := fs.String("description", "", "a markdown file with the description, - reads it from stdin")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, editUsage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, editUsage, "task edit takes one task, an id or a part of its title")
	}
	if !given(fs, "title") && !given(fs, "importance") && !given(fs, "description") {
		return usageError(env, fs, editUsage, "task edit needs --title, --importance or --description")
	}
	var p store.TaskUpdatePayload
	if given(fs, "title") {
		p.Title = strings.TrimSpace(*title)
		if p.Title == "" {
			return usageError(env, fs, editUsage, "the title is empty")
		}
	}
	level, err := importanceLevel(*importance)
	if err != nil {
		return usageError(env, fs, editUsage, err.Error())
	}
	p.Importance = level
	var md string
	if *description != "" {
		if md, err = readMarkdown(env, *description); err != nil {
			return fail(env, err)
		}
		if strings.TrimSpace(md) == "" {
			return usageError(env, fs, editUsage, "the description is empty")
		}
	}
	token, host, code := preflight(ctx, env)
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
	line := func(_ context.Context, x store.Task) string { return "[" + x.Importance + "]" }
	suffix := ""
	if md != "" {
		html, changed := ui.MergeDescription(t.Description, md)
		switch {
		case changed:
			p.Description, suffix = html, "  description updated"
		case p.Title == "" && p.Importance == "":
			_, _ = fmt.Fprintln(env.Stderr, "wrikery: description unchanged")
			return reportUnchanged(ctx, env, t, &ref, *asJSON, line(ctx, t))
		default:
			// The other fields still go out, the editor's "description unchanged" toast becomes a note.
			_, _ = fmt.Fprintln(env.Stderr, "wrikery: description unchanged")
		}
	}
	return finishUpdate(ctx, env, token, host, t, &ref, p, *asJSON, line, suffix)
}

// given reports whether the flag was on the command line, so --title "" is refused while a missing --title is no edit.
func given(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

const commentUsage = `usage: wrikery task comment T TEXT... [--json]

Adds a comment to a task as you. The words after the task make the text, quotes are optional.
Put -- before a text with a word that starts with a dash, everything after it is text.
`

func runTaskComment(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task comment", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, commentUsage, args)
	if done {
		return code
	}
	if len(positional) < 2 {
		return usageError(env, fs, commentUsage, "task comment takes a task and the text")
	}
	text := strings.TrimSpace(strings.Join(positional[1:], " "))
	if text == "" {
		return usageError(env, fs, commentUsage, "task comment needs a text")
	}
	// The comment is written as the user, whose id the first sync stores. A cache read, so it comes before the token.
	meID, err := env.Store.GetMeta(ctx, store.MetaKeyMe)
	if errors.Is(err, store.ErrNotFound) {
		return fail(env, errors.New("run wrikery sync first, the cache does not know who you are yet"))
	}
	if err != nil {
		return fail(env, err)
	}
	token, host, code := preflight(ctx, env)
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
	rowID, err := env.Store.Outbox().EnqueueComment(ctx, t.ID, meID, text)
	if err != nil {
		return fail(env, err)
	}
	out, reason, err := send(ctx, env, token, host, rowID)
	if err != nil {
		return fail(env, err)
	}
	// The write is decided, a Ctrl-C from here on must not stop the reads that report it.
	ctx = context.WithoutCancel(ctx)
	task, readOK := t, false
	if out == sent || out == queued {
		task, readOK = readBack(ctx, env, out, t.ID, t, &ref, *asJSON)
	}
	if *asJSON || out == rejected || out == blocked || out == discarded || !readOK {
		return reportWrite(ctx, env, out, reason, *asJSON, readOK, task, &ref, "comment on "+task.Title)
	}
	if out == queued {
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: queued, not on Wrike yet: %s\n", reason)
		_, _ = fmt.Fprintln(env.Stdout, "queued: comment on "+task.Title)
		return exitQueued
	}
	_, _ = fmt.Fprintln(env.Stdout, "commented on "+task.Title)
	return exitOK
}

const assignUsage = `usage: wrikery task assign T P... [--json]
       wrikery task unassign T P... [--json]

Puts people on a task, or takes them off, one write.
P is me, a contact id, or a part of a name that matches exactly one contact in the cache.
`

// runTaskAssign serves assign and unassign, the same command with the other list of the write.
func runTaskAssign(ctx context.Context, env Env, args []string, remove bool) int {
	name := "task assign"
	if remove {
		name = "task unassign"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, assignUsage, args)
	if done {
		return code
	}
	if len(positional) < 2 {
		return usageError(env, fs, assignUsage, name+" takes a task and at least one person")
	}
	token, host, code := preflight(ctx, env)
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
	var ids []string
	for _, arg := range positional[1:] {
		id, err := resolveContact(ctx, env.Store, &ref, arg)
		if err != nil {
			return fail(env, err)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	var keep []string
	for _, id := range ids {
		on := slices.Contains(t.ResponsibleIDs, id)
		switch {
		case on && !remove:
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: %s is already on the task\n", contactName(ref, id))
		case !on && remove:
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: %s is not on the task\n", contactName(ref, id))
		default:
			keep = append(keep, id)
		}
	}
	line := func(_ context.Context, x store.Task) string { return peopleLine(x.ResponsibleIDs, &ref) }
	if len(keep) == 0 {
		return reportUnchanged(ctx, env, t, &ref, *asJSON, line(ctx, t))
	}
	var p store.TaskUpdatePayload
	if remove {
		p.RemoveResponsibles = keep
	} else {
		p.AddResponsibles = keep
	}
	return finishUpdate(ctx, env, token, host, t, &ref, p, *asJSON, line, "")
}

func peopleLine(ids []string, ref *refData) string {
	if len(ids) == 0 {
		return "nobody"
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, contactName(*ref, id))
	}
	return strings.Join(names, ", ")
}

const moveUsage = `usage: wrikery task move T [--to F]... [--from F]... [--json]

Puts a task in folders and takes it out of others, one write.
A task can sit in several folders, so --to alone adds one and keeps the rest, and a move out of a folder names it with --from.
F is a folder, project or space, by id or by a part of its title.
`

// multiFlag collects a flag given more than once, --to A --to B.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ", ") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func runTaskMove(ctx context.Context, env Env, args []string) int {
	fs := flag.NewFlagSet("task move", flag.ContinueOnError)
	var to, from multiFlag
	fs.Var(&to, "to", "a folder to put the task in, may repeat")
	fs.Var(&from, "from", "a folder to take the task out of, may repeat")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code, done := parse(env, fs, moveUsage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, moveUsage, "task move takes one task, an id or a part of its title")
	}
	if len(to)+len(from) == 0 {
		return usageError(env, fs, moveUsage, "task move needs --to or --from")
	}
	token, host, code := preflight(ctx, env)
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
	folders := func(args []string) ([]string, error) {
		var ids []string
		for _, arg := range args {
			fo, err := resolveFolder(ctx, env.Store, arg)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(ids, fo.ID) {
				ids = append(ids, fo.ID)
			}
		}
		return ids, nil
	}
	toIDs, err := folders(to)
	if err != nil {
		return fail(env, err)
	}
	fromIDs, err := folders(from)
	if err != nil {
		return fail(env, err)
	}
	for _, id := range toIDs {
		if slices.Contains(fromIDs, id) {
			return usageError(env, fs, moveUsage, "a folder cannot be both --to and --from")
		}
	}
	var add, remove []string
	for _, id := range toIDs {
		if slices.Contains(t.ParentIDs, id) {
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: already in %s\n", ref.folderTitle(ctx, env.Store, id))
			continue
		}
		add = append(add, id)
	}
	for _, id := range fromIDs {
		if !slices.Contains(t.ParentIDs, id) {
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: not in %s\n", ref.folderTitle(ctx, env.Store, id))
			continue
		}
		remove = append(remove, id)
	}
	line := func(ctx context.Context, x store.Task) string { return foldersLine(ctx, env, x.ParentIDs, &ref) }
	if len(add)+len(remove) == 0 {
		return reportUnchanged(ctx, env, t, &ref, *asJSON, line(ctx, t))
	}
	if len(t.ParentIDs)-len(remove)+len(add) == 0 {
		// Wrike accepts a task with no folder left and puts it in the account root, which no followed scope covers,
		// so it would drop out of the cache at the next sync. The folders box in the interface refuses it the same way.
		return fail(env, errors.New("a task needs a folder, give --to as well"))
	}
	return finishUpdate(ctx, env, token, host, t, &ref, store.TaskUpdatePayload{AddParents: add, RemoveParents: remove}, *asJSON, line, "")
}

func foldersLine(ctx context.Context, env Env, ids []string, ref *refData) string {
	if len(ids) == 0 {
		return "no folder"
	}
	titles := make([]string, 0, len(ids))
	for _, id := range ids {
		titles = append(titles, ref.folderTitle(ctx, env.Store, id))
	}
	return strings.Join(titles, ", ")
}
