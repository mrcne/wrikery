package wrike

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

const tasksFixture = `{"kind":"tasks","nextPageToken":"PAGE2","data":[
  {"id":"IEAAAATSK1","title":"Fix login","description":"<p>The token expires early</p>",
   "status":"Active","customStatusId":"IEAAAACS1","importance":"High",
   "responsibleIds":["KUAAAA01"],"parentIds":["IEAAAAFD2"],
   "dates":{"type":"Planned","duration":960,"start":"2026-09-02T09:00:00","due":"2026-09-04T17:00:00"},
   "createdDate":"2026-08-30T08:00:00Z","updatedDate":"2026-09-01T10:15:00Z",
   "permalink":"https://www.wrike.com/open.htm?id=1"},
  {"id":"IEAAAATSK2","title":"Write docs","status":"Active","customStatusId":"IEAAAACS2",
   "importance":"Normal","responsibleIds":[],"parentIds":["IEAAAAFD2"],
   "createdDate":"2026-08-31T08:00:00Z","updatedDate":"2026-09-01T09:00:00Z",
   "permalink":"https://www.wrike.com/open.htm?id=2"}
]}`

func TestTasksSearchBuildsQueryAndPages(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/folders/IEAAAAFD2/tasks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("descendants"); got != "true" {
			t.Errorf("descendants = %q", got)
		}
		if got := q.Get("updatedDate"); got != `{"start":"2026-09-01T00:00:00Z"}` {
			t.Errorf("updatedDate = %q", got)
		}
		if got := q.Get("fields"); got != `["description","responsibleIds","parentIds"]` {
			t.Errorf("fields = %q", got)
		}
		if got := q.Get("pageSize"); got != "500" {
			t.Errorf("pageSize = %q", got)
		}
		if got := q.Get("nextPageToken"); got != "PAGE1" {
			t.Errorf("nextPageToken = %q", got)
		}
		_, _ = w.Write([]byte(tasksFixture))
	}))

	page, err := c.Tasks(context.Background(), TaskParams{
		FolderID:     "IEAAAAFD2",
		Descendants:  true,
		UpdatedAfter: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Fields:       []string{"description", "responsibleIds", "parentIds"},
		PageSize:     500,
		PageToken:    "PAGE1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextPageToken != "PAGE2" || len(page.Tasks) != 2 {
		t.Fatalf("page = %+v", page)
	}
	tk := page.Tasks[0]
	if tk.Title != "Fix login" || tk.Dates == nil || tk.Dates.Due != "2026-09-04T17:00:00" {
		t.Errorf("task = %+v", tk)
	}
	if !tk.UpdatedDate.Equal(time.Date(2026, 9, 1, 10, 15, 0, 0, time.UTC)) {
		t.Errorf("updatedDate = %v", tk.UpdatedDate)
	}
}

func TestTasksInSpaceUsesSpacePath(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spaces/IEAAAASP1/tasks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("descendants"); got != "true" {
			t.Errorf("descendants = %q", got)
		}
		_, _ = w.Write([]byte(tasksFixture))
	}))

	page, err := c.Tasks(context.Background(), TaskParams{SpaceID: "IEAAAASP1", Descendants: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 2 {
		t.Errorf("page = %+v", page)
	}
}

func TestTasksSearchAccountWideOmitsEmptyParams(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		for _, key := range []string{"descendants", "updatedDate", "fields", "pageSize", "nextPageToken"} {
			if q.Has(key) {
				t.Errorf("query must omit %s, got %q", key, q.Get(key))
			}
		}
		_, _ = w.Write([]byte(`{"kind":"tasks","data":[]}`))
	}))

	page, err := c.Tasks(context.Background(), TaskParams{Descendants: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 0 || page.NextPageToken != "" {
		t.Errorf("page = %+v", page)
	}
}

func TestTasksByIDsJoinsAndLimits(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/IEAAAATSK1,IEAAAATSK2" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("fields"); got != `["description"]` {
			t.Errorf("fields = %q", got)
		}
		_, _ = w.Write([]byte(tasksFixture))
	}))

	got, err := c.TasksByIDs(context.Background(), []string{"IEAAAATSK1", "IEAAAATSK2"}, []string{"description"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("tasks = %+v", got)
	}

	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = "X"
	}
	if _, err := c.TasksByIDs(context.Background(), tooMany, nil); err == nil {
		t.Error("want error for more than 1000 ids, got nil")
	}
	if _, err := c.TasksByIDs(context.Background(), nil, nil); err == nil {
		t.Error("want error for zero ids, got nil")
	}
}

func TestUpdateTaskSendsOnlyChangedFields(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/tasks/IEAAAATSK1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("customStatus"); got != "IEAAAACS2" {
			t.Errorf("customStatus = %q", got)
		}
		if got := r.PostForm.Get("addResponsibles"); got != `["KUAAAA02"]` {
			t.Errorf("addResponsibles = %q", got)
		}
		if got := r.PostForm.Get("dates"); got != `{"type":"Planned","duration":960,"start":"2026-09-02T09:00:00","due":"2026-09-05T17:00:00"}` {
			t.Errorf("dates = %q", got)
		}
		if r.PostForm.Has("title") || r.PostForm.Has("removeResponsibles") {
			t.Errorf("unchanged fields must be omitted, form = %v", r.PostForm)
		}
		_, _ = w.Write([]byte(`{"kind":"tasks","data":[
  {"id":"IEAAAATSK1","title":"Fix login","status":"Active","customStatusId":"IEAAAACS2",
   "createdDate":"2026-08-30T08:00:00Z","updatedDate":"2026-09-01T11:00:00Z",
   "permalink":"https://www.wrike.com/open.htm?id=1"}]}`))
	}))

	got, err := c.UpdateTask(context.Background(), "IEAAAATSK1", TaskUpdate{
		CustomStatusID:  "IEAAAACS2",
		AddResponsibles: []string{"KUAAAA02"},
		Dates:           &TaskDates{Type: "Planned", Duration: 960, Start: "2026-09-02T09:00:00", Due: "2026-09-05T17:00:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomStatusID != "IEAAAACS2" {
		t.Errorf("task = %+v", got)
	}
}

func TestUpdateTaskRejectsEmptyID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must be sent")
	}))

	if _, err := c.UpdateTask(context.Background(), "", TaskUpdate{Title: "x"}); err == nil {
		t.Error("want error for empty task id, got nil")
	}
}

func TestTasksSendsResponsibles(t *testing.T) {
	var got url.Values
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"kind":"tasks","data":[]}`))
	}))
	_, err := c.Tasks(context.Background(), TaskParams{Responsibles: []string{"KUAAAAA1", "KUAAAAA2"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("responsibles") != `["KUAAAAA1","KUAAAAA2"]` {
		t.Errorf("responsibles = %q, want the JSON array", got.Get("responsibles"))
	}
}

func TestTasksOmitsEmptyResponsibles(t *testing.T) {
	var got url.Values
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"kind":"tasks","data":[]}`))
	}))
	if _, err := c.Tasks(context.Background(), TaskParams{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["responsibles"]; ok {
		t.Error("responsibles sent for an empty filter")
	}
}
