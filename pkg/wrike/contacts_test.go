package wrike

import (
	"context"
	"net/http"
	"testing"
)

const contactsFixture = `{"kind":"contacts","data":[
  {"id":"KUAAAA01","firstName":"Ada","lastName":"Lovelace","type":"Person",
   "profiles":[{"accountId":"IEAAAA","email":"ada@example.test","role":"User","external":false,"admin":false,"owner":false}],
   "avatarUrl":"https://example.test/a.png","timezone":"Europe/Vienna","locale":"en",
   "deleted":false,"me":true,"primaryEmail":"ada@example.test"},
  {"id":"KUAAAA02","firstName":"Grace","lastName":"Hopper","type":"Person",
   "avatarUrl":"","timezone":"UTC","locale":"en","deleted":false,"me":false,"primaryEmail":"grace@example.test"}
]}`

func TestMeQueriesContactsWithMeFlag(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/contacts" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("me"); got != "true" {
			t.Errorf("me = %q, want true", got)
		}
		_, _ = w.Write([]byte(`{"kind":"contacts","data":[
  {"id":"KUAAAA01","firstName":"Ada","lastName":"Lovelace","type":"Person",
   "timezone":"Europe/Vienna","locale":"en","deleted":false,"me":true,"primaryEmail":"ada@example.test"}]}`))
	}))

	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.ID != "KUAAAA01" || !me.Me || me.PrimaryEmail != "ada@example.test" {
		t.Errorf("me = %+v", me)
	}
}

func TestMeRejectsEmptyResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"kind":"contacts","data":[]}`))
	}))

	if _, err := c.Me(context.Background()); err == nil {
		t.Fatal("want error for empty me response, got nil")
	}
}

func TestContactsListsEveryone(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(contactsFixture))
	}))

	got, err := c.Contacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].FirstName != "Grace" {
		t.Errorf("contacts = %+v", got)
	}
}
