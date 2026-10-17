package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
)

// socketPath is the control socket, next to the database like the sync lock so two data directories never share one.
// A unix socket path has room for 104 bytes on macOS and 108 on Linux, so a long data directory gets a hashed name
// in the temp dir instead, computed the same way by the interface and by a command.
func socketPath(p config.Paths) string {
	path := filepath.Join(filepath.Dir(p.DBFile), "wrikery.sock")
	if len(path) <= 100 {
		return path
	}
	sum := sha256.Sum256([]byte(p.DBFile))
	return filepath.Join(os.TempDir(), "wrikery-"+hex.EncodeToString(sum[:4])+".sock")
}

// demoSocketPath is where a demo listens, one fixed name since a demo's data directory is a throwaway nobody can find.
func demoSocketPath() string {
	return filepath.Join(os.TempDir(), "wrikery-demo.sock")
}

// controlWait is how long a request waits for the interface's reply before it is called busy, a variable for the tests.
var controlWait = 5 * time.Second

// controlHandler answers the socket: a reload becomes the two messages the engine forwarder sends after a cycle,
// everything else goes to the interface as a ControlMsg and comes back through its reply channel.
// The send runs on its own goroutine because Program.Send blocks while the program is held by an external editor,
// and the handler must answer the command inside the wait all the same.
func controlHandler(send func(tea.Msg), counts func() (pending, failed int, err error)) func(control.Request) control.Reply {
	return func(req control.Request) control.Reply {
		if req.Op == control.OpReload {
			send(ui.StoreChangedMsg{Entities: entityNames(allEntities)})
			if pending, failed, err := counts(); err == nil {
				send(ui.OutboxChangedMsg{Pending: pending, Failed: failed})
			}
			return control.Reply{OK: true}
		}
		reply := make(chan ui.ControlReply, 1)
		go send(ui.ControlMsg{
			Op: req.Op, Importance: req.Importance, Person: req.Person, Statuses: req.Statuses,
			TaskID: req.TaskID, ParentID: req.ParentID, FolderID: req.FolderID, Shape: req.Shape, Screen: req.Screen,
			Deadline: time.Now().Add(controlWait), Reply: reply,
		})
		select {
		case r := <-reply:
			if r.Err != nil {
				return control.Reply{Error: r.Err.Error()}
			}
			return control.Reply{OK: true, Text: r.Text}
		case <-time.After(controlWait):
			return control.Reply{Error: "the interface is busy"}
		}
	}
}

var allEntities = []syncer.EntityKind{syncer.KindTasks, syncer.KindFolders, syncer.KindSpaces, syncer.KindContacts,
	syncer.KindWorkflows, syncer.KindComments, syncer.KindTimelogs, syncer.KindDependencies}

func entityNames(kinds []syncer.EntityKind) []string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	return names
}

// startControl binds the socket and logs instead of failing: an interface without control is still the interface.
// A socket another interface holds is the normal second window, anything else is a surprise worth a warning.
func startControl(path string, handle func(control.Request) control.Reply) *control.Listener {
	l, err := control.Listen(path, handle)
	if err != nil {
		if errors.Is(err, control.ErrInUse) {
			slog.Info("control socket in use by another interface, running without it", "path", path)
		} else {
			slog.Warn("control socket unavailable, running without it", "path", path, "error", err)
		}
		return nil
	}
	return l
}
