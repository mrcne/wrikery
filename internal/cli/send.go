package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mrcne/wrikery/internal/control"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/syncer"
	"github.com/mrcne/wrikery/pkg/wrike"
)

type outcome int

const (
	queued    outcome = iota // the row is still pending, Wrike was not reached inside the deadline
	sent                     // the row completed, the change is on Wrike
	rejected                 // Wrike refused the write, the row is failed and listed on the changes screen
	blocked                  // the token was rejected, the row is pending but only a new sign in in the interface sends it
	discarded                // the row was discarded in the interface while the command waited, nothing goes out
	held                     // someone changed a field of the edit on Wrike since it was queued, the row is failed until a person sends it anyway or drops it
)

func (o outcome) String() string {
	return [...]string{"queued", "sent", "rejected", "blocked", "discarded", "held"}[o]
}

// preflight checks the sign in and resolves the host before anything is queued,
// so a failure here never hides a row behind an error exit.
// It is the only place that reads the credentials, the commands that only read the cache never touch the keychain.
// unchecked is the reason the account could not be compared with the cache's, Wrike did not answer:
// a write is then queued and not sent, the next command or the interface's first cycle compares before anything goes out.
func preflight(ctx context.Context, env Env) (host string, code int, unchecked error) {
	if env.Offline {
		// Nothing is sent, so nothing needs the credentials or the host.
		return "", exitOK, nil
	}
	ok, err := env.SignedIn()
	if err != nil {
		return "", fail(env, err), nil
	}
	if !ok {
		return "", fail(env, errors.New("not signed in, run wrikery once to sign in")), nil
	}
	host, err = env.Host(ctx)
	if err != nil {
		return "", fail(env, fmt.Errorf("could not reach Wrike: %w", err)), nil
	}
	code, unchecked = checkHome(ctx, env, host)
	if code != exitOK {
		return "", code, nil
	}
	return host, exitOK, unchecked
}

// checkHome refuses a command whose credential belongs to another account, data center or person than the cache,
// before anything is queued or fetched: only the interface asks the user, a command has nobody to ask.
// A cache with a record was checked by the sign in that stored its credential, so only a cache from before the record
// and a WRIKERY_TOKEN, which no sign in saw, read the account.
// A rejected token goes on, its own send reports it blocked as it always did, and a rejected token sends nothing anywhere.
func checkHome(ctx context.Context, env Env, host string) (code int, unchecked error) {
	// A local read, a Ctrl-C before it must leave the decision to the network part, which reports an interrupt.
	record, err := env.Store.Home(context.WithoutCancel(ctx))
	if err != nil {
		return fail(env, err), nil
	}
	if record.AccountID != "" && !env.TokenFromEnv {
		return exitOK, nil
	}
	// The write deadline bounds the read, a Wrike that does not answer must not hold a command for the client's whole retry budget.
	if env.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, env.Deadline)
		defer cancel()
	}
	met, m, err := syncer.MeetHome(ctx, env.Client(host), env.Store, host, wrike.Contact{})
	var apiErr *wrike.APIError
	switch {
	case errors.As(err, &apiErr) && (apiErr.IsAuth() || apiErr.IsWrongHost()):
		return exitOK, nil
	case err != nil:
		return exitOK, err
	case m != store.MeetAsk:
		return exitOK, nil
	}
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: this sign in is for %s on %s, the cache belongs to %s on %s, open wrikery to decide\n",
		met.Name(), met.Host, record.Name(), record.Host)
	return exitError, nil
}

// uncheckedReason words an account read Wrike did not answer the way a send that did not go out is worded.
func uncheckedReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Wrike did not answer in time"
	}
	return err.Error()
}

