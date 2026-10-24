package wrike

import (
	"context"
	"net/http"
	"testing"
)

func TestAccountReadsTheCurrentAccount(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/account" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"kind":"accounts","data":[{"id":"IEAG3VSG","name":"YellowTerm","dateFormat":"dd/MM/yyyy",
  "firstDayOfWeek":"Mon","workDays":["Mon","Tue","Wed","Thu","Fri"],"rootFolderId":"IEAG3VSGI7777777",
  "recycleBinId":"IEAG3VSGI7777776","createdDate":"2026-09-01T10:00:00Z","joinedDate":"2026-09-01T10:00:00Z"}]}`))
	}))
	got, err := c.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (Account{ID: "IEAG3VSG", Name: "YellowTerm"}) {
		t.Errorf("account = %+v", got)
	}
}

func TestAccountWithAnEmptyAnswerIsAnError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"kind":"accounts","data":[]}`))
	}))
	if _, err := c.Account(context.Background()); err == nil {
		t.Error("an empty answer gave no error")
	}
}
