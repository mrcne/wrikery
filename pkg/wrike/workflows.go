package wrike

import (
	"context"
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
func (c *Client) Workflows(ctx context.Context) ([]Workflow, error) {
	var out []Workflow
	if _, err := c.do(ctx, http.MethodGet, "/workflows", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
