package wrike

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthApp is an API application registered in Wrike's App Console, the client credentials of the OAuth 2.0 flow.
// The flow is described at https://developers.wrike.com/docs/oauth-20-authorization.md .
type OAuthApp struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

const (
	authorizeURL = "https://login.wrike.com/oauth2/authorize/v4"
	// LoginTokenURL is where an authorization code is exchanged for the first token pair.
	LoginTokenURL = "https://login.wrike.com/oauth2/token"
	// defaultExpiry stands in for an expires_in the token endpoint left out, the hour the documentation names.
	defaultExpiry = 3600
)

// TokenURL is the token endpoint of the data center a session lives in, where a refresh goes.
// The documentation sends the exchange to login.wrike.com and every later token request to the host the answer named.
func TokenURL(host string) string { return "https://" + host + "/oauth2/token" }

// AuthorizeURL is the page the browser opens so the user can allow the app.
// The state comes back unchanged on the redirect and the caller checks it.
// Scopes are joined with commas, the separator Wrike documents.
func AuthorizeURL(app OAuthApp, state string, scopes ...string) string {
	q := url.Values{}
	q.Set("client_id", app.ClientID)
	q.Set("response_type", "code")
	if app.RedirectURI != "" {
		q.Set("redirect_uri", app.RedirectURI)
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, ","))
	}
	if state != "" {
		q.Set("state", state)
	}
	return authorizeURL + "?" + q.Encode()
}

// TokenResponse is the answer of the token endpoint, for the exchange and for a refresh alike.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int    // seconds, one hour in practice
	Host         string // the API host of the user's data center, empty when the endpoint left it out
}

// OAuth talks to the token endpoints for one app.
type OAuth struct {
	App        OAuthApp
	HTTPClient *http.Client // nil means a client with a 30 second timeout
	UserAgent  string
}

// Exchange turns the code of a redirect into the first token pair, at LoginTokenURL outside the tests.
func (o OAuth) Exchange(ctx context.Context, tokenURL, code string) (TokenResponse, error) {
	form := o.form("authorization_code")
	form.Set("code", code)
	if o.App.RedirectURI != "" {
		form.Set("redirect_uri", o.App.RedirectURI)
	}
	return o.post(ctx, tokenURL, form)
}

// Refresh trades a refresh token for a new pair, at TokenURL of the session's host.
// The old refresh token is invalid afterwards, so the caller stores the new pair before it uses it.
func (o OAuth) Refresh(ctx context.Context, tokenURL, refreshToken string) (TokenResponse, error) {
	form := o.form("refresh_token")
	form.Set("refresh_token", refreshToken)
	return o.post(ctx, tokenURL, form)
}

func (o OAuth) form(grant string) url.Values {
	f := url.Values{}
	f.Set("client_id", o.App.ClientID)
	f.Set("client_secret", o.App.ClientSecret)
	f.Set("grant_type", grant)
	return f
}

func (o OAuth) post(ctx context.Context, tokenURL string, form url.Values) (TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if o.UserAgent != "" {
		req.Header.Set("User-Agent", o.UserAgent)
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return TokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return TokenResponse{}, err
	}
	if resp.StatusCode >= 300 {
		return TokenResponse{}, parseAPIError(http.MethodPost, req.URL.Path, resp.StatusCode, resp.Header, raw)
	}
	// The documentation shows expires_in as the string "3600", json.Number takes a number and a quoted one.
	var body struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		TokenType    string      `json:"token_type"`
		ExpiresIn    json.Number `json:"expires_in"`
		Host         string      `json:"host"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return TokenResponse{}, fmt.Errorf("decoding the token response: %w", err)
	}
	if body.AccessToken == "" {
		return TokenResponse{}, errors.New("wrike: the token response carries no access token")
	}
	expires, _ := body.ExpiresIn.Int64()
	if expires <= 0 {
		// Without an expiry a caller would treat the token as expired and renew it on every request.
		slog.Warn("token response without expires_in, assuming an hour", "value", string(body.ExpiresIn))
		expires = defaultExpiry
	}
	return TokenResponse{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		TokenType:    body.TokenType,
		ExpiresIn:    int(expires),
		Host:         body.Host,
	}, nil
}
