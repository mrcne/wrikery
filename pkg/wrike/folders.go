package wrike

import (
	"context"
	"errors"
	"net/http"
)

type Folder struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Scope    string   `json:"scope"`
	ChildIDs []string `json:"childIds"`
	Project  *Project `json:"project"`
	// Space marks the root folder of a space ("Is folder a space" in the API reference).
	Space    bool     `json:"space"`
}

// Project is present on folders that are projects. A plain folder has none.
type Project struct {
	AuthorID       string   `json:"authorId"`
	OwnerIDs       []string `json:"ownerIds"`
	Status         string   `json:"status"`
	CustomStatusID string   `json:"customStatusId"`
	StartDate      string   `json:"startDate"`
	EndDate        string   `json:"endDate"`
}

func (c *Client) FolderTree(ctx context.Context) ([]Folder, error) {
	var out []Folder
	if _, err := c.do(ctx, http.MethodGet, "/folders", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SpaceFolders(ctx context.Context, spaceID string) ([]Folder, error) {
	if spaceID == "" {
		return nil, errors.New("wrike: space id is required")
	}
	var out []Folder
	if _, err := c.do(ctx, http.MethodGet, "/spaces/"+spaceID+"/folders", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
