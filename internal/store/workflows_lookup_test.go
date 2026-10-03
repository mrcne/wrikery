package store

import "testing"

func TestWorkflowFor(t *testing.T) {
	wfs := []Workflow{
		{ID: "A", CustomStatuses: []CustomStatus{{ID: "a1"}, {ID: "a2"}}},
		{ID: "B", CustomStatuses: []CustomStatus{{ID: "b1"}}},
	}
	for _, tc := range []struct {
		name, status, want string
		ok                 bool
	}{
		{"first workflow", "a2", "A", true},
		{"second workflow", "b1", "B", true},
		{"unknown status", "zz", "", false},
		{"empty status", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := WorkflowFor(wfs, tc.status)
			if ok != tc.ok || got.ID != tc.want {
				t.Errorf("WorkflowFor(%q) = %q, %v, want %q, %v", tc.status, got.ID, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := WorkflowFor(nil, "a1"); ok {
		t.Error("no workflows must find nothing")
	}
}

func TestFirstActiveStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		wfs  []Workflow
		want string
	}{
		{"none", nil, ""},
		{"standard workflow first visible active", []Workflow{{Standard: true, CustomStatuses: []CustomStatus{
			{ID: "n", Group: "New"}, {ID: "h", Group: "Active", Hidden: true}, {ID: "a", Group: "Active"}, {ID: "b", Group: "Active"}}}}, "a"},
		{"custom workflow ignored", []Workflow{{CustomStatuses: []CustomStatus{{ID: "x", Group: "Active"}}}}, ""},
		{"standard after custom", []Workflow{
			{CustomStatuses: []CustomStatus{{ID: "x", Group: "Active"}}},
			{Standard: true, CustomStatuses: []CustomStatus{{ID: "s", Group: "Active"}}}}, "s"},
		{"standard without active", []Workflow{{Standard: true, CustomStatuses: []CustomStatus{{ID: "n", Group: "New"}}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstActiveStatus(tc.wfs); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContactName(t *testing.T) {
	for _, tc := range []struct {
		c    Contact
		want string
	}{
		{Contact{FirstName: "Ada", LastName: "Lovelace"}, "Ada Lovelace"},
		{Contact{FirstName: "Ada"}, "Ada"},
		{Contact{LastName: "Lovelace"}, "Lovelace"},
		{Contact{}, ""},
	} {
		if got := tc.c.Name(); got != tc.want {
			t.Errorf("%+v Name() = %q, want %q", tc.c, got, tc.want)
		}
	}
}
