package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
)

// candidateLimit caps the rows an ambiguous fragment lists back, the next attempt uses an id anyway.
const candidateLimit = 20

// resolveTask takes an id, a local id or a title fragment that matches exactly one cached task.
// The id is tried first, so a fragment can never shadow a real id, and the match is refused when it is not unique.
func resolveTask(ctx context.Context, st *store.Store, arg string) (store.Task, error) {
	// An empty fragment would become LIKE '%%' and match a cache that holds one row.
	if strings.TrimSpace(arg) == "" {
		return store.Task{}, errors.New("a task is needed, an id or a part of its title")
	}
	t, err := st.Tasks().Get(ctx, arg)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Task{}, err
	}
	hits, err := st.Tasks().FindByTitle(ctx, arg, candidateLimit+1)
	if err != nil {
		return store.Task{}, err
	}
	switch len(hits) {
	case 0:
		return store.Task{}, fmt.Errorf("no task matching %q in the cache, follow its space or run wrikery sync", arg)
	case 1:
		// The search rows come without the description columns, so read the whole task by its id.
		return st.Tasks().Get(ctx, hits[0].ID)
	}
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, h.ID+"  "+h.Title)
	}
	return store.Task{}, ambiguous(arg, "tasks", lines)
}

func resolveFolder(ctx context.Context, st *store.Store, arg string) (store.Folder, error) {
	if strings.TrimSpace(arg) == "" {
		return store.Folder{}, errors.New("a folder is needed, an id or a part of its title")
	}
	fo, err := st.Folders().Get(ctx, arg)
	if err == nil {
		return fo, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Folder{}, err
	}
	hits, err := st.Folders().FindByTitle(ctx, arg, candidateLimit+1)
	if err != nil {
		return store.Folder{}, err
	}
	switch len(hits) {
	case 0:
		return store.Folder{}, fmt.Errorf("no folder matching %q in the cache, follow its space or run wrikery sync", arg)
	case 1:
		return hits[0], nil
	}
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, h.ID+"  "+h.Title)
	}
	return store.Folder{}, ambiguous(arg, "folders", lines)
}

func ambiguous(arg, what string, lines []string) error {
	count := fmt.Sprint(len(lines))
	if len(lines) > candidateLimit {
		count = "more than " + fmt.Sprint(candidateLimit)
		lines = lines[:candidateLimit]
	}
	return fmt.Errorf("%q matches %s %s, be more specific:\n  %s", arg, count, what, strings.Join(lines, "\n  "))
}

// findStatus picks the status called name inside the workflow holding currentID, the status dialog's rule:
// any other workflow would be the wrong offer, a status picked from it moves the task onto that workflow.
func findStatus(workflows []store.Workflow, currentID, name string) (store.CustomStatus, error) {
	var wf *store.Workflow
	for i := range workflows {
		for _, cs := range workflows[i].CustomStatuses {
			if cs.ID == currentID {
				wf = &workflows[i]
				break
			}
		}
		if wf != nil {
			break
		}
	}
	if wf == nil {
		return store.CustomStatus{}, errors.New("no workflow known for this task yet, run wrikery sync")
	}
	want := strings.ToLower(strings.TrimSpace(name))
	var names []string
	for _, cs := range wf.CustomStatuses {
		if cs.Hidden {
			continue
		}
		if strings.ToLower(cs.Name) == want {
			return cs, nil
		}
		names = append(names, cs.Name)
	}
	return store.CustomStatus{}, fmt.Errorf("no status %q in workflow %s, it has: %s", name, wf.Name, strings.Join(names, ", "))
}
