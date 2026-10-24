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

var cmdTaskEdit = Command{
	Name:     "task edit",
	Synopsis: []string{"task edit T [--title TITLE] [--importance L] [--description FILE|-]"},
	Short:    "change the title, the importance or the description",
	Notes: `Changes the title, the importance or the description of a task, one write for the flags given.
The description is markdown, the text the E editor in the interface writes, from a file or from stdin with -.
It is merged with the description the cache holds: a paragraph left as description_markdown prints it keeps the bytes Wrike stored.`,
	Flags: func() *flag.FlagSet { return new(editOpts).flagSet() },
}

type editOpts struct {
	title, importance, description string
	json                           bool
}

func (o *editOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("task edit", &o.json)
	fs.StringVar(&o.title, "title", "", "the new title")
	fs.StringVar(&o.importance, "importance", "", "High, Normal or Low")
	fs.StringVar(&o.description, "description", "", "a markdown file with the description, - reads it from stdin")
	return fs
}

func runTaskEdit(ctx context.Context, env Env, args []string) int {
	var o editOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdTaskEdit.usage(), args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, cmdTaskEdit.usage(), "task edit takes one task, an id or a part of its title")
	}
	if !given(fs, "title") && !given(fs, "importance") && !given(fs, "description") {
		return usageError(env, fs, cmdTaskEdit.usage(), "task edit needs --title, --importance or --description")
	}
	// A flag given empty, --importance "$LEVEL" with the variable unset, is refused rather than sent as nothing.
	var p store.TaskUpdatePayload
	if given(fs, "title") {
		p.Title = strings.TrimSpace(o.title)
		if p.Title == "" {
			return usageError(env, fs, cmdTaskEdit.usage(), "the title is empty")
		}
	}
	if given(fs, "importance") && strings.TrimSpace(o.importance) == "" {
		return usageError(env, fs, cmdTaskEdit.usage(), "the importance is empty")
	}
	level, err := importanceLevel(o.importance)
	if err != nil {
		return usageError(env, fs, cmdTaskEdit.usage(), err.Error())
	}
	p.Importance = level
	var md string
	if given(fs, "description") {
		if o.description == "" {
			return usageError(env, fs, cmdTaskEdit.usage(), "the description is empty")
		}
		if md, err = readMarkdown(env, o.description); err != nil {
			return fail(env, err)
		}
		if strings.TrimSpace(md) == "" {
			return usageError(env, fs, cmdTaskEdit.usage(), "the description is empty")
		}
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	line := func(_ context.Context, x store.Task) string { return "[" + x.Importance + "]" }
	suffix := ""
	if md != "" {
		if html, changed := ui.MergeDescription(t.Description, md); changed {
			p.Description, suffix = html, "  description updated"
		} else {
			// The other fields still go out, the editor's "description unchanged" toast becomes a note.
			_, _ = fmt.Fprintln(env.Stderr, "wrikery: description unchanged")
		}
	}
	return finishUpdate(ctx, env, t, &ref, p, o.json, line, suffix)
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

var cmdTaskComment = Command{
	Name:     "task comment",
	Synopsis: []string{"task comment T TEXT..."},
	Short:    "add a comment as you",
	Notes: `Adds a comment to a task as you. The words after the task make the text, quotes are optional.
Put -- before a text with a word that starts with a dash, everything after it is text.`,
	Flags: func() *flag.FlagSet { return new(jsonOpts).flagSet("task comment") },
}

func runTaskComment(ctx context.Context, env Env, args []string) int {
	var o jsonOpts
	fs := o.flagSet("task comment")
	positional, code, done := parse(env, fs, cmdTaskComment.usage(), args)
	if done {
		return code
	}
	if len(positional) < 2 {
		return usageError(env, fs, cmdTaskComment.usage(), "task comment takes a task and the text")
	}
	text := strings.TrimSpace(strings.Join(positional[1:], " "))
	if text == "" {
		return usageError(env, fs, cmdTaskComment.usage(), "task comment needs a text")
	}
	// The comment is written as the user. A cache read, so it comes before the token.
	me, err := meID(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	host, code, unchecked := preflight(ctx, env)
	if code != exitOK {
		return code
	}
	rowID, err := env.Store.Outbox().EnqueueComment(ctx, t.ID, me, text)
	if err != nil {
		return fail(env, err)
	}
	out, reason, task, readOK, err := deliver(ctx, env, host, unchecked, rowID, t, &ref, o.json)
	if err != nil {
		return fail(env, err)
	}
	// The write is decided, a Ctrl-C from here on must not stop the reads that report it.
	ctx = context.WithoutCancel(ctx)
	if o.json && readOK {
		return reportComment(ctx, env, out, reason, rowID, task, &ref, text)
	}
	if o.json || !readOK {
		return reportWrite(ctx, env, out, reason, o.json, readOK, task, &ref, "comment on "+task.Title)
	}
	if out == queued {
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: queued, not on Wrike yet: %s\n", reason)
		_, _ = fmt.Fprintln(env.Stdout, "queued: comment on "+task.Title)
		return exitQueued
	}
	_, _ = fmt.Fprintln(env.Stdout, "commented on "+task.Title)
	return exitOK
}

// reportComment is the JSON ending of a comment: the task as every write prints it, plus the comment itself,
// under the server's id once the create landed and under its local id while it is queued, so a script can refer to it.
func reportComment(ctx context.Context, env Env, out outcome, reason string, rowID int64, t store.Task, ref *refData, text string) int {
	if out != sent && out != queued {
		return reportWrite(ctx, env, out, reason, true, true, t, ref, "")
	}
	if out == queued {
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: queued, not on Wrike yet: %s\n", reason)
	}
	id := store.LocalID(rowID)
	if real, err := env.Store.Outbox().RealID(ctx, id); err == nil {
		id = real
	}
	c := commentJSON{ID: id, Text: text}
	if comments, err := env.Store.Comments().ListForTask(ctx, t.ID); err == nil {
		for _, row := range comments {
			if row.ID == id {
				c = commentJSON{ID: row.ID, AuthorID: row.AuthorID, Author: contactName(*ref, row.AuthorID), Created: row.CreatedDate, Text: row.Text}
			}
		}
	}
	type result struct {
		taskJSON
		Comment commentJSON `json:"comment"`
		Sent    bool        `json:"sent"`
	}
	if code := printJSON(env, result{taskJSON: taskRow(ctx, env, t, ref), Comment: c, Sent: out == sent}); code != exitOK {
		return code
	}
	if out == queued {
		return exitQueued
	}
	return exitOK
}

const assignNotes = `Puts people on a task, or takes them off, one write.
P is me, a contact id, or a part of a name that matches exactly one person in the cache, or on the task for an unassign.`

var (
	cmdTaskAssign = Command{
		Name:     "task assign",
		Synopsis: []string{"task assign T P..."},
		Short:    "put people on a task",
		Notes:    assignNotes,
		Flags:    func() *flag.FlagSet { return new(jsonOpts).flagSet("task assign") },
	}
	cmdTaskUnassign = Command{
		Name:     "task unassign",
		Synopsis: []string{"task unassign T P..."},
		Short:    "take people off a task",
		Notes:    assignNotes,
		Flags:    func() *flag.FlagSet { return new(jsonOpts).flagSet("task unassign") },
	}
)

// runTaskAssign serves assign and unassign, the same command with the other list of the write.
func runTaskAssign(ctx context.Context, env Env, args []string, remove bool) int {
	cmd := cmdTaskAssign
	if remove {
		cmd = cmdTaskUnassign
	}
	var o jsonOpts
	fs := o.flagSet(cmd.Name)
	positional, code, done := parse(env, fs, cmd.usage(), args)
	if done {
		return code
	}
	if len(positional) < 2 {
		return usageError(env, fs, cmd.usage(), cmd.Name+" takes a task and at least one person")
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	var ids []string
	for _, arg := range positional[1:] {
		var id string
		if remove {
			id, err = resolveOnTask(ctx, env.Store, &ref, t, arg)
		} else {
			id, err = resolveContact(ctx, env.Store, &ref, arg)
		}
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
	var p store.TaskUpdatePayload
	if remove {
		p.RemoveResponsibles = keep
	} else {
		p.AddResponsibles = keep
	}
	return finishUpdate(ctx, env, t, &ref, p, o.json, line, "")
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

var cmdTaskMove = Command{
	Name:     "task move",
	Synopsis: []string{"task move T [--to F]... [--from F]..."},
	Short:    "put a task in folders, take it out of others",
	Notes: `Puts a task in folders and takes it out of others, one write.
A task can sit in several folders, so --to alone adds one and keeps the rest, and a move out of a folder names it with --from.
F is a folder, project or space, by id or by a part of its title.`,
	Flags: func() *flag.FlagSet { return new(moveOpts).flagSet() },
}

type moveOpts struct {
	to, from multiFlag
	json     bool
}

func (o *moveOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("task move", &o.json)
	fs.Var(&o.to, "to", "a folder to put the task in, may repeat")
	fs.Var(&o.from, "from", "a folder to take the task out of, may repeat")
	return fs
}

// multiFlag collects a flag given more than once, --to A --to B.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ", ") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func runTaskMove(ctx context.Context, env Env, args []string) int {
	var o moveOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdTaskMove.usage(), args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, cmdTaskMove.usage(), "task move takes one task, an id or a part of its title")
	}
	if len(o.to)+len(o.from) == 0 {
		return usageError(env, fs, cmdTaskMove.usage(), "task move needs --to or --from")
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
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
	toIDs, err := folders(o.to)
	if err != nil {
		return fail(env, err)
	}
	fromIDs, err := folders(o.from)
	if err != nil {
		return fail(env, err)
	}
	for _, id := range toIDs {
		if slices.Contains(fromIDs, id) {
			return usageError(env, fs, cmdTaskMove.usage(), "a folder cannot be both --to and --from")
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
		return reportUnchanged(ctx, env, t, &ref, o.json, line(ctx, t))
	}
	var left []string
	for _, id := range t.ParentIDs {
		if !slices.Contains(remove, id) {
			left = append(left, id)
		}
	}
	left = append(left, add...)
	if len(left) == 0 {
		// Wrike accepts a task with no folder left and puts it in the account root, which no followed scope covers,
		// so it would drop out of the cache at the next sync. The folders box in the interface refuses it the same way.
		return fail(env, errors.New("a task needs a folder, give --to as well"))
	}
	// The cache holds the whole folder tree, so --to can name a folder nobody follows, which the box cannot offer.
	// A move to another team's space may be meant, so it goes out with a note instead of a refusal.
	if followed, err := followedFolders(ctx, env.Store); err == nil && len(followed) > 0 && !slices.ContainsFunc(left, func(id string) bool { return followed[id] }) {
		_, _ = fmt.Fprintln(env.Stderr, "wrikery: no followed folder is left on the task, it leaves the cache at the next sync")
	}
	return finishUpdate(ctx, env, t, &ref, store.TaskUpdatePayload{AddParents: add, RemoveParents: remove}, o.json, line, "")
}

// followedFolders is every folder under a followed space or project, the tree the interface shows.
func followedFolders(ctx context.Context, st *store.Store) (map[string]bool, error) {
	scopes, err := st.Scopes().Followed(ctx)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, sc := range scopes {
		if sc.Kind != store.ScopeKindSpace && sc.Kind != store.ScopeKindProject {
			continue
		}
		sub, err := st.Folders().Subtree(ctx, sc.ID)
		if err != nil {
			return nil, err
		}
		ids[sc.ID] = true
		for _, fo := range sub {
			ids[fo.ID] = true
		}
	}
	return ids, nil
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
