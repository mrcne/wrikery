package syncer

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/mrcne/wrikery/pkg/wrike"
)

// The settings screen saves a new poll interval while the engine waits, the wait restarts with it measured from the save.
func TestEngineSetPollIntervalRearmsTheWait(t *testing.T) {
	st := newTestStore(t)
	fc := &fakeClient{}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour})
	waitFor(t, e, "first cycle", isState(StateIdle))
	calls := len(fc.callLog())

	e.SetPollInterval(5 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for len(fc.callLog()) == calls {
		if time.Now().After(deadline) {
			t.Fatal("the hour long wait was not cut by the new poll interval")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A backoff after a failed cycle paces the next try, a new poll interval does not cut it.
func TestEngineSetPollIntervalLeavesABackoffWaitAlone(t *testing.T) {
	st := newTestStore(t)
	fc := &fakeClient{me: func() (wrike.Contact, error) {
		return wrike.Contact{}, &net.OpError{Op: "dial", Err: errors.New("refused")}
	}}
	e := startEngine(t, fc, st, Config{PollInterval: time.Hour, ReconnectBase: time.Hour, ReconnectCeil: time.Hour})
	waitFor(t, e, "offline after the failed cycle", isState(StateOffline))
	calls := len(fc.callLog())

	e.SetPollInterval(5 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if now := len(fc.callLog()); now != calls {
		t.Fatalf("calls went from %d to %d, a backoff wait was cut by a new poll interval", calls, now)
	}
}

// A save of the same interval must not restart the wait, so nothing is queued for the loop.
func TestEngineSetPollIntervalIgnoresTheSameValue(t *testing.T) {
	e := New(&fakeClient{}, newTestStore(t), Config{PollInterval: time.Hour}, nil)
	e.SetPollInterval(time.Hour)
	if len(e.rearm) != 0 {
		t.Error("the same interval queued a rearm")
	}
	e.SetPollInterval(time.Minute)
	if len(e.rearm) != 1 {
		t.Error("a new interval queued no rearm")
	}
}
