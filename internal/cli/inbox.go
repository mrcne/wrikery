package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

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
	Short:    "list the comments for you, on followed tasks and mentions, 30 days",
	Notes: "Lists what the inbox screen shows: the comments other people wrote in the last 30 days on the tasks you follow and the ones that mention you, newest first, at most 200.\n" +
		"A row names the task and the author, says mentioned you when it is a mention, and starts with a star while it arrived after the inbox was last seen.\n" +
		"--unread keeps the rows with a star and --limit N stops after N rows.\n" +
		"The command changes nothing unless --seen is given, which marks the rows it printed as seen, the way leaving the inbox screen does, so the unread count of the interface drops.\n" +
		"--seen does not go with --limit, the rows the limit cut off would be marked with the rest.",
	Flags: func() *flag.FlagSet { return new(inboxOpts).flagSet() },
}

type inboxOpts struct {
	json, unread, seen bool
	limit              int
}

func (o *inboxOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("inbox", &o.json)
	fs.BoolVar(&o.unread, "unread", false, "only the rows not seen yet")
	fs.IntVar(&o.limit, "limit", 0, "stop after `N` rows")
	fs.BoolVar(&o.seen, "seen", false, "mark the rows printed as seen")
	return fs
}

// inboxJSON is one comment of the inbox for a script.
type inboxJSON struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
	AuthorID  string `json:"author_id"`
	Author    string `json:"author"`
	Created   string `json:"created"`
	Mentioned bool   `json:"mentioned"` // the comment mentions the user, a row without it is a comment on a followed task
	Unread    bool   `json:"unread"`    // it arrived after the inbox was last seen, the star of the screen
	Text      string `json:"text"`
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
		// The marker is one stamp, everything that arrived up to it counts as seen, so the rows a limit hid would go unread to seen unprinted.
		return usageError(env, fs, usage, "--seen marks every row up to the newest one printed, it does not go with --limit")
	}
	me, err := meID(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	rows, seenTo, err := ui.LoadInbox(ctx, env.Store, me, env.Store.Now())
	if err != nil {
		return fail(env, err)
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	if o.unread {
		kept := make([]store.InboxRow, 0, len(rows))
		for _, r := range rows {
			if r.Unread(seenTo) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	if o.limit > 0 && len(rows) > o.limit {
		rows = rows[:o.limit]
	}
	if o.json {
		out := make([]inboxJSON, 0, len(rows))
		for _, r := range rows {
			out = append(out, inboxJSON{
				ID: r.ID, TaskID: r.TaskID, TaskTitle: r.TaskTitle, AuthorID: r.AuthorID, Author: contactName(ref, r.AuthorID),
				Created: r.CreatedDate, Mentioned: r.Mentioned, Unread: r.Unread(seenTo), Text: r.Text,
			})
		}
		if code := printJSON(env, out); code != exitOK {
			return code
		}
	} else {
		printInbox(env, rows, seenTo, ref, o.unread)
	}
	if !o.seen {
		return exitOK
	}
	// The rows went out first: a marker written before a print that failed would hide comments nobody saw.
	newest := ""
	for _, r := range rows {
		newest = max(newest, r.ArrivedAt)
	}
	if newest <= seenTo {
		return exitOK
	}
	if err := env.Store.Comments().MarkInboxSeen(ctx, newest); err != nil {
		return fail(env, err)
	}
	// The count in the status bar of a running interface is read from the marker, so it is told to read again.
	if env.Socket != "" {
		control.Nudge(ctx, env.Socket, string(syncer.KindComments))
	}
	if !o.json && env.Width > 0 {
		_, _ = fmt.Fprintln(env.Stdout, lipgloss.NewStyle().Foreground(env.Theme.Dim).Render("marked as seen"))
	}
	return exitOK
}

// printInbox writes one line per comment: the star of an unread row, when it was written, the task, the author and the text on one line.
// On a terminal a dim header and a count line frame the rows and a line is cut to the width, in a pipe only the rows go out, whole, as the changes do.
func printInbox(env Env, rows []store.InboxRow, seenTo string, ref refData, unreadOnly bool) {
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
	now := env.Store.Now()
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
