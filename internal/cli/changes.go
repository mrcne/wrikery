package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

var cmdChanges = Command{
	Name:     "changes",
	Synopsis: []string{"changes [--source WORD] [--limit N]"},
	Short:    "list the changes made through Wrikery, the queue first, then 30 days",
	Notes: "Lists the writes made through Wrikery from this machine, what the changes screen shows: the writes still queued or failed first, in the order they are sent, then the ones sent or discarded in the last 30 days, newest first.\n" +
		"A row names the task, the field with its old and new value, the state and whether the interface or a command made it, and a second line carries the values in full or the error Wrike returned.\n" +
		"In JSON the stored values stand next to the text, with ids where the text has names, before and payload as the outbox keeps them, in Wrike's own field names.\n" +
		"--source keeps the rows of one source, tui for the interface, cli for a command or the word WRIKERY_SOURCE gave an agent's commands, and --limit N stops after N rows, the queue first, so a script reads what it needs.",
	Flags: func() *flag.FlagSet { return new(changesOpts).flagSet() },
}

type changesOpts struct {
	json   bool
	source string
	limit  int
}

func (o *changesOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("changes", &o.json)
	fs.StringVar(&o.source, "source", "", "only the rows of this source `WORD`, tui, cli or an agent's word")
	fs.IntVar(&o.limit, "limit", 0, "stop after `N` rows, the queue first")
	return fs
}

// changeJSON is one write for a script: the text the screen shows and the stored values beside it.
type changeJSON struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Source    string `json:"source"`
	EntityID  string `json:"entity_id"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"` // the cache's title, the title at enqueue once the task left it, empty when never known
	Summary   string `json:"summary"`
	Detail    string `json:"detail"`
	// Before holds the values the write replaced, in the payload's shape, null when it replaced nothing.
	Before   json.RawMessage `json:"before"`
	Payload  json.RawMessage `json:"payload"`
	Error    string          `json:"error"`
	Attempts int             `json:"attempts"`
	Created  string          `json:"created"`
	Done     string          `json:"done"`
}

func runChanges(ctx context.Context, env Env, args []string) int {
	var o changesOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdChanges.usage(), args)
	if done {
		return code
	}
	if len(positional) > 0 {
		return usageError(env, fs, cmdChanges.usage(), "changes takes flags only")
	}
	if o.limit < 0 {
		return usageError(env, fs, cmdChanges.usage(), "--limit takes a count")
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	fields, err := env.Store.CustomFields().List(ctx)
	if err != nil {
		return fail(env, err)
	}
	names := ui.ChangeNames{Statuses: ref.statuses, Contacts: ref.contacts, CustomFields: fields}
	rows, err := ui.LoadChanges(ctx, env.Store, names, ref.folders)
	if err != nil {
		return fail(env, err)
	}
	rows = narrow(rows, o.source, o.limit)
	if o.json {
		out := make([]changeJSON, 0, len(rows))
		for _, c := range rows {
			out = append(out, changeRow(c))
		}
		return printJSON(env, out)
	}
	printChanges(env, rows, o.source != "")
	return exitOK
}

// narrow keeps the rows of one source and then the first n, the queue stays first, an empty source and a zero n keep everything.
// The rows are loaded whole first: the list is a month of this machine's writes, and the description of each row needs its neighbours' titles anyway.
func narrow(rows []ui.Change, source string, n int) []ui.Change {
	if source != "" {
		kept := make([]ui.Change, 0, len(rows))
		for _, c := range rows {
			if c.Row.Source == source {
				kept = append(kept, c)
			}
		}
		rows = kept
	}
	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

func changeRow(c ui.Change) changeJSON {
	title := c.Row.TaskTitle
	if c.Task.ID != "" {
		title = c.Task.Title
	}
	return changeJSON{
		ID: c.Row.ID, Kind: string(c.Row.Kind), State: string(c.Row.State), Source: c.Row.Source,
		EntityID: c.Row.EntityID, TaskID: c.Row.TaskID, TaskTitle: title, Summary: c.Summary, Detail: c.Detail,
		Before: stored(c.Row.Before), Payload: stored(c.Row.Payload),
		Error: c.Row.LastError, Attempts: c.Row.Attempts, Created: c.Row.CreatedAt, Done: c.Row.DoneAt,
	}
}

// stored passes the JSON the outbox keeps through as it is, and an empty column as null.
func stored(b []byte) json.RawMessage {
	if json.Valid(b) {
		return b
	}
	return nil
}

// printChanges writes one line per write, the state with its time, the source, the task and the summary,
// and under it the error of a queued or failed row or the values the summary cut.
// On a terminal a dim header and a count line frame the rows and a line is cut to the width, in a pipe only the rows go out, whole, as the task list does.
func printChanges(env Env, rows []ui.Change, filtered bool) {
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
		case tty && filtered:
			line(muted.Render("No changes from that source."))
		case tty:
			line(muted.Render(fmt.Sprintf("No changes in the last %d days.", store.HistoryDays)))
		}
		return
	}
	now := env.Store.Now()
	type cells struct{ when, source, title string }
	cols := make([]cells, len(rows))
	widths := [3]int{}
	if tty {
		widths = [3]int{len("WHEN"), len("SOURCE"), len("TASK")}
	}
	for i, c := range rows {
		// A command retries nothing itself, so a queued row reads queued whatever its last try, with the error under it,
		// the interface's engine says retrying on its own screen when it is the one trying.
		cols[i] = cells{ui.StateText(c.Row, now, true), c.Row.Source, ansi.Truncate(c.Title, 40, "...")}
		for j, s := range []string{cols[i].when, cols[i].source, cols[i].title} {
			widths[j] = max(widths[j], ansi.StringWidth(s))
		}
	}
	if tty {
		line(dim.Render(fmt.Sprintf("%s  %s  %s  %s", padRight("WHEN", widths[0]), padRight("SOURCE", widths[1]), padRight("TASK", widths[2]), "CHANGE")))
	}
	queued, failed := 0, 0
	for i, c := range rows {
		switch c.Row.State {
		case store.StatePending, store.StateInflight:
			queued++
		case store.StateFailed:
			failed++
		}
		line(fmt.Sprintf("%s  %s  %s  %s", muted.Render(padRight(cols[i].when, widths[0])), dim.Render(padRight(cols[i].source, widths[1])), padRight(cols[i].title, widths[2]), c.Summary))
		second, style := "", muted
		if p := c.Problem(); p != "" {
			second, style = p, lipgloss.NewStyle().Foreground(th.Error)
		} else if c.Detail != "" && !strings.Contains(c.Summary, c.Detail) {
			second = c.Detail
		}
		if second != "" {
			line("    " + style.Render(second))
		}
	}
	if tty {
		count := fmt.Sprintf("%d changes", len(rows))
		if len(rows) == 1 {
			count = "1 change"
		}
		if queued > 0 {
			count += fmt.Sprintf(", %d queued", queued)
		}
		if failed > 0 {
			count += fmt.Sprintf(", %d failed", failed)
		}
		line(dim.Render(count))
	}
}
