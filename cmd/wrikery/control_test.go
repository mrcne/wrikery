package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	userDir := filepath.Join("/tmp", fmt.Sprintf("wrikery-%d", os.Getuid()))
	if len(got) > 100 || !strings.HasSuffix(got, ".sock") || filepath.Dir(got) != userDir {
		t.Errorf("long = %q, want a short hashed name under the user's own %s", got, userDir)
	}
	if info, err := os.Stat(userDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("user dir = %v, %v, want it made with mode 0700", info, err)
	}
	if again := socketPath(long); again != got {
		t.Errorf("the fallback must be the same every time, %q then %q", got, again)
	}
	other := socketPath(config.Paths{DBFile: filepath.Join("/", strings.Repeat("e", 120), "wrike.db")})
	if other == got {
		t.Errorf("two databases share the fallback socket %q", got)
	}
}

func TestDemoSocketLivesInTheDataDirectory(t *testing.T) {
	p := config.Paths{DBFile: "/home/u/.local/share/wrikery/wrike.db"}
	if got := demoSocketPath(p); got != "/home/u/.local/share/wrikery/wrikery-demo.sock" {
		t.Errorf("demo = %q, want it beside the real database, in a directory only the user owns", got)
	}
}

func TestControlOpsMatchTheInterface(t *testing.T) {
	pairs := map[string]string{
		control.OpFilter: ui.OpFilter, control.OpSelect: ui.OpSelect, control.OpFolder: ui.OpFolder,
		control.OpShape: ui.OpShape, control.OpScreen: ui.OpScreen, control.OpRefresh: ui.OpRefresh,
	}
	for wire, model := range pairs {
		if wire != model {
			t.Errorf("the socket says %q and the interface %q, a request would be refused as unknown", wire, model)
		}
	}
}

func TestControlHandlerMapsARequestAndItsReply(t *testing.T) {
	var got ui.ControlMsg
	send := func(msg tea.Msg) {
		got = msg.(ui.ControlMsg)
		got.Reply <- ui.ControlReply{Text: "showing High | Ada Nowak (me)"}
	}
	handle := controlHandler(send, func() (int, int, error) { return 0, 0, nil })
	reply := handle(control.Request{Op: control.OpFilter, Importance: []string{"High"}, Person: "me", Statuses: []string{"New"}, TaskID: "T1", ParentIDs: []string{"F1", "F3"}, ResponsibleIDs: []string{"U1"}, FolderID: "F2", Shape: "board", Screen: "changes"})
	if !reply.OK || reply.Text != "showing High | Ada Nowak (me)" || reply.Error != "" {
		t.Errorf("reply = %+v", reply)
	}
	if got.Op != "filter" || got.Importance[0] != "High" || got.Person != "me" || got.Statuses[0] != "New" ||
		got.TaskID != "T1" || strings.Join(got.ParentIDs, ",") != "F1,F3" || strings.Join(got.ResponsibleIDs, ",") != "U1" || got.FolderID != "F2" || got.Shape != "board" || got.Screen != "changes" || got.Deadline.IsZero() {
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
	sent := make(chan tea.Msg, 4)
	handle := controlHandler(func(msg tea.Msg) { sent <- msg }, func() (int, int, error) { return 2, 1, nil })
	if reply := handle(control.Request{Op: control.OpReload, Entities: []string{"tasks", "comments"}}); !reply.OK {
		t.Errorf("reply = %+v", reply)
	}
	first, second := <-sent, <-sent
	if store, ok := first.(ui.StoreChangedMsg); !ok || strings.Join(store.Entities, ",") != "tasks,comments" {
		t.Errorf("first = %+v, want the kinds the request named", first)
	}
	if outbox, ok := second.(ui.OutboxChangedMsg); !ok || outbox.Pending != 2 || outbox.Failed != 1 {
		t.Errorf("second = %+v, want the counts", second)
	}
	if reply := handle(control.Request{Op: control.OpReload}); !reply.OK {
		t.Errorf("reply = %+v", reply)
	}
	if store, ok := (<-sent).(ui.StoreChangedMsg); !ok || len(store.Entities) != 8 {
		t.Errorf("a reload without kinds = %+v, want every kind", store)
	}
	<-sent
}

func TestControlHandlerReloadDoesNotWaitForTheProgram(t *testing.T) {
	// An editor holds the program, so Program.Send blocks: the nudge of a write command must still get its answer at once.
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	handle := controlHandler(func(tea.Msg) { <-block }, func() (int, int, error) { return 0, 0, nil })
	start := time.Now()
	if reply := handle(control.Request{Op: control.OpReload}); !reply.OK {
		t.Errorf("reply = %+v", reply)
	}
	if time.Since(start) > time.Second {
		t.Errorf("the reload waited %v on the held program", time.Since(start))
	}
}

func TestBusyReachesTheCommandThroughTheSocket(t *testing.T) {
	was := controlWait
	controlWait = 30 * time.Millisecond
	t.Cleanup(func() { controlWait = was })
	dir, err := os.MkdirTemp("", "wk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	l, err := control.Listen(filepath.Join(dir, "ui.sock"), controlHandler(func(tea.Msg) { <-block }, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	reply, err := control.Call(context.Background(), filepath.Join(dir, "ui.sock"), control.Request{Op: control.OpRefresh})
	if err != nil || reply.OK || reply.Error != "the interface is busy" {
		t.Errorf("reply = %+v, err = %v, want the busy answer and not a socket timeout", reply, err)
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
