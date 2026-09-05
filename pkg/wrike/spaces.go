package wrike

import (
	"context"
	"net/http"
)

type Space struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AccessType string `json:"accessType"`
	Archived   bool   `json:"archived"`
}

func (c *Client) Spaces(ctx context.Context) ([]Space, error) {
	var out []Space
	if _, err := c.do(ctx, http.MethodGet, "/spaces", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
