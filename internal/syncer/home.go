package syncer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mrcne/wrikery/internal/store"
)

// AnotherAccountError stops the engine before it sends or pulls anything:
// the credential belongs to another account, data center or user than the cache, and only the user's yes moves the cache.
type AnotherAccountError struct {
	Met store.Home
}

func (e *AnotherAccountError) Error() string {
	return fmt.Sprintf("sync: the credential is for %s on %s, the cache belongs to another account", homeName(e.Met), e.Met.Host)
}

// homeName is the account's name, or its id when the record has no name.
func homeName(h store.Home) string {
	if h.AccountName != "" {
		return h.AccountName
	}
	return h.AccountID
}

// MeetHome reads who the client's credential belongs to on host and how that stands to the cache's record.
// host must be the one that already answered for the credential, the account read answers 401 anywhere else.
// A cache from before the record adopts what it meets when that matches what it remembers.
func MeetHome(ctx context.Context, c Client, st *store.Store, host string) (store.Home, store.Meeting, error) {
	me, err := c.Me(ctx)
	if err != nil {
		return store.Home{}, 0, err
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
func checkQueue(ctx context.Context, c Client, st *store.Store, home store.Home) error {
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
			if r.TaskID != "" && !store.IsLocalID(r.TaskID) {
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
		if err != nil {
			return err
		}
		for _, f := range tree {
			inAccount[f.ID] = true
		}
		// A task can be created right in a space, whose root folder has the space's id.
		spaces, err := c.Spaces(ctx)
		if err != nil {
			return err
		}
		for _, sp := range spaces {
			inAccount[sp.ID] = true
		}
	}
	mine := map[string]bool{} // the entry ids the new user has on the tasks of queued entry edits
	for taskID := range entries {
		logs, err := c.TaskTimelogs(ctx, taskID)
		if err != nil && !isNotFound(err) {
			return err
		}
		for _, l := range logs {
			if l.UserID == home.ContactID {
				mine[l.ID] = true
			}
		}
	}

	notFound := fmt.Sprintf("not found in %s on %s", homeName(home), home.Host)
	different := fmt.Sprintf("a different task in %s on %s", homeName(home), home.Host)
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
		if r.TaskID != "" && !store.IsLocalID(r.TaskID) && !mine[r.EntityID] {
			return notFound
		}
	}
	return ""
}
