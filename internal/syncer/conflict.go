package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
)

// ConflictPrefix starts the error text of a task edit held because someone changed one of its fields on Wrike since it was queued.
// The command line reads it off the row to tell a held write from one Wrike rejected.
const ConflictPrefix = store.HeldPrefix

// IsConflict tells the error text of a held edit from any other.
func IsConflict(text string) bool { return strings.HasPrefix(text, ConflictPrefix) }

// Conflict is the answer of the check before a task edit: the fields Wrike holds other values for than the edit replaced,
// worded for the changes screen, and the row's before with Wrike's values in their place.
type Conflict struct {
	Fields []string
	Before []byte
}

func (c *Conflict) Error() string { return ConflictPrefix + strings.Join(c.Fields, ", ") }

// conflictOf reads the task from Wrike and compares the fields the edit sets with the values its row remembered at the enqueue.
// A row that remembers nothing, a task the cache did not hold or one whose create had not landed, has nothing to compare,
// and so has an edit of the people or the folders alone: an add or a remove cannot replace somebody else's change.
// The read's own error comes back as it is, the drain treats it like the send's.
func conflictOf(ctx context.Context, c Client, st *store.Store, row store.OutboxRow, p store.TaskUpdatePayload) (*Conflict, error) {
	if len(row.Before) == 0 || (p.Title == "" && p.Description == "" && p.CustomStatusID == "" && p.Importance == "" && p.Dates == nil) {
		return nil, nil
	}
	var before store.TaskUpdatePayload
	if err := json.Unmarshal(row.Before, &before); err != nil {
		return nil, fmt.Errorf("%w %d: before: %w", errCorruptRow, row.ID, err)
	}
	answer, err := c.TasksByIDs(ctx, []string{row.EntityID}, nil)
	if err != nil {
		return nil, err
	}
	var now store.Task
	for _, t := range answer {
		if t.ID == row.EntityID {
			now = taskFromWrike(t)
		}
	}
	if now.ID == "" {
		// The send itself answers for a task Wrike no longer lists.
		return nil, nil
	}
	// A field Wrike already holds at the edit's own value is no conflict: the edit's send landed and its answer was lost,
	// a quit or a timeout mid-send, or someone made the same change, and sending it again changes nothing.
	var fields []string
	if p.CustomStatusID != "" && before.CustomStatusID != now.CustomStatusID && p.CustomStatusID != now.CustomStatusID {
		names, err := statusNames(ctx, st)
		if err != nil {
			return nil, err
		}
		fields = append(fields, fmt.Sprintf("status %q now, was %q", nameOr(names, now.CustomStatusID), nameOr(names, before.CustomStatusID)))
		before.CustomStatusID, before.Status = now.CustomStatusID, now.Status
	}
	if p.Title != "" && before.Title != now.Title && p.Title != now.Title {
		fields = append(fields, fmt.Sprintf("title %q now, was %q", now.Title, before.Title))
		before.Title = now.Title
	}
	if p.Importance != "" && before.Importance != now.Importance && p.Importance != now.Importance {
		fields = append(fields, fmt.Sprintf("importance %s now, was %s", now.Importance, before.Importance))
		before.Importance = now.Importance
	}
	if p.Dates != nil {
		wrike := backlogOr(now.Dates)
		if !sameDates(before.Dates, wrike) && !sameDates(p.Dates, wrike) {
			fields = append(fields, "dates")
			before.Dates = wrike
		}
	}
	// Wrike writes = in a URL as &#61; in its answer and turns paragraphs into line breaks about a minute after a write,
	// so two edits of one description in a row would hold the second over markup nobody changed.
	// The text is what a person changes, a change of markup alone stays last write wins.
	if text := descriptionText(now.Description); p.Description != "" && descriptionText(before.Description) != text && descriptionText(p.Description) != text {
		fields = append(fields, "description")
		before.Description = now.Description
	}
	if len(fields) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(before)
	if err != nil {
		return nil, err
	}
	return &Conflict{Fields: fields, Before: raw}, nil
}

func statusNames(ctx context.Context, st *store.Store) (map[string]string, error) {
	workflows, err := st.Workflows().List(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, wf := range workflows {
		for _, cs := range wf.CustomStatuses {
			names[cs.ID] = cs.Name
		}
	}
	return names, nil
}

func nameOr(names map[string]string, id string) string {
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

// backlogOr is a task's dates block, a task without one is the Backlog an edit writes to clear its dates.
func backlogOr(d *store.TaskDates) *store.TaskDates {
	if d == nil {
		return &store.TaskDates{Type: "Backlog"}
	}
	return d
}

// sameDates compares the type and the day of the start and the due.
// The dates box writes a bare day and Wrike answers it with its working hour, the day followed by T09:00:00,
// and it derives the duration itself, so neither tells a change apart.
func sameDates(a, b *store.TaskDates) bool {
	a, b = backlogOr(a), backlogOr(b)
	day := func(s string) string { return s[:min(len(s), len("2006-01-02"))] }
	return a.Type == b.Type && day(a.Start) == day(b.Start) && day(a.Due) == day(b.Due)
}

// descriptionText is a description reduced to its words: the tags dropped, every entity decoded and the white space collapsed.
func descriptionText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(store.StripHTML(s))), " ")
}