// send runs one drain pass for the row just queued and reads its fate back from the outbox.
// The pass sends every due row older than this one first, the order the engine keeps.
// The deadline bounds the wait for the lock and the start of a write, a create already sent runs to Wrike's answer.
// The drain error is the reason behind a queued outcome or the sign of a rejected token, otherwise the row's state is the truth.
func send(ctx context.Context, env Env, host string, rowID int64) (outcome, string, error) {
	eng := syncer.New(env.Client(host), env.Store,
		syncer.Config{PollInterval: env.Config.PollInterval, LockFile: env.LockFile, Host: host, PullEffort: env.Config.Budget.Source == "effort"}, slog.Default())
	drainErr := eng.Drain(ctx, env.Deadline)
	// Ctrl-C cancels ctx while the drain runs, the engine puts its row back on purpose.
	// A read on the cancelled ctx would fail at once, so the outcome is read on one that cannot be cancelled.
	// The row stays in the outbox after it went out, as sent, so the state is read and not the row's absence.
	// A row that is gone was swept, which cannot happen inside one command, so that is an error like any other.
	row, err := env.Store.Outbox().Get(context.WithoutCancel(ctx), rowID)
	if err != nil {
		return queued, "", err
	}
	switch row.State {
	case store.StateSent:
		return sent, "", nil
	case store.StateFailed:
		if store.IsHeld(row.LastError) {
			return held, row.LastError, nil
		}
		return rejected, row.LastError, nil
	case store.StateDiscarded:
		return discarded, "", nil
	}
	var apiErr *wrike.APIError
	if errors.As(drainErr, &apiErr) && (apiErr.IsAuth() || apiErr.IsWrongHost()) {
		// Nothing sends the row until a person signs in again, so a script must not read this as "goes out with the next sync".
		return blocked, "", nil
	}
	// The write this row waits for on its own task, or the create of its task, comes before the write the pass stopped at:
	// the pass may have stopped at another task's write, and this one keeps holding the row after that one goes through.
	// A blocker in backoff was not even tried by the pass, so its failure is in the row and not in the drain error.
	blocker, blockerErr := env.Store.Outbox().Blocker(context.WithoutCancel(ctx), rowID)
	var reason string
	var sendErr *syncer.SendError
	switch {
	case errors.Is(drainErr, syncer.ErrLocked):
		reason = "another wrikery is sending"
	case errors.Is(drainErr, context.Canceled):
		reason = "interrupted"
	case blockerErr == nil && blocker.LastError != "":
		reason = fmt.Sprintf("waiting behind an earlier write that failed, %s: %s", subject(ctx, env, blocker), blocker.LastError)
	case errors.Is(drainErr, context.DeadlineExceeded):
		reason = "Wrike did not answer in time"
	case errors.As(drainErr, &sendErr) && sendErr.Row.ID != rowID:
		// The drain stopped at an earlier write, the error is that write's and not this command's.
		reason = fmt.Sprintf("waiting behind an earlier write that failed, %s: %v", subject(ctx, env, sendErr.Row), sendErr.Err)
	case drainErr != nil:
		reason = drainErr.Error()
	case blockerErr == nil:
		reason = "waiting behind an earlier write, " + subject(ctx, env, blocker)
	default:
		reason = "waiting for an earlier write"
	}
	return queued, reason, nil
}

// subject names the write the drain stopped at by its kind and the task it is on, as far as the cache can tell.
// A time entry edit or delete names the entry, the task it was on is kept on the row from the enqueue,
// a delete took the entry's own row with it when it was queued.
func subject(ctx context.Context, env Env, row store.OutboxRow) string {
	ctx = context.WithoutCancel(ctx)
	switch row.Kind {
	case store.KindTaskCreate:
		// A create row names the folder it goes into, the task it makes is the local row.
		return "a new task " + taskName(ctx, env, store.LocalID(row.ID))
	case store.KindCommentCreate:
		return "a comment on " + taskName(ctx, env, row.EntityID)
	case store.KindTimelogCreate:
		return "a time entry on " + taskName(ctx, env, row.EntityID)
	case store.KindTimelogUpdate:
		if row.TaskID != "" {
			return "a time entry on " + taskName(ctx, env, row.TaskID)
		}
		return "a time entry"
	case store.KindTimelogDelete:
		if row.TaskID != "" {
			return "a time entry on " + taskName(ctx, env, row.TaskID)
		}
		return "a time entry delete"
	}
	return "a change to " + taskName(ctx, env, row.EntityID)
}

// taskName is the quoted title of a cached task, or the id as it is for anything the cache has no title for.
// The quotes are plain, %q would write the parts of a joined emoji with an escape between them.
func taskName(ctx context.Context, env Env, id string) string {
	if t, err := env.Store.Tasks().Get(ctx, id); err == nil && t.Title != "" {
		return `"` + t.Title + `"`
	}
	return id
}

