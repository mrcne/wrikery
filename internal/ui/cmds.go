package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

func (m Model) loadRef() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		ref := refData{contacts: map[string]store.Contact{}, statuses: map[string]store.CustomStatus{}}
		contacts, err := st.Contacts().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		for _, c := range contacts {
			ref.contacts[c.ID] = c
		}
		ref.workflows, err = st.Workflows().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		for _, w := range ref.workflows {
			for _, cs := range w.CustomStatuses {
				ref.statuses[cs.ID] = cs
			}
		}
		ref.meID, err = st.GetMeta(ctx, store.MetaKeyMe)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return errMsg{err}
		}
		return refLoadedMsg{ref: ref}
	}
}

func (m Model) loadScopes() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		scopes, err := st.Scopes().Followed(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return scopesLoadedMsg{scopes: scopes}
	}
}

// loadPicker reads spaces and the projects right under each space root for the first run checklist and the Follow box.
func (m Model) loadPicker() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		spaces, err := st.Spaces().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		followed, err := st.Scopes().Followed(ctx)
		if err != nil {
			return errMsg{err}
		}
		out := pickerLoadedMsg{spaces: spaces, projects: map[string][]store.Folder{}, followed: followed}
		for _, sp := range spaces {
			children, err := st.Folders().Children(ctx, sp.ID)
			if err != nil {
				return errMsg{err}
			}
			for _, f := range children {
				if f.Project != nil {
					out.projects[sp.ID] = append(out.projects[sp.ID], f)
				}
			}
		}
		return out
	}
}

// saveScopes writes the followed set, asks the engine for a full cycle and reads the set back, all in one command,
// so the error of a failed write is the only thing reported for it.
func (m Model) saveScopes(selected []store.Scope, toast string) tea.Cmd {
	st, hooks := m.opts.Store, m.opts.Hooks
	return func() tea.Msg {
		ctx := context.Background()
		all := append([]store.Scope{{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true}}, selected...)
		if err := st.Scopes().SetFollowed(ctx, all); err != nil {
			return errMsg{err}
		}
		if hooks.Refresh != nil {
			hooks.Refresh()
		}
		scopes, err := st.Scopes().Followed(ctx)
		if err != nil {
			return errMsg{err}
		}
		return scopesSavedMsg{scopes: scopes, toast: toast}
	}
}

// loadHost reads the Wrike host the token probe kept, for the settings screen.
// A read that fails only leaves the row at "not detected yet", the same as a cache without the key.
func (m Model) loadHost() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		host, _ := st.GetMeta(context.Background(), store.MetaKeyHost)
		return hostLoadedMsg{host: host}
	}
}

// loadFlags reads the choices kept in the meta table: whether the sidebar pane was hidden when the app last ran,
// and whether the first one key status move was confirmed.
// It is its own command so the sidebar flag lands before the tree, which takes two store round trips, and the pane is not drawn and then taken away.
// A flag that cannot be read counts as unset, a missing row is the normal case and anything else goes to the log.
func (m Model) loadFlags() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		read := func(key string) bool {
			v, err := st.GetMeta(context.Background(), key)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				slog.Error("ui", "error", err)
			}
			return v == "1"
		}
		return savedFlagsMsg{sidebarHidden: read(store.MetaKeySidebarHidden), moveConfirmed: read(store.MetaKeyStatusMoveConfirmed)}
	}
}

