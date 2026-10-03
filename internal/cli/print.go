package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

// The JSON shapes are the contract a script reads, snake case and stable. Slices are never null.
type taskJSON struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Status       string       `json:"status"`
	StatusGroup  string       `json:"status_group"`
	Importance   string       `json:"importance"`
	Permalink    string       `json:"permalink"`
	Responsibles []personJSON `json:"responsibles"`
	Folders      []folderJSON `json:"folders"`
	Dates        *datesJSON   `json:"dates"`
	Created      string       `json:"created"`
	Updated      string       `json:"updated"`
	Pending      bool         `json:"pending"` // a write is waiting to be sent or being sent
	Failed       bool         `json:"failed"`  // Wrike refused a write, it is listed under sync issues
}

type personJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type folderJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type datesJSON struct {
	Type     string `json:"type"`
	Start    string `json:"start"`
	Due      string `json:"due"`
	Duration int    `json:"duration"`
}

type taskDetailJSON struct {
	taskJSON
	DescriptionText string        `json:"description_text"`
	DescriptionHTML string        `json:"description_html"`
	Comments        []commentJSON `json:"comments"`
}

type commentJSON struct {
	ID       string `json:"id"`
	AuthorID string `json:"author_id"`
	Author   string `json:"author"`
	Created  string `json:"created"`
	Text     string `json:"text"`
}

func taskRow(ctx context.Context, env Env, t store.Task, ref *refData) taskJSON {
	row := taskJSON{
		ID: t.ID, Title: t.Title, Status: ref.statusName(t), StatusGroup: t.Status,
		Importance: t.Importance, Permalink: t.Permalink,
		Responsibles: make([]personJSON, 0, len(t.ResponsibleIDs)),
		Folders:      make([]folderJSON, 0, len(t.ParentIDs)),
		Created:      t.CreatedDate, Updated: t.UpdatedDate,
	}
	state := ref.pending[t.ID]
	row.Pending = state == store.StatePending || state == store.StateInflight
	row.Failed = state == store.StateFailed
	for _, id := range t.ResponsibleIDs {
		row.Responsibles = append(row.Responsibles, personJSON{ID: id, Name: contactName(*ref, id)})
	}
	for _, id := range t.ParentIDs {
		row.Folders = append(row.Folders, folderJSON{ID: id, Title: ref.folderTitle(ctx, env.Store, id)})
	}
	if t.Dates != nil {
		row.Dates = &datesJSON{Type: t.Dates.Type, Start: t.Dates.Start, Due: t.Dates.Due, Duration: t.Dates.Duration}
	}
	return row
}

func printJSON(env Env, v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fail(env, err)
	}
	_, _ = fmt.Fprintln(env.Stdout, string(b))
	return exitOK
}

// printTaskList writes one row per task. On a terminal a dim header and a count line frame the rows,
// in a pipe only the rows go out so cut and grep work on them.
func printTaskList(env Env, tasks []store.Task, ref *refData, hidden int) {
	th := env.Theme
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	idW, statusW := 2, 6
	ids := make([]string, len(tasks))
	names := make([]string, len(tasks))
	for i, t := range tasks {
		mark := " "
		switch ref.pending[t.ID] {
		case store.StatePending, store.StateInflight:
			mark = th.Glyphs.Pending
		case store.StateFailed:
			mark = lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed)
		}
		ids[i] = t.ID + " " + mark
		names[i] = ref.statusName(t)
		idW = max(idW, ansi.StringWidth(ids[i]))
		statusW = max(statusW, ansi.StringWidth(names[i])+2)
	}
	tty := env.Width > 0
	if tty {
		_, _ = fmt.Fprintln(env.Stdout, dim.Render(fmt.Sprintf("%-*s  %-*s  %s", idW, "ID", statusW, "STATUS", "TITLE")))
	}
	for i, t := range tasks {
		id := dim.Render(padRight(ids[i], idW))
		cs := ref.statuses[t.CustomStatusID]
		if cs.ID == "" {
			cs.Group = t.Status
		}
		status := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).
			Render(padRight(th.StatusGlyph(cs.Group)+" "+names[i], statusW))
		_, _ = fmt.Fprintf(env.Stdout, "%s  %s  %s %s\n", id, status, th.ImportanceMark(t.Importance), t.Title)
	}
	if tty {
		count := fmt.Sprintf("%d tasks", len(tasks))
		if len(tasks) == 1 {
			count = "1 task"
		}
		if hidden > 0 {
			count += fmt.Sprintf(", %d done hidden", hidden)
		}
		_, _ = fmt.Fprintln(env.Stdout, dim.Render(count))
	}
}

