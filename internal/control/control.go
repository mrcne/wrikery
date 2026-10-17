// Package control is the channel between a command and a running interface:
// a unix socket next to the database, one JSON request in, one JSON reply out, a connection per request.
// The interface listens through cmd/wrikery, a command calls, and internal/ui sees neither end.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
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
// ParentIDs are the folders of a task to select, the interface opens the first one its sidebar has.
// Entities are the kinds a reload names, every kind when empty.
type Request struct {
	Op         string   `json:"op"`
	Importance []string `json:"importance,omitempty"`
	Person     string   `json:"person,omitempty"`
	Statuses   []string `json:"statuses,omitempty"`
	TaskID     string   `json:"task_id,omitempty"`
	ParentIDs  []string `json:"parent_ids,omitempty"`
	FolderID   string   `json:"folder_id,omitempty"`
	Shape      string   `json:"shape,omitempty"`
	Screen     string   `json:"screen,omitempty"`
	Entities   []string `json:"entities,omitempty"`
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

// BusyWait is how long the interface's side waits for the model's answer before it calls the interface busy.
// Call reads for longer than that, so the busy answer reaches the command instead of a timeout of its own.
const BusyWait = 5 * time.Second

// Listener serves requests until Close.
type Listener struct {
	listener net.Listener
	path     string
	handle   func(Request) Reply
	wg       sync.WaitGroup
}

// Listen binds the socket and answers every request with handle, each on a goroutine of its own.
// A file that answers a connect belongs to a running interface, ErrInUse.
// A file that refuses the connect is what a crash leaves behind, it is removed before the bind, a missing one needs nothing.
// Any other answer to the connect is left alone, a socket an interface still holds must not be deleted under it.
// The file ends up 0600, the data directory is 0700 already, so only the user's own processes reach it.
func Listen(path string, handle func(Request) Reply) (*Listener, error) {
	conn, err := net.DialTimeout("unix", path, time.Second)
	switch {
	case err == nil:
		_ = conn.Close()
		return nil, ErrInUse
	case errors.Is(err, syscall.ECONNREFUSED):
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("removing a stale control socket: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, fmt.Errorf("checking the control socket: %w", err)
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

// Close stops serving. The listener unlinks its own file on the way down, see net.UnixListener.SetUnlinkOnClose,
// and nothing is removed after that: an interface that bound the path meanwhile keeps its socket.
func (l *Listener) Close() error {
	err := l.listener.Close()
	l.wg.Wait()
	return err
}

// Call sends one request to the interface at path and returns its reply.
// A missing or refused socket is ErrNotRunning, the interface's own refusal comes back as a Reply with OK false.
// The context ends the call early, a Ctrl-C while the interface is held.
func Call(ctx context.Context, path string, req Request) (Reply, error) {
	return call(ctx, path, req, BusyWait+2*time.Second)
}

func call(ctx context.Context, path string, req Request, wait time.Duration) (Reply, error) {
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	defer func() { _ = conn.Close() }()
	// A read on a unix socket ignores the context, closing the connection is what ends it.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
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

// Nudge tells a running interface to re-read the kinds named, every kind when none is, and says nothing when none runs:
// a script's normal case. The wait is short, a write command must not hang on a held interface.
func Nudge(ctx context.Context, path string, kinds ...string) {
	_, _ = call(ctx, path, Request{Op: OpReload, Entities: kinds}, 2*time.Second)
}
