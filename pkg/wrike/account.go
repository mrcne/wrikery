package wrike

import (
	"context"
	"errors"
	"net/http"
)

// Account is the account a token belongs to, the fields of GET /account the client reads.
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Account returns the account of the token, see https://developers.wrike.com/api/v4/account/.
// It must run on the host that already answered for the token: another data center answers 401 to it,
// where /contacts answers the 300 a probe looks for, so a 401 here would read as a bad token.
func (c *Client) Account(ctx context.Context) (Account, error) {
	var out []Account
	if _, err := c.do(ctx, http.MethodGet, "/account", nil, nil, &out); err != nil {
		return Account{}, err
	}
	if len(out) == 0 {
		return Account{}, errors.New("wrike: empty response to account request")
	}
	return out[0], nil
}
