package syncer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mrcne/wrikery/internal/lockfile"
	"github.com/mrcne/wrikery/internal/store"
)

// ErrLocked is returned when another wrikery process held the sync lock until the deadline of the wait.
var ErrLocked = lockfile.ErrLocked

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
// The mutable fields below the channels are owned by whichever of Run, Once or Drain is running.
// Once and Drain are for an engine whose Run is not running, the two never overlap on one engine.
// Reach is the one exception: it runs on the caller's goroutine while Run runs,
// so it may touch only client, st, log and events, which New sets and nothing changes after.
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
	// unreached holds the referenced ids Wrike did not answer, skipped until the next manual cycle, see reachReferenced.
	unreached  map[string]bool
	failures   int
	lockWarned bool
}

func New(c Client, st *store.Store, cfg Config, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		client:    c,
		st:        st,
		cfg:       cfg.withDefaults(),
		log:       log,
		events:    make(chan Event, 16),
		unreached: map[string]bool{},
		refresh:   make(chan struct{}, 1),
		wake:      make(chan struct{}, 1),
		state:     StateIdle,
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

// Once runs one cycle and reports the state it ended in, the deletion sweep and a forced reference pull only when full is set.
// The reference data is still pulled on a fresh engine, lastReference is zero, which is what a command wants.
// A command uses it where the TUI uses Run.
// Like every drain pass it first puts the in-flight rows of a dead process back to pending.
// A create already on the wire is not cut off by a Ctrl-C, see Drain.
func (e *Engine) Once(ctx context.Context, full bool) (SyncState, error) {
	if err := e.ensureMeScope(ctx); err != nil {
		e.setState(stateAfter(err))
		return e.state, err
	}
	err := e.cycle(ctx, context.WithoutCancel(ctx), full)
	e.setState(stateAfter(err))
	return e.state, err
}

// postCeiling is three times the client's request timeout, the longest one create may take from the moment it is sent.
const postCeiling = 90 * time.Second

// Drain runs one outbox pass under the sync lock and returns the error that stopped it, if any.
// Like every drain pass it first puts the in-flight rows of a dead process back to pending.
// The budget bounds the wait for the lock and the start of each row, zero means only ctx does.
// A create that has started runs to Wrike's answer, neither the budget nor a cancelled ctx cuts it off, up to postCeiling.
// ErrLocked means the lock was held by another process until the budget or the deadline of ctx ran out and nothing was sent.
// A cancelled ctx returns context.Canceled instead.
// When the budget ends while a create is still running past it, the next NextDue fails on the ended context and the context error comes back, a caller that reads the row's state can ignore it.
// A row that finishes inside the budget gives nil.
func (e *Engine) Drain(ctx context.Context, budget time.Duration) error {
	start := ctx
	if budget > 0 {
		var cancel context.CancelFunc
		start, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	// A POST may have reached Wrike when the context ends, so cutting it off would leave a row the next drain sends again.
	// sendRow puts a ceiling on each create, see there.
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
	// The row claim in the store is atomic on its own, the lock is there for two things it cannot cover:
	// a startup reset while another process is mid-send, which would send that row twice,
	// and two drainers sending two rows on the same task out of order.
	lock, err := takeLock(start, e.cfg.LockFile, e.log, &e.lockWarned)
	if err != nil {
		return false, err
	}
	defer lock.Release()
	// Under the lock no other process is sending, so an inflight row is the leftover of a process that died mid-send.
	// Sending it again is the crash ambiguity the drain already accepts.
	if _, err := e.st.Outbox().ResetInflight(start); err != nil {
		return false, err
	}
	return drainOutbox(start, post, e.client, e.st, e.cfg.BackoffBase, e.cfg.BackoffCeil)
}

// takeLock waits for the sync lock until ctx ends, ErrLocked when another process held it all that time.
// When the file system cannot take the lock, a file owned by another user or flock unsupported on a network mount,
// it goes on without one and warns once: that is the behavior before the lock existed, which beats a sync that never runs.
func takeLock(ctx context.Context, path string, log *slog.Logger, warned *bool) (*lockfile.Lock, error) {
	lock, err := lockfile.Acquire(ctx, path)
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, ErrLocked) || ctx.Err() != nil {
		return nil, err
	}
	if !*warned {
		*warned = true
		log.Warn("sync lock unavailable, continuing without it", "error", err, "path", path)
	}
	return &lockfile.Lock{}, nil
}

// ResetInflight puts back to pending the rows a process that died mid-send left in flight, the first step of every drain pass,
// for a start that runs no drain, offline mode, so the changes screen can retry or discard them.
// It waits for the sync lock up to budget. A lock another process holds leaves the rows alone, that process may be sending one.
func ResetInflight(ctx context.Context, st *store.Store, lockFile string, budget time.Duration, log *slog.Logger) error {
	lctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	warned := false
	lock, err := takeLock(lctx, lockFile, log, &warned)
	if errors.Is(err, ErrLocked) {
		log.Info("sync lock held by another process, its rows in flight stay so", "path", lockFile)
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	_, err = st.Outbox().ResetInflight(ctx)
	return err
}

func (e *Engine) cycle(ctx, post context.Context, manual bool) error {
	e.setState(StateSyncing)

	changed, err := e.drain(ctx, post)
	if changed {
		e.emitOutboxCounts(ctx)
		e.emit(Event{Kind: EventStoreChanged,
			Entities: []EntityKind{KindTasks, KindComments, KindTimelogs}})
	}
	// A write Wrike did not answer stays first in line, which keeps the order after an outage.
	// The pulls then tell an outage from a write whose own request Wrike never answers: see the end of the cycle.
	var unanswered *SendError
	if err != nil {
		if !errors.As(err, &unanswered) || classify(unanswered.Err) != failUnavailable {
			return err
		}
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

	// The tasks behind the time entries and the relations, then a re-read of the reached tasks no scope poll covers:
	// all of them on a manual cycle, only the recently opened ones on a poll.
	if manual {
		clear(e.unreached)
	}
	// The stamped tasks are listed before the reach, so a task the reach brings in is not read a second time in the same cycle.
	openedSince := ""
	if !manual {
		openedSince = rfc3339(time.Now().Add(-e.cfg.ThreadWindow))
	}
	stamped, err := e.st.Tasks().ReachedIDs(ctx, openedSince)
	if err != nil {
		return err
	}
	// The time entries count inside the window the pull keeps current, the rows outside it outlive their tasks.
	timelogFrom, _ := store.TimelogWindow(time.Now())
	reached, err := reachReferenced(ctx, e.client, e.st, meID, timelogFrom, e.unreached, e.log, time.Now())
	if err != nil {
		return err
	}
	reread, err := rereadReached(ctx, e.client, e.st, stamped, e.log)
	if err != nil {
		return err
	}
	if reached || reread {
		e.emit(Event{Kind: EventStoreChanged, Entities: []EntityKind{KindTasks}})
	}

	// Last, so the opened task is served by its thread refresh before a first pull's catch-up of edges runs its batch.
	changedDeps, err := pullDependencies(ctx, e.client, e.st, e.log)
	if err != nil {
		return err
	}
	if changedDeps {
		e.emit(Event{Kind: EventStoreChanged, Entities: []EntityKind{KindDependencies}})
	}
	if unanswered != nil {
		// The pulls went through, so Wrike answers and the failure is the write's own, a payload it times out on for example.
		// It gets its own wait, the one a 500 earns, so the rest of the queue goes on instead of the whole sync stopping at it.
		row := unanswered.Row
		next := rfc3339(time.Now().Add(backoff(row.Attempts, e.cfg.BackoffBase, e.cfg.BackoffCeil)))
		if err := e.st.Outbox().Reschedule(ctx, row.ID, unanswered.Err.Error(), next); err != nil {
			return err
		}
		e.emitOutboxCounts(ctx)
		e.WakeOutbox()
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
