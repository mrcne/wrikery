package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// errCorruptRow marks an outbox row the engine cannot replay: an unknown kind or a payload that does not decode.
// Only a bug or a hand edited database produces one. It fails like a rejected write so it cannot block the queue,
// a transient error here would be retried first on every cycle forever.
var errCorruptRow = errors.New("sync: corrupt outbox row")

// drainOutbox sends the due writes oldest first.
// It stops at the first transient or auth failure, and the due query holds a later write on a task back
// while an earlier one backs off, so a backed off row is never overtaken by a later one on the same task.
// changed reports whether any row completed or failed, so the caller knows to emit events.
// Store failures after a successful send are returned like transient errors, the crash-retry ambiguity is accepted in the spec.
// start bounds whether another row is started, post is the context a create runs on, see sendRow.
func drainOutbox(start, post context.Context, c Client, st *store.Store, backoffBase, backoffCeil time.Duration) (bool, error) {
	// A command's deadline or a Ctrl-C can end start in the middle of a send.
	// The row must still be put back to pending with its backoff, or it stays in flight and nothing sends it again.
	bookkeeping := context.WithoutCancel(start)
	changed := false
	for {
		row, err := st.Outbox().NextDue(start, rfc3339(time.Now()))
		if errors.Is(err, store.ErrNotFound) {
			return changed, nil
		}
		if err != nil {
			return changed, err
		}
		if err := st.Outbox().MarkInflight(start, row.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// Discarded from the sync issues view between the read and the claim.
				continue
			}
			return changed, err
		}
		sendErr := sendRow(start, post, c, st, row)
		if sendErr == nil {
			changed = true
			continue
		}
		switch classify(sendErr) {
		case failPermanent:
			if err := st.Outbox().Fail(bookkeeping, row.ID, sendErr.Error()); err != nil {
				return changed, err
			}
			changed = true
			continue
		case failAuth:
			// Nothing is wrong with the row, only the token. It goes back to pending untouched so it is due
			// the moment a new token arrives. The drain stops at the first failure, so it is the only inflight row.
			if _, err := st.Outbox().ResetInflight(bookkeeping); err != nil {
				return changed, err
			}
			return changed, &SendError{Row: row, Err: sendErr}
		}
		next := rfc3339(time.Now().Add(backoff(row.Attempts, backoffBase, backoffCeil)))
		if err := st.Outbox().Reschedule(bookkeeping, row.ID, sendErr.Error(), next); err != nil {
			return changed, err
		}
		return changed, &SendError{Row: row, Err: sendErr}
	}
}

func sendRow(start, post context.Context, c Client, st *store.Store, row store.OutboxRow) error {
	// A create may have reached Wrike when its context ends, and the client does not retry a POST after a network or server error, so it runs on post.
	// Cancelling an update or a delete is harmless, they can be sent again, and the retry backoff must not outlive the budget.
	// Each create gets its own ceiling from the moment it is sent.
	// The ceiling ends the sleeps of rate limit retries, which are safe to cut because Wrike answers 429 instead of processing the request,
	// see https://developers.wrike.com/faq/.
	// A late retry attempt can still be cut on the wire, which is the crash ambiguity the drain accepts.
	ctx := start
	switch row.Kind {
	case store.KindTaskCreate, store.KindCommentCreate, store.KindTimelogCreate:
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(post, postCeiling)
		defer cancel()
	}
	// Once Wrike has answered the write has landed, so the local commit must not fail on a context that ended meanwhile.
	// A context error there would reschedule the row and the next drain would send it again.
	done := context.WithoutCancel(ctx)
	switch row.Kind {
	case store.KindTaskCreate:
		var p store.TaskCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		// The client refuses an empty title or folder with a plain error, which classify would retry forever.
		// Only a damaged row has one, the box never queues it.
		if p.Title == "" || row.EntityID == "" {
			return fmt.Errorf("%w %d: create without a title or a folder", errCorruptRow, row.ID)
		}
		task, err := c.CreateTask(ctx, row.EntityID, wrike.TaskCreate{Title: p.Title, Responsibles: p.Responsibles})
		if err != nil {
			return err
		}
		return st.Outbox().CompleteTaskCreate(done, row.ID, taskFromWrike(task))
	case store.KindTaskUpdate:
		var p store.TaskUpdatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		u := wrike.TaskUpdate{
			Title:              p.Title,
			Description:        p.Description,
			CustomStatusID:     p.CustomStatusID,
			Importance:         p.Importance,
			AddResponsibles:    p.AddResponsibles,
			RemoveResponsibles: p.RemoveResponsibles,
			AddParents:         p.AddParents,
			RemoveParents:      p.RemoveParents,
		}
		if p.Dates != nil {
			u.Dates = &wrike.TaskDates{Type: p.Dates.Type, Duration: p.Dates.Duration,
				Start: p.Dates.Start, Due: p.Dates.Due}
		}
		task, err := c.UpdateTask(ctx, row.EntityID, u)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteTask(done, row.ID, taskFromWrike(task))

	case store.KindCommentCreate:
		var p store.CommentCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		cm, err := c.CreateComment(ctx, row.EntityID, p.Text)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteComment(done, row.ID, commentFromWrike(cm))

	case store.KindTimelogCreate:
		var p store.TimelogCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		tl, err := c.CreateTimelog(ctx, row.EntityID, p.Hours, p.TrackedDate, p.Comment)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteTimelog(done, row.ID, timelogFromWrike(tl))

	case store.KindTimelogUpdate:
		var p store.TimelogUpdatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		tl, err := c.UpdateTimelog(ctx, row.EntityID, wrike.TimelogUpdate{
			Hours: p.Hours, TrackedDate: p.TrackedDate, Comment: p.Comment})
		if err != nil {
			return err
		}
		// CompleteTimelog drops the row and writes the server version in one transaction.
		// Its local id cleanup matches nothing for an update, the row never had a local timelog.
		return st.Outbox().CompleteTimelog(done, row.ID, timelogFromWrike(tl))

	case store.KindTimelogDelete:
		if err := c.DeleteTimelog(ctx, row.EntityID); err != nil {
			if isNotFound(err) {
				// Already gone on the server, which is what we wanted.
				return st.Outbox().Complete(done, row.ID)
			}
			return err
		}
		return st.Outbox().Complete(done, row.ID)
	}
	return fmt.Errorf("%w %d: unknown kind %q", errCorruptRow, row.ID, row.Kind)
}

// SendError is the failure the drain stopped at, with the row it belongs to as the drain read it.
// A pass sends the due rows oldest first and stops at the first one Wrike could not take,
// so the error a later row's command sees may belong to another write, and the command needs to tell.
// The row's EntityID is the task of an update, a comment or a time entry create,
// the folder of a task create, and the timelog of a time entry update or delete, Kind tells which.
// Err is the client's error, reachable through Unwrap, so classify and the callers branch on it as before.
type SendError struct {
	Row store.OutboxRow
	Err error
}

func (e *SendError) Error() string { return e.Err.Error() }
func (e *SendError) Unwrap() error { return e.Err }