// loadTree builds the sidebar from the followed scopes: My tasks first,
// then each followed space's subtree, then followed projects that are not inside a followed space.
func (m Model) loadTree() tea.Cmd {
	st, meID := m.opts.Store, m.ref.meID
	// The pins are read once. A later load keeps the ones in memory, a key pressed while the load ran would otherwise be undone on screen.
	needPins := !m.sidebar.pinsLoaded
	return func() tea.Msg {
		ctx := context.Background()
		scopes, err := st.Scopes().Followed(ctx)
		if err != nil {
			return errMsg{err}
		}
		statuses := m.ref.statuses
		var nodes []treeNode
		covered := map[string]bool{}
		open := 0
		if meID != "" {
			mine, err := st.Tasks().ListForResponsible(ctx, meID)
			if err != nil {
				return errMsg{err}
			}
			for _, t := range mine {
				if !isDone(t) {
					open++
				}
			}
		}
		nodes = append(nodes, treeNode{id: store.ScopeKindMe, title: "My tasks", kind: nodeMe, count: open})
		addSubtree := func(rootID string, rootKind nodeKind) error {
			folders, err := st.Folders().Subtree(ctx, rootID)
			if err != nil {
				return err
			}
			if len(folders) == 0 {
				// A followed space whose id is not also a folder id has no tree to draw, and dropping it silently looks like a sync bug.
				slog.Warn("followed scope has no folder tree", "scope", rootID)
				return nil
			}
			byID := map[string]store.Folder{}
			for _, f := range folders {
				byID[f.ID] = f
				covered[f.ID] = true
			}
			var add func(f store.Folder, depth int) int
			add = func(f store.Folder, depth int) int {
				idx := len(nodes)
				n := treeNode{id: f.ID, title: f.Title, kind: nodeFolder, depth: depth}
				if depth == 0 {
					n.kind, n.expanded = rootKind, true
				}
				if f.Project != nil {
					n.kind = nodeProject
					n.statusGroup = statuses[f.Project.CustomStatusID].Group
				}
				nodes = append(nodes, n)
				children := make([]store.Folder, 0, len(f.ChildIDs))
				for _, cid := range f.ChildIDs {
					if c, ok := byID[cid]; ok {
						children = append(children, c)
					}
				}
				// By title without regard to case, so a folder named in lower case does not sink under the others,
				// then by the title as written so two that differ only in case keep one order between reloads.
				slices.SortFunc(children, func(a, b store.Folder) int {
					return cmp.Or(strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)), strings.Compare(a.Title, b.Title))
				})
				for _, c := range children {
					// nodes grows while we recurse, so index by idx and never hold a pointer into the slice.
					nodes[idx].children = append(nodes[idx].children, add(c, depth+1))
				}
				return idx
			}
			add(folders[0], 0)
			return nil
		}
		for _, sc := range scopes {
			if sc.Kind == store.ScopeKindSpace {
				// The space root folder id is assumed equal to the space id, the demo data is built that way.
				if err := addSubtree(sc.ID, nodeSpace); err != nil {
					return errMsg{err}
				}
			}
		}
		for _, sc := range scopes {
			if sc.Kind == store.ScopeKindProject && !covered[sc.ID] {
				if err := addSubtree(sc.ID, nodeProject); err != nil {
					return errMsg{err}
				}
			}
		}
		if !needPins {
			return treeLoadedMsg{nodes: nodes}
		}
		pinned, err := st.Pins().List(ctx)
		if err != nil {
			return errMsg{err}
		}
		only, err := st.GetMeta(ctx, store.MetaKeySidebarPinned)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return errMsg{err}
		}
		return treeLoadedMsg{nodes: nodes, pins: &pinState{ids: pinned, only: only == "1"}}
	}
}

func isDone(t store.Task) bool { return t.Status == "Completed" || t.Status == "Cancelled" }

// loadTasks reads a node's rows. selectID names the row the list should land on, when there is one:
// the create just queued, the target of a jump, or the task on screen while its create may land any moment.
// It travels with the load, so an older load that is still in flight cannot use it up.
// A local id the rows no longer hold is looked up in the swap table, so the cursor follows the task and not its old position.
func (m Model) loadTasks(node treeNode, crumb, selectID string, lift bool) tea.Cmd {
	st, meID := m.opts.Store, m.ref.meID
	return func() tea.Msg {
		ctx := context.Background()
		var tasks []store.Task
		var err error
		if node.kind == nodeMe {
			tasks, err = st.Tasks().ListForResponsible(ctx, meID)
		} else {
			tasks, err = st.Tasks().ListInFolder(ctx, node.id)
		}
		if err != nil {
			return errMsg{err}
		}
		states, err := st.Outbox().StatesByEntity(ctx)
		if err != nil {
			return errMsg{err}
		}
		if store.IsLocalID(selectID) && !slices.ContainsFunc(tasks, func(t store.Task) bool { return t.ID == selectID }) {
			real, err := st.Outbox().RealID(ctx, selectID)
			if err != nil {
				// Still queued but not in this node, or discarded: nothing to land on.
				real = ""
			}
			selectID = real
		}
		return tasksLoadedMsg{nodeID: node.id, crumb: crumb, tasks: tasks, states: states, selectID: selectID, lift: lift}
	}
}

