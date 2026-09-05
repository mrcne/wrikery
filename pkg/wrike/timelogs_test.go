package wrike

import (
	"context"
	"net/http"
	"testing"
)

const timelogsFixture = `{"kind":"timelogs","nextPageToken":"TLPAGE2","data":[
  {"id":"IEAAAATL1","taskId":"IEAAAATSK1","userId":"KUAAAA01","categoryId":"",
   "hours":1.5,"trackedDate":"2026-09-01","comment":"code review",
   "lockStatus":"Locked","approvalStatus":"Approved",
   "createdDate":"2026-09-01T15:00:00Z","updatedDate":"2026-09-01T15:00:00Z"}]}`

func TestTimelogsBuildsFilters(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/timelogs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("me"); got != "true" {
			t.Errorf("me = %q", got)
		}
		if got := q.Get("trackedDate"); got != `{"start":"2026-08-31","end":"2026-09-06"}` {
			t.Errorf("trackedDate = %q", got)
		}
		if got := q.Get("pageSize"); got != "200" {
			t.Errorf("pageSize = %q", got)
		}
		if got := q.Get("nextPageToken"); got != "TLPAGE1" {
			t.Errorf("nextPageToken = %q", got)
		}
		_, _ = w.Write([]byte(timelogsFixture))
	}))

	page, err := c.Timelogs(context.Background(), TimelogParams{
		Me: true, TrackedFrom: "2026-08-31", TrackedTo: "2026-09-06",
		PageSize: 200, PageToken: "TLPAGE1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextPageToken != "TLPAGE2" {
		t.Errorf("nextPageToken = %q", page.NextPageToken)
	}
	got := page.Timelogs
	if len(got) != 1 || got[0].Hours != 1.5 || got[0].TrackedDate != "2026-09-01" {
		t.Errorf("timelogs = %+v", got)
	}
	if got[0].LockStatus != "Locked" || got[0].ApprovalStatus != "Approved" {
		t.Errorf("lock state = %q %q", got[0].LockStatus, got[0].ApprovalStatus)
	}
}

func TestTimelogsOmitsUnsetFilters(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("me") || q.Has("trackedDate") || q.Has("pageSize") || q.Has("nextPageToken") {
			t.Errorf("filters must be omitted, query = %v", q)
		}
		_, _ = w.Write([]byte(`{"kind":"timelogs","data":[]}`))
	}))

	if _, err := c.Timelogs(context.Background(), TimelogParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskTimelogs(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/IEAAAATSK1/timelogs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(timelogsFixture))
	}))

	got, err := c.TaskTimelogs(context.Background(), "IEAAAATSK1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Comment != "code review" {
		t.Errorf("timelogs = %+v", got)
	}
}

func TestCreateTimelog(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tasks/IEAAAATSK1/timelogs" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("hours"); got != "1.5" {
			t.Errorf("hours = %q", got)
		}
		if got := r.PostForm.Get("trackedDate"); got != "2026-09-01" {
			t.Errorf("trackedDate = %q", got)
		}
		if got := r.PostForm.Get("comment"); got != "code review" {
			t.Errorf("comment = %q", got)
		}
		_, _ = w.Write([]byte(timelogsFixture))
	}))

	got, err := c.CreateTimelog(context.Background(), "IEAAAATSK1", 1.5, "2026-09-01", "code review")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "IEAAAATL1" {
		t.Errorf("timelog = %+v", got)
	}
}

func TestCreateTimelogValidates(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must be sent")
	}))

	if _, err := c.CreateTimelog(context.Background(), "", 1, "2026-09-01", ""); err == nil {
		t.Error("want error for empty task id")
	}
	if _, err := c.CreateTimelog(context.Background(), "IEAAAATSK1", 0, "2026-09-01", ""); err == nil {
		t.Error("want error for zero hours")
	}
	if _, err := c.CreateTimelog(context.Background(), "IEAAAATSK1", 1, "", ""); err == nil {
		t.Error("want error for empty tracked date")
	}
}

func TestUpdateTimelogSendsOnlySetFields(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/timelogs/IEAAAATL1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("hours"); got != "2" {
			t.Errorf("hours = %q", got)
		}
		if r.PostForm.Has("trackedDate") || r.PostForm.Has("comment") {
			t.Errorf("unset fields must be omitted, form = %v", r.PostForm)
		}
		_, _ = w.Write([]byte(timelogsFixture))
	}))

	if _, err := c.UpdateTimelog(context.Background(), "IEAAAATL1", TimelogUpdate{Hours: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTimelog(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/timelogs/IEAAAATL1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(timelogsFixture))
	}))

	if err := c.DeleteTimelog(context.Background(), "IEAAAATL1"); err != nil {
		t.Fatal(err)
	}
}

func TestTimelogsFromOnlyAndToOnlyRanges(t *testing.T) {
	var got string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("trackedDate")
		_, _ = w.Write([]byte(`{"kind":"timelogs","data":[]}`))
	}))

	if _, err := c.Timelogs(context.Background(), TimelogParams{TrackedFrom: "2026-08-31"}); err != nil {
		t.Fatal(err)
	}
	if got != `{"start":"2026-08-31"}` {
		t.Errorf("trackedDate = %q", got)
	}

	if _, err := c.Timelogs(context.Background(), TimelogParams{TrackedTo: "2026-09-06"}); err != nil {
		t.Fatal(err)
	}
	if got != `{"end":"2026-09-06"}` {
		t.Errorf("trackedDate = %q", got)
	}
}
