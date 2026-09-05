package main

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
)

// forwardEvents turns engine events into UI messages until ctx ends.
// This is the one place that knows both vocabularies, so internal/ui never imports the syncer.
func forwardEvents(ctx context.Context, events <-chan syncer.Event, send func(tea.Msg)) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			switch ev.Kind {
			case syncer.EventStateChanged:
				send(ui.SyncStateMsg{State: string(ev.State)})
			case syncer.EventStoreChanged:
				kinds := make([]string, len(ev.Entities))
				for i, k := range ev.Entities {
					kinds[i] = string(k)
				}
				send(ui.StoreChangedMsg{Entities: kinds})
			case syncer.EventOutboxChanged:
				send(ui.OutboxChangedMsg{Pending: ev.Pending, Failed: ev.Failed})
			}
		}
	}
}
