package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// AnotherAccountError stops the engine before it sends or pulls anything:
// the credential belongs to another account, data center or user than the cache, and only the user's yes moves the cache.
type AnotherAccountError struct {
	Met store.Home
}

func (e *AnotherAccountError) Error() string {
	return fmt.Sprintf("sync: the credential is for %s on %s, the cache belongs to another account", e.Met.Name(), e.Met.Host)
}

// ErrAccountUnchecked answers a fetch before the engine compared its credential's account with the cache's,
// a fetch then could pull a task of another account into the cache.
var ErrAccountUnchecked = errors.New("sync: the account is not compared yet")

// MeetHome reads who the client's credential belongs to on host and how that stands to the cache's record.
// host must be the one that already answered for the credential, the account read answers 401 anywhere else.
// me is the person when the caller has read it already, a sign in has, an empty one is read here.
// A cache from before the record adopts what it meets when that matches what it remembers,
// and when it holds folders, only when they include the account's root: one person has one contact id in every account
// it can reach, so the root folder is what tells a person's other account apart.
func MeetHome(ctx context.Context, c Client, st *store.Store, host string, me wrike.Contact) (store.Home, store.Meeting, error) {
	if me.ID == "" {
		var err error
		if me, err = c.Me(ctx); err != nil {
			return store.Home{}, 0, err
		}
	}
	account, err := c.Account(ctx)
	if err != nil {
		return store.Home{}, 0, err
	}
	met := store.Home{AccountID: account.ID, AccountName: account.Name, Host: host, ContactID: me.ID}
	record, err := st.Home(ctx)
	if err != nil {
		return store.Home{}, 0, err
	}
	m := record.Meet(met)
	if m == store.MeetAdopt && account.RootFolderID != "" {
		known, cached, err := st.RootFolder(ctx, account.RootFolderID)
		if err != nil {
			return store.Home{}, 0, err
		}
		if cached && !known {
			m = store.MeetAsk
		}
	}
	if m == store.MeetAdopt {
		if err := st.AdoptHome(ctx, met); err != nil {
			return store.Home{}, 0, err
		}
	}
	return met, m, nil
}

