package wrike

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Comment is a comment posted on a Wrike task.
// A comment carries taskId or folderId, one of the two, so an empty TaskID means a comment on a folder or project.
type Comment struct {
	ID          string    `json:"id"`
	AuthorID    string    `json:"authorId"`
	Text        string    `json:"text"`
	TaskID      string    `json:"taskId"`
	CreatedDate time.Time `json:"createdDate"`
}

// CommentParams filters the account wide comments read.
// The API takes a creation window of at most seven days, a longer one answers 400,
// and returns at most Limit comments, 1000 when unset, with no paging (https://developers.wrike.com/api/v4/comments/).
type CommentParams struct {
	CreatedFrom time.Time
	CreatedTo   time.Time
	Limit       int
}

// Comments lists the comments of the whole account created in the window, always as plain text.
// The answer is in no documented order, a caller that gets exactly Limit comments cannot know what was left out.
func (c *Client) Comments(ctx context.Context, p CommentParams) ([]Comment, error) {
	q := url.Values{}
	q.Set("plainText", "true")
	q.Set("createdDate", instantRange(p.CreatedFrom, p.CreatedTo))
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	var out []Comment
	if _, err := c.do(ctx, http.MethodGet, "/comments", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// instantRange writes an InstantRange parameter, each end only when set, in the UTC form the API takes.
func instantRange(start, end time.Time) string {
	var parts []string
	if !start.IsZero() {
		parts = append(parts, fmt.Sprintf(`"start":%q`, start.UTC().Format("2006-01-02T15:04:05Z")))
	}
	if !end.IsZero() {
		parts = append(parts, fmt.Sprintf(`"end":%q`, end.UTC().Format("2006-01-02T15:04:05Z")))
	}
	return "{" + strings.Join(parts, ",") + "}"
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
