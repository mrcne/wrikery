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

// TaskParams narrows a task query. FolderID and SpaceID each select their own endpoint,
// GET /folders/{folderId}/tasks and GET /spaces/{spaceId}/tasks, the reference names both
// under the descendants parameter. Descendants only applies together with one of them.
// Without it Wrike returns the tasks placed directly in the folder and skips every subfolder,
// which is not what "follow a project" means.
type TaskParams struct {
	FolderID     string
	SpaceID      string
	Descendants  bool
	UpdatedAfter time.Time
	Fields       []string
	PageSize     int
	PageToken    string
	// Responsibles narrows the search to tasks assigned to any of the given contact ids.
	// The reference sends it as a JSON array.
	Responsibles []string
}

type TasksPage struct {
	Tasks         []Task
	NextPageToken string
}

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
	AddResponsibles    []string
	RemoveResponsibles []string
	Dates              *TaskDates
}

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
