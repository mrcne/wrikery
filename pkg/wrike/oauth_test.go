package wrike

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAuthorizeURLCarriesTheAppStateAndScope(t *testing.T) {
	raw := AuthorizeURL(OAuthApp{ClientID: "id1", RedirectURI: "http://localhost:8731/callback"}, "st4te", "wsReadWrite")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "login.wrike.com" || u.Path != "/oauth2/authorize/v4" {
		t.Errorf("url = %s", raw)
	}
	q := u.Query()
	want := map[string]string{"client_id": "id1", "response_type": "code", "redirect_uri": "http://localhost:8731/callback", "scope": "wsReadWrite", "state": "st4te"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

type tokenCall struct {
	form   url.Values
	header http.Header
}

func tokenServer(t *testing.T, status int, body string) (*httptest.Server, *tokenCall) {
	t.Helper()
	call := &tokenCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth2/token" {
			t.Errorf("%s %s, want POST /oauth2/token", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		call.form, call.header = r.PostForm, r.Header
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, call
}

var testApp = OAuthApp{ClientID: "id1", ClientSecret: "s3cret", RedirectURI: "http://localhost:8731/callback"}

func TestExchangePostsTheDocumentedFieldsAndDecodesAStringExpiry(t *testing.T) {
	srv, call := tokenServer(t, http.StatusOK, `{"access_token":"at1","refresh_token":"rt1","token_type":"bearer","expires_in":"3600","host":"app-eu.wrike.com"}`)
	o := OAuth{App: testApp, UserAgent: "wrikery/test"}
	got, err := o.Exchange(context.Background(), srv.URL+"/oauth2/token", "c0de")
	if err != nil {
		t.Fatal(err)
	}
	want := TokenResponse{AccessToken: "at1", RefreshToken: "rt1", TokenType: "bearer", ExpiresIn: 3600, Host: "app-eu.wrike.com"}
	if got != want {
		t.Errorf("response = %+v, want %+v", got, want)
	}
	for k, v := range map[string]string{"client_id": "id1", "client_secret": "s3cret", "grant_type": "authorization_code", "code": "c0de", "redirect_uri": testApp.RedirectURI} {
		if call.form.Get(k) != v {
			t.Errorf("form %s = %q, want %q", k, call.form.Get(k), v)
		}
	}
	if ct := call.header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ua := call.header.Get("User-Agent"); ua != "wrikery/test" {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestRefreshPostsTheRefreshTokenAndDecodesANumberExpiry(t *testing.T) {
	srv, call := tokenServer(t, http.StatusOK, `{"access_token":"at2","refresh_token":"rt2","token_type":"bearer","expires_in":1800,"host":"www.wrike.com"}`)
	got, err := OAuth{App: testApp}.Refresh(context.Background(), srv.URL+"/oauth2/token", "rt1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at2" || got.RefreshToken != "rt2" || got.ExpiresIn != 1800 || got.Host != "www.wrike.com" {
		t.Errorf("response = %+v", got)
	}
	for k, v := range map[string]string{"client_id": "id1", "client_secret": "s3cret", "grant_type": "refresh_token", "refresh_token": "rt1"} {
		if call.form.Get(k) != v {
			t.Errorf("form %s = %q, want %q", k, call.form.Get(k), v)
		}
	}
	if call.form.Has("code") || call.form.Has("redirect_uri") {
		t.Errorf("a refresh carries no code and no redirect: %v", call.form)
	}
}

func TestExchangeAssumesAnHourWithoutAUsableExpiresIn(t *testing.T) {
	for name, field := range map[string]string{"missing": ``, "zero": `,"expires_in":0`, "empty": `,"expires_in":""`, "text": `,"expires_in":"soon"`} {
		t.Run(name, func(t *testing.T) {
			srv, _ := tokenServer(t, http.StatusOK, `{"access_token":"at1","refresh_token":"rt1","token_type":"bearer","host":"www.wrike.com"`+field+`}`)
			got, err := OAuth{App: testApp}.Exchange(context.Background(), srv.URL+"/oauth2/token", "c0de")
			if err != nil {
				t.Fatal(err)
			}
			if got.ExpiresIn != 3600 || !got.AssumedExpiry {
				t.Errorf("ExpiresIn = %d, AssumedExpiry = %v, want the hour and the flag so the caller can say so", got.ExpiresIn, got.AssumedExpiry)
			}
		})
	}
}

func TestTokenEndpointErrorIsAnAPIErrorWithEitherDescriptionKey(t *testing.T) {
	for name, body := range map[string]string{
		"oauth spelling": `{"error":"invalid_grant","error_description":"refresh token is dead"}`,
		"api spelling":   `{"error":"invalid_grant","errorDescription":"refresh token is dead"}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := tokenServer(t, http.StatusBadRequest, body)
			_, err := OAuth{App: testApp}.Refresh(context.Background(), srv.URL+"/oauth2/token", "rt1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an APIError", err)
			}
			if apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "invalid_grant" || apiErr.Description != "refresh token is dead" {
				t.Errorf("apiErr = %+v", apiErr)
			}
		})
	}
}

func TestExchangeWithoutAnAccessTokenIsAnError(t *testing.T) {
	srv, _ := tokenServer(t, http.StatusOK, `{"token_type":"bearer"}`)
	o := OAuth{App: testApp}
	if _, err := o.Exchange(context.Background(), srv.URL+"/oauth2/token", "c0de"); err == nil {
		t.Fatal("a 2xx answer without an access token was accepted")
	}
}