// padRight pads by terminal cells, fmt pads by bytes and would misalign a non-ASCII or wide status name.
func padRight(s string, width int) string {
	if n := ansi.StringWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// renderMode is the glamour style the description uses, the rule the detail pane applies:
// a terminal that cannot draw the theme glyphs cannot draw glamour's bullets and rules either.
func renderMode(cfg config.UIConfig) string {
	if cfg.ASCII {
		return "ascii"
	}
	return cfg.Theme
}

func printTaskShow(ctx context.Context, env Env, t store.Task, comments []store.Comment, ref *refData) {
	th := env.Theme
	label := lipgloss.NewStyle().Foreground(th.Muted)
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	w := env.Stdout
	_, _ = fmt.Fprintln(w, lipgloss.NewStyle().Bold(true).Render(t.Title))
	line := func(name, value string) {
		if value == "" {
			return
		}
		_, _ = fmt.Fprintf(w, "%s%s\n", label.Render(padRight(name, 12)), value)
	}
	line("id", t.ID)
	cs := ref.statuses[t.CustomStatusID]
	if cs.ID == "" {
		cs.Group = t.Status
	}
	status := lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group) + " " + ref.statusName(t))
	if wf := ref.workflowOf[t.CustomStatusID]; wf != "" {
		status += " " + dim.Render("("+wf+")")
	}
	line("status", status)
	line("importance", t.Importance)
	names := make([]string, 0, len(t.ResponsibleIDs))
	for _, id := range t.ResponsibleIDs {
		names = append(names, contactName(*ref, id))
	}
	line("assignees", strings.Join(names, ", "))
	if t.Dates != nil {
		line("dates", dateRange(t.Dates.Start, t.Dates.Due))
	}
	titles := make([]string, 0, len(t.ParentIDs))
	for _, id := range t.ParentIDs {
		titles = append(titles, ref.folderTitle(ctx, env.Store, id))
	}
	line("folders", strings.Join(titles, ", "))
	line("link", t.Permalink)
	switch ref.pending[t.ID] {
	case store.StatePending, store.StateInflight:
		line("queued", "a write is waiting to be sent")
	case store.StateFailed:
		line("queued", "a write was rejected, see the sync issues screen")
	}

	body := t.DescriptionPlain
	if env.Width > 0 {
		body = ui.RenderDescription(t.Description, t.DescriptionPlain, min(env.Width, 100), renderMode(env.Config.UI))
	}
	if strings.TrimSpace(body) != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, body)
	}
	if len(comments) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, label.Render("comments"))
	}
	accent := lipgloss.NewStyle().Foreground(th.Accent)
	for _, c := range comments {
		_, _ = fmt.Fprintf(w, "%s  %s\n%s\n\n", accent.Render(contactName(*ref, c.AuthorID)), dim.Render(commentTime(c.CreatedDate)), strings.TrimSpace(c.Text))
	}
}

// dateRange writes the arrow only when a task has both ends, so a single date does not trail off into nothing.
func dateRange(start, due string) string {
	switch {
	case start == "":
		return shortDate(due)
	case due == "":
		return shortDate(start)
	}
	return shortDate(start) + " -> " + shortDate(due)
}

// commentTime is the stamp in the local zone to the minute, the rest of the precision says nothing to a reader.
func commentTime(rfc string) string {
	ts, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return rfc
	}
	return ts.Local().Format("2006-01-02 15:04")
}
