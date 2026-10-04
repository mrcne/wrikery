package wrike

import (
	"context"
	"net/http"
)

// Space is a Wrike space, the top level container folders and projects live under.
type Space struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AccessType string `json:"accessType"`
	Archived   bool   `json:"archived"`
	// DefaultTaskWorkflowID is the workflow a task created anywhere in the space starts in.
	// GET /spaces returns it without a fields parameter, checked against a live account on 2026-10-04.
	DefaultTaskWorkflowID string `json:"defaultTaskWorkflowId"`
}

// Spaces lists the spaces the token can see.
func (c *Client) Spaces(ctx context.Context) ([]Space, error) {
	var out []Space
	if _, err := c.do(ctx, http.MethodGet, "/spaces", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
