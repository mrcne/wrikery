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
