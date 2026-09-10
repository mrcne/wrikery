// Package demo seeds a store with fixture data for --demo runs, README screenshots and the UI tests.
package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

const (
	MeID          = "KUAAAAME"
	SpacePlatform = "IEAAPLAT"
	SpaceMobile   = "IEAAMOBI"
	ProjectAPI    = "IEAAAPI1"
	taskCount     = 60
)

var contacts = []store.Contact{
	{ID: MeID, FirstName: "Ada", LastName: "Nowak", Type: "Person", PrimaryEmail: "ada@example.com", Me: true},
	{ID: "KUAAAAB1", FirstName: "Bartek", LastName: "Lis", Type: "Person", PrimaryEmail: "bartek@example.com"},
	{ID: "KUAAAAC1", FirstName: "Celina", LastName: "Wrona", Type: "Person", PrimaryEmail: "celina@example.com"},
	{ID: "KUAAAAD1", FirstName: "Dawid", LastName: "Mroz", Type: "Person", PrimaryEmail: "dawid@example.com"},
	{ID: "KUAAAAE1", FirstName: "Ewa", LastName: "Zajac", Type: "Person", PrimaryEmail: "ewa@example.com"},
	{ID: "KUAAAAF1", FirstName: "Filip", LastName: "Gone", Type: "Person", Deleted: true},
}

var workflows = []store.Workflow{
	{ID: "IEAAWF01", Name: "Default Workflow", Standard: true, CustomStatuses: []store.CustomStatus{
		{ID: "IEAAST01", Name: "New", Color: "Blue", Group: "Active", Standard: true},
		{ID: "IEAAST02", Name: "In Progress", Color: "Turquoise", Group: "Active"},
		{ID: "IEAAST03", Name: "Completed", Color: "Green", Group: "Completed", Standard: true},
		{ID: "IEAAST04", Name: "On Hold", Color: "Yellow", Group: "Deferred", Standard: true},
		{ID: "IEAAST05", Name: "Cancelled", Color: "Gray", Group: "Cancelled", Standard: true},
	}},
	{ID: "IEAAWF02", Name: "Engineering", CustomStatuses: []store.CustomStatus{
		{ID: "IEAAST11", Name: "Backlog", Color: "Gray", Group: "Active"},
		{ID: "IEAAST12", Name: "In progress", Color: "Blue", Group: "Active"},
		{ID: "IEAAST13", Name: "In review", Color: "Purple", Group: "Active"},
		{ID: "IEAAST14", Name: "Blocked", Color: "Red", Group: "Active"},
		{ID: "IEAAST15", Name: "Done", Color: "Green", Group: "Completed"},
		{ID: "IEAAST16", Name: "Later", Color: "Yellow", Group: "Deferred"},
		{ID: "IEAAST17", Name: "Dropped", Color: "Gray", Group: "Cancelled"},
		{ID: "IEAAST18", Name: "Hidden one", Color: "Gray", Group: "Active", Hidden: true},
	}},
}

// statusCycle cycles tasks through the Engineering workflow so every group shows up in the list.
var statusCycle = []struct{ id, group string }{
	{"IEAAST12", "Active"}, {"IEAAST11", "Active"}, {"IEAAST13", "Active"}, {"IEAAST15", "Completed"},
	{"IEAAST12", "Active"}, {"IEAAST14", "Active"}, {"IEAAST16", "Deferred"}, {"IEAAST15", "Completed"},
	{"IEAAST17", "Cancelled"},
}

var folders = []store.Folder{
	{ID: SpacePlatform, Title: "Platform", Scope: "WsFolder", Space: true, ChildIDs: []string{ProjectAPI, "IEAAWEB1", "IEAADSGN", "IEAAINFR"}},
	{ID: ProjectAPI, Title: "API", Scope: "WsFolder", Project: &store.Project{Status: "Green", CustomStatusID: "IEAAST12"}},
	{ID: "IEAAWEB1", Title: "Web", Scope: "WsFolder", Project: &store.Project{Status: "Green", CustomStatusID: "IEAAST12"}},
	{ID: "IEAADSGN", Title: "Design system", Scope: "WsFolder"},
	{ID: "IEAAINFR", Title: "Infra", Scope: "WsFolder", ChildIDs: []string{"IEAAINF2"}},
	{ID: "IEAAINF2", Title: "On-call", Scope: "WsFolder"},
	{ID: SpaceMobile, Title: "Mobile", Scope: "WsFolder", Space: true, ChildIDs: []string{"IEAAIOS1"}},
	{ID: "IEAAIOS1", Title: "iOS app", Scope: "WsFolder", Project: &store.Project{Status: "Yellow", CustomStatusID: "IEAAST14"}},
}

