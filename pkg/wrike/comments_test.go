package wrike

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestTaskCommentsRequestsPlainText(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/IEAAAATSK1/comments" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("plainText"); got != "true" {
			t.Errorf("plainText = %q, want true", got)
		}
		_, _ = w.Write([]byte(`{"kind":"comments","data":[
  {"id":"IEAAAACM1","authorId":"KUAAAA01","text":"looks done to me","taskId":"IEAAAATSK1",
   "createdDate":"2026-09-01T09:30:00Z"}]}`))
	}))

	got, err := c.TaskComments(context.Background(), "IEAAAATSK1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "looks done to me" || got[0].AuthorID != "KUAAAA01" {
		t.Errorf("comments = %+v", got)
	}
	if !got[0].CreatedDate.Equal(time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("createdDate = %v", got[0].CreatedDate)
	}
}

func TestCreateComment(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tasks/IEAAAATSK1/comments" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("text"); got != "shipping tomorrow" {
			t.Errorf("text = %q", got)
		}
		if got := r.PostForm.Get("plainText"); got != "true" {
			t.Errorf("plainText = %q, want true", got)
		}
		_, _ = w.Write([]byte(`{"kind":"comments","data":[
  {"id":"IEAAAACM2","authorId":"KUAAAA01","text":"shipping tomorrow","taskId":"IEAAAATSK1",
   "createdDate":"2026-09-01T12:00:00Z"}]}`))
	}))

	got, err := c.CreateComment(context.Background(), "IEAAAATSK1", "shipping tomorrow")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "IEAAAACM2" || got.Text != "shipping tomorrow" {
		t.Errorf("comment = %+v", got)
	}
}

func TestCreateCommentRejectsEmpty(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must be sent")
	}))

	if _, err := c.CreateComment(context.Background(), "IEAAAATSK1", ""); err == nil {
		t.Error("want error for empty text, got nil")
	}
	if _, err := c.CreateComment(context.Background(), "", "hi"); err == nil {
		t.Error("want error for empty task id, got nil")
	}
}

func TestCommentsSendsTheWindowAndPlainText(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/comments" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("plainText"); got != "true" {
			t.Errorf("plainText = %q, want true", got)
		}
		if got := q.Get("createdDate"); got != `{"start":"2026-09-01T09:00:00Z","end":"2026-09-03T09:00:00Z"}` {
			t.Errorf("createdDate = %q", got)
		}
		if got := q.Get("limit"); got != "1000" {
			t.Errorf("limit = %q", got)
		}
		_, _ = w.Write([]byte(`{"kind":"comments","data":[
  {"id":"IEAAAACM1","authorId":"KUAAAA01","text":"on a task","taskId":"IEAAAATSK1","createdDate":"2026-09-02T09:30:00Z","updatedDate":"2026-09-02T09:30:00Z"},
  {"id":"IEAAAACM2","authorId":"KUAAAA01","text":"on a folder","folderId":"IEAAAAFLD1","createdDate":"2026-09-02T10:00:00Z","updatedDate":"2026-09-02T10:00:00Z"}]}`))
	}))
	got, err := c.Comments(context.Background(), CommentParams{
		CreatedFrom: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), CreatedTo: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC), Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TaskID != "IEAAAATSK1" || got[1].TaskID != "" {
		t.Errorf("comments = %+v, want the folder comment with an empty task id", got)
	}
}
