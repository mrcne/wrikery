package main

import (
	"context"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
)

func TestForwardEventsMapsEveryKind(t *testing.T) {
	events := make(chan syncer.Event, 3)
	events <- syncer.Event{Kind: syncer.EventStateChanged, State: syncer.StateOffline}
	events <- syncer.Event{Kind: syncer.EventStoreChanged, Entities: []syncer.EntityKind{syncer.KindTasks, syncer.KindComments}}
	events <- syncer.Event{Kind: syncer.EventOutboxChanged, Pending: 2, Failed: 1}

	var got []tea.Msg
	ctx, cancel := context.WithCancel(context.Background())
	send := func(msg tea.Msg) {
		got = append(got, msg)
		if len(got) == 3 {
			cancel()
		}
	}
	forwardEvents(ctx, events, send)

	want := []tea.Msg{
		ui.SyncStateMsg{State: "offline"},
		ui.StoreChangedMsg{Entities: []string{"tasks", "comments"}},
		ui.OutboxChangedMsg{Pending: 2, Failed: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}
