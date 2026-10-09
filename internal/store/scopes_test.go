package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestScopeUpsertAndFollowed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	scopes := st.Scopes()

	if err := scopes.Upsert(ctx, Scope{ID: "me", Kind: ScopeKindMe, Title: "My tasks", Followed: true}); err != nil {
		t.Fatal(err)
	}
	if err := scopes.Upsert(ctx, Scope{ID: "F1", Kind: ScopeKindProject, Title: "Alpha", Followed: true}); err != nil {
		t.Fatal(err)
	}
	if err := scopes.Upsert(ctx, Scope{ID: "F2", Kind: ScopeKindSpace, Title: "Ops", Followed: false}); err != nil {
		t.Fatal(err)
	}
	// Upsert replaces, the title change must stick.
	if err := scopes.Upsert(ctx, Scope{ID: "F1", Kind: ScopeKindProject, Title: "Alpha v2", Followed: true}); err != nil {
		t.Fatal(err)
	}

	got, err := scopes.Followed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("followed = %d scopes, want 2", len(got))
	}
	s, err := scopes.Get(ctx, "F1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Alpha v2" || s.Cursor != "" {
		t.Errorf("scope = %+v, want title Alpha v2 and empty cursor", s)
	}
	if _, err := scopes.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing scope error = %v, want ErrNotFound", err)
	}
}

// A re-followed scope must start from scratch: its old cursor would ask Wrike only for changes since the day it was dropped.
func TestScopeSetFollowedReplacesTheSetAndClearsAnUnfollowedCursor(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	scopes := st.Scopes()
	for _, sc := range []Scope{
		{ID: "me", Kind: ScopeKindMe, Title: "My tasks", Followed: true},
		{ID: "S1", Kind: ScopeKindSpace, Title: "Platform", Followed: true},
		{ID: "S2", Kind: ScopeKindSpace, Title: "Mobile", Followed: true},
	} {
		if err := scopes.Upsert(ctx, sc); err != nil {
			t.Fatal(err)
		}
		if err := st.Tasks().ApplyPage(ctx, sc.ID, nil, "cursor-"+sc.ID); err != nil {
			t.Fatal(err)
		}
	}

	// Keep me and Platform, drop Mobile, add the Web project.
	err := scopes.SetFollowed(ctx, []Scope{
		{ID: "me", Kind: ScopeKindMe, Title: "My tasks"},
		{ID: "S1", Kind: ScopeKindSpace, Title: "Platform"},
		{ID: "P3", Kind: ScopeKindProject, Title: "Web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := scopes.Followed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, sc := range got {
		ids = append(ids, sc.ID+":"+sc.Cursor)
	}
	// Followed lists by title: My tasks, Platform, Web. The kept scopes keep their cursor, the new one has none.
	if want := "me:cursor-me S1:cursor-S1 P3:"; strings.Join(ids, " ") != want {
		t.Fatalf("followed = %q, want %q", strings.Join(ids, " "), want)
	}
	dropped, err := scopes.Get(ctx, "S2")
	if err != nil {
		t.Fatal(err)
	}
	if dropped.Followed || dropped.Cursor != "" {
		t.Errorf("unfollowed scope = %+v, want followed false and no cursor", dropped)
	}

	// Following Mobile again starts from scratch.
	err = scopes.SetFollowed(ctx, []Scope{
		{ID: "me", Kind: ScopeKindMe, Title: "My tasks"},
		{ID: "S2", Kind: ScopeKindSpace, Title: "Mobile"},
	})
	if err != nil {
		t.Fatal(err)
	}
	back, err := scopes.Get(ctx, "S2")
	if err != nil {
		t.Fatal(err)
	}
	if !back.Followed || back.Cursor != "" {
		t.Errorf("re-followed scope = %+v, want followed true and no cursor", back)
	}
}

// A pull that is on the wire while the user unfollows its scope writes the cursor back onto the unfollowed row,
// and the refresh that follows sweeps the scope's tasks out of the cache.
// A re-follow has to start from scratch whatever the row holds, or the scope comes back with almost no tasks.
func TestScopeReFollowStartsFromScratchWhateverCursorTheRowHolds(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	scopes := st.Scopes()
	for _, sc := range []Scope{
		{ID: "me", Kind: ScopeKindMe, Title: "My tasks", Followed: true},
		{ID: "S2", Kind: ScopeKindSpace, Title: "Mobile", Followed: true},
	} {
		if err := scopes.Upsert(ctx, sc); err != nil {
			t.Fatal(err)
		}
	}
	if err := scopes.SetFollowed(ctx, []Scope{{ID: "me", Kind: ScopeKindMe, Title: "My tasks"}}); err != nil {
		t.Fatal(err)
	}
	// The pull of the dropped scope lands after the drop.
	if err := st.Tasks().ApplyPage(ctx, "S2", nil, "2026-09-01T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := scopes.SetFollowed(ctx, []Scope{{ID: "me", Kind: ScopeKindMe, Title: "My tasks"}, {ID: "S2", Kind: ScopeKindSpace, Title: "Mobile"}}); err != nil {
		t.Fatal(err)
	}
	got, err := scopes.Get(ctx, "S2")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Followed || got.Cursor != "" || got.LastSyncedAt != "" {
		t.Errorf("after the re-follow S2 = followed %v, cursor %q, last synced %q, want followed with no cursor", got.Followed, got.Cursor, got.LastSyncedAt)
	}
	// A scope that stays followed keeps its cursor, see the test above.
	if err := st.Tasks().ApplyPage(ctx, "S2", nil, "kept"); err != nil {
		t.Fatal(err)
	}
	if err := scopes.SetFollowed(ctx, []Scope{{ID: "me", Kind: ScopeKindMe, Title: "My tasks"}, {ID: "S2", Kind: ScopeKindSpace, Title: "Mobile"}}); err != nil {
		t.Fatal(err)
	}
	if got, err = scopes.Get(ctx, "S2"); err != nil || got.Cursor != "kept" {
		t.Errorf("S2 kept followed should keep its cursor, got %q, %v", got.Cursor, err)
	}
}
