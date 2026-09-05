package syncer

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

func TestEndToEndSync(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Scopes().Upsert(ctx, store.Scope{ID: "F1", Kind: store.ScopeKindProject,
		Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}

	var phase atomic.Int32
	phase.Store(1)
	var commentPosted atomic.Bool

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
	taskJSON := func(id, title, updated string) string {
		return `{"id":"` + id + `","title":"` + title + `","status":"Active",` +
			`"description":"<p>body</p>","parentIds":["F1"],` +
			`"createdDate":"2026-09-01T10:00:00Z","updatedDate":"` + updated + `"}`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/contacts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"contacts","data":[{"id":"U1","firstName":"Anna","lastName":"Nowak","me":true}]}`)
	})
	mux.HandleFunc("/folders", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"folders","data":[{"id":"F1","title":"Alpha","scope":"WsFolder","project":{"status":"Green"}}]}`)
	})
	mux.HandleFunc("/spaces", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"spaces","data":[{"id":"S1","title":"Dev","accessType":"Private"}]}`)
	})
	mux.HandleFunc("/workflows", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"workflows","data":[{"id":"W1","name":"Default","customStatuses":[{"id":"CS1","name":"New","group":"Active"}]}]}`)
	})
	mux.HandleFunc("/folders/F1/tasks", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("descendants") != "true" {
			t.Error("folder task query without descendants=true")
		}
		if q.Get("updatedDate") == "" {
			// Full pulls: the initial sync and the sweep.
			if phase.Load() == 1 {
				writeJSON(w, `{"kind":"tasks","data":[`+
					taskJSON("T1", "first", "2026-09-01T10:00:00Z")+`,`+
					taskJSON("T2", "second", "2026-09-02T10:00:00Z")+`]}`)
				return
			}
			writeJSON(w, `{"kind":"tasks","data":[`+
				taskJSON("T1", "first", "2026-09-01T10:00:00Z")+`,`+
				taskJSON("T2", "second v2", "2026-09-03T10:00:00Z")+`]}`)
			return
		}
		// Incremental pull: only what changed after the cursor.
		if phase.Load() == 2 && strings.Contains(q.Get("updatedDate"), "2026-09-02T10:00:00") {
			writeJSON(w, `{"kind":"tasks","data":[`+taskJSON("T2", "second v2", "2026-09-03T10:00:00Z")+`]}`)
			return
		}
		writeJSON(w, `{"kind":"tasks","data":[]}`)
	})
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		// The me scope, account wide by responsibles.
		if r.URL.Query().Get("responsibles") != `["U1"]` {
			t.Errorf("account wide query responsibles = %q", r.URL.Query().Get("responsibles"))
		}
		writeJSON(w, `{"kind":"tasks","data":[`+taskJSON("T1", "first", "2026-09-01T10:00:00Z")+`]}`)
	})
	mux.HandleFunc("/tasks/T1/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			commentPosted.Store(true)
			writeJSON(w, `{"kind":"comments","data":[{"id":"C9","authorId":"U1","taskId":"T1","text":"from the tui","createdDate":"2026-09-03T11:00:00Z"}]}`)
			return
		}
		if commentPosted.Load() {
			writeJSON(w, `{"kind":"comments","data":[{"id":"C9","authorId":"U1","taskId":"T1","text":"from the tui","createdDate":"2026-09-03T11:00:00Z"}]}`)
			return
		}
		writeJSON(w, `{"kind":"comments","data":[]}`)
	})
	mux.HandleFunc("/tasks/T1/timelogs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"timelogs","data":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := wrike.New("test-token", wrike.WithBaseURL(srv.URL))
	e := New(client, st, Config{PollInterval: time.Hour}, slog.New(slog.DiscardHandler))
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = e.Run(runCtx); close(done) }()
	defer func() { cancel(); <-done }()

	// Initial sync.
	waitFor(t, e, "initial idle", isState(StateIdle))
	task, err := st.Tasks().Get(ctx, "T2")
	if err != nil || task.Title != "second" || task.DescriptionPlain != "body" {
		t.Fatalf("task after initial sync = %+v, %v", task, err)
	}
	sc, err := st.Scopes().Get(ctx, "F1")
	if err != nil || sc.Cursor != "2026-09-02T10:00:00Z" {
		t.Fatalf("scope = %+v, %v", sc, err)
	}
	if _, err := st.Folders().Get(ctx, "F1"); err != nil {
		t.Fatalf("folder tree missing: %v", err)
	}

	// A queued write drains on the wake signal.
	if err := st.Tasks().MarkOpened(ctx, "T1", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "from the tui"); err != nil {
		t.Fatal(err)
	}
	e.WakeOutbox()
	waitFor(t, e, "comment drained", func(ev Event) bool {
		return ev.Kind == EventOutboxChanged && ev.Pending == 0
	})
	waitFor(t, e, "idle after wake", isState(StateIdle))
	comments, err := st.Comments().ListForTask(ctx, "T1")
	if err != nil || len(comments) != 1 || comments[0].ID != "C9" {
		t.Fatalf("comments = %+v, %v, want the confirmed C9", comments, err)
	}

	// Incremental pull picks up the remote change.
	phase.Store(2)
	e.Refresh()
	waitFor(t, e, "idle after incremental", isState(StateIdle))
	task, err = st.Tasks().Get(ctx, "T2")
	if err != nil || task.Title != "second v2" {
		t.Fatalf("task after incremental = %+v, %v", task, err)
	}
	sc, err = st.Scopes().Get(ctx, "F1")
	if err != nil || sc.Cursor != "2026-09-03T10:00:00Z" {
		t.Fatalf("cursor after incremental = %+v, %v", sc, err)
	}
}