// taskFolders is where tasks live. Space roots hold none directly, like most real accounts.
var taskFolders = []string{ProjectAPI, "IEAAWEB1", "IEAADSGN", "IEAAINFR", "IEAAINF2", "IEAAIOS1"}

var verbs = []string{"Fix", "Add", "Rotate", "Document", "Refactor", "Remove", "Bump", "Investigate", "Design", "Write tests for"}
var objects = []string{"auth retry loop", "signing keys", "rate limit test", "error codes", "sync cursor", "legacy config loader",
	"dependencies", "flaky CI job", "onboarding flow", "search ranking", "timesheet export", "dark theme"}

var descriptions = []string{
	`<p>The retry loop sends the request again on <b>401</b>, which never helps.</p><ul><li>stop after the first 401</li><li>ask for a new token</li></ul>`,
	`<h2>Steps</h2><ol><li>Generate the new key pair</li><li>Deploy the public key</li><li>Rotate the secret in the vault</li></ol><pre><code>make rotate KEY=prod</code></pre>`,
	`<p>See the <a href="https://developers.wrike.com/">API reference</a> for the error shape.</p><table><tr><th>Code</th><th>Meaning</th></tr><tr><td>429</td><td>rate limit</td></tr><tr><td>401</td><td>bad token</td></tr></table>`,
	`<p>Plain paragraph, nothing fancy. Long enough to wrap in a narrow detail pane so the wrapping code gets exercised too.</p>`,
	``,
}

