package wrike

import (
	"context"
	"net/http"
	"testing"
)

func TestSpaces(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spaces" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"kind":"spaces","data":[
  {"id":"IEAAAASPACE1","title":"Engineering","accessType":"Private","archived":false},
  {"id":"IEAAAASPACE2","title":"Old Stuff","accessType":"Public","archived":true}]}`))
	}))

	got, err := c.Spaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Engineering" || !got[1].Archived {
		t.Errorf("spaces = %+v", got)
	}
}
