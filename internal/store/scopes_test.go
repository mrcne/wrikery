package store

import (
	"context"
	"errors"
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
