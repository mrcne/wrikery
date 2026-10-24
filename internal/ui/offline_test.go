package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func offlineModel(t *testing.T) Model {
	t.Helper()
	opts := rootTestOptions(t)
	opts.Offline = true
	m := New(opts)
	m.width, m.height = 120, 40
	return m
}

func TestOfflineModeConfirmSaysWhenNothingIsQueued(t *testing.T) {
	m := offlineModel(t)
	next, _ := m.refresh()
	d, ok := next.dialog.(confirmDialog)
	if !ok || d.prompt != "Go online? Nothing is queued, the pull starts at once." {
		t.Errorf("dialog = %#v, want the confirm for an empty queue", next.dialog)
	}
	if _, ok := d.onYes.(goOnlineMsg); !ok {
		t.Errorf("the yes sends %#v, want goOnlineMsg", d.onYes)
	}
}

// There is no engine to send a retried row with, so r on the changes screen leaves the row as it is.
func TestOfflineModeRefusesARetryOnTheChangesScreen(t *testing.T) {
	m := offlineModel(t)
	ctx := context.Background()
	st := m.opts.Store
	if err := st.Tasks().Upsert(ctx, []store.Task{{ID: "T1", Title: "a", Status: "Active"}}); err != nil {
		t.Fatal(err)
	}
	id, err := st.Outbox().EnqueueComment(ctx, "T1", "U1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Outbox().Fail(ctx, id, "boom"); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(retryWriteMsg{id: id})
	msgs := collect(cmd)
	if len(msgs) != 1 || msgs[0] != (toastMsg{text: "offline mode, R goes online"}) {
		t.Errorf("messages = %#v, want the refusal alone", msgs)
	}
	if row, err := st.Outbox().Get(ctx, id); err != nil || row.State != store.StateFailed {
		t.Errorf("row = %+v, %v, want it still failed", row, err)
	}
}

func TestControlRefreshIsRefusedInOfflineMode(t *testing.T) {
	opts := rootTestOptions(t)
	opts.Offline = true
	called := 0
	opts.Hooks.Refresh = func() { called++ }
	m := New(opts)
	m.width, m.height = 120, 40
	_, r := control(t, m, ControlMsg{Op: "refresh"})
	if r.Err == nil || r.Err.Error() != "offline mode, R in the interface goes online" || called != 0 {
		t.Errorf("reply = %+v, refresh called %d times, want the refusal and no refresh", r, called)
	}
}

func TestReachErrorTextForOfflineMode(t *testing.T) {
	if got := reachErrorText(store.TaskRef{Kind: store.RefID, Value: "T9"}, ErrOffline); got != "offline mode, R goes online" {
		t.Errorf("text = %q", got)
	}
	if !errors.Is(ErrOffline, ErrOffline) || !strings.Contains(ErrOffline.Error(), "R goes online") {
		t.Error("ErrOffline must name the way out")
	}
}

func TestSettingsShowsStartOffline(t *testing.T) {
	s := testSettings()
	if row := settingRowNamed(s, "Start offline"); row.value != "no" || row.key != "offline" {
		t.Errorf("row = %+v, want no with the key offline", row)
	}
	s.cfg.Offline = true
	if row := settingRowNamed(s, "Start offline"); row.value != "yes" {
		t.Errorf("row = %+v, want yes from the file", row)
	}
}

func settingRowNamed(s settingsModel, label string) settingRow {
	for _, r := range s.rows() {
		if r.label == label {
			return r
		}
	}
	return settingRow{}
}
