package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mrcne/wrikery/internal/store"
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
	Pending      bool         `json:"pending"`
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

//nolint:unused // the show command fills it, it has no caller yet
type taskDetailJSON struct {
	taskJSON
	DescriptionText string        `json:"description_text"`
	DescriptionHTML string        `json:"description_html"`
	Comments        []commentJSON `json:"comments"`
}

//nolint:unused // the show command fills it, it has no caller yet
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
	_, row.Pending = ref.pending[t.ID]
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
		if _, ok := ref.pending[t.ID]; ok {
			mark = th.Glyphs.Pending
		}
		ids[i] = t.ID + " " + mark
		names[i] = ref.statusName(t)
		idW = max(idW, utf8.RuneCountInString(ids[i]))
		statusW = max(statusW, utf8.RuneCountInString(names[i])+2)
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

// padRight pads by runes, fmt pads by bytes and would misalign a non-ASCII status name.
func padRight(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
