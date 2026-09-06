package ui

import (
	"testing"

	"github.com/mrcne/wrikery/internal/store"
)

func TestSummarize(t *testing.T) {
	cases := []struct {
		row  store.OutboxRow
		want string
	}{
		{store.OutboxRow{Kind: store.KindCommentCreate, Payload: []byte(`{"text":"Queued while offline, quite a long comment indeed"}`)}, "comment: Queued while offline, quite a long comm..."},
		{store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"customStatusId":"X"}`)}, "status change"},
		{store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"addResponsibles":["A"]}`)}, "assignee change"},
		{store.OutboxRow{Kind: store.KindTaskUpdate, Payload: []byte(`{"dates":{"type":"Planned"}}`)}, "dates change"},
		{store.OutboxRow{Kind: store.KindTimelogCreate, Payload: []byte(`{"hours":1,"trackedDate":"2026-08-26"}`)}, "time entry 1.0 h on 2026-08-26"},
		{store.OutboxRow{Kind: store.KindTimelogDelete}, "delete time entry"},
	}
	for _, c := range cases {
		if got := summarize(c.row); got != c.want {
			t.Errorf("summarize(%s) = %q, want %q", c.row.Kind, got, c.want)
		}
	}
}