// Seed fills an empty store. Seeding twice appends more outbox rows, so it is for a fresh store only.
// Everything derives from now except the rows the store stamps itself (optimistic outbox rows, last sync times), which carry the real time.
func Seed(ctx context.Context, st *store.Store, now time.Time) error {
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	stamp := func(offset, hour int) string {
		d := now.AddDate(0, 0, offset)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}

	if err := st.SetMeta(ctx, store.MetaKeyMe, MeID); err != nil {
		return err
	}
	// The demo store never syncs, so it marks the timelog window itself or the timesheet would call every week unsynced.
	windowFrom, _ := store.TimelogWindow(now)
	if err := st.SetMeta(ctx, store.MetaKeyTimelogFrom, windowFrom); err != nil {
		return err
	}
	if err := st.Contacts().ReplaceAll(ctx, contacts); err != nil {
		return err
	}
	if err := st.Workflows().ReplaceAll(ctx, workflows); err != nil {
		return err
	}
	if err := st.Spaces().ReplaceAll(ctx, []store.Space{
		{ID: SpacePlatform, Title: "Platform", AccessType: "Private"},
		{ID: SpaceMobile, Title: "Mobile", AccessType: "Public"},
	}); err != nil {
		return err
	}
	if err := st.Folders().ReplaceTree(ctx, folders); err != nil {
		return err
	}

	var tasks []store.Task
	var mine []store.Task
	for i := 0; i < taskCount; i++ {
		// i picks the folder, row is the task's position inside it.
		// Keying the other cycles on row gives every folder all statuses, every assignee mix and some undated tasks.
		row := i / len(taskFolders)
		cs := statusCycle[row%len(statusCycle)]
		t := store.Task{
			ID:             fmt.Sprintf("IEAATASK%02d", i),
			Title:          verbs[i%len(verbs)] + " " + objects[(i/len(verbs)+i)%len(objects)],
			Description:    descriptions[i%len(descriptions)],
			Status:         cs.group,
			CustomStatusID: cs.id,
			Importance:     "Normal",
			Permalink:      fmt.Sprintf("https://www.wrike.com/open.htm?id=%d", 1200000+i),
			ParentIDs:      []string{taskFolders[i%len(taskFolders)]},
			CreatedDate:    stamp(-(30 + i%20), 9),
			UpdatedDate:    stamp(-(i % 12), 8+i%9),
		}
		if i%13 == 0 {
			t.Importance = "High"
		}
		switch row % 3 {
		case 0:
			t.ResponsibleIDs = []string{MeID}
		case 1:
			t.ResponsibleIDs = []string{"KUAAAAB1"}
		default:
			t.ResponsibleIDs = []string{MeID, "KUAAAAC1"}
		}
		if row%4 != 0 {
			// Every fourth row in a folder has no dates block, the rest are Planned with the due date after the start.
			t.Dates = &store.TaskDates{Type: "Planned", Start: day(-(i % 9)), Due: day(-(i % 9) + 1 + i%11)}
		}
		if row%3 == 0 {
			mine = append(mine, t)
		}
		tasks = append(tasks, t)
	}
	if err := st.Tasks().Upsert(ctx, tasks); err != nil {
		return err
	}

	for i, t := range tasks {
		row := i / len(taskFolders)
		if row%3 != 1 {
			continue
		}
		comments := []store.Comment{
			{ID: fmt.Sprintf("IEAACMT%02da", i), TaskID: t.ID, AuthorID: "KUAAAAB1", Text: "Reproduced on staging, logs attached in the thread.", CreatedDate: stamp(-1, 15)},
			{ID: fmt.Sprintf("IEAACMT%02db", i), TaskID: t.ID, AuthorID: MeID, Text: "Taking this one.", CreatedDate: stamp(-3, 11)},
		}
		if err := st.Comments().ReplaceForTask(ctx, t.ID, comments); err != nil {
			return err
		}
	}

	// Three weeks of my time on my tasks, weekdays only, so the timesheet has totals to show.
	var logs []store.Timelog
	n := 0
	for offset := -21; offset <= 0; offset++ {
		d := now.AddDate(0, 0, offset)
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		for j := 0; j < 2; j++ {
			task := mine[n%len(mine)]
			n++
			logs = append(logs, store.Timelog{
				ID:          fmt.Sprintf("IEAATLOG%03d", len(logs)),
				TaskID:      task.ID,
				UserID:      MeID,
				TrackedDate: d.Format("2006-01-02"),
				Comment:     "",
				Hours:       []float64{1.5, 2, 0.5, 3}[len(logs)%4],
				CreatedDate: stamp(offset, 17),
				UpdatedDate: stamp(offset, 17),
			})
		}
	}
	// One locked entry so the refusal path is visible.
	logs[0].LockStatus = "Locked"
	if err := st.Timelogs().Upsert(ctx, logs); err != nil {
		return err
	}

	cursor := now.UTC().Format(time.RFC3339)
	for _, sc := range []store.Scope{
		{ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true},
		{ID: SpacePlatform, Kind: store.ScopeKindSpace, Title: "Platform", Followed: true},
		{ID: SpaceMobile, Kind: store.ScopeKindSpace, Title: "Mobile", Followed: true},
	} {
		if err := st.Scopes().Upsert(ctx, sc); err != nil {
			return err
		}
		// ApplyPage with no tasks and a cursor is how a scope gets marked as synced.
		if err := st.Tasks().ApplyPage(ctx, sc.ID, nil, cursor); err != nil {
			return err
		}
	}

	// Outbox: one pending comment and two failures so the markers and the issues screen have content.
	if _, err := st.Outbox().EnqueueComment(ctx, tasks[0].ID, MeID, "Queued while offline."); err != nil {
		return err
	}
	// A failed update never rolls back its optimistic write.
	// The target status must stay in the same group as the task's own Status, or the row would fail its own consistency check forever.
	id, err := st.Outbox().EnqueueTaskUpdate(ctx, tasks[1].ID, store.TaskUpdatePayload{CustomStatusID: "IEAAST13"})
	if err != nil {
		return err
	}
	if err := st.Outbox().Fail(ctx, id, "wrike: 404 Task not found"); err != nil {
		return err
	}
	id, err = st.Outbox().EnqueueTimelogCreate(ctx, tasks[2].ID, MeID, store.TimelogCreatePayload{Hours: 1, TrackedDate: day(-8)})
	if err != nil {
		return err
	}
	return st.Outbox().Fail(ctx, id, "wrike: 400 Timesheet is locked")
}
