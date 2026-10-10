package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	DescriptionText string           `json:"description_text"`
	DescriptionHTML string           `json:"description_html"`
	AttachmentCount int              `json:"attachment_count"`
	SuperTasks      []relatedJSON    `json:"super_tasks"`
	Subtasks        []subtaskJSON    `json:"subtasks"`
	Predecessors    []dependencyJSON `json:"predecessors"`
	Successors      []dependencyJSON `json:"successors"`
	Comments        []commentJSON    `json:"comments"`
}

// relatedJSON names a task the cache may not hold, the title is empty then and the id is all there is.
type relatedJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type subtaskJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	StatusGroup string `json:"status_group"`
}

// dependencyJSON is one edge seen from the shown task, so it names the other end only.
type dependencyJSON struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Relation   string `json:"relation"`
	LagMinutes int    `json:"lag_minutes"`
}

// relations is what the show command reads next to the task itself.
type relations struct {
	subtasks []store.Task
	deps     []store.Dependency
	related  map[string]store.Task // the cached super tasks and dependency ends, a missing id is a task outside the followed spaces
}

func loadRelations(ctx context.Context, st *store.Store, t store.Task) (relations, error) {
	var r relations
	var err error
	if r.subtasks, err = st.Tasks().Subtasks(ctx, t.ID); err != nil {
		return r, err
	}
	if r.deps, err = st.Dependencies().ListForTask(ctx, t.ID); err != nil {
		return r, err
	}
	ends, err := st.Tasks().ByIDs(ctx, store.RelatedIDs(t, r.deps))
	if err != nil {
		return r, err
	}
	r.related = make(map[string]store.Task, len(ends))
	for _, rt := range ends {
		r.related[rt.ID] = rt
	}
	return r, nil
}

// other is the end of the edge that is not the shown task, and whether the task comes after it.
func (r relations) other(dep store.Dependency, taskID string) (id string, predecessor bool) {
	if dep.SuccessorID == taskID {
		return dep.PredecessorID, true
	}
	return dep.SuccessorID, false
}

func (r relations) json(t store.Task, ref *refData) (super []relatedJSON, subs []subtaskJSON, pred, succ []dependencyJSON) {
	super, subs, pred, succ = []relatedJSON{}, []subtaskJSON{}, []dependencyJSON{}, []dependencyJSON{}
	for _, id := range t.SuperTaskIDs {
		super = append(super, relatedJSON{ID: id, Title: r.related[id].Title})
	}
	for _, s := range r.subtasks {
		subs = append(subs, subtaskJSON{ID: s.ID, Title: s.Title, Status: ref.statusName(s), StatusGroup: s.Status})
	}
	for _, dep := range r.deps {
		id, predecessor := r.other(dep, t.ID)
		row := dependencyJSON{ID: id, Title: r.related[id].Title, Relation: dep.RelationType, LagMinutes: dep.LagMinutes}
		if predecessor {
			pred = append(pred, row)
		} else {
			succ = append(succ, row)
		}
	}
	return super, subs, pred, succ
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
	cells := make([]string, len(tasks))
	for i, t := range tasks {
		mark := " "
		switch ref.pending[t.ID] {
		case store.StatePending, store.StateInflight:
			mark = th.Glyphs.Pending
		case store.StateFailed:
			mark = lipgloss.NewStyle().Foreground(th.Error).Render(th.Glyphs.Failed)
		}
		ids[i] = t.ID + " " + mark
		cells[i] = th.StatusGlyph(statusOf(t, ref).Group) + " " + ref.statusName(t)
		idW = max(idW, ansi.StringWidth(ids[i]))
		statusW = max(statusW, ansi.StringWidth(cells[i]))
	}
	tty := env.Width > 0
	if tty {
		_, _ = fmt.Fprintln(env.Stdout, dim.Render(fmt.Sprintf("%-*s  %-*s  %s", idW, "ID", statusW, "STATUS", "TITLE")))
	}
	for i, t := range tasks {
		id := dim.Render(padRight(ids[i], idW))
		status := lipgloss.NewStyle().Foreground(th.StatusColor(statusOf(t, ref))).Render(padRight(cells[i], statusW))
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

// statusOf is the custom status of a task, or a bare one carrying the group when the cache does not know it.
func statusOf(t store.Task, ref *refData) store.CustomStatus {
	cs := ref.statuses[t.CustomStatusID]
	if cs.ID == "" {
		cs.Group = t.Status
	}
	return cs
}

// padRight pads by terminal cells, fmt pads by bytes and would misalign a non-ASCII or wide status name.
func padRight(s string, width int) string {
	if n := ansi.StringWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func printTaskShow(ctx context.Context, env Env, t store.Task, comments []store.Comment, rel relations, ref *refData) {
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
	// A related task gets the glyph the list gives it, an end outside the cache has nothing to draw one from.
	outside := dim.Render("a task outside the followed spaces")
	glyphed := func(rt store.Task) string {
		cs := statusOf(rt, ref)
		return lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group)) + " " + rt.Title
	}
	for _, id := range t.SuperTaskIDs {
		if rt, ok := rel.related[id]; ok {
			line("subtask of", glyphed(rt))
		} else {
			line("subtask of", outside)
		}
	}
	if t.AttachmentCount > 0 {
		line("attachments", strconv.Itoa(t.AttachmentCount))
	}
	for _, s := range rel.subtasks {
		line("subtasks", glyphed(s))
	}
	for _, dep := range rel.deps {
		id, predecessor := rel.other(dep, t.ID)
		name := "successor"
		if predecessor {
			name = "predecessor"
		}
		text := ui.RelationText(dep.RelationType)
		if lag := ui.LagText(dep.LagMinutes); lag != "" {
			text += ", " + lag
		}
		end := outside
		if rt, ok := rel.related[id]; ok {
			end = glyphed(rt)
		}
		line(name, end+" "+dim.Render("("+text+")"))
	}
	line("link", t.Permalink)
	switch ref.pending[t.ID] {
	case store.StatePending, store.StateInflight:
		line("queued", "a write is waiting to be sent")
	case store.StateFailed:
		line("queued", "a write was rejected, see the sync issues screen")
	}

	body := t.DescriptionPlain
	if env.Width > 0 {
		// The detail pane's rule: a terminal that cannot draw the theme glyphs cannot draw glamour's bullets and rules either.
		mode := env.Config.UI.Theme
		if env.Theme.ASCII {
			mode = "ascii"
		}
		body = ui.RenderDescription(t.Description, t.DescriptionPlain, min(env.Width, 100), mode)
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
