package wrike

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// Folder is a Wrike folder, project or space root, folders and spaces share this representation.
type Folder struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Scope    string   `json:"scope"`
	ChildIDs []string `json:"childIds"`
	Project  *Project `json:"project"`
	// Space marks the root folder of a space ("Is folder a space", https://developers.wrike.com/api/v4/folders-projects/).
	Space bool `json:"space"`
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

// FolderTree lists every folder the token's account can see, flat, use ChildIDs to build the tree.
// The space flag is an optional field ("Get Folder Tree", https://developers.wrike.com/api/v4/folders-projects/).
// A request that does not name it gets no flag on any folder, and then no root reads as a space.
func (c *Client) FolderTree(ctx context.Context) ([]Folder, error) {
	q := url.Values{}
	q.Set("fields", jsonArray([]string{"space"}))
	var out []Folder
	if _, err := c.do(ctx, http.MethodGet, "/folders", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SpaceFolders lists the folders that live directly under one space.
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
