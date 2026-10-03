package syncer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mrcne/wrikery/internal/store"
)

type Config struct {
	PollInterval   time.Duration // pull cadence, default 60s
	ReferenceEvery time.Duration // reference data refresh, default 1h
	ThreadWindow   time.Duration // recently opened window, default 7 days
	ThreadLimit    int           // max tasks per thread refresh, default 50
	BackoffBase    time.Duration // outbox row backoff, default 2s
	BackoffCeil    time.Duration // outbox row backoff cap, default 5m
	ReconnectBase  time.Duration // engine wait after a failed cycle, default 1s
	ReconnectCeil  time.Duration // cap for that wait, default 2m
	LockFile       string        // the cross-process sync lock next to the database, empty means none
}

func (c Config) withDefaults() Config {
	if c.PollInterval <= 0 {
		c.PollInterval = 60 * time.Second
	}
	if c.ReferenceEvery <= 0 {
		c.ReferenceEvery = time.Hour
	}
	if c.ThreadWindow <= 0 {
		c.ThreadWindow = 7 * 24 * time.Hour
	}
	if c.ThreadLimit <= 0 {
		c.ThreadLimit = 50
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 2 * time.Second
	}
	if c.BackoffCeil <= 0 {
		c.BackoffCeil = 5 * time.Minute
	}
	if c.ReconnectBase <= 0 {
		c.ReconnectBase = time.Second
	}
	if c.ReconnectCeil <= 0 {
		c.ReconnectCeil = 2 * time.Minute
	}
	return c
}

// Engine keeps the store and the API in step from one goroutine.
// All the mutable fields below the channels are owned by Run and never touched from outside it.
type Engine struct {
	client Client
	st     *store.Store
	cfg    Config
	log    *slog.Logger

	events  chan Event
	refresh chan struct{}
	wake    chan struct{}

	state         SyncState
	meID          string
	lastReference time.Time
	failures      int
}

func New(c Client, st *store.Store, cfg Config, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		client:  c,
		st:      st,
		cfg:     cfg.withDefaults(),
		log:     log,
		events:  make(chan Event, 16),
		refresh: make(chan struct{}, 1),
		wake:    make(chan struct{}, 1),
		state:   StateIdle,
	}
}

func (e *Engine) Events() <-chan Event { return e.events }

// Refresh requests a full cycle now, reference data and the deletion sweep included.
// Safe from any goroutine, coalesces when one is queued.
func (e *Engine) Refresh() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

// WakeOutbox requests a cycle soon.
// The app calls it after every enqueue so a write does not wait for the next poll.
func (e *Engine) WakeOutbox() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Once runs one full cycle, reference pull and sweep included, and reports the state it ended in.
// A command uses it where the TUI uses Run.
// Like every drain pass it first puts the in-flight rows of a dead process back to pending.
// A create already on the wire is not cut off by a Ctrl-C, see Drain.
func (e *Engine) Once(ctx context.Context) (SyncState, error) {
	if err := e.ensureMeScope(ctx); err != nil {
		e.setState(stateAfter(err))
		return e.state, err
	}
	err := e.cycle(ctx, context.WithoutCancel(ctx), true)
	e.setState(stateAfter(err))
	return e.state, err
}

// Drain runs one outbox pass under the sync lock and returns the error that stopped it, if any.
// Like every drain pass it first puts the in-flight rows of a dead process back to pending.
// The budget bounds the wait for the lock and the start of each row, zero means only ctx does.
// A create that has started runs to Wrike's answer, neither the budget nor a cancelled ctx cuts it off.
// ErrLocked means the lock was held by another process until the budget or the deadline of ctx ran out and nothing was sent.
// A cancelled ctx returns context.Canceled instead.
func (e *Engine) Drain(ctx context.Context, budget time.Duration) error {
	start := ctx
	if budget > 0 {
		var cancel context.CancelFunc
		start, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	// A POST may have reached Wrike when the context ends, and the client never retries a POST for the same reason.
	// Cutting it off would leave a row the next drain sends again, so the client's own timeout is the only limit.
	post := context.WithoutCancel(ctx)
	_, err := e.drain(start, post)
	return err
}

// stateAfter is the state a cycle outcome puts the engine in.
// A rejected request means Wrike is reachable and said no, offline would send the user to check the network.
func stateAfter(err error) SyncState {
	switch {
	case err == nil:
		return StateIdle
	case classify(err) == failAuth:
		return StateAuthRequired
	case classify(err) == failPermanent:
		return StateFailed
	}
	return StateOffline
}

func (e *Engine) emit(ev Event) {
	select {
	case e.events <- ev:
	default:
		// The receiver lags. Events are hints, the store holds the truth, so dropping is safer than blocking the engine.
	}
}

func (e *Engine) setState(s SyncState) {
	if e.state == s {
		return
	}
	e.state = s
	e.emit(Event{Kind: EventStateChanged, State: s})
}

func (e *Engine) emitOutboxCounts(ctx context.Context) {
	pending, failed, err := e.st.Outbox().Counts(ctx)
	if err != nil {
		e.log.Warn("outbox counts", "error", err)
		return
	}
	e.emit(Event{Kind: EventOutboxChanged, Pending: pending, Failed: failed})
}

// Run drives the loop until ctx ends.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.startupLocal(ctx); err != nil {
		return err
	}
	manual := true
	for {
		err := e.cycle(ctx, ctx, manual)
		manual = false
		var wait time.Duration
		state := stateAfter(err)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err == nil:
			e.failures = 0
			wait = e.cfg.PollInterval
		case state == StateAuthRequired:
			e.failures = 0
		default:
			e.failures++
			e.log.Warn("sync cycle failed", "error", err)
			wait = backoff(e.failures-1, e.cfg.ReconnectBase, e.cfg.ReconnectCeil)
		}
		e.setState(state)
		if e.state == StateAuthRequired {
			// Only a new token can help, so only a manual refresh or the end of the program wakes the engine.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-e.refresh:
				manual = true
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		case <-e.refresh:
			manual = true
		case <-e.wake:
		}
	}
}

