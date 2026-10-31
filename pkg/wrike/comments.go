package wrike

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Comment is a comment posted on a Wrike task.
// A comment carries taskId or folderId, one of the two, so an empty TaskID means a comment on a folder or project.
// Text is HTML from the two reads and plain text from CreateComment, which posts and is answered in that form.
type Comment struct {
	ID          string    `json:"id"`
	AuthorID    string    `json:"authorId"`
	Text        string    `json:"text"`
	TaskID      string    `json:"taskId"`
	CreatedDate time.Time `json:"createdDate"`
}

// Mention is one anchor of a comment that names someone: the value of its rel and the text it shows without the leading @.
// ID is a user's contact id, "@assignees" or "@followers" for the two group forms, or the form Wrike writes for a user group,
// "GD~..." on the live account, which no read maps back to the group, so Name is what names the group.
type Mention struct {
	ID   string
	Name string
}

// Mentions lists who the comment mentions, in text order.
// A mention is an anchor with the class stream-user-id and the user id in rel,
// `<a class="stream-user-id avatar" rel="KUAAAAAA">@Name</a>`, the group forms carry the group name in rel
// (https://developers.wrike.com/docs/special-syntax).
// The API answers the @ encoded as &#64;, in the text and in the rel of a group form, so both are decoded here.
// A rel with a comma or a space is no id and is left out: the value comes from whoever posted the comment,
// and a caller keeps the list comma joined.
func (c Comment) Mentions() []Mention {
	var out []Mention
	text := c.Text
	for {
		start := strings.IndexByte(text, '<')
		if start < 0 {
			return out
		}
		end := strings.IndexByte(text[start:], '>')
		if end < 0 {
			return out
		}
		tag := text[start+1 : start+end]
		text = text[start+end+1:]
		name := tag
		if sp := strings.IndexAny(name, " \t\r\n/"); sp >= 0 {
			name = name[:sp]
		}
		if !strings.EqualFold(name, "a") {
			continue
		}
		attrs := attributes(tag[len(name):])
		if !slices.Contains(strings.Fields(attrs["class"]), "stream-user-id") {
			continue
		}
		rel := html.UnescapeString(attrs["rel"])
		if rel == "" || strings.ContainsAny(rel, ", \t\r\n") {
			continue
		}
		// The anchor's text runs to its closing tag, or to the end when the markup never closes it.
		inner := text
		if close := strings.Index(strings.ToLower(text), "</a"); close >= 0 {
			inner = text[:close]
		}
		shown := strings.TrimSpace(html.UnescapeString(inner))
		out = append(out, Mention{ID: rel, Name: strings.TrimPrefix(shown, "@")})
	}
}

// attributes reads the name="value" pairs of one tag, with double or single quotes, a bare value runs to the next space.
func attributes(tag string) map[string]string {
	out := map[string]string{}
	for tag = strings.TrimSpace(tag); tag != ""; tag = strings.TrimSpace(tag) {
		eq := strings.IndexByte(tag, '=')
		if eq < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(tag[:eq]))
		rest := strings.TrimSpace(tag[eq+1:])
		var value string
		if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
			close := strings.IndexByte(rest[1:], rest[0])
			if close < 0 {
				break
			}
			value, tag = rest[1:1+close], rest[2+close:]
		} else {
			space := strings.IndexByte(rest, ' ')
			if space < 0 {
				space = len(rest)
			}
			value, tag = rest[:space], rest[space:]
		}
		out[name] = value
	}
	return out
}

// CommentParams filters the account wide comments read.
// The API takes a creation window of at most seven days, a longer one answers 400,
// and returns at most Limit comments, 1000 when unset, with no paging (https://developers.wrike.com/api/v4/comments/).
type CommentParams struct {
	CreatedFrom time.Time
	CreatedTo   time.Time
	Limit       int
}

// Comments lists the comments of the whole account created in the window, in the HTML form.
// The answer is in no documented order, a caller that gets exactly Limit comments cannot know what was left out.
func (c *Client) Comments(ctx context.Context, p CommentParams) ([]Comment, error) {
	q := url.Values{}
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

// TaskComments lists the comments on a task, in the HTML form.
// Both reads take the HTML form on purpose: a mention is an anchor in it (see Mentions),
// and the plain form the API can answer instead keeps only the name, which is not a mention.
func (c *Client) TaskComments(ctx context.Context, taskID string) ([]Comment, error) {
	if taskID == "" {
		return nil, errors.New("wrike: task id is required")
	}
	var out []Comment
	if _, err := c.do(ctx, http.MethodGet, "/tasks/"+taskID+"/comments", nil, nil, &out); err != nil {
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
