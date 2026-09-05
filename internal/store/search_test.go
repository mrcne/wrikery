package store

import (
	"context"
	"testing"
)

func TestSearchFindsTitleAndDescription(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	a := makeTask("T1", "login flow")
	a.Description = "<p>the token expires early</p>"
	b := makeTask("T2", "billing export")
	b.Description = "<p>csv output is wrong</p>"
	if err := st.Tasks().Upsert(ctx, []Task{a, b}); err != nil {
		t.Fatal(err)
	}

	got, err := st.Tasks().Search(ctx, "token", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "T1" {
		t.Fatalf("search token = %v, want [T1]", ids(got))
	}
	got, err = st.Tasks().Search(ctx, "billing", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "T2" {
		t.Fatalf("search billing = %v, want [T2]", ids(got))
	}
}

func ids(tasks []Task) []string {
	var out []string
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

func TestSearchFollowsUpdatesAndDeletes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	a := makeTask("T1", "alpha release")
	if err := st.Tasks().Upsert(ctx, []Task{a}); err != nil {
		t.Fatal(err)
	}
	a.Title = "beta release"
	a.Description = "<p>body of beta release</p>"
	if err := st.Tasks().Upsert(ctx, []Task{a}); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Tasks().Search(ctx, "alpha", 10); err != nil || len(got) != 0 {
		t.Fatalf("search alpha after rename = %v, %v, want empty", ids(got), err)
	}
	if got, err := st.Tasks().Search(ctx, "beta", 10); err != nil || len(got) != 1 {
		t.Fatalf("search beta after rename = %v, %v, want [T1]", ids(got), err)
	}
	if err := st.Tasks().Delete(ctx, "T1"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Tasks().Search(ctx, "beta", 10); err != nil || len(got) != 0 {
		t.Fatalf("search beta after delete = %v, %v, want empty", ids(got), err)
	}
}

func TestSearchPrefixOnLastTerm(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "billing export job")}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Tasks().Search(ctx, "billing exp", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("prefix search = %v, want [T1]", ids(got))
	}
}

func TestSearchSurvivesFTSSyntax(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.Tasks().Upsert(ctx, []Task{makeTask("T1", "quoting")}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`"broken`, `a AND (b`, `x NOT`, `-`, `*`} {
		if _, err := st.Tasks().Search(ctx, q, 10); err != nil {
			t.Errorf("query %q returned error: %v", q, err)
		}
	}
	if got, err := st.Tasks().Search(ctx, "   ", 10); err != nil || got != nil {
		t.Errorf("blank query = %v, %v, want nil, nil", got, err)
	}
}
