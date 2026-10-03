package cli

import (
	"context"
	"encoding/json"
	"fmt"

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

//nolint:unused // used in task show command, task 5
type taskDetailJSON struct {
	taskJSON
	DescriptionText string        `json:"description_text"`
	DescriptionHTML string        `json:"description_html"`
	Comments        []commentJSON `json:"comments"`
}

//nolint:unused // used in task show command, task 5
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
