package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
)

// Home is what a cache was built from: the account, the host its credential was verified on and the user.
// A sign in or a first cycle that meets anything else must not send the queue there without the user's yes.
type Home struct {
	AccountID   string
	AccountName string
	Host        string
	ContactID   string
}

// Meeting is how a met account stands to the record.
type Meeting int

const (
	MeetSame  Meeting = iota // the record's account, host and user, nothing to do
	MeetAdopt                // a cache from before the record existed meets what it remembers, the record is written without asking
	MeetAsk                  // anything else, the user decides
)

// Meet compares the record with what a sign in or a first cycle met.
// The name is left out, an account can be renamed.
// A record without an account id is a cache from before the record existed:
// it adopts only when the host and the user it remembers, each where it remembers one, are the met ones.
func (h Home) Meet(met Home) Meeting {
	if h.AccountID != "" {
		if h.AccountID == met.AccountID && h.Host == met.Host && h.ContactID == met.ContactID {
			return MeetSame
		}
		return MeetAsk
	}
	if (h.Host == "" || h.Host == met.Host) && (h.ContactID == "" || h.ContactID == met.ContactID) {
		return MeetAdopt
	}
	return MeetAsk
}

// Home reads the record.
// A cache from before the record falls back to the host the probe remembered, and then to the host of a cached task's link:
// a cache built with host in the config never wrote api_host, and its links still name the data center the rows came from.
func (s *Store) Home(ctx context.Context) (Home, error) {
	var h Home
	for key, field := range map[string]*string{
		MetaKeyAccountID: &h.AccountID, MetaKeyAccountName: &h.AccountName, MetaKeyHomeHost: &h.Host, MetaKeyMe: &h.ContactID,
	} {
		v, err := s.GetMeta(ctx, key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Home{}, err
		}
		*field = v
	}
	if h.Host != "" {
		return h, nil
	}
	host, err := s.GetMeta(ctx, MetaKeyHost)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Home{}, err
	}
	if host != "" {
		h.Host = host
		return h, nil
	}
	var link string
	err = s.reader.QueryRowContext(ctx, `SELECT permalink FROM tasks WHERE permalink <> '' LIMIT 1`).Scan(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return h, nil
	}
	if err != nil {
		return Home{}, err
	}
	if u, err := url.Parse(link); err == nil {
		h.Host = u.Hostname()
	}
	return h, nil
}

// AdoptHome writes the record of a cache from before it existed, or after a sign in to the same account.
func (s *Store) AdoptHome(ctx context.Context, h Home) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeHomeTx(ctx, tx, h); err != nil {
		return err
	}
	return tx.Commit()
}

// MoveHome is the user's yes to another account or data center, in one transaction:
// the record, every followed scope back to a pull from scratch, the pins when the account changed,
// the old person's time entries when the person changed, and the marker that makes the next drain pass check the queue.
// The caller stores the new credential after it, so a failure in between leaves a record a next sign in meets as the same.
func (s *Store) MoveHome(ctx context.Context, to Home) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	oldAccount, err := metaTx(ctx, tx, MetaKeyAccountID)
	if err != nil {
		return err
	}
	oldContact, err := metaTx(ctx, tx, MetaKeyMe)
	if err != nil {
		return err
	}
	if err := writeHomeTx(ctx, tx, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scopes SET cursor = NULL, last_synced_at = NULL`); err != nil {
		return err
	}
	// A folder id may name nothing in another account.
	if oldAccount != "" && oldAccount != to.AccountID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pins`); err != nil {
			return err
		}
	}
	// The timesheet reads the new person and the pull replaces only that person's window, the old rows would stay for good.
	if oldContact != "" && oldContact != to.ContactID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM timelogs WHERE user_id = ?`, oldContact); err != nil {
			return err
		}
	}
	if err := setMetaTx(ctx, tx, MetaKeySwitchPending, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

// SwitchPending says whether a move waits for its queue check.
func (s *Store) SwitchPending(ctx context.Context) (bool, error) {
	v, err := s.GetMeta(ctx, MetaKeySwitchPending)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return v == "1", err
}

// ClearSwitch ends the pending check once a drain pass ran it through.
func (s *Store) ClearSwitch(ctx context.Context) error {
	_, err := s.writer.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, MetaKeySwitchPending)
	return err
}

func writeHomeTx(ctx context.Context, tx *sql.Tx, h Home) error {
	for key, value := range map[string]string{
		MetaKeyAccountID: h.AccountID, MetaKeyAccountName: h.AccountName, MetaKeyHomeHost: h.Host, MetaKeyMe: h.ContactID,
	} {
		// The engine takes any stored contact id as the user, an empty value is never written.
		if value == "" {
			continue
		}
		if err := setMetaTx(ctx, tx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func metaTx(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var v string
	err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func setMetaTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
