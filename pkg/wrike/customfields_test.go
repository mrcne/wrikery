package wrike

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestCustomFieldsListsTheAccountFields(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/customfields" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"kind":"customfields","data":[
  {"id":"IEAAAACFAAAABUDG","accountId":"IEAAAAAA","title":"Budget","type":"Duration","sharedIds":[],"settings":{"readOnly":false}},
  {"id":"IEAAAACFAAAASPCE","accountId":"IEAAAAAA","title":"Budget","type":"Numeric","spaceId":"IEAAAASP","settings":{"readOnly":false}}]}`))
	}))
	got, err := c.CustomFields(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []CustomField{{ID: "IEAAAACFAAAABUDG", Title: "Budget", Type: "Duration"}, {ID: "IEAAAACFAAAASPCE", Title: "Budget", Type: "Numeric", SpaceID: "IEAAAASP"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %+v, want %+v", got, want)
	}
}
