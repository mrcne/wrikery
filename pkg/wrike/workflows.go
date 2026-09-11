package wrike

import (
	"context"
	"errors"
	"net/http"
)

// Workflow is a Wrike workflow, the set of custom statuses a task can move through.
type Workflow struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Standard       bool           `json:"standard"`
	Hidden         bool           `json:"hidden"`
	CustomStatuses []CustomStatus `json:"customStatuses"`
}

// CustomStatus is one status a task can hold within its workflow, referenced by Task.CustomStatusID.
type CustomStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Color        string `json:"color"`
	Group        string `json:"group"`
	StandardName bool   `json:"standardName"`
	Standard     bool   `json:"standard"`
	Hidden       bool   `json:"hidden"`
}

// Workflows lists the workflows defined for the account, including their custom statuses.
// A workflow that belongs to a space is not among them, see SpaceWorkflows.
func (c *Client) Workflows(ctx context.Context) ([]Workflow, error) {
	var out []Workflow
	if _, err := c.do(ctx, http.MethodGet, "/workflows", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SpaceWorkflows lists the workflows that belong to one space, the ones Workflows leaves out.
// GET /spaces/{id}/workflows is not on the reference page,
// https://developers.wrike.com/api/v4/workflows/ documents the account call only.
// A live account answers it with the same shape as the account call,
// and a task in such a space carries a status from these workflows and no other.
func (c *Client) SpaceWorkflows(ctx context.Context, spaceID string) ([]Workflow, error) {
	if spaceID == "" {
		return nil, errors.New("wrike: space id is required")
	}
	var out []Workflow
	if _, err := c.do(ctx, http.MethodGet, "/spaces/"+spaceID+"/workflows", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
