package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
)

var cmdInbox = Command{
	Name:     "inbox",
	Synopsis: []string{"inbox [--unread] [--limit N] [--seen]"},
	Short:    fmt.Sprintf("list the comments for you, on followed tasks and mentions, %d days", store.InboxDays),
	Notes: fmt.Sprintf(`Lists what the inbox screen shows: the comments other people wrote in the last %d days on the tasks you follow and the ones that mention you, newest first, the newest %d of them.
A row names the task and the author, says mentioned you when it is a mention, and starts with a star while it arrived after the inbox was last seen.
--unread lists every row with a star instead, the ones the status bar of the interface counts, and --limit N stops after N rows.
The command changes nothing unless --seen is given, which marks the rows it printed as seen, the way leaving the inbox screen does, so the unread count of the interface drops.
The mark covers everything that arrived up to the newest row printed, so --seen marks nothing and exits with 1 when a row not seen yet was not printed, and it does not go with --limit: inbox --unread --seen prints and marks them all.`,
		store.InboxDays, store.InboxLimit),
	Flags: func() *flag.FlagSet { return new(inboxOpts).flagSet() },
}

type inboxOpts struct {
	json, unread, seen bool
	limit              int
}

func (o *inboxOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("inbox", &o.json)
	fs.BoolVar(&o.unread, "unread", false, "every row not seen yet, and only those")
	fs.IntVar(&o.limit, "limit", 0, "stop after `N` rows")
	fs.BoolVar(&o.seen, "seen", false, "mark the rows printed as seen")
	return fs
}

// inboxJSON is one comment of the inbox for a script, the comment as task show prints it with its task and the two marks.
type inboxJSON struct {
	commentJSON
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
	Mentioned bool   `json:"mentioned"` // the comment mentions the user, a row without it is a comment on a followed task
	Unread    bool   `json:"unread"`    // it arrived after the inbox was last seen, the star of the screen
}

// writeNoter passes the bytes on and remembers the first write that failed, for the one command whose next step depends on its output.
type writeNoter struct {
	w   io.Writer
	err error
}

func (n *writeNoter) Write(p []byte) (int, error) {
	written, err := n.w.Write(p)
	if err != nil && n.err == nil {
		n.err = err
	}
	return written, err
}

