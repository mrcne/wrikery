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
