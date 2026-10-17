// Package control is the channel between a command and a running interface:
// a unix socket next to the database, one JSON request in, one JSON reply out, a connection per request.
// The interface listens through cmd/wrikery, a command calls, and internal/ui sees neither end.
package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// The operations a request names. The interface answers each from its update loop, except reload,
// which cmd/wrikery turns into the engine's own messages without asking the interface.
const (
	OpFilter  = "filter"
	OpSelect  = "select"
	OpFolder  = "folder"
	OpShape   = "shape"
	OpScreen  = "screen"
	OpRefresh = "refresh"
	OpReload  = "reload"
)

// Request is one operation with the fields it uses, the rest stay empty.
type Request struct {
	Op         string   `json:"op"`
	Importance []string `json:"importance,omitempty"`
	Person     string   `json:"person,omitempty"`
	Statuses   []string `json:"statuses,omitempty"`
	TaskID     string   `json:"task_id,omitempty"`
	ParentID   string   `json:"parent_id,omitempty"`
	FolderID   string   `json:"folder_id,omitempty"`
	Shape      string   `json:"shape,omitempty"`
	Screen     string   `json:"screen,omitempty"`
}

// Reply is what the interface says back: a line for the person on success, the reason on a refusal.
type Reply struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

var (
	ErrInUse      = errors.New("another wrikery interface is listening on the socket")
	ErrNotRunning = errors.New("no wrikery interface is running")
)

// Listener serves requests until Close.
type Listener struct {
	listener net.Listener
	path     string
	handle   func(Request) Reply
	wg       sync.WaitGroup
}

// Listen binds the socket and answers every request with handle, each on a goroutine of its own.
// A file that answers a connect belongs to a running interface, ErrInUse.
// A file that refuses or is missing is what a crash leaves behind, it is removed before the bind.
// The file ends up 0600, the data directory is 0700 already, so only the user's own processes reach it.
func Listen(path string, handle func(Request) Reply) (*Listener, error) {
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = conn.Close()
		return nil, ErrInUse
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("removing a stale control socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	l := &Listener{listener: ln, path: path, handle: handle}
	l.wg.Add(1)
	go l.serve()
	return l, nil
}

func (l *Listener) serve() {
	defer l.wg.Done()
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			// Close ends the accept loop, nothing else is expected on a unix socket.
			return
		}
		go l.one(conn)
	}
}

// one answers a single request. A client that sends nothing is dropped after the deadline, so a stuck caller cannot hold a goroutine.
func (l *Listener) one(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Reply{Error: "bad request: " + err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(l.handle(req))
}

// Close stops serving and removes the socket file, so the next interface binds without the stale check.
func (l *Listener) Close() error {
	err := l.listener.Close()
	l.wg.Wait()
	if rerr := os.Remove(l.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) && err == nil {
		err = rerr
	}
	return err
}

// Call sends one request to the interface at path and returns its reply.
// A missing or refused socket is ErrNotRunning, the interface's own refusal comes back as a Reply with OK false.
func Call(path string, req Request) (Reply, error) {
	return call(path, req, 5*time.Second)
}

func call(path string, req Request, wait time.Duration) (Reply, error) {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(wait))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Reply{}, fmt.Errorf("sending to the interface: %w", err)
	}
	var reply Reply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return Reply{}, fmt.Errorf("reading the interface's reply: %w", err)
	}
	return reply, nil
}

// Nudge tells a running interface to re-read the cache after a write, and says nothing when none runs:
// a script's normal case. The wait is short, a write command must not hang on a held interface.
func Nudge(path string) {
	_, _ = call(path, Request{Op: OpReload}, 2*time.Second)
}