// readBack reads the task and, for JSON, the pending marks after a write whose fate is known.
// A read that fails does not change the fate, so the exit code stays the write's and the caller prints what it knows:
// fallback is the task as it was before the write, it keeps the id for the fate-only report and the marks are guessed from the outcome.
// ok is false when the task read failed, the caller must then print the fate alone: the fallback holds values from before the write.
func readBack(ctx context.Context, env Env, out outcome, id string, fallback store.Task, ref *refData, wantMarks bool) (task store.Task, ok bool) {
	task, err := currentTask(ctx, env, id)
	ok = err == nil
	if !ok {
		warnReadBack(env, out, err)
		task = fallback
	}
	if !wantMarks {
		return task, ok
	}
	// The pending marks changed under the drain, read them again.
	marks, err := env.Store.Outbox().StatesByEntity(ctx)
	if err != nil {
		warnReadBack(env, out, err)
		marks = map[string]store.OutboxState{}
		if out == queued {
			marks[task.ID] = store.StatePending
		}
	}
	ref.pending = marks
	return task, ok
}

func warnReadBack(env Env, out outcome, err error) {
	_, _ = fmt.Fprintf(env.Stderr, "wrikery: the change is %s, reading the task back: %v\n", out, err)
}

func reportBlocked(env Env) int {
	_, _ = fmt.Fprintln(env.Stderr, "wrikery: sign in rejected, run wrikery to sign in again, the change stays queued")
	return exitError
}

// reportWrite is the common ending of a write command. text is the line for the sent and queued cases
// without its leading word, the function adds "now:" or "queued:".
// readOK is false when the task could not be read back, then only the fate is printed.
func reportWrite(ctx context.Context, env Env, out outcome, reason string, asJSON, readOK bool, t store.Task, ref *refData, text string) int {
	switch out {
	case blocked:
		return reportBlocked(env)
	case rejected:
		return fail(env, fmt.Errorf("the change was rejected by Wrike: %s, it is listed on the changes screen in wrikery", reason))
	case held:
		// Two kinds of hold: a field changed on Wrike, and the check after a move to another account or data center.
		if fields, ok := strings.CutPrefix(reason, store.HeldPrefix); ok {
			reason = "the task changed on Wrike since you set it: " + fields
		}
		return fail(env, fmt.Errorf("not sent, %s, r on the changes screen in wrikery sends it anyway", reason))
	case discarded:
		return fail(env, errors.New("the change was discarded in wrikery before it was sent"))
	case queued:
		_, _ = fmt.Fprintf(env.Stderr, "wrikery: queued, not on Wrike yet: %s\n", reason)
	}
	if !readOK {
		return reportFate(env, out, t.ID, asJSON)
	}
	if asJSON {
		type result struct {
			taskJSON
			Sent bool `json:"sent"`
		}
		if code := printJSON(env, result{taskJSON: taskRow(ctx, env, t, ref), Sent: out == sent}); code != exitOK {
			return code
		}
		if out == queued {
			return exitQueued
		}
		return exitOK
	}
	if out == queued {
		_, _ = fmt.Fprintln(env.Stdout, "queued: "+text)
		return exitQueued
	}
	_, _ = fmt.Fprintln(env.Stdout, "now: "+text)
	return exitOK
}

// reportFate prints what is known when the task cannot be read back: the id and whether the write reached Wrike.
// After a create the id is still the local one, the server's is in the row the read failed on, and the word sent is right all the same.
func reportFate(env Env, out outcome, id string, asJSON bool) int {
	if asJSON {
		type fate struct {
			ID       string `json:"id"`
			Sent     bool   `json:"sent"`
			ReadBack bool   `json:"read_back"`
		}
		if code := printJSON(env, fate{ID: id, Sent: out == sent}); code != exitOK {
			return code
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "%s %s\n", out, id)
	}
	if out == queued {
		return exitQueued
	}
	return exitOK
}

