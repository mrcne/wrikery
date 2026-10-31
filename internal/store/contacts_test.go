package store

import (
	"context"
	"slices"
	"testing"
)

func TestContactsKeepTheirGroupMembers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	contacts := []Contact{
		{ID: "U1", FirstName: "Ada", LastName: "Nowak", Type: "Person"},
		{ID: "U2", FirstName: "Bartek", LastName: "Lis", Type: "Person"},
		{ID: "G1", FirstName: "QA", Type: "Group", MemberIDs: []string{"U1", "U2"}},
		{ID: "G2", FirstName: "Ops", Type: "Group", MemberIDs: []string{"U2"}},
	}
	if err := st.Contacts().ReplaceAll(ctx, contacts); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Contacts().Get(ctx, "G1"); !slices.Equal(got.MemberIDs, []string{"U1", "U2"}) {
		t.Errorf("Get(G1).MemberIDs = %v", got.MemberIDs)
	}
	list, _ := st.Contacts().List(ctx)
	byID := map[string]Contact{}
	for _, c := range list {
		byID[c.ID] = c
	}
	if !slices.Equal(byID["G2"].MemberIDs, []string{"U2"}) || byID["U1"].MemberIDs != nil {
		t.Errorf("List: G2 = %v, U1 = %v, want the members on the groups alone", byID["G2"].MemberIDs, byID["U1"].MemberIDs)
	}
	names := func(groups []Contact) []string {
		var out []string
		for _, g := range groups {
			out = append(out, g.FirstName)
		}
		return out
	}
	if got, _ := st.Contacts().GroupsOf(ctx, "U1"); !slices.Equal(names(got), []string{"QA"}) {
		t.Errorf("GroupsOf(U1) = %v, want QA", names(got))
	}
	if got, _ := st.Contacts().GroupsOf(ctx, "U2"); !slices.Equal(names(got), []string{"Ops", "QA"}) {
		t.Errorf("GroupsOf(U2) = %v, want both by name", names(got))
	}
	// The next pull replaces the members with the contacts, a member taken out of a group goes.
	contacts[2].MemberIDs = []string{"U2"}
	if err := st.Contacts().ReplaceAll(ctx, contacts); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Contacts().GroupsOf(ctx, "U1"); len(got) != 0 {
		t.Errorf("GroupsOf(U1) after the change = %v, want none", got)
	}
}