// checkQueue runs once after a move, under the sync lock, before anything of the queue goes to the new account.
// It holds every queued or failed row whose task, folder or time entry the new account does not have,
// and every row whose task carries a title the cache never knew for it:
// if the move renumbered anything, an old id must not send a comment or a time entry to another task.
// A read error returns before anything is held, the marker stays and the next pass tries again.
// A read Wrike answers for good, a 404 or a 400 for an id it cannot parse among them, is no answer: its rows are held.
func checkQueue(ctx context.Context, c Client, st *store.Store, home store.Home, log *slog.Logger) error {
	// rejected logs a read Wrike refused for good and says so, a read that may pass later stops the check and keeps the marker.
	rejected := func(what string, err error) bool {
		if err == nil || classify(err) != failPermanent {
			return false
		}
		log.Warn("queue check after a move, the read was refused, its rows are held", "read", what, "error", err)
		return true
	}
	all, err := st.Outbox().ListChanges(ctx)
	if err != nil {
		return err
	}
	var rows []store.OutboxRow
	for _, r := range all {
		if r.State == store.StatePending || r.State == store.StateFailed {
			rows = append(rows, r)
		}
	}
	titles := map[string]map[string]bool{} // the titles the cache knows per task the queue names
	folders := map[string]bool{}
	entries := map[string]bool{} // the tasks of queued time entry edits and deletes
	for _, r := range rows {
		switch r.Kind {
		case store.KindTaskUpdate, store.KindCommentCreate, store.KindTimelogCreate:
			if store.IsLocalID(r.EntityID) {
				continue
			}
			known := titles[r.EntityID]
			if known == nil {
				known = map[string]bool{}
				if t, err := st.Tasks().Get(ctx, r.EntityID); err == nil {
					known[t.Title] = true
				}
				titles[r.EntityID] = known
			}
			if r.Kind == store.KindTaskUpdate {
				var p, b store.TaskUpdatePayload
				_ = json.Unmarshal(r.Payload, &p)
				_ = json.Unmarshal(r.Before, &b)
				for _, title := range []string{p.Title, b.Title} {
					if title != "" {
						known[title] = true
					}
				}
				for _, id := range append(p.AddParents, p.RemoveParents...) {
					folders[id] = true
				}
			}
		case store.KindTaskCreate:
			folders[r.EntityID] = true
		case store.KindTimelogUpdate, store.KindTimelogDelete:
			// An entry logged offline has a local id, the edit waits for its create, which is checked through its task.
			if r.TaskID != "" && !store.IsLocalID(r.TaskID) && !store.IsLocalID(r.EntityID) {
				entries[r.TaskID] = true
			}
		}
	}

	answered := map[string]string{}
	var ids []string
	for id := range titles {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += reachBatch {
		tasks, err := c.TasksByIDs(ctx, ids[start:min(start+reachBatch, len(ids))], nil)
		// Wrike answers 404 to a read it knows none of the ids of, and 400 invalid_request to one it cannot parse,
		// the case of an account whose ids changed in a move: no task of the batch answered, its rows are held.
		if rejected("tasks", err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.Scope != scopeRecycleBin {
				answered[t.ID] = t.Title
			}
		}
	}
	inAccount := map[string]bool{}
	if len(folders) > 0 {
		tree, err := c.FolderTree(ctx)
		if err != nil && !rejected("folders", err) {
			return err
		}
		for _, f := range tree {
			inAccount[f.ID] = true
		}
		// A task can be created right in a space, whose root folder has the space's id.
		spaces, err := c.Spaces(ctx)
		if err != nil && !rejected("spaces", err) {
			return err
		}
		for _, sp := range spaces {
			inAccount[sp.ID] = true
		}
	}
	mine := map[string]bool{} // the entry ids the new user has on the tasks of queued entry edits
	for taskID := range entries {
		logs, err := c.TaskTimelogs(ctx, taskID)
		if err != nil && !rejected("time entries", err) {
			return err
		}
		for _, l := range logs {
			if l.UserID == home.ContactID {
				mine[l.ID] = true
			}
		}
	}

	notFound := fmt.Sprintf("%snot found in %s on %s", store.MovePrefix, home.Name(), home.Host)
	different := fmt.Sprintf("%sa different task in %s on %s", store.MovePrefix, home.Name(), home.Host)
	for _, r := range rows {
		reason := rowReason(r, titles, answered, inAccount, mine, notFound, different)
		if reason == "" {
			continue
		}
		if err := st.Outbox().Hold(ctx, r.ID, reason, r.Before); err != nil {
			return err
		}
	}
	return nil
}

// rowReason is why a row is held after a move, empty when it may go.
func rowReason(r store.OutboxRow, titles map[string]map[string]bool, answered map[string]string,
	inAccount, mine map[string]bool, notFound, different string) string {
	switch r.Kind {
	case store.KindTaskUpdate, store.KindCommentCreate, store.KindTimelogCreate:
		if store.IsLocalID(r.EntityID) {
			return ""
		}
		title, ok := answered[r.EntityID]
		if !ok {
			return notFound
		}
		if !titles[r.EntityID][title] {
			return different
		}
		if r.Kind == store.KindTaskUpdate {
			var p store.TaskUpdatePayload
			_ = json.Unmarshal(r.Payload, &p)
			for _, id := range append(p.AddParents, p.RemoveParents...) {
				if !inAccount[id] {
					return notFound
				}
			}
		}
	case store.KindTaskCreate:
		if !inAccount[r.EntityID] {
			return notFound
		}
	case store.KindTimelogUpdate, store.KindTimelogDelete:
		if r.TaskID != "" && !store.IsLocalID(r.TaskID) && !store.IsLocalID(r.EntityID) && !mine[r.EntityID] {
			return notFound
		}
	}
	return ""
}
