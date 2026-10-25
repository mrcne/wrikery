package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// errUncached says the argument names one task by id, number or link and the cache does not hold it,
// so the command can fetch it from Wrike before it goes on.
// A title fragment never gets it.
type errUncached struct {
	arg string
	ref store.TaskRef
}

func (e *errUncached) Error() string { return fmt.Sprintf("no task %q in the cache", e.arg) }

// resolveOrReach resolves a task argument, and fetches a link, a number or an id the cache lacks from Wrike first.
// This is the one read that talks to Wrike, so the sign in and the host are checked only once the cache misses,
// and a cached task costs none of it.
// The code is exitOK or the failure already printed.
func resolveOrReach(ctx context.Context, env Env, arg string) (store.Task, int) {
	t, err := resolveTask(ctx, env.Store, arg)
	var uncached *errUncached
	if !errors.As(err, &uncached) {
		if err != nil {
			return store.Task{}, fail(env, err)
		}
		return t, exitOK
	}
	if env.Offline {
		return store.Task{}, fail(env, fmt.Errorf("no task %q in the cache, offline mode", arg))
	}
	host, code, unchecked := preflight(ctx, env)
	if unchecked != nil {
		return store.Task{}, fail(env, fmt.Errorf("could not reach Wrike: %w", unchecked))
	}
	if code != exitOK {
		return store.Task{}, code
	}
	eng := syncer.New(env.Client(host), env.Store,
		syncer.Config{PollInterval: env.Config.PollInterval, LockFile: env.LockFile, Host: host, PullEffort: env.Config.Budget.PullEffort()}, slog.Default())
	// The write deadline bounds the fetch too, a Wrike that does not answer must not hold a read for the client's whole retry budget.
	if env.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, env.Deadline)
		defer cancel()
	}
	t, err = eng.Reach(ctx, uncached.ref)
	// A rejected session and a Wrike that does not answer get the words a write prints, a script reads one message for both.
	var apiErr *wrike.APIError
	switch {
	case errors.Is(err, syncer.ErrNoSuchTask) && uncached.ref.Kind == store.RefNumber:
		// A number is never tried as a title, so a task titled with one is still named by more of its title.
		return store.Task{}, fail(env, fmt.Errorf("no task %q on Wrike, or no access, a title that is a number needs more of it", arg))
	case errors.Is(err, syncer.ErrNoSuchTask):
		return store.Task{}, fail(env, fmt.Errorf("no task %q on Wrike, or no access", arg))
	case errors.Is(err, context.DeadlineExceeded):
		_, _ = fmt.Fprintln(env.Stderr, "wrikery: Wrike did not answer in time")
		return store.Task{}, exitError
	case errors.As(err, &apiErr) && (apiErr.IsAuth() || apiErr.IsWrongHost()):
		_, _ = fmt.Fprintln(env.Stderr, "wrikery: sign in rejected, run wrikery to sign in again")
		return store.Task{}, exitError
	case err != nil:
		return store.Task{}, fail(env, err)
	}
	return t, exitOK
}

// candidateLimit caps the rows an ambiguous fragment lists back, the next attempt uses an id anyway.
const candidateLimit = 20

