package demo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

const (
	// SpaceWork is the space Extend generates and FolderEverything its folder that takes the first bigFolderTasks tasks,
	// so one list is large whatever the size asked for.
	SpaceWork         = "IEAAWORK"
	FolderEverything  = "IEAAGEN1"
	bigFolderTasks    = 5000
	extendProjects    = 60
	foldersPerProject = 5
	extendPeople      = 50
	extendBatch       = 1000
)

// longDescription is a paragraph of twelve sentences and a list, about a kilobyte, the size of a description written with some care.
var longDescription = "<p>" + strings.Repeat("A longer description with a few sentences that wrap in the detail pane, the way a task written with some care reads. ", 12) +
	"</p><ul><li>one</li><li>two</li><li>three</li></ul>"

// Extend adds n generated tasks on top of Seed, to measure the interface and the store at the size of a large account.
// They go into a third space of sixty projects with five folders each, and one more folder takes the first five thousand of them.
// Every tenth task is the user's and followed, the others are on generated people, every second one has dates,
// the descriptions cycle the sample ones with the long one every seventh,
// and every fifth task has two comments by other people, dated over the last four hundred days so that a few fall into the inbox window.
// The comments go in with the store clock two days back, below the seed's seen marker, so they count as seen and the status bar keeps the seed's count.
// The tasks are not marked thread read, no repo call stamps that in bulk and the demo has no engine that reads the stale list,
// so their detail says the thread was not read yet.
// It is for a store Seed just filled, and once, a second call repeats the ids.
func Extend(ctx context.Context, st *store.Store, now time.Time, n int) error {
	if n < 1 {
		return nil
	}
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	stamp := func(offset, hour int) string {
		d := now.AddDate(0, 0, offset)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}

	people := append([]store.Contact{}, contacts...)
	for i := range extendPeople {
		people = append(people, store.Contact{ID: fmt.Sprintf("KUGEN%03d", i), FirstName: fmt.Sprintf("Person %02d", i), LastName: "Generated",
			Type: "Person", PrimaryEmail: fmt.Sprintf("person%02d@example.com", i)})
	}
	if err := st.Contacts().ReplaceAll(ctx, people); err != nil {
		return err
	}
	generated := func(i int) string { return people[len(contacts)+i%extendPeople].ID }

	tree := append([]store.Folder{}, folders...)
	space := store.Folder{ID: SpaceWork, Title: "Work", Scope: "WsFolder", Space: true}
	var leaves []string
	for p := range extendProjects {
		project := store.Folder{ID: fmt.Sprintf("IEAAPRJ%03d", p), Title: fmt.Sprintf("Project %02d", p), Scope: "WsFolder",
			Project: &store.Project{Status: "Green", CustomStatusID: "IEAAST12"}}
		for f := range foldersPerProject {
			id := fmt.Sprintf("IEAAFLD%03d%d", p, f)
			project.ChildIDs = append(project.ChildIDs, id)
			tree = append(tree, store.Folder{ID: id, Title: fmt.Sprintf("Folder %02d.%d", p, f), Scope: "WsFolder"})
			leaves = append(leaves, id)
		}
		space.ChildIDs = append(space.ChildIDs, project.ID)
		tree = append(tree, project)
	}
	space.ChildIDs = append(space.ChildIDs, FolderEverything)
	tree = append(tree, space, store.Folder{ID: FolderEverything, Title: "Everything", Scope: "WsFolder"})
	if err := st.Folders().ReplaceTree(ctx, tree); err != nil {
		return err
	}
	all := append(append([]store.Space{}, spaces...), store.Space{ID: SpaceWork, Title: "Work", AccessType: "Private", DefaultTaskWorkflowID: "IEAAWF02"})
	if err := st.Spaces().ReplaceAll(ctx, all); err != nil {
		return err
	}

	var batch []store.Task
	var comments []store.Comment
	flush := func() error {
		if err := st.Tasks().Upsert(ctx, batch); err != nil {
			return err
		}
		if _, err := st.Comments().UpsertCached(ctx, comments); err != nil {
			return err
		}
		batch, comments = batch[:0], comments[:0]
		return nil
	}
	prevNow := st.Now
	st.Now = func() time.Time { return now.AddDate(0, 0, -2) }
	defer func() { st.Now = prevNow }()
	for i := range n {
		folder := FolderEverything
		if i >= bigFolderTasks {
			folder = leaves[(i-bigFolderTasks)%len(leaves)]
		}
		cs := statusCycle[i%len(statusCycle)]
		desc := descriptions[i%len(descriptions)]
		if i%7 == 0 {
			desc = longDescription
		}
		t := store.Task{
			ID:             fmt.Sprintf("IEAAGEN%06d", i),
			Title:          fmt.Sprintf("%s %s %d", verbs[i%len(verbs)], objects[(i/len(verbs))%len(objects)], i),
			Description:    desc,
			Status:         cs.group,
			CustomStatusID: cs.id,
			Importance:     "Normal",
			Permalink:      fmt.Sprintf("https://www.wrike.com/open.htm?id=%d", 2000000+i),
			ParentIDs:      []string{folder},
			CreatedDate:    stamp(-(i%400)-1, 9),
			UpdatedDate:    stamp(-(i % 90), 8+i%9),
		}
		if i%10 == 0 {
			t.ResponsibleIDs, t.FollowedByMe = []string{MeID}, true
		} else {
			t.ResponsibleIDs = []string{generated(i)}
		}
		if i%2 == 0 {
			t.Dates = &store.TaskDates{Type: "Planned", Start: day(-(i % 30)), Due: day((i % 40) - 10)}
		}
		batch = append(batch, t)
		if i%5 == 0 {
			for j := range 2 {
				comments = append(comments, store.Comment{ID: fmt.Sprintf("IEAAGENC%06d%d", i, j), TaskID: t.ID, AuthorID: generated(i + j + 1),
					Text: "A comment on the task, a sentence or two the way a thread reads.", CreatedDate: stamp(-(i%400)-1, 10+j)})
			}
		}
		if len(batch) == extendBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	sc := store.Scope{ID: SpaceWork, Kind: store.ScopeKindSpace, Title: "Work", Followed: true}
	if err := st.Scopes().Upsert(ctx, sc); err != nil {
		return err
	}
	// ApplyPage with no tasks and a cursor is how a scope gets marked as synced, the way Seed does it.
	return st.Tasks().ApplyPage(ctx, sc.ID, nil, now.UTC().Format(time.RFC3339))
}
