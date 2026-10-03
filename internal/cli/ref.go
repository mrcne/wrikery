package cli

import (
	"context"

	"github.com/mrcne/wrikery/internal/store"
)

// refData is what the commands look up by id while printing: statuses, people, folder titles and the pending marks.
// It is loaded once per command from the cache, the same way the TUI loads it once per refresh.
type refData struct {
	statuses   map[string]store.CustomStatus
	workflows  []store.Workflow
	workflowOf map[string]string // custom status id -> workflow name
	contacts   map[string]store.Contact
	pending    map[string]store.OutboxState
	folders    map[string]string // folder id -> title, filled on demand
}

func loadRef(ctx context.Context, st *store.Store) (refData, error) {
	r := refData{
		statuses:   map[string]store.CustomStatus{},
		workflowOf: map[string]string{},
		contacts:   map[string]store.Contact{},
		folders:    map[string]string{},
	}
	wfs, err := st.Workflows().List(ctx)
	if err != nil {
		return r, err
	}
	r.workflows = wfs
	for _, wf := range wfs {
		for _, cs := range wf.CustomStatuses {
			r.statuses[cs.ID] = cs
			r.workflowOf[cs.ID] = wf.Name
		}
	}
	contacts, err := st.Contacts().List(ctx)
	if err != nil {
		return r, err
	}
	for _, c := range contacts {
		r.contacts[c.ID] = c
	}
	r.pending, err = st.Outbox().StatesByEntity(ctx)
	return r, err
}

func (r refData) statusName(t store.Task) string {
	if cs, ok := r.statuses[t.CustomStatusID]; ok {
		return cs.Name
	}
	return t.Status
}

// folderTitle reads a title from the cache once and remembers it, a list names the same few folders many times.
func (r *refData) folderTitle(ctx context.Context, st *store.Store, id string) string {
	if title, ok := r.folders[id]; ok {
		return title
	}
	title := id
	if fo, err := st.Folders().Get(ctx, id); err == nil {
		title = fo.Title
	}
	r.folders[id] = title
	return title
}

func contactName(ref refData, id string) string {
	c, ok := ref.contacts[id]
	if !ok {
		return id
	}
	return c.Name()
}

func isDone(t store.Task) bool {
	return t.Status == "Completed" || t.Status == "Cancelled"
}

// shortDate is the calendar day of a Wrike date or stamp, the first ten characters, nothing for an empty one.
func shortDate(s string) string {
	if len(s) < 10 {
		return s
	}
	return s[:10]
}
