package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"2026-09-03T10:00:00Z": "2 h ago",
		"2026-09-02T15:00:00Z": "yesterday",
		"2026-09-01T20:00:00Z": "1 Sep",   // forty hours back, two calendar days, so not yesterday
		"2026-09-03T13:00:00Z": "0 s ago", // a stamp ahead of this machine's clock
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
	for _, want := range []string{"Fix auth retry", "(failed, ! to review)", "#1200001", "Platform / API", "In progress", "Ada Nowak, Bartek Lis", "8 Sep -> 12 Sep", "Body text", "Comments (2)", "Ada Nowak (sending)", "Reproduced", "Time (1)", "1h 30m"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail lacks %q:\n%s", want, out)
		}
	}
	if d.title() != "#1200001" {
		t.Errorf("title = %q", d.title())
	}
}

func TestDetailMarksAQueuedTask(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{
		task:   store.Task{ID: "T1", Title: "Fix auth retry", UpdatedDate: "2026-09-03T10:00:00Z"},
		states: map[string]store.OutboxState{"T1": store.StatePending},
	})
	d.layout(th, refData{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 60, 30, "dark")
	if out := d.View(); !strings.Contains(out, "Fix auth retry (sending)") {
		t.Errorf("a queued task should say so on the title:\n%s", out)
	}
}

// A read runs while the cursor is free to move, so the answer can arrive for a task nobody is looking at any more.
func TestDetailIgnoresALateLoadForAnotherTask(t *testing.T) {
	m := New(Options{Config: config.Config{UI: config.UIConfig{Theme: "dark", ASCII: true}}})
	m.selectedTaskID = "B"
	next, _ := m.Update(taskLoadedMsg{task: store.Task{ID: "A", Title: "Late answer"}})
	if got := next.(Model).detail.task.ID; got == "A" {
		t.Errorf("a load for A landed while B is selected, detail task = %q", got)
	}
}

// boxText is the text inside a drawn box with the frame and the padding gone, so a wrapped sentence can be found whole.
func boxText(out string) string {
	var words []string
	for _, line := range strings.Split(ansi.Strip(out), "\n") {
		if r := []rune(line); len(r) > 2 {
			words = append(words, strings.Fields(string(r[1:len(r)-1]))...)
		}
	}
	return strings.Join(words, " ")
}

// The pane wraps and pads its lines before the box strips the joiners, so the strip has to come first or the end of a line is cut.
func TestDetailKeepsItsLinesWholeWithAJoinedEmoji(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	family := "\U0001F468\u200d\U0001F469\u200d\U0001F467"
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{
		task:     store.Task{ID: "T1", Title: "Plan " + family + " trip with the kids ab END1 second line", Status: "Active", UpdatedDate: "2026-09-03T10:00:00Z"},
		comments: []store.Comment{{ID: "C1", AuthorID: "U1", Text: "We take " + family + " to the lake and swim", CreatedDate: "2026-09-02T11:00:00Z"}},
		crumb:    "Platform / " + family + " Family",
	})
	d.layout(th, refData{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 30, 30, "dark")
	out := th.box("T", d.View(), 32, 32, true)
	text := boxText(out)
	for _, want := range []string{"kids ab END1 second line", "to the lake and swim", "Family"} {
		if !strings.Contains(text, want) {
			t.Errorf("the box cut %q off its line:\n%s", want, ansi.Strip(out))
		}
	}
}

func TestDetailShowsTheRelations(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	ref := refData{
		statuses: map[string]store.CustomStatus{
			"S1": {ID: "S1", Name: "In progress", Group: "Active", Color: "Blue"},
			"S2": {ID: "S2", Name: "Done", Group: "Completed", Color: "Green"},
		},
	}
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{
		task: store.Task{ID: "T1", Title: "Fix auth retry", CustomStatusID: "S1", Status: "Active", UpdatedDate: "2026-09-03T10:00:00Z",
			SuperTaskIDs: []string{"P1", "P9"}, AttachmentCount: 2},
		related: map[string]store.Task{
			"P1": {ID: "P1", Title: "Plan the release", CustomStatusID: "S1", Status: "Active"},
			"T2": {ID: "T2", Title: "Design the API", CustomStatusID: "S2", Status: "Completed"},
		},
		subtasks: []store.Task{
			{ID: "C1", Title: "Write the retry test", CustomStatusID: "S1", Status: "Active"},
			{ID: "C2", Title: "Add the backoff", CustomStatusID: "S2", Status: "Completed"},
		},
		deps: []store.Dependency{
			{ID: "X", PredecessorID: "T2", SuccessorID: "T1", RelationType: "FinishToStart", LagMinutes: 960},
			{ID: "Y", PredecessorID: "T1", SuccessorID: "T3", RelationType: "StartToStart"},
		},
		states: map[string]store.OutboxState{},
	})
	d.layout(th, ref, now, 70, 40, "dark")
	out := d.View()
	for _, want := range []string{
		"Subtask of   o Plan the release",
		"a task outside the followed spaces",
		"Attachments  2",
		"-- Subtasks (2) ",
		"o Write the retry test",
		"v Add the backoff",
		"-- Dependencies (2) ",
		"predecessor  v Design the API  finish to start, lag 2 days",
		"successor    a task outside the followed spaces  start to start",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail lacks %q:\n%s", want, out)
		}
	}
	if i, j := strings.Index(out, "-- Dependencies"), strings.Index(out, "-- Comments"); i > j {
		t.Error("the dependencies come before the comments")
	}
}