func runInbox(ctx context.Context, env Env, args []string) int {
	var o inboxOpts
	fs := o.flagSet()
	usage := cmdInbox.usage()
	positional, code, done := parse(env, fs, usage, args)
	if done {
		return code
	}
	switch {
	case len(positional) > 0:
		return usageError(env, fs, usage, "inbox takes flags only")
	case o.limit < 0:
		return usageError(env, fs, usage, "--limit takes a count")
	case o.seen && o.limit > 0:
		// The marker is one stamp, everything that arrived up to it counts as seen, so the rows a limit hid would go from unread to seen unprinted.
		return usageError(env, fs, usage, "--seen marks every row up to the newest one printed, it does not go with --limit")
	}
	me, err := meID(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	now := env.Store.Now()
	rows, seenTo, err := env.Store.Comments().InboxList(ctx, me, now, o.unread, o.limit)
	if err != nil {
		return fail(env, err)
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	out := &writeNoter{w: env.Stdout}
	env.Stdout = out
	if o.json {
		list := make([]inboxJSON, 0, len(rows))
		for _, r := range rows {
			list = append(list, inboxJSON{
				commentJSON: commentJSON{ID: r.ID, AuthorID: r.AuthorID, Author: contactName(ref, r.AuthorID), Created: r.CreatedDate, Text: r.Text},
				TaskID:      r.TaskID, TaskTitle: r.TaskTitle, Mentioned: r.Mentioned, Unread: r.Unread(seenTo),
			})
		}
		if code := printJSON(env, list); code != exitOK {
			return code
		}
	} else {
		printInbox(env, rows, seenTo, ref, now, o.unread)
	}
	if !o.unread && o.limit == 0 && len(rows) == store.InboxLimit {
		// Neither the text in a pipe nor the JSON can say that the list was cut, and the unread rows may sit past the cut.
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: the list stops at %d rows, --unread lists every comment not seen yet\n", store.InboxLimit)
	}
	if !o.seen {
		return exitOK
	}
	return markSeen(ctx, env, me, rows, seenTo, out.err, !o.json && env.Width > 0)
}

// markSeen moves the seen marker to the newest arrival among the rows printed, after they went out.
// The marker is one stamp and every row that arrived up to it counts as seen, so it moves only when each of those rows was printed:
// the whole list is cut by the creation date, and a comment that arrived late sits past the cut and under the marker.
// The counts are the status bar's, the unread rows before the move less the ones still unread after it.
// say adds a line for a person at a terminal, a script reads the exit code.
func markSeen(ctx context.Context, env Env, me string, rows []store.InboxRow, seenTo string, printErr error, say bool) int {
	if printErr != nil {
		return fail(env, fmt.Errorf("the rows could not be written, nothing was marked as seen: %w", printErr))
	}
	newest, printed := "", 0
	for _, r := range rows {
		newest = max(newest, r.ArrivedAt)
		if r.Unread(seenTo) {
			printed++
		}
	}
	if newest <= seenTo {
		return exitOK
	}
	since := store.InboxSince(env.Store.Now())
	before, err := env.Store.Comments().InboxUnread(ctx, me, since, seenTo)
	if err != nil {
		return fail(env, err)
	}
	after, err := env.Store.Comments().InboxUnread(ctx, me, since, newest)
	if err != nil {
		return fail(env, err)
	}
	if hidden := before - after - printed; hidden > 0 {
		return fail(env, fmt.Errorf("%d comments not seen yet were not among the rows printed, nothing was marked as seen, wrikery inbox --unread --seen prints and marks them all", hidden))
	}
	if err := env.Store.Comments().MarkInboxSeen(ctx, newest); err != nil {
		return fail(env, err)
	}
	// The count in the status bar of a running interface is read from the marker, so it is told to read again.
	if env.Socket != "" {
		control.Nudge(ctx, env.Socket, string(syncer.KindComments))
	}
	if say {
		_, _ = fmt.Fprintln(env.Stdout, lipgloss.NewStyle().Foreground(env.Theme.Dim).Render("marked as seen"))
	}
	return exitOK
}

// printInbox writes one line per comment: the star of an unread row, when it was written, the task, the author and the text on one line.
// On a terminal a dim header and a count line frame the rows and a line is cut to the width, in a pipe only the rows go out, whole, as the changes do.
func printInbox(env Env, rows []store.InboxRow, seenTo string, ref refData, now time.Time, unreadOnly bool) {
	th := env.Theme
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	tty := env.Width > 0
	line := func(s string) {
		if tty {
			s = ansi.Truncate(s, env.Width, "")
		}
		_, _ = fmt.Fprintln(env.Stdout, s)
	}
	if len(rows) == 0 {
		switch {
		case tty && unreadOnly:
			line(muted.Render("No unread comments."))
		case tty:
			line(muted.Render(fmt.Sprintf("No comments for you in the last %d days.", store.InboxDays)))
		}
		return
	}
	type cells struct{ when, title string }
	cols := make([]cells, len(rows))
	widths := [2]int{}
	if tty {
		widths = [2]int{len("WHEN"), len("TASK")}
	}
	for i, r := range rows {
		cols[i] = cells{ui.RelTime(r.CreatedDate, now), ansi.Truncate(r.TaskTitle, 40, "...")}
		widths[0] = max(widths[0], ansi.StringWidth(cols[i].when))
		widths[1] = max(widths[1], ansi.StringWidth(cols[i].title))
	}
	if tty {
		line(dim.Render(fmt.Sprintf("  %s  %s  %s", padRight("WHEN", widths[0]), padRight("TASK", widths[1]), "COMMENT")))
	}
	unread := 0
	star := lipgloss.NewStyle().Foreground(th.Accent).Render("*")
	for i, r := range rows {
		mark := " "
		if r.Unread(seenTo) {
			unread++
			mark = star
		}
		sep := ": "
		if r.Mentioned {
			sep = " mentioned you: "
		}
		// The text on one line, its line breaks as spaces, the thread and --json have it as written.
		text := strings.Join(strings.Fields(r.Text), " ")
		line(fmt.Sprintf("%s %s  %s  %s%s%s", mark, muted.Render(padRight(cols[i].when, widths[0])), padRight(cols[i].title, widths[1]), contactName(ref, r.AuthorID), sep, text))
	}
	if tty {
		count := fmt.Sprintf("%d comments", len(rows))
		if len(rows) == 1 {
			count = "1 comment"
		}
		if unread > 0 {
			count += fmt.Sprintf(", %d unread", unread)
		}
		line(dim.Render(count))
	}
}
