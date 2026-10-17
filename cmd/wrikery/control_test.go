package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/ui"
)

func TestSocketPathFallsBackForALongPath(t *testing.T) {
	short := config.Paths{DBFile: "/home/u/.local/share/wrikery/wrike.db"}
	if got := socketPath(short); got != "/home/u/.local/share/wrikery/wrikery.sock" {
		t.Errorf("short = %q, want the socket next to the database", got)
	}
	long := config.Paths{DBFile: filepath.Join("/", strings.Repeat("d", 120), "wrike.db")}
	got := socketPath(long)
	if len(got) > 100 || !strings.HasSuffix(got, ".sock") || !strings.Contains(got, "wrikery-") {
		t.Errorf("long = %q, want a short hashed name in the temp dir", got)
	}
	if again := socketPath(long); again != got {
		t.Errorf("the fallback must be the same every time, %q then %q", got, again)
	}
	other := socketPath(config.Paths{DBFile: filepath.Join("/", strings.Repeat("e", 120), "wrike.db")})
	if other == got {
		t.Errorf("two databases share the fallback socket %q", got)
	}
}

func TestControlHandlerMapsARequestAndItsReply(t *testing.T) {
	var got ui.ControlMsg
	send := func(msg tea.Msg) {
		got = msg.(ui.ControlMsg)
		got.Reply <- ui.ControlReply{Text: "showing High | Ada Nowak (me)"}
	}
	handle := controlHandler(send, func() (int, int, error) { return 0, 0, nil })
	reply := handle(control.Request{Op: control.OpFilter, Importance: []string{"High"}, Person: "me", Statuses: []string{"New"}, TaskID: "T1", ParentID: "F1", FolderID: "F2", Shape: "board", Screen: "changes"})
	if !reply.OK || reply.Text != "showing High | Ada Nowak (me)" || reply.Error != "" {
		t.Errorf("reply = %+v", reply)
	}
	if got.Op != "filter" || got.Importance[0] != "High" || got.Person != "me" || got.Statuses[0] != "New" ||
		got.TaskID != "T1" || got.ParentID != "F1" || got.FolderID != "F2" || got.Shape != "board" || got.Screen != "changes" || got.Deadline.IsZero() {
		t.Errorf("message = %+v, want every field carried over and a deadline set", got)
	}
	refusing := controlHandler(func(msg tea.Msg) {
		msg.(ui.ControlMsg).Reply <- ui.ControlReply{Err: errors.New("a box is open in the interface")}
	}, nil)
	if reply := refusing(control.Request{Op: control.OpRefresh}); reply.OK || reply.Error != "a box is open in the interface" {
		t.Errorf("refusal = %+v", reply)
	}
}

func TestControlHandlerReloadSendsTheEngineMessages(t *testing.T) {
	var sent []tea.Msg
	handle := controlHandler(func(msg tea.Msg) { sent = append(sent, msg) }, func() (int, int, error) { return 2, 1, nil })
	if reply := handle(control.Request{Op: control.OpReload}); !reply.OK {
		t.Errorf("reply = %+v", reply)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want the store and the outbox ones", len(sent))
	}
	store, ok := sent[0].(ui.StoreChangedMsg)
	if !ok || !strings.Contains(strings.Join(store.Entities, ","), "tasks") || !strings.Contains(strings.Join(store.Entities, ","), "comments") {
		t.Errorf("first = %+v, want every entity kind", sent[0])
	}
	if outbox, ok := sent[1].(ui.OutboxChangedMsg); !ok || outbox.Pending != 2 || outbox.Failed != 1 {
		t.Errorf("second = %+v, want the counts", sent[1])
	}
}

func TestControlHandlerReportsBusyWithoutAReply(t *testing.T) {
	was := controlWait
	controlWait = 20 * time.Millisecond
	t.Cleanup(func() { controlWait = was })
	// The program is held, an editor is open, so the message is never received.
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	handle := controlHandler(func(tea.Msg) { <-block }, nil)
	start := time.Now()
	reply := handle(control.Request{Op: control.OpRefresh})
	if reply.OK || reply.Error != "the interface is busy" {
		t.Errorf("reply = %+v", reply)
	}
	if time.Since(start) > time.Second {
		t.Errorf("the handler waited %v with the program held", time.Since(start))
	}
}
