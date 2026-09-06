package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"2026-09-03T10:00:00Z": "2 h ago",
		"2026-09-02T15:00:00Z": "yesterday",
		"2026-08-20T15:00:00Z": "20 Aug",
		"bad":                  "bad",
	} {
		if got := relTime(in, now); got != want {
			t.Errorf("relTime(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestDetailShowsMetadataCommentsAndMarkers(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	ref := refData{
		contacts: map[string]store.Contact{"U1": {ID: "U1", FirstName: "Ada", LastName: "Nowak"}, "U2": {ID: "U2", FirstName: "Bartek", LastName: "Lis"}},
		statuses: map[string]store.CustomStatus{"S1": {ID: "S1", Name: "In progress", Group: "Active", Color: "Blue"}},
	}
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{
		task: store.Task{ID: "T1", Title: "Fix auth retry", Permalink: "https://www.wrike.com/open.htm?id=1200001", CustomStatusID: "S1", Status: "Active",
			ResponsibleIDs: []string{"U1", "U2"}, Dates: &store.TaskDates{Start: "2026-09-08", Due: "2026-09-12"}, UpdatedDate: "2026-09-03T10:00:00Z",
			Description: "<p>Body text</p>"},
		comments: []store.Comment{{ID: "local:7", AuthorID: "U1", Text: "Queued", CreatedDate: "2026-09-03T11:00:00Z"}, {ID: "C1", AuthorID: "U2", Text: "Reproduced", CreatedDate: "2026-09-02T11:00:00Z"}},
		logs:     []store.Timelog{{ID: "L1", UserID: "U1", TrackedDate: "2026-09-02", Hours: 1.5}},
		states:   map[string]store.OutboxState{"T1": store.StateFailed},
		crumb:    "Platform / API",
	})
	d.layout(th, ref, now, 60, 30, "dark")
	out := d.View()
	for _, want := range []string{"Fix auth retry", "(failed, ! to review)", "#1200001", "Platform / API", "In progress", "Ada Nowak, Bartek Lis", "8 Sep -> 12 Sep", "Body text", "Comments (2)", "Ada Nowak (sending)", "Reproduced", "Time (1)", "1.5 h"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail lacks %q:\n%s", want, out)
		}
	}
	if d.title() != "#1200001" {
		t.Errorf("title = %q", d.title())
	}
}
