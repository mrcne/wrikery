package wrike

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Task is a Wrike task, the unit of work assignees, dates and statuses attach to.
type Task struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Status         string     `json:"status"`
	CustomStatusID string     `json:"customStatusId"`
	Importance     string     `json:"importance"`
	Permalink      string     `json:"permalink"`
	ResponsibleIDs []string   `json:"responsibleIds"`
	ParentIDs      []string   `json:"parentIds"`
	Dates          *TaskDates `json:"dates"`
	CreatedDate    time.Time  `json:"createdDate"`
	UpdatedDate    time.Time  `json:"updatedDate"`
}

// TaskDates keeps start and due as the API's zone-less strings.
// Parsing them into time.Time would invent a timezone the API never stated.
type TaskDates struct {
	Type     string `json:"type"`
	Duration int    `json:"duration,omitempty"`
	Start    string `json:"start,omitempty"`
	Due      string `json:"due,omitempty"`
}

// TaskParams narrows a task query, see https://developers.wrike.com/api/v4/tasks/ for the reference.
type TaskParams struct {
	// FolderID selects GET /folders/{folderId}/tasks over the plain /tasks endpoint.
	FolderID string
	// SpaceID selects GET /spaces/{spaceId}/tasks over the plain /tasks endpoint.
	SpaceID string
	// Descendants maps to the descendants parameter, it adds all descendant folders to the search scope.
	// It only applies together with FolderID or SpaceID,
	// without it Wrike returns the tasks placed directly in the folder and skips every subfolder,
	// which is not what "follow a project" means.
	Descendants bool
	// UpdatedAfter maps to the updatedDate parameter's start, a range filter on the last update time.
	UpdatedAfter time.Time
	// Fields names the optional response fields to include, maps to the fields parameter.
	Fields []string
	// PageSize maps to the pageSize parameter, Wrike allows up to 1000 items per page.
	PageSize int
	// PageToken maps to the nextPageToken parameter, it continues a paged query.
	PageToken string
	// Responsibles maps to the responsibles parameter, an assignees filter matching any of the given contact ids.
	// The reference sends it as a JSON array.
	Responsibles []string
}

// TasksPage is one page of a Tasks query, with the token to fetch the next one.
type TasksPage struct {
	Tasks         []Task
	NextPageToken string
}

// Tasks queries tasks from /tasks, or from /folders/{id}/tasks or /spaces/{id}/tasks when FolderID or SpaceID is set.
func (c *Client) Tasks(ctx context.Context, p TaskParams) (TasksPage, error) {
	path := "/tasks"
	switch {
	case p.FolderID != "":
		path = "/folders/" + p.FolderID + "/tasks"
	case p.SpaceID != "":
		path = "/spaces/" + p.SpaceID + "/tasks"
	}
	q := url.Values{}
	if path != "/tasks" && p.Descendants {
		q.Set("descendants", "true")
	}
	if !p.UpdatedAfter.IsZero() {
		q.Set("updatedDate", fmt.Sprintf(`{"start":%q}`, p.UpdatedAfter.UTC().Format("2006-01-02T15:04:05Z")))
	}
	if len(p.Fields) > 0 {
		q.Set("fields", jsonArray(p.Fields))
	}
	if len(p.Responsibles) > 0 {
		q.Set("responsibles", jsonArray(p.Responsibles))
	}
	if p.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(p.PageSize))
	}
	if p.PageToken != "" {
		q.Set("nextPageToken", p.PageToken)
	}
	var out []Task
	next, err := c.do(ctx, http.MethodGet, path, q, nil, &out)
	if err != nil {
		return TasksPage{}, err
	}
	return TasksPage{Tasks: out, NextPageToken: next}, nil
}

// TasksByIDs fetches up to 1000 tasks by id in one request.
func (c *Client) TasksByIDs(ctx context.Context, ids []string, fields []string) ([]Task, error) {
	if len(ids) == 0 {
		return nil, errors.New("wrike: at least one task id is required")
	}
	// The reference for GET /tasks/{taskIds} states "Limit : 1000".
	if len(ids) > 1000 {
		return nil, fmt.Errorf("wrike: at most 1000 task ids per request, got %d", len(ids))
	}
	q := url.Values{}
	if len(fields) > 0 {
		q.Set("fields", jsonArray(fields))
	}
	var out []Task
	if _, err := c.do(ctx, http.MethodGet, "/tasks/"+strings.Join(ids, ","), q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TaskUpdate lists the changes to apply. Zero values mean leave unchanged.
type TaskUpdate struct {
	Title              string
	CustomStatusID     string
	Importance         string // High, Normal or Low
	AddResponsibles    []string
	RemoveResponsibles []string
	Dates              *TaskDates
}

// UpdateTask applies a partial update to one task and returns it as Wrike stored it.
func (c *Client) UpdateTask(ctx context.Context, taskID string, u TaskUpdate) (Task, error) {
	if taskID == "" {
		return Task{}, errors.New("wrike: task id is required")
	}
	form := url.Values{}
	if u.Title != "" {
		form.Set("title", u.Title)
	}
	if u.CustomStatusID != "" {
		form.Set("customStatus", u.CustomStatusID)
	}
	if u.Importance != "" {
		form.Set("importance", u.Importance)
	}
	if len(u.AddResponsibles) > 0 {
		form.Set("addResponsibles", jsonArray(u.AddResponsibles))
	}
	if len(u.RemoveResponsibles) > 0 {
		form.Set("removeResponsibles", jsonArray(u.RemoveResponsibles))
	}
	if u.Dates != nil {
		raw, err := json.Marshal(u.Dates)
		if err != nil {
			return Task{}, err
		}
		form.Set("dates", string(raw))
	}
	var out []Task
	if _, err := c.do(ctx, http.MethodPut, "/tasks/"+taskID, nil, form, &out); err != nil {
		return Task{}, err
	}
	if len(out) == 0 {
		return Task{}, errors.New("wrike: empty response to task update")
	}
	return out[0], nil
}

// jsonArray renders a string slice as the JSON array Wrike expects in query and form parameters.
func jsonArray(items []string) string {
	raw, _ := json.Marshal(items)
	return string(raw)
}