// loadTask reads one task and its thread. It runs on every cursor move, so it only reads.
// Recording that a task was opened is markOpened's job.
func (m Model) loadTask(id string) tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		asked := id
		task, err := st.Tasks().Get(ctx, id)
		if errors.Is(err, store.ErrNotFound) && store.IsLocalID(id) {
			// The create landed between the selection and the read, the detail follows the swap.
			if real, rerr := st.Outbox().RealID(ctx, id); rerr == nil {
				id = real
				task, err = st.Tasks().Get(ctx, id)
			}
		}
		if errors.Is(err, store.ErrNotFound) {
			// A sweep or a 404 on its thread removed the task, or its create was discarded.
			return taskGoneMsg{id: asked}
		}
		if err != nil {
			return errMsg{err}
		}
		comments, err := st.Comments().ListForTask(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		logs, err := st.Timelogs().ListForTask(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		states, err := st.Outbox().StatesByEntity(ctx)
		if err != nil {
			return errMsg{err}
		}
		// A task can sit in more than one folder, the detail names them all.
		var crumbs []string
		for _, pid := range task.ParentIDs {
			if f, err := st.Folders().Get(ctx, pid); err == nil {
				crumbs = append(crumbs, f.Title)
			}
		}
		subtasks, err := st.Tasks().Subtasks(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		deps, err := st.Dependencies().ListForTask(ctx, id)
		if err != nil {
			return errMsg{err}
		}
		// The super tasks and the other ends of the edges, as far as the cache has them.
		ends, err := st.Tasks().ByIDs(ctx, store.RelatedIDs(task, deps))
		if err != nil {
			return errMsg{err}
		}
		related := make(map[string]store.Task, len(ends))
		for _, rt := range ends {
			related[rt.ID] = rt
		}
		return taskLoadedMsg{asked: asked, task: task, comments: comments, logs: logs, subtasks: subtasks, deps: deps, related: related, states: states, crumb: strings.Join(crumbs, ", ")}
	}
}

// markOpened records that the reader opened the task, which is what puts it on the syncer's list of threads to refresh.
// It runs on the deliberate open, not on the cursor preview, or walking a folder would queue a refresh for every task in it.
// Nothing on screen waits for it, so a failure is logged and never shown.
func (m Model) markOpened(id string) tea.Cmd {
	st, now := m.opts.Store, m.opts.Now
	return func() tea.Msg {
		if err := st.Tasks().MarkOpened(context.Background(), id, now().UTC().Format(time.RFC3339)); err != nil {
			slog.Warn("mark opened", "task", id, "error", err)
		}
		return nil
	}
}

// loadNotes reads the task's entries for the note field of the log time box, see earlierNotes.
// The notes only feed the hint, so a failed read is logged and the box works without them.
func (m Model) loadNotes(taskID, current string) tea.Cmd {
	st, meID := m.opts.Store, m.ref.meID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		logs, err := st.Timelogs().ListForTask(ctx, taskID)
		if err != nil {
			slog.Warn("earlier notes", "task", taskID, "error", err)
			return nil
		}
		return notesLoadedMsg{taskID: taskID, notes: earlierNotes(logs, meID, current)}
	}
}

// errReachUnavailable stands in for the hook the demo and the tests run without.
var errReachUnavailable = errors.New("not available in the demo")

