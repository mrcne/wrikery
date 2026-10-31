package wrike

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestTaskCommentsReadsTheHTMLForm(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/IEAAAATSK1/comments" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Has("plainText") {
			t.Errorf("plainText = %q, the read must take the HTML form so the mention anchors are seen", r.URL.Query().Get("plainText"))
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

func TestCommentsSendsTheWindow(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/comments" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Has("plainText") {
			t.Errorf("plainText = %q, the read must take the HTML form so the mention anchors are seen", q.Get("plainText"))
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

func TestCommentMentions(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Mention
	}{
		{"user", `<a class="stream-user-id avatar" rel="KUAAAA01">&#64;Ada Nowak</a> please look`, []Mention{{ID: "KUAAAA01", Name: "Ada Nowak"}}},
		{"group forms", `<a class="stream-user-id avatar quasi-contact" rel="&#64;assignees">&#64;assignees</a> and <a class="stream-user-id avatar quasi-contact" rel="&#x40;followers">&#64;followers</a>`, []Mention{{ID: "@assignees", Name: "assignees"}, {ID: "@followers", Name: "followers"}}},
		{"two users", `<a class="stream-user-id avatar" rel="KUAAAA01">&#64;Ada</a><a class="stream-user-id avatar" rel="KUAAAA02">&#64;Bartek</a>`, []Mention{{ID: "KUAAAA01", Name: "Ada"}, {ID: "KUAAAA02", Name: "Bartek"}}},
		{"a group as the web app writes it", `hey <a class="stream-user-id avatar highlighted" rel="GD~0MjIRGbS9t">&#64;My Team</a>`, []Mention{{ID: "GD~0MjIRGbS9t", Name: "My Team"}}},
		{"single quotes", `<a class='stream-user-id avatar' rel='KUAAAA03'>&#64;Celina</a>`, []Mention{{ID: "KUAAAA03", Name: "Celina"}}},
		{"a link is not a mention", `see <a href="https://example.com">the page</a>`, nil},
		{"upper case tag", `<A CLASS="stream-user-id avatar" REL="KUAAAA05">&#64;Dawid</A>`, []Mention{{ID: "KUAAAA05", Name: "Dawid"}}},
		{"another tag with the class", `<abbr class="stream-user-id" rel="U9">x</abbr>`, nil},
		{"a rel with a comma is not an id", `<a class="stream-user-id avatar" rel="KUAAAA01,U1">&#64;x</a>`, nil},
		{"a rel with a space is not an id", `<a class="stream-user-id avatar" rel="U1 U2">&#64;x</a>`, nil},
		{"anchor without rel", `<a class="stream-user-id avatar">&#64;Nobody</a>`, nil},
		{"anchor never closed", `<a class="stream-user-id avatar" rel="KUAAAA01">&#64;Ada and the rest`, []Mention{{ID: "KUAAAA01", Name: "Ada and the rest"}}},
		{"plain text", `@Ada Nowak typed by hand`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (Comment{Text: c.text}).Mentions(); !slices.Equal(got, c.want) {
				t.Errorf("Mentions() = %v, want %v", got, c.want)
			}
		})
	}
}
