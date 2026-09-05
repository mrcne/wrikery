package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// drainOutbox sends queued writes oldest first.
// It stops at the first transient or auth failure so a backed off row can never be overtaken by a later one,
// which the conflict policy depends on. changed reports whether any row completed or failed, so the caller knows to emit events.
// Store failures after a successful send are returned like transient errors, the crash-retry ambiguity is accepted in the spec.
func drainOutbox(ctx context.Context, c Client, st *store.Store, backoffBase, backoffCeil time.Duration) (bool, error) {
	changed := false
	for {
		row, err := st.Outbox().NextDue(ctx, time.Now().UTC().Format(time.RFC3339))
		if errors.Is(err, store.ErrNotFound) {
			return changed, nil
		}
		if err != nil {
			return changed, err
		}
		if err := st.Outbox().MarkInflight(ctx, row.ID); err != nil {
			return changed, err
		}
		sendErr := sendRow(ctx, c, st, row)
		if sendErr == nil {
			changed = true
			continue
		}
		if classify(sendErr) == failPermanent {
			if err := st.Outbox().Fail(ctx, row.ID, sendErr.Error()); err != nil {
				return changed, err
			}
			changed = true
			continue
		}
		next := time.Now().UTC().Add(backoff(row.Attempts, backoffBase, backoffCeil)).Format(time.RFC3339)
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
			return fmt.Errorf("sync: outbox row %d payload: %w", row.ID, err)
		}
		u := wrike.TaskUpdate{
			Title:              p.Title,
			CustomStatusID:     p.CustomStatusID,
			AddResponsibles:    p.AddResponsibles,
			RemoveResponsibles: p.RemoveResponsibles,
		}
		if p.Dates != nil {
			u.Dates = &wrike.TaskDates{Type: p.Dates.Type, Duration: p.Dates.Duration,
				Start: p.Dates.Start, Due: p.Dates.Due}
		}
		task, err := c.UpdateTask(ctx, row.EntityID, u)
		if err != nil {
			return err
		}
		if err := st.Outbox().Complete(ctx, row.ID); err != nil {
			return err
		}
		return st.Tasks().Upsert(ctx, []store.Task{taskFromWrike(task)})

	case store.KindCommentCreate:
		var p store.CommentCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("sync: outbox row %d payload: %w", row.ID, err)
		}
		cm, err := c.CreateComment(ctx, row.EntityID, p.Text)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteComment(ctx, row.ID, commentFromWrike(cm))

	case store.KindTimelogCreate:
		var p store.TimelogCreatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("sync: outbox row %d payload: %w", row.ID, err)
		}
		tl, err := c.CreateTimelog(ctx, row.EntityID, p.Hours, p.TrackedDate, p.Comment)
		if err != nil {
			return err
		}
		return st.Outbox().CompleteTimelog(ctx, row.ID, timelogFromWrike(tl))

	case store.KindTimelogUpdate:
		var p store.TimelogUpdatePayload
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("sync: outbox row %d payload: %w", row.ID, err)
		}
		tl, err := c.UpdateTimelog(ctx, row.EntityID, wrike.TimelogUpdate{
			Hours: p.Hours, TrackedDate: p.TrackedDate, Comment: p.Comment})
		if err != nil {
			return err
		}
		if err := st.Outbox().Complete(ctx, row.ID); err != nil {
			return err
		}
		return st.Timelogs().Upsert(ctx, []store.Timelog{timelogFromWrike(tl)})

	case store.KindTimelogDelete:
		if err := c.DeleteTimelog(ctx, row.EntityID); err != nil {
			var apiErr *wrike.APIError
			if errors.As(err, &apiErr) && apiErr.IsNotFound() {
				// Already gone on the server, which is what we wanted.
				return st.Outbox().Complete(ctx, row.ID)
			}
			return err
		}
		return st.Outbox().Complete(ctx, row.ID)
	}
	return fmt.Errorf("sync: unknown outbox kind %s", row.Kind)
}