// startupLocal prepares the store before the first network call: the me scope always exists,
// and the UI gets its first outbox counts.
// It resets nothing, the first drain pass puts crashed inflight rows back to pending under the sync lock.
//
// It does not also emit a state event: e.state already defaults to Idle, and setState only emits on a real transition.
// Broadcasting that same Idle value here as well would be indistinguishable on the Events channel from the Idle
// that setState emits once the first cycle actually finishes, which is what callers wait on to know a sync completed.
func (e *Engine) startupLocal(ctx context.Context) error {
	if err := e.ensureMeScope(ctx); err != nil {
		return err
	}
	e.emitOutboxCounts(ctx)
	return nil
}

// ensureMeScope makes sure the My tasks scope exists and is followed, without it a cycle pulls no task.
func (e *Engine) ensureMeScope(ctx context.Context) error {
	return e.st.Scopes().Upsert(ctx, store.Scope{
		ID: store.ScopeKindMe, Kind: store.ScopeKindMe, Title: "My tasks", Followed: true,
	})
}

// drain runs one outbox pass under the sync lock, the pulls that follow in a cycle run without it.
// start bounds the wait for the lock and whether another row is started, post is the context of a send that may have reached Wrike.
func (e *Engine) drain(start, post context.Context) (bool, error) {
	lock, err := acquire(start, e.cfg.LockFile)
	if err != nil {
		return false, err
	}
	defer lock.release()
	// Under the lock no other process is sending, so an inflight row is the leftover of a process that died mid-send.
	// Sending it again is the crash ambiguity the drain already accepts.
	if _, err := e.st.Outbox().ResetInflight(start); err != nil {
		return false, err
	}
	return drainOutbox(start, post, e.client, e.st, e.cfg.BackoffBase, e.cfg.BackoffCeil)
}

func (e *Engine) cycle(ctx, post context.Context, manual bool) error {
	e.setState(StateSyncing)

	changed, err := e.drain(ctx, post)
	if changed {
		e.emitOutboxCounts(ctx)
		e.emit(Event{Kind: EventStoreChanged,
			Entities: []EntityKind{KindTasks, KindComments, KindTimelogs}})
	}
	if err != nil {
		return err
	}

	meID, err := e.ensureMe(ctx)
	if err != nil {
		return err
	}

	if manual || e.lastReference.IsZero() || time.Since(e.lastReference) >= e.cfg.ReferenceEvery {
		if err := pullReference(ctx, e.client, e.st); err != nil {
			return err
		}
		e.lastReference = time.Now()
		e.emit(Event{Kind: EventStoreChanged,
			Entities: []EntityKind{KindFolders, KindSpaces, KindContacts, KindWorkflows}})
	}

	scopes, err := e.st.Scopes().Followed(ctx)
	if err != nil {
		return err
	}
	partial := false
	pulled := 0
	// Scopes pulled from scratch this cycle. The sweep reuses their ids instead of crawling them again.
	full := map[string][]string{}
	for _, sc := range scopes {
		ids, err := pullScope(ctx, e.client, e.st, sc, meID)
		if err != nil {
			if classify(err) != failPermanent {
				return err
			}
			// Access revoked or the folder deleted. Waiting will not help, and one scope must not stop the others.
			e.log.Warn("scope pull rejected", "scope", sc.ID, "error", err)
			partial = true
			continue
		}
		pulled += len(ids)
		if sc.Cursor == "" {
			full[sc.ID] = ids
		}
	}
	if pulled > 0 {
		e.emit(Event{Kind: EventStoreChanged, Entities: []EntityKind{KindTasks}})
	}

	// The sweep prunes with the union of every scope. Without the rejected one it would delete that scope's tasks.
	if manual && !partial {
		if err := sweep(ctx, e.client, e.st, scopes, meID, full); err != nil {
			return err
		}
	}

	changedLogs, err := pullMyTimelogs(ctx, e.client, e.st, meID, time.Now())
	if err != nil {
		return err
	}
	if changedLogs {
		e.emit(Event{Kind: EventStoreChanged, Entities: []EntityKind{KindTimelogs}})
	}

	touched, err := refreshThreads(ctx, e.client, e.st, e.log, e.cfg.ThreadWindow, e.cfg.ThreadLimit)
	if err != nil {
		return err
	}
	if len(touched) > 0 {
		e.emit(Event{Kind: EventStoreChanged, Entities: touched})
	}
	return nil
}

func (e *Engine) ensureMe(ctx context.Context) (string, error) {
	if e.meID != "" {
		return e.meID, nil
	}
	id, err := e.st.GetMeta(ctx, store.MetaKeyMe)
	if err == nil {
		e.meID = id
		return id, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	me, err := e.client.Me(ctx)
	if err != nil {
		return "", err
	}
	if err := e.st.SetMeta(ctx, store.MetaKeyMe, me.ID); err != nil {
		return "", err
	}
	e.meID = me.ID
	return me.ID, nil
}
