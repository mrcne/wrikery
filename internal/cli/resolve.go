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

// resolveTask takes an id, a local id, the number or link from the browser or a title fragment that matches exactly one cached task.
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
	if number, isLink := linkNumber(arg); isLink {
		if number == "" {
			return store.Task{}, fmt.Errorf("%q is not a Wrike task link", arg)
		}
		t, err := st.Tasks().ByPermalinkID(ctx, number)
		if errors.Is(err, store.ErrNotFound) {
			return store.Task{}, fmt.Errorf("no task with the link %q in the cache, follow its space or run wrikery sync", arg)
		}
		return t, err
	} else if isDigits(arg) {
		// A number is never a title fragment, "12" would otherwise pick the one task titled "Release 1.12" when task 12 is not cached.
		t, err := st.Tasks().ByPermalinkID(ctx, arg)
		if errors.Is(err, store.ErrNotFound) {
			return store.Task{}, fmt.Errorf("no task with the number %q in the cache, follow its space or run wrikery sync, a title that is a number needs more of it or the id", arg)
		}
		return t, err
	}
	hits, err := st.Tasks().FindByTitle(ctx, arg, candidateLimit+1)
	if err != nil {
		return store.Task{}, err
	}
	switch len(hits) {
	case 0:
		return store.Task{}, fmt.Errorf("no task matching %q in the cache, follow its space or run wrikery sync", arg)
	case 1:
		return hits[0], nil
	}
	titles := make([]string, len(hits))
	for i, h := range hits {
		titles[i] = h.Title
	}
	if i := exactTitle(arg, titles); i >= 0 {
		return hits[i], nil
	}
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, h.ID+"  "+h.Title)
	}
	return store.Task{}, ambiguous(arg, "tasks", lines)
}

// linkNumber reports whether arg is a Wrike link and gives the digits after id=, empty when there are none.
// The host is ignored, a link from app-eu.wrike.com is the same task as the cached www.wrike.com one.
func linkNumber(arg string) (number string, isLink bool) {
	_, rest, found := strings.Cut(arg, "open.htm?id=")
	if !found {
		return "", false
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	return rest[:end], true
}

// exactTitle gives the index of the one title equal to arg ignoring case, or -1 when there is none or more than one.
// It lets a full title win over longer titles that contain it.
// The hits are cut at candidateLimit+1, so an exact title past that cut is missed, which only happens with a fragment that is too short to have been meant as a title.
func exactTitle(arg string, titles []string) int {
	arg = strings.TrimSpace(arg)
	found := -1
	for i, title := range titles {
		if !strings.EqualFold(strings.TrimSpace(title), arg) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	titles := make([]string, len(hits))
	for i, h := range hits {
		titles[i] = h.Title
	}
	if i := exactTitle(arg, titles); i >= 0 {
		return hits[i], nil
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

// statusGuess is the status the optimistic row shows until Wrike answers, the TUI's fallback rule:
// the first visible Active status of the standard workflow. It is not sent, Wrike applies the folder's default.
func statusGuess(workflows []store.Workflow) string {
	for _, wf := range workflows {
		if !wf.Standard {
			continue
		}
		for _, cs := range wf.CustomStatuses {
			if cs.Group == "Active" && !cs.Hidden {
				return cs.ID
			}
		}
	}
	return ""
}
