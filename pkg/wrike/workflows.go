package wrike

import (
	"context"
	"net/http"
)

type Workflow struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Standard       bool           `json:"standard"`
	Hidden         bool           `json:"hidden"`
	CustomStatuses []CustomStatus `json:"customStatuses"`
}

type CustomStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Color        string `json:"color"`
	Group        string `json:"group"`
	StandardName bool   `json:"standardName"`
	Standard     bool   `json:"standard"`
	Hidden       bool   `json:"hidden"`
}

func (c *Client) Workflows(ctx context.Context) ([]Workflow, error) {
	var out []Workflow
	if _, err := c.do(ctx, http.MethodGet, "/workflows", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