// reach asks the engine for a task the cache lacks, through the hook, so the interface itself never talks to Wrike.
func (m Model) reach(ref string, origin reachOrigin) tea.Cmd {
	hook := m.opts.Hooks.Reach
	return func() tea.Msg {
		if hook == nil {
			return reachedMsg{ref: ref, origin: origin, err: errReachUnavailable}
		}
		task, err := hook(context.Background(), ref)
		return reachedMsg{ref: ref, origin: origin, task: task, err: err}
	}
}

// reachErrorText words a failed reach for the search row and the toast.
func reachErrorText(ref string, err error) string {
	switch {
	case errors.Is(err, ErrNoSuchTask):
		if r, ok := store.ParseTaskRef(ref); ok && r.Kind == store.RefNumber {
			return "no task with this link on Wrike, or no access"
		}
		return "no task with this id on Wrike, or no access"
	case errors.Is(err, errReachUnavailable):
		return err.Error()
	default:
		return "Wrike could not be reached"
	}
}

// runSearch reads the crumb for each hit's first parent, the same folder title the list pane shows.
func (m Model) runSearch(seq int, query string) tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		ctx := context.Background()
		tasks, err := st.Tasks().Search(ctx, query, 30)
		if err != nil {
			return errMsg{err}
		}
		// A link, a number or an id names one task: first when the cache has it, else the reach row fetches it.
		// The title hits stay either way, a number can be part of a title and a word can have the shape of an id.
		ref := ""
		if r, ok := store.ParseTaskRef(query); ok {
			var hit store.Task
			if r.Kind == store.RefNumber {
				hit, err = st.Tasks().ByPermalinkID(ctx, r.Value)
			} else {
				hit, err = st.Tasks().Get(ctx, r.Value)
			}
			switch {
			case err == nil:
				tasks = append([]store.Task{hit}, slices.DeleteFunc(tasks, func(t store.Task) bool { return t.ID == hit.ID })...)
			case errors.Is(err, store.ErrNotFound):
				ref = strings.TrimSpace(query)
			default:
				return errMsg{err}
			}
		}
		crumbs := map[string]string{}
		for _, t := range tasks {
			// The first of the task's folders the cache holds, the first one of all may be outside the followed scopes.
			for _, id := range t.ParentIDs {
				if f, err := st.Folders().Get(ctx, id); err == nil {
					crumbs[t.ID] = f.Title
					break
				}
			}
		}
		return searchResultsMsg{seq: seq, tasks: tasks, crumbs: crumbs, ref: ref}
	}
}

func (m Model) enqueue(op func(ctx context.Context) error, toast string) tea.Cmd {
	return m.enqueueSelecting(func(ctx context.Context) (string, error) { return "", op(ctx) }, toast)
}

// enqueueSelecting runs one outbox call, wakes the engine and reports back. Every write in the UI goes through here.
// The counts are read right after, so the status bar reflects the new row without waiting for the
// next OutboxChangedMsg from the sync engine.
// The op hands back the id of a row the write added, so the list can land on it, or an empty string.
func (m Model) enqueueSelecting(op func(ctx context.Context) (string, error), toast string) tea.Cmd {
	st, hooks := m.opts.Store, m.opts.Hooks
	return func() tea.Msg {
		ctx := context.Background()
		selectID, err := op(ctx)
		if errors.Is(err, store.ErrNotFound) {
			// The write named a local id whose create was discarded meanwhile, there is no task to write to.
			return toastMsg{text: "nothing queued, the task is gone", isErr: true}
		}
		if err != nil {
			return errMsg{err}
		}
		if hooks.WakeOutbox != nil {
			hooks.WakeOutbox()
		}
		pending, failed, err := st.Outbox().Counts(ctx)
		if err != nil {
			return errMsg{err}
		}
		return writeQueuedMsg{toast: toast, pending: pending, failed: failed, selectID: selectID}
	}
}

