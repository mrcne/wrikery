package syncer

import (
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func taskFromWrike(t wrike.Task) store.Task {
	out := store.Task{
		ID:             t.ID,
		Title:          t.Title,
		Description:    t.Description,
		Status:         t.Status,
		CustomStatusID: t.CustomStatusID,
		Importance:     t.Importance,
		Permalink:      t.Permalink,
		ResponsibleIDs: t.ResponsibleIDs,
		ParentIDs:      t.ParentIDs,
		CreatedDate:    rfc3339(t.CreatedDate),
		UpdatedDate:    rfc3339(t.UpdatedDate),
	}
	if t.Dates != nil {
		out.Dates = &store.TaskDates{
			Type:     t.Dates.Type,
			Duration: t.Dates.Duration,
			Start:    t.Dates.Start,
			Due:      t.Dates.Due,
		}
	}
	return out
}

func tasksFromWrike(in []wrike.Task) []store.Task {
	out := make([]store.Task, len(in))
	for i, t := range in {
		out[i] = taskFromWrike(t)
	}
	return out
}

func folderFromWrike(f wrike.Folder) store.Folder {
	out := store.Folder{ID: f.ID, Title: f.Title, Scope: f.Scope, Space: f.Space, ChildIDs: f.ChildIDs}
	if f.Project != nil {
		out.Project = &store.Project{
			Status:         f.Project.Status,
			CustomStatusID: f.Project.CustomStatusID,
			StartDate:      f.Project.StartDate,
			EndDate:        f.Project.EndDate,
		}
	}
	return out
}

func foldersFromWrike(in []wrike.Folder) []store.Folder {
	out := make([]store.Folder, len(in))
	for i, f := range in {
		out[i] = folderFromWrike(f)
	}
	return out
}

func spacesFromWrike(in []wrike.Space) []store.Space {
	out := make([]store.Space, len(in))
	for i, s := range in {
		out[i] = store.Space{ID: s.ID, Title: s.Title, AccessType: s.AccessType, Archived: s.Archived}
	}
	return out
}

func contactsFromWrike(in []wrike.Contact) []store.Contact {
	out := make([]store.Contact, len(in))
	for i, c := range in {
		out[i] = store.Contact{
			ID:           c.ID,
			FirstName:    c.FirstName,
			LastName:     c.LastName,
			Type:         c.Type,
			PrimaryEmail: c.PrimaryEmail,
			Deleted:      c.Deleted,
			Me:           c.Me,
		}
	}
	return out
}

func workflowsFromWrike(in []wrike.Workflow) []store.Workflow {
	out := make([]store.Workflow, len(in))
	for i, w := range in {
		sw := store.Workflow{ID: w.ID, Name: w.Name, Standard: w.Standard, Hidden: w.Hidden}
		for _, cs := range w.CustomStatuses {
			sw.CustomStatuses = append(sw.CustomStatuses, store.CustomStatus{
				ID:       cs.ID,
				Name:     cs.Name,
				Color:    cs.Color,
				Group:    cs.Group,
				Standard: cs.Standard,
				Hidden:   cs.Hidden,
			})
		}
		out[i] = sw
	}
	return out
}

func commentFromWrike(c wrike.Comment) store.Comment {
	return store.Comment{
		ID:          c.ID,
		TaskID:      c.TaskID,
		AuthorID:    c.AuthorID,
		Text:        c.Text,
		CreatedDate: rfc3339(c.CreatedDate),
	}
}

func commentsFromWrike(in []wrike.Comment) []store.Comment {
	out := make([]store.Comment, len(in))
	for i, c := range in {
		out[i] = commentFromWrike(c)
	}
	return out
}

func timelogFromWrike(t wrike.Timelog) store.Timelog {
	return store.Timelog{
		ID:             t.ID,
		TaskID:         t.TaskID,
		UserID:         t.UserID,
		CategoryID:     t.CategoryID,
		TrackedDate:    t.TrackedDate,
		Comment:        t.Comment,
		Hours:          t.Hours,
		LockStatus:     t.LockStatus,
		ApprovalStatus: t.ApprovalStatus,
		CreatedDate:    rfc3339(t.CreatedDate),
		UpdatedDate:    rfc3339(t.UpdatedDate),
	}
}

func timelogsFromWrike(in []wrike.Timelog) []store.Timelog {
	out := make([]store.Timelog, len(in))
	for i, t := range in {
		out[i] = timelogFromWrike(t)
	}
	return out
}
