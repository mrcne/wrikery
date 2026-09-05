package wrike

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Timelog struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"taskId"`
	UserID      string    `json:"userId"`
	CategoryID  string    `json:"categoryId"`
	TrackedDate string    `json:"trackedDate"`
	Comment     string    `json:"comment"`
	Hours       float64   `json:"hours"`
	CreatedDate time.Time `json:"createdDate"`
	UpdatedDate time.Time `json:"updatedDate"`
}

// TimelogParams filters the account wide timelog listing.
// Dates are yyyy-MM-dd strings because that is what the API takes and returns.
type TimelogParams struct {
	Me          bool
	TrackedFrom string
	TrackedTo   string
}

func (c *Client) Timelogs(ctx context.Context, p TimelogParams) ([]Timelog, error) {
	q := url.Values{}
	if p.Me {
		q.Set("me", "true")
	}
	if p.TrackedFrom != "" || p.TrackedTo != "" {
		rangeJSON := "{"
		if p.TrackedFrom != "" {
			rangeJSON += fmt.Sprintf(`"start":%q`, p.TrackedFrom)
		}
		if p.TrackedTo != "" {
			if p.TrackedFrom != "" {
				rangeJSON += ","
			}
			rangeJSON += fmt.Sprintf(`"end":%q`, p.TrackedTo)
		}
		rangeJSON += "}"
		q.Set("trackedDate", rangeJSON)
	}
	var out []Timelog
	if _, err := c.do(ctx, http.MethodGet, "/timelogs", q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) TaskTimelogs(ctx context.Context, taskID string) ([]Timelog, error) {
	if taskID == "" {
		return nil, errors.New("wrike: task id is required")
	}
	var out []Timelog
	if _, err := c.do(ctx, http.MethodGet, "/tasks/"+taskID+"/timelogs", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateTimelog(ctx context.Context, taskID string, hours float64, trackedDate, comment string) (Timelog, error) {
	if taskID == "" {
		return Timelog{}, errors.New("wrike: task id is required")
	}
	if hours <= 0 {
		return Timelog{}, errors.New("wrike: hours must be positive")
	}
	if trackedDate == "" {
		return Timelog{}, errors.New("wrike: tracked date is required")
	}
	form := url.Values{}
	form.Set("hours", strconv.FormatFloat(hours, 'f', -1, 64))
	form.Set("trackedDate", trackedDate)
	if comment != "" {
		form.Set("comment", comment)
	}
	var out []Timelog
	if _, err := c.do(ctx, http.MethodPost, "/tasks/"+taskID+"/timelogs", nil, form, &out); err != nil {
		return Timelog{}, err
	}
	if len(out) == 0 {
		return Timelog{}, errors.New("wrike: empty response to timelog creation")
	}
	return out[0], nil
}

// TimelogUpdate lists the changes to apply.
// Hours is sent when positive, the strings are sent when non empty.
type TimelogUpdate struct {
	Hours       float64
	TrackedDate string
	Comment     string
}

func (c *Client) UpdateTimelog(ctx context.Context, timelogID string, u TimelogUpdate) (Timelog, error) {
	if timelogID == "" {
		return Timelog{}, errors.New("wrike: timelog id is required")
	}
	form := url.Values{}
	if u.Hours > 0 {
		form.Set("hours", strconv.FormatFloat(u.Hours, 'f', -1, 64))
	}
	if u.TrackedDate != "" {
		form.Set("trackedDate", u.TrackedDate)
	}
	if u.Comment != "" {
		form.Set("comment", u.Comment)
	}
	var out []Timelog
	if _, err := c.do(ctx, http.MethodPut, "/timelogs/"+timelogID, nil, form, &out); err != nil {
		return Timelog{}, err
	}
	if len(out) == 0 {
		return Timelog{}, errors.New("wrike: empty response to timelog update")
	}
	return out[0], nil
}

func (c *Client) DeleteTimelog(ctx context.Context, timelogID string) error {
	if timelogID == "" {
		return errors.New("wrike: timelog id is required")
	}
	_, err := c.do(ctx, http.MethodDelete, "/timelogs/"+timelogID, nil, nil, nil)
	return err
}