// loadCounts reads the outbox pending and failed counts once at startup.
// Without this, a demo store's seeded failures only reach the status bar on the first OutboxChangedMsg,
// which never comes in demo mode, so the bar would start blank instead of showing what was seeded.
func (m Model) loadCounts() tea.Cmd {
	st := m.opts.Store
	return func() tea.Msg {
		pending, failed, err := st.Outbox().Counts(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return OutboxChangedMsg{Pending: pending, Failed: failed}
	}
}

// reloadCurrent re-reads the task and the list a write may have changed the outbox state of.
// selectID is the row the list should land on, the create just queued, or empty to keep the task on screen.
// The zero node has no folder to list, the same guard reload and OutboxChangedMsg use before the first selection.
func (m Model) reloadCurrent(selectID string) tea.Cmd {
	// A create names its new row and wants it on screen, a reload that only follows a swap does not lift anything.
	lift := selectID != ""
	if selectID == "" {
		selectID = m.swapWatch()
	}
	cmds := []tea.Cmd{m.reloadTask()}
	if m.selectedNode.kind != nodeNone {
		cmds = append(cmds, m.loadTasks(m.selectedNode, m.sidebar.crumb(m.selectedNode), selectID, lift))
	}
	return tea.Batch(cmds...)
}

// swapWatch is the selected task's id while its create is still queued, so a reload can follow the swap when it lands.
func (m Model) swapWatch() string {
	if store.IsLocalID(m.selectedTaskID) {
		return m.selectedTaskID
	}
	return ""
}

// loadChanges reads the rows for the changes screen and resolves what they name:
// the task for its current title and the folder a jump needs, and the folder ids a summary shows by title.
// A task gone from the cache keeps the title the row took at enqueue and gets no task id, enter has nowhere to go,
// a time entry whose task was never cached shows the entry id.
// The same task and folder come up many times in a month of writes, each is read once, and the folder titles the tree
// already holds are not read at all.
func (m Model) loadChanges() tea.Cmd {
	st, ref := m.opts.Store, m.ref
	folders := map[string]string{}
	for id, title := range m.list.folders.title {
		folders[id] = title
	}
	return func() tea.Msg {
		ctx := context.Background()
		all, err := st.Outbox().ListChanges(ctx)
		if err != nil {
			return errMsg{err}
		}
		type known struct {
			task store.Task
			ok   bool
		}
		tasks := map[string]known{}
		var rows []changeRow
		for _, r := range all {
			cr := changeRow{row: r, title: r.TaskTitle}
			if r.TaskID == "" {
				cr.title = "(time entry " + r.EntityID + ")"
			} else {
				k, seen := tasks[r.TaskID]
				if !seen {
					if t, err := st.Tasks().Get(ctx, r.TaskID); err == nil {
						k = known{task: t, ok: true}
					}
					tasks[r.TaskID] = k
				}
				if k.ok {
					cr.task, cr.title = k.task, k.task.Title
				} else if cr.title == "" {
					cr.title = "(task " + r.TaskID + ")"
				}
			}
			if r.Kind == store.KindTaskUpdate {
				var p store.TaskUpdatePayload
				_ = json.Unmarshal(r.Payload, &p)
				for _, ids := range [][]string{p.AddParents, p.RemoveParents} {
					for _, id := range ids {
						if _, seen := folders[id]; seen {
							continue
						}
						// A folder the store has no row for keeps its id as the title, and is not asked for again.
						folders[id] = id
						if f, err := st.Folders().Get(ctx, id); err == nil {
							folders[id] = f.Title
						}
					}
				}
			}
			cr.summary, cr.detail = describe(r, ref, folders)
			rows = append(rows, cr)
		}
		return changesLoadedMsg{rows: rows}
	}
}

// enqueueWriteOp runs a retry or discard for the changes screen. ErrNotFound means the engine
// already took the row inflight or somebody else cleared it, which is not a failure worth an
// error toast, just a sign the list is stale and needs another read.
// The counts travel with every answer, the status bar takes them as they are.
func (m Model) enqueueWriteOp(op func(ctx context.Context) error, doneToast string) tea.Cmd {
	st, hooks := m.opts.Store, m.opts.Hooks
	return func() tea.Msg {
		ctx := context.Background()
		err := op(ctx)
		if errors.Is(err, store.ErrNotFound) {
			pending, failed, err := st.Outbox().Counts(ctx)
			if err != nil {
				return errMsg{err}
			}
			return writeQueuedMsg{toast: "already being sent, list refreshed", pending: pending, failed: failed}
		}
		if err != nil {
			return errMsg{err}
		}
		if hooks.WakeOutbox != nil {
			hooks.WakeOutbox()
		}
		pending, failed, err := st.Outbox().Counts(ctx)
		if err != nil {
			return errMsg{err}
		}
		return writeQueuedMsg{toast: doneToast, pending: pending, failed: failed}
	}
}

// loadWeek reads the current user's timelogs for the seven days starting at start,
// along with the title of each task involved and the outbox state of each entry, for the pending marker.
// A zero start means the week the clock is in right now.
func (m Model) loadWeek(start time.Time) tea.Cmd {
	st, meID, ref := m.opts.Store, m.ref.meID, m.ref
	if start.IsZero() {
		start = weekOf(m.opts.Now())
	}
	return func() tea.Msg {
		ctx := context.Background()
		if meID == "" {
			// T pressed before the reference data has loaded: the first sync wrote the id to meta already.
			if id, err := st.GetMeta(ctx, store.MetaKeyMe); err == nil {
				meID = id
			}
		}
		var windowFrom time.Time
		if from, err := st.GetMeta(ctx, store.MetaKeyTimelogFrom); err == nil {
			windowFrom, _ = time.Parse("2006-01-02", from)
		}
		from, to := start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")
		logs, err := st.Timelogs().ListForUser(ctx, meID, from, to)
		if err != nil {
			return errMsg{err}
		}
		// The title names the row and the status draws its glyph.
		// An entry can belong to a task outside every followed scope, that task is not in the store and its row says so.
		titles, statuses := map[string]string{}, map[string]store.CustomStatus{}
		for _, l := range logs {
			if _, done := titles[l.TaskID]; done {
				continue
			}
			titles[l.TaskID] = ""
			if t, err := st.Tasks().Get(ctx, l.TaskID); err == nil {
				titles[l.TaskID], statuses[l.TaskID] = t.Title, ref.statusOf(t)
			}
		}
		states, err := st.Outbox().StatesByEntity(ctx)
		if err != nil {
			return errMsg{err}
		}
		return weekLoadedMsg{weekStart: start, windowFrom: windowFrom, logs: logs, titles: titles, statuses: statuses, states: states}
	}
}

// startSignIn asks the hook for a browser sign in and opens the address.
// A browser that does not open is no error, the address is on the screen for a copy.
func (m Model) startSignIn(attempt int, secret string) tea.Cmd {
	start, open := m.opts.Hooks.SignIn, m.opts.Hooks.OpenURL
	return func() tea.Msg {
		if start == nil {
			return signInStartedMsg{attempt: attempt, err: errors.New("the browser sign in is not available")}
		}
		si, err := start(secret)
		if err != nil {
			return signInStartedMsg{attempt: attempt, err: err}
		}
		if open != nil {
			if err := open(si.URL); err != nil {
				slog.Debug("could not open the browser, the address is on the screen", "error", err)
			}
		}
		return signInStartedMsg{attempt: attempt, signIn: si}
	}
}

// waitSignIn turns the one answer of a browser sign in into a message.
func waitSignIn(attempt int, result <-chan SignInResult) tea.Cmd {
	return func() tea.Msg {
		r := <-result
		return signInResultMsg{attempt: attempt, name: r.Name, app: r.App, err: r.Err}
	}
}

func (m Model) verifyToken(token string) tea.Cmd {
	verify := m.opts.Hooks.VerifyToken
	return func() tea.Msg {
		if verify == nil {
			return tokenVerifiedMsg{err: errors.New("token verification is not available")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		name, err := verify(ctx, token)
		return tokenVerifiedMsg{name: name, err: err}
	}
}