// deliver sends the row just queued and reads the task back, the middle every write command shares.
// After it the write is decided, so the caller reports on a context a Ctrl-C cannot cut.
func deliver(ctx context.Context, env Env, host string, unchecked error, rowID int64, t store.Task, ref *refData, asJSON bool) (out outcome, reason string, after store.Task, readOK bool, err error) {
	switch {
	case env.Offline:
		// The row waits in the queue until the interface goes online or a command runs without the mode.
		out, reason = queued, "offline mode"
	case unchecked != nil:
		// The account was not compared, so nothing of the queue goes out from here.
		out, reason = queued, uncheckedReason(unchecked)
	default:
		out, reason, err = send(ctx, env, host, rowID)
		if err != nil {
			return out, reason, t, false, err
		}
	}
	after = t
	if out == sent || out == queued {
		after, readOK = readBack(context.WithoutCancel(ctx), env, out, t.ID, t, ref, asJSON)
	}
	// The cache changed whatever the outcome, a rejected write is on the changes screen, so a running interface re-reads it now.
	// The kinds are what a write can touch, the ones the engine's own drain reports.
	if env.Socket != "" {
		control.Nudge(ctx, env.Socket, string(syncer.KindTasks), string(syncer.KindComments), string(syncer.KindTimelogs))
	}
	return out, reason, after, readOK, nil
}

// finishUpdate drops the fields of p the task already has, sends what is left and reports the task before and after,
// in the two line shape of task status: line is what follows the title on both lines, the status, the importance,
// the people or the folders, suffix ends the now line.
// A field already so is noted on stderr, the way the boxes close without a write on the same value,
// and with nothing left the command ends as unchanged without reading the token.
func finishUpdate(ctx context.Context, env Env, t store.Task, ref *refData, p store.TaskUpdatePayload, asJSON bool,
	line func(context.Context, store.Task) string, suffix string) int {
	same := func(field, now string, want *string) {
		if *want != "" && *want == now {
			_, _ = fmt.Fprintf(env.Stderr, "wrikery: %s unchanged\n", field)
			*want = ""
		}
	}
	same("title", t.Title, &p.Title)
	same("description", t.Description, &p.Description)
	same("importance", t.Importance, &p.Importance)
	if p.CustomStatusID != "" && p.CustomStatusID == t.CustomStatusID {
		_, _ = fmt.Fprintln(env.Stderr, "wrikery: status unchanged")
		p.CustomStatusID, p.Status = "", ""
	}
	if p.Title == "" && p.Description == "" && p.Importance == "" && p.CustomStatusID == "" && p.Dates == nil &&
		len(p.AddResponsibles)+len(p.RemoveResponsibles)+len(p.AddParents)+len(p.RemoveParents) == 0 {
		return reportUnchanged(ctx, env, t, ref, asJSON, line(ctx, t))
	}
	before := line(ctx, t)
	host, code, unchecked := preflight(ctx, env)
	if code != exitOK {
		return code
	}
	rowID, err := env.Store.Outbox().EnqueueTaskUpdate(ctx, t.ID, p)
	if err != nil {
		return fail(env, err)
	}
	out, reason, after, readOK, err := deliver(ctx, env, host, unchecked, rowID, t, ref, asJSON)
	if err != nil {
		return fail(env, err)
	}
	ctx = context.WithoutCancel(ctx)
	if !asJSON && readOK {
		_, _ = fmt.Fprintf(env.Stdout, "was: %s  %s\n", t.Title, before)
	}
	return reportWrite(ctx, env, out, reason, asJSON, readOK, after, ref, fmt.Sprintf("%s  %s%s", after.Title, line(ctx, after), suffix))
}

// reportUnchanged ends a write command that found nothing to send: everybody named is on the task already,
// the task is in the folder already, or every field is as it was. Nothing is queued, so the exit is 0.
func reportUnchanged(ctx context.Context, env Env, t store.Task, ref *refData, asJSON bool, line string) int {
	if asJSON {
		type unchanged struct {
			taskJSON
			Sent      bool `json:"sent"`
			Unchanged bool `json:"unchanged"`
		}
		return printJSON(env, unchanged{taskJSON: taskRow(ctx, env, t, ref), Unchanged: true})
	}
	_, _ = fmt.Fprintf(env.Stdout, "unchanged: %s  %s\n", t.Title, line)
	return exitOK
}
