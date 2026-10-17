package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/internal/ui"
)

// socketPath is the control socket, next to the database like the sync lock so two data directories never share one.
// A unix socket path has room for 104 bytes on macOS and 108 on Linux, so a long data directory gets a hashed name
// in a directory of the user's own under /tmp instead, computed the same way by the interface and by a command.
// An empty result means no socket: the interface runs without control and a command finds no interface.
func socketPath(p config.Paths) string {
	path := filepath.Join(filepath.Dir(p.DBFile), "wrikery.sock")
	if len(path) <= 100 {
		return path
	}
	dir, err := userTempDir()
	if err != nil {
		slog.Warn("no directory for the control socket, running without it", "error", err)
		return ""
	}
	sum := sha256.Sum256([]byte(p.DBFile))
	return filepath.Join(dir, hex.EncodeToString(sum[:4])+".sock")
}

// userTempDir is /tmp/wrikery-<uid>, made 0700 and checked to be the user's own directory,
// so another user of the machine cannot plant a socket under a name a command would talk to.
// It is /tmp itself and not os.TempDir(): $TMPDIR differs between a terminal and a cron job on macOS,
// and both sides have to compute one path.
func userTempDir() (string, error) {
	dir := filepath.Join("/tmp", fmt.Sprintf("wrikery-%d", os.Getuid()))
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ok || int(st.Uid) != os.Getuid() {
		return "", fmt.Errorf("%s is not a directory of this user with mode 0700", dir)
	}
	return dir, nil
}

// demoSocketPath is where a demo listens: beside the real database, in the user's own data directory,
// one fixed name since a demo's own data directory is a throwaway nobody can find.
func demoSocketPath(p config.Paths) string {
	return filepath.Join(filepath.Dir(p.DBFile), "wrikery-demo.sock")
}

// controlWait is how long a request waits for the interface's reply before it is called busy, a variable for the tests.
var controlWait = control.BusyWait

// controlHandler answers the socket: a reload becomes the two messages the engine forwarder sends after a cycle,
// everything else goes to the interface as a ControlMsg and comes back through its reply channel.
// Every send runs on a goroutine of its own because Program.Send blocks while the program is held by an external editor,
// and the handler must answer the command inside the wait all the same, a reload at once.
func controlHandler(send func(tea.Msg), counts func() (pending, failed int, err error)) func(control.Request) control.Reply {
	return func(req control.Request) control.Reply {
		if req.Op == control.OpReload {
			kinds := req.Entities
			if len(kinds) == 0 {
				kinds = entityNames(syncer.AllEntities)
			}
			go func() {
				send(ui.StoreChangedMsg{Entities: kinds})
				if pending, failed, err := counts(); err == nil {
					send(ui.OutboxChangedMsg{Pending: pending, Failed: failed})
				}
			}()
			return control.Reply{OK: true}
		}
		reply := make(chan ui.ControlReply, 1)
		go send(ui.ControlMsg{
			Op: req.Op, Importance: req.Importance, Person: req.Person, Statuses: req.Statuses,
			TaskID: req.TaskID, ParentIDs: req.ParentIDs, FolderID: req.FolderID, Shape: req.Shape, Screen: req.Screen,
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
	if path == "" {
		return nil
	}
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
