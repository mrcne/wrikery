package store

import (
	"context"
	"errors"
	"testing"
)

func TestMeetComparesTheRecord(t *testing.T) {
	full := Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}
	cases := []struct {
		name   string
		record Home
		met    Home
		want   Meeting
	}{
		{"the same", full, full, MeetSame},
		{"another name only", full, Home{AccountID: "ACC1", AccountName: "Acme Ltd", Host: "www.wrike.com", ContactID: "U1"}, MeetSame},
		{"another host", full, Home{AccountID: "ACC1", AccountName: "Acme", Host: "app-eu.wrike.com", ContactID: "U1"}, MeetAsk},
		{"another account", full, Home{AccountID: "ACC2", AccountName: "Other", Host: "www.wrike.com", ContactID: "U1"}, MeetAsk},
		{"another person", full, Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U2"}, MeetAsk},
		{"legacy, host and person match", Home{Host: "www.wrike.com", ContactID: "U1"}, full, MeetAdopt},
		{"legacy, nothing remembered", Home{}, full, MeetAdopt},
		{"legacy, another host", Home{Host: "www.wrike.com", ContactID: "U1"}, Home{AccountID: "ACC1", Host: "app-eu.wrike.com", ContactID: "U1"}, MeetAsk},
		{"legacy, another person", Home{Host: "www.wrike.com", ContactID: "U1"}, Home{AccountID: "ACC1", Host: "www.wrike.com", ContactID: "U2"}, MeetAsk},
	}
	for _, c := range cases {
		if got := c.record.Meet(c.met); got != c.want {
			t.Errorf("%s: Meet = %v, want %v", c.name, got, c.want)
		}
	}
}

// A cache built with host in the config never wrote api_host, its tasks' links still name the data center the rows came from.
func TestHomeFallsBackToTheApiHostAndThePermalinks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	task := makeTask("T1", "a")
	task.Permalink = "https://www.wrike.com/open.htm?id=1"
	if err := st.Tasks().Upsert(ctx, []Task{task}); err != nil {
		t.Fatal(err)
	}
	host := func() string {
		t.Helper()
		h, err := st.Home(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return h.Host
	}
	if got := host(); got != "www.wrike.com" {
		t.Errorf("host from the permalinks = %q", got)
	}
	if err := st.SetMeta(ctx, MetaKeyHost, "app-us2.wrike.com"); err != nil {
		t.Fatal(err)
	}
	if got := host(); got != "app-us2.wrike.com" {
		t.Errorf("host from api_host = %q", got)
	}
	if err := st.AdoptHome(ctx, Home{AccountID: "ACC1", AccountName: "Acme", Host: "app-eu.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
	h, err := st.Home(ctx)
	if err != nil || h != (Home{AccountID: "ACC1", AccountName: "Acme", Host: "app-eu.wrike.com", ContactID: "U1"}) {
		t.Errorf("home after adoption = %+v, %v", h, err)
	}
}

func TestMoveHomeClearsWhatTheOldAccountLeft(t *testing.T) {
	for _, moved := range []bool{true, false} {
		st := newTestStore(t)
		ctx := context.Background()
		old := Home{AccountID: "ACC1", AccountName: "Acme", Host: "www.wrike.com", ContactID: "U1"}
		if err := st.AdoptHome(ctx, old); err != nil {
			t.Fatal(err)
		}
		if err := st.Scopes().Upsert(ctx, Scope{ID: "F1", Kind: ScopeKindProject, Title: "Alpha", Followed: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.writer.ExecContext(ctx, `UPDATE scopes SET cursor = 'c1', last_synced_at = '2026-10-22T10:00:00Z'`); err != nil {
			t.Fatal(err)
		}
		if err := st.Pins().Set(ctx, "F1", true); err != nil {
			t.Fatal(err)
		}
		if err := st.Timelogs().Upsert(ctx, []Timelog{{ID: "L1", TaskID: "T1", UserID: "U1", TrackedDate: "2026-10-20", Hours: 1},
			{ID: "L2", TaskID: "T1", UserID: "U2", TrackedDate: "2026-10-20", Hours: 2}}); err != nil {
			t.Fatal(err)
		}
		to := Home{AccountID: "ACC2", AccountName: "Other", Host: "app-eu.wrike.com", ContactID: "U2"}
		if !moved {
			to = Home{AccountID: "ACC1", AccountName: "Acme", Host: "app-eu.wrike.com", ContactID: "U1"}
		}
		if err := st.MoveHome(ctx, to); err != nil {
			t.Fatal(err)
		}
		if h, err := st.Home(ctx); err != nil || h != to {
			t.Errorf("moved %v: record = %+v, %v, want %+v", moved, h, err, to)
		}
		sc, err := st.Scopes().Get(ctx, "F1")
		if err != nil || sc.Cursor != "" || sc.LastSyncedAt != "" {
			t.Errorf("moved %v: scope = %+v, %v, want the cursor and the last sync cleared", moved, sc, err)
		}
		pins, err := st.Pins().List(ctx)
		if err != nil || (moved && len(pins) != 0) || (!moved && len(pins) != 1) {
			t.Errorf("moved %v: pins = %v, %v", moved, pins, err)
		}
		_, err = st.Timelogs().Get(ctx, "L1")
		if moved && err == nil {
			t.Errorf("the old person's entry stayed after a change of person")
		}
		if !moved && err != nil {
			t.Errorf("the entry went although the person stayed: %v", err)
		}
		if _, err := st.Timelogs().Get(ctx, "L2"); err != nil {
			t.Errorf("moved %v: the new person's entry went: %v", moved, err)
		}
		if pending, err := st.SwitchPending(ctx); err != nil || !pending {
			t.Errorf("moved %v: switch pending = %v, %v", moved, pending, err)
		}
	}
}

func TestClearSwitchEndsThePending(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if pending, err := st.SwitchPending(ctx); err != nil || pending {
		t.Fatalf("fresh store pending = %v, %v", pending, err)
	}
	if err := st.MoveHome(ctx, Home{AccountID: "ACC1", Host: "www.wrike.com", ContactID: "U1"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ClearSwitch(ctx); err != nil {
		t.Fatal(err)
	}
	if pending, err := st.SwitchPending(ctx); err != nil || pending {
		t.Errorf("pending after the clear = %v, %v", pending, err)
	}
}

// The engine takes any stored me_contact_id as the user, an empty one would pull nobody's tasks.
func TestAdoptHomeWritesNoEmptyKey(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.AdoptHome(ctx, Home{AccountID: "ACC1", Host: "www.wrike.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMeta(ctx, MetaKeyMe); !errors.Is(err, ErrNotFound) {
		t.Errorf("me_contact_id read = %v, want it never written", err)
	}
}
