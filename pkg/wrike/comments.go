package wrike

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// Comment is a comment posted on a Wrike task.
type Comment struct {
	ID          string    `json:"id"`
	AuthorID    string    `json:"authorId"`
	Text        string    `json:"text"`
	TaskID      string    `json:"taskId"`
	CreatedDate time.Time `json:"createdDate"`
}

// TaskComments lists the comments on a task, always as plain text.
// The API returns HTML otherwise and rendering happens far away from this package.
func (c *Client) TaskComments(ctx context.Context, taskID string) ([]Comment, error) {
	if taskID == "" {
		return nil, errors.New("wrike: task id is required")
	}
	q := url.Values{}
	q.Set("plainText", "true")
	var out []Comment
	if _, err := c.do(ctx, http.MethodGet, "/tasks/"+taskID+"/comments", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateComment posts a plain text comment on a task and returns the comment as Wrike stored it.
func (c *Client) CreateComment(ctx context.Context, taskID, text string) (Comment, error) {
	if taskID == "" {
		return Comment{}, errors.New("wrike: task id is required")
	}
	if text == "" {
		return Comment{}, errors.New("wrike: comment text is required")
	}
	form := url.Values{}
	form.Set("text", text)
	form.Set("plainText", "true")
	var out []Comment
	if _, err := c.do(ctx, http.MethodPost, "/tasks/"+taskID+"/comments", nil, form, &out); err != nil {
		return Comment{}, err
	}
	if len(out) == 0 {
		return Comment{}, errors.New("wrike: empty response to comment creation")
	}
	return out[0], nil
}