// resolveTask takes an id, a local id, the number or link from the browser or a title fragment that matches exactly one cached task.
// The id is tried first, so a fragment can never shadow a real id, and the match is refused when it is not unique.
func resolveTask(ctx context.Context, st *store.Store, arg string) (store.Task, error) {
	arg = strings.TrimSpace(arg)
	// An empty fragment folds to empty and would match every row.
	if arg == "" {
		return store.Task{}, errors.New("a task is needed, an id or a part of its title")
	}
	t, err := st.Tasks().Get(ctx, arg)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Task{}, err
	}
	ref, isRef := store.ParseTaskRef(arg)
	if !isRef && strings.Contains(arg, "open.htm?id=") {
		return store.Task{}, fmt.Errorf("%q is not a Wrike task link", arg)
	}
	if isRef && ref.Kind == store.RefNumber {
		// A number is never a title fragment, "12" would otherwise pick the one task titled "Release 1.12" when task 12 is not cached.
		t, err := st.Tasks().ByRef(ctx, ref)
		if errors.Is(err, store.ErrNotFound) {
			return store.Task{}, &errUncached{arg: arg, ref: ref}
		}
		return t, err
	}
	hits, err := st.Tasks().FindByTitle(ctx, arg, candidateLimit+1)
	if err != nil {
		return store.Task{}, err
	}
	switch len(hits) {
	case 0:
		// An argument shaped like an id is tried as a fragment first, a word can have that shape, and fetched only when no title has it.
		if isRef {
			return store.Task{}, &errUncached{arg: arg, ref: ref}
		}
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

// exactTitle gives the index of the one title equal to arg, or -1 when there is none or more than one.
// It lets a full title win over longer titles that contain it, typed with or without its accents.
// The titles are compared letter for letter ignoring case first, and folded the way the search folds only when none matched that way,
// or a title and its twin without the accents would refuse each other.
// The hits are cut at candidateLimit+1, so an exact title past that cut is missed, which only happens with a fragment that is too short to have been meant as a title.
func exactTitle(arg string, titles []string) int {
	if i := onlyMatch(titles, func(title string) bool { return strings.EqualFold(strings.TrimSpace(title), arg) }); i >= 0 {
		return i
	}
	folded := store.FoldTitle(arg)
	return onlyMatch(titles, func(title string) bool { return store.FoldTitle(strings.TrimSpace(title)) == folded })
}

// onlyMatch is the index of the one title accept takes, -1 for none or several.
func onlyMatch(titles []string, accept func(string) bool) int {
	found := -1
	for i, title := range titles {
		if !accept(title) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

func resolveFolder(ctx context.Context, st *store.Store, arg string) (store.Folder, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
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

// meID is the user's own contact id, which the first sync stores.
func meID(ctx context.Context, st *store.Store) (string, error) {
	id, err := st.GetMeta(ctx, store.MetaKeyMe)
	if errors.Is(err, store.ErrNotFound) {
		return "", errors.New("run wrikery sync first, the cache does not know who you are yet")
	}
	return id, err
}

// resolveContact takes me, a contact id or a part of a name that matches exactly one person in the cache.
// The name is folded like a title, so "nowak" and "Nowak" are the same person.
// A Wrike contact is a Person or a Group (https://developers.wrike.com/api/v4/contacts/), and the assignee box offers people only,
// so a group is no match here either, by name or by id, nor is a deleted contact: a task is not assigned to someone who left.
func resolveContact(ctx context.Context, st *store.Store, ref *refData, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", errors.New("a person is needed, me, a contact id or a part of a name")
	}
	if strings.EqualFold(arg, "me") {
		return meID(ctx, st)
	}
	person := func(c store.Contact) bool { return !c.Deleted && c.Type != "Group" }
	if c, ok := ref.contacts[arg]; ok && person(c) {
		return c.ID, nil
	}
	var people []store.Contact
	for _, c := range ref.contacts {
		if person(c) {
			people = append(people, c)
		}
	}
	return matchContact(arg, people)
}

// resolveOnTask is the resolver for an unassign: the people on the task come first, by id or by a part of the name,
// whatever their state, so a person who left the account can still be taken off, and an id the cache never saw as well.
// "anna" is one person when one Anna is on the task, however many the cache holds.
// Nobody on the task matching falls back to the cache, so the caller can say that the person is not on the task.
func resolveOnTask(ctx context.Context, st *store.Store, ref *refData, t store.Task, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if strings.EqualFold(arg, "me") {
		return meID(ctx, st)
	}
	var on []store.Contact
	for _, id := range t.ResponsibleIDs {
		if id == arg {
			return id, nil
		}
		if c, ok := ref.contacts[id]; ok {
			on = append(on, c)
		}
	}
	if id, err := matchContact(arg, on); err == nil {
		return id, nil
	}
	return resolveContact(ctx, st, ref, arg)
}

// matchContact picks the one contact whose folded name holds the folded fragment, with the candidates listed when several do.
func matchContact(arg string, contacts []store.Contact) (string, error) {
	folded := store.FoldTitle(arg)
	var hits []store.Contact
	for _, c := range contacts {
		if strings.Contains(store.FoldTitle(c.Name()), folded) {
			hits = append(hits, c)
		}
	}
	// The contacts may come from a map, the candidates are listed in a steady order.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Name() != hits[j].Name() {
			return hits[i].Name() < hits[j].Name()
		}
		return hits[i].ID < hits[j].ID
	})
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no contact matching %q in the cache", arg)
	case 1:
		return hits[0].ID, nil
	}
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.Name()
	}
	if i := exactTitle(arg, names); i >= 0 {
		return hits[i].ID, nil
	}
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, h.ID+"  "+h.Name())
	}
	return "", ambiguous(arg, "contacts", lines)
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
	wf, found := store.WorkflowFor(workflows, currentID)
	if !found {
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
