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

// drainOutbox sends queued writes oldest first.
// It stops at the first transient or auth failure so a backed off row can never be overtaken by a later one,
// which the conflict policy depends on. changed reports whether any row completed or failed, so the caller knows to emit events.
// Store failures after a successful send are returned like transient errors, the crash-retry ambiguity is accepted in the spec.
func drainOutbox(ctx context.Context, c Client, st *store.Store, backoffBase, backoffCeil time.Duration) (bool, error) {
	changed := false
	for {
		row, err := st.Outbox().NextDue(ctx, rfc3339(time.Now()))
		if errors.Is(err, store.ErrNotFound) {
			return changed, nil
		}
		if err != nil {
			return changed, err
		}
		if err := st.Outbox().MarkInflight(ctx, row.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// Discarded from the sync issues view between the read and the claim.
				continue
			}
			return changed, err
		}
		sendErr := sendRow(ctx, c, st, row)
		if sendErr == nil {
			changed = true
			continue
		}
		switch classify(sendErr) {
		case failPermanent:
			if err := st.Outbox().Fail(ctx, row.ID, sendErr.Error()); err != nil {
				return changed, err
			}
			changed = true
			continue
		case failAuth:
			// Nothing is wrong with the row, only the token. It goes back to pending untouched so it is due
			// the moment a new token arrives. The drain stops at the first failure, so it is the only inflight row.
			if _, err := st.Outbox().ResetInflight(ctx); err != nil {
				return changed, err
			}
			return changed, sendErr
		}
		next := rfc3339(time.Now().Add(backoff(row.Attempts, backoffBase, backoffCeil)))
		if err := st.Outbox().Reschedule(ctx, row.ID, sendErr.Error(), next); err != nil {
			return changed, err
		}
		return changed, sendErr
	}
}

func sendRow(ctx context.Context, c Client, st *store.Store, row store.OutboxRow) error {
	switch row.Kind {
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
		return st.Outbox().CompleteTask(ctx, row.ID, taskFromWrike(task))

	case store.KindCommentCreate:
		var p store.CommentCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		cm, err := c.CreateComment(ctx, row.EntityID, p.Text)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteComment(ctx, row.ID, commentFromWrike(cm))

	case store.KindTimelogCreate:
		var p store.TimelogCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("%w %d: payload: %w", errCorruptRow, row.ID, err)
		}
		tl, err := c.CreateTimelog(ctx, row.EntityID, p.Hours, p.TrackedDate, p.Comment)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteTimelog(ctx, row.ID, timelogFromWrike(tl))

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
		return st.Outbox().CompleteTimelog(ctx, row.ID, timelogFromWrike(tl))

	case store.KindTimelogDelete:
		if err := c.DeleteTimelog(ctx, row.EntityID); err != nil {
			if isNotFound(err) {
				// Already gone on the server, which is what we wanted.
				return st.Outbox().Complete(ctx, row.ID)
			}
			return err
		}
		return st.Outbox().Complete(ctx, row.ID)
	}
	return fmt.Errorf("%w %d: unknown kind %q", errCorruptRow, row.ID, row.Kind)
}