func TestDetailLeavesTheRelationRowsOutWhenEmpty(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{task: store.Task{ID: "T1", Title: "Alone", Status: "Active", UpdatedDate: "2026-09-03T10:00:00Z"}, states: map[string]store.OutboxState{}})
	d.layout(th, refData{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 70, 40, "dark")
	out := d.View()
	for _, absent := range []string{"Subtask of", "Attachments", "-- Subtasks", "-- Dependencies"} {
		if strings.Contains(out, absent) {
			t.Errorf("detail of a task without relations shows %q:\n%s", absent, out)
		}
	}
}

func TestLagText(t *testing.T) {
	for in, want := range map[int]string{0: "", 480: "lag 1 day", 960: "lag 2 days", 90: "lag 1.5 h", 60: "lag 1 h",
		-480: "lead 1 day", -960: "lead 2 days", -90: "lead 1.5 h", 100: "lag 1.7 h", 30: "lag 0.5 h"} {
		if got := LagText(in); got != want {
			t.Errorf("LagText(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDetailMovesTheRelationToASecondLineWhenItDoesNotFit(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{
		task:    store.Task{ID: "T1", Title: "Short", Status: "Active", UpdatedDate: "2026-09-03T10:00:00Z"},
		related: map[string]store.Task{"T2": {ID: "T2", Title: "Rotate the signing keys", Status: "Active"}},
		deps:    []store.Dependency{{ID: "X", PredecessorID: "T2", SuccessorID: "T1", RelationType: "FinishToStart", LagMinutes: 960}},
		states:  map[string]store.OutboxState{},
	})
	d.layout(th, refData{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 40, 30, "dark")
	out := d.View()
	// The viewport pads every line to the width, so the two lines are checked on their own.
	if !strings.Contains(out, "predecessor  o Rotate the signing keys") || !strings.Contains(out, "\n             finish to start, lag 2 days") {
		t.Errorf("a relation that does not fit the line goes under the title, indented to the label column:\n%s", out)
	}
	d.layout(th, refData{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 80, 30, "dark")
	if !strings.Contains(d.View(), "predecessor  o Rotate the signing keys  finish to start, lag 2 days") {
		t.Errorf("a relation that fits stays on the line:\n%s", d.View())
	}
}

func TestDetailCutsALongRelatedTitleToThePane(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	long := "Rotate the signing keys for every staging and production cluster"
	d := taskDetailModel{keys: defaultKeyMap()}
	d.set(taskLoadedMsg{
		task:     store.Task{ID: "T1", Title: "Short", Status: "Active", UpdatedDate: "2026-09-03T10:00:00Z", SuperTaskIDs: []string{"P1"}},
		related:  map[string]store.Task{"P1": {ID: "P1", Title: long, Status: "Active"}, "T2": {ID: "T2", Title: long, Status: "Active"}},
		subtasks: []store.Task{{ID: "C1", Title: long, Status: "Active"}},
		deps:     []store.Dependency{{ID: "X", PredecessorID: "T2", SuccessorID: "T1", RelationType: "FinishToStart"}},
		states:   map[string]store.OutboxState{},
	})
	d.layout(th, refData{}, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 40, 30, "dark")
	for _, line := range strings.Split(d.View(), "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line wider than the pane (%d): %q", w, line)
		}
		if strings.Contains(line, "Rotate the signing") && !strings.Contains(line, "...") {
			t.Errorf("a long related title is cut with an ellipsis, not by the frame: %q", line)
		}
	}
}
