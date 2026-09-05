package wrike

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type Contact struct {
	ID           string `json:"id"`
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	Type         string `json:"type"`
	AvatarURL    string `json:"avatarUrl"`
	Timezone     string `json:"timezone"`
	Locale       string `json:"locale"`
	PrimaryEmail string `json:"primaryEmail"`
	Deleted      bool   `json:"deleted"`
	Me           bool   `json:"me"`
}

// Me returns the contact of the token's owner. It doubles as the token
// verification call during first run.
func (c *Client) Me(ctx context.Context) (Contact, error) {
	q := url.Values{}
	q.Set("me", "true")
	var out []Contact
	if _, err := c.do(ctx, http.MethodGet, "/contacts", q, nil, &out); err != nil {
		return Contact{}, err
	}
	if len(out) == 0 {
		return Contact{}, errors.New("wrike: empty response to contacts me request")
	}
	return out[0], nil
}

func (c *Client) Contacts(ctx context.Context) ([]Contact, error) {
	var out []Contact
	if _, err := c.do(ctx, http.MethodGet, "/contacts", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
