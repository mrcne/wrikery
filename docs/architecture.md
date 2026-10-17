# Architecture

One rule shapes the whole design: the UI never waits for the network.
Everything on screen is read from a local SQLite database.
A sync engine runs in the background, keeps that database up to date with Wrike and sends local changes back.

## Modules

The code is split into six parts with strict boundaries, plus two small packages, the config and the lock file helper:

- `pkg/wrike` talks to the Wrike REST API v4: typed requests and responses, the auth header, a User-Agent that names the app and its version, paging, retries and rate limits.
  A request that ran into the rate limit is always retried.
  After a server or network error only GET, PUT and DELETE are retried, never a POST.
  The server may have applied the POST already, and a second attempt would create a duplicate comment or timelog.
  The package imports only the standard library and knows nothing about the rest of the app, which a depguard rule in `.golangci.yml` enforces.
  It could become a library of its own one day.
- `internal/store` is the SQLite layer: schema, migrations, queries, the search index and the outbox table.
  No HTTP, no UI code.
- `internal/syncer` is the only module that knows both sides.
  It pulls changes from Wrike into the store and sends the writes waiting in the outbox back to Wrike.
  It depends on a small interface that covers only the part of the client it needs, so its tests run against a fake.
- `internal/ui` is the bubbletea application.
  It reads from the store, writes user actions to the store and the outbox, and never talks to the network.
  When sync changes the store, the UI hears about it through a bubbletea message and refreshes.
  The task list owns the rows and their order, grouped or not, and two views draw them: the list with sections and a board with a column per workflow status and a lane per group.
  The board moves the list's own cursor, so there is one selection and no state to carry across when the shape changes.
- `internal/control` is the channel between a command and a running interface, a unix socket with one JSON line each way.
- `internal/cli` is the command line front end: one operation per run, on the same store and outbox as the TUI.
  It parses its own arguments with the standard library, resolves names against the cache, queues writes the way the TUI does and prints text or JSON.
  It imports the TUI's theme, its description renderer and the editor's markdown translation, so both front ends look the same and write a description the same way, and nothing flows the other way.
- `internal/config` loads the TOML config file and resolves the paths listed at the end.
- `internal/lockfile` is the flock helper the sync engine and the token renewal share, a lock the kernel releases when the holder dies.

Data flows in one line: ui <-> store <-> syncer <-> `pkg/wrike` <-> Wrike API.

`cmd/wrikery` wires the parts together.
It loads the stored sign in through `internal/auth`, a permanent token or a browser session, hands the client a token source, builds the engine, starts the bubbletea program and forwards engine events into it as messages.
The UI gets a few callbacks (refresh, wake the outbox, verify a token, start a browser sign in), so it never imports the client or the engine.
A new sign in rebuilds the client and the engine, because the client holds the token source.
A browser session is the OAuth 2.0 authorization code flow: `internal/auth` listens on the port of the redirect URI, hands the browser Wrike's authorize page, exchanges the code it gets back and keeps the pair with the expiry and the host the token answer named.
The source renews the access token under five minutes before its expiry and after a 401, under a lock file beside the token file, and re-reads the stored session first, so a pair the other process renewed is adopted instead of a second refresh, which would leave one of them with a dead refresh token.
The stored session is the authority: a renewal that finds nothing there, or a permanent token, stops, so an interface still open after a sign out never writes the session back.
The refresh itself runs past the request's context, Wrike rotates the pair as it answers and a quit at that moment would lose the only live refresh token.
Wrike serves accounts from more than one data center under a different host, so the first run with a token probes the known hosts and keeps the one that accepted the token in the store meta table.
Later runs read that host back instead of probing again, a browser session carries its host, and the `host` config key overrides all of them when set.
When arguments remain after the global flags, main hands them to `internal/cli` with the store, the config, the sign in check, the client constructor and the host resolver instead of starting the program.
A command appends to the same log file and never rotates it, the interface does the rotation at its start.

A running interface listens on a unix socket next to the database, `wrikery.sock` beside the sync lock, and `internal/control` is the channel: one JSON request in, one JSON reply out, a connection per request.
`cmd/wrikery` owns the listener as it owns the engine forwarder, and turns a request into a `ui.ControlMsg` with a buffered reply channel that the model answers from its update loop, so `internal/ui` sees no socket.
Every send to the program runs on a goroutine of its own, because the program blocks while an external editor holds it, and a request nobody answers in five seconds is reported as busy and carries a deadline past which the model drops it.
The command reads for longer than that wait, so the busy answer reaches it instead of a timeout of its own.
A reload, what every write command and `wrikery sync` send when they are done, becomes the two messages the engine forwarder sends after a cycle and never reaches the model as a request, and it names the kinds it touched, a write the tasks, comments and time entries, a sync every kind.
A socket path has room for 104 bytes on macOS, so a long data directory gets a hashed name in a directory of the user's own under /tmp, made 0700 and checked, computed the same way on both sides.
The first interface keeps the socket, a second one logs that and runs without it, a file a crash left behind is removed before the bind, and the demo listens on a fixed name in the data directory.

## What gets cached

On the first run, and later from the settings screen, the user picks which spaces and projects to follow.
Only these are synced and searchable, which keeps the database small and the first sync short even on a larger Wrike account.
The user own tasks are always included, and anything outside the followed set can still be fetched on demand while online.

Cached entities: tasks with descriptions, folders and projects, spaces, contacts, comments, timelogs, workflows and the dependencies between tasks.
Workflows are there because custom statuses come from them.
The account call lists only the account workflows, so the reference pull also asks each space for the workflows it owns, since a task in such a space carries a status from one of those.
Search over task titles and descriptions uses FTS5, the full text search built into SQLite.

## The database

One SQLite file holds everything, with a table for each cached entity.
Most of the mapping is direct, so only three parts need explaining:

- tasks keep their dates in flat columns, with task_responsibles, task_parents, task_supertasks and task_dependencies as join tables.
  The subtasks of a task are the tasks that name it in task_supertasks, nothing is kept on the parent, because adding a subtask on Wrike leaves the parent's updatedDate alone and a list on the parent would go stale.
- dependencies holds one row per edge between two tasks, predecessor, successor, relation type and lag, and either end can be a task outside the cache
- folders keep the project fields inline, with folder_children for the tree
- workflows keep custom_statuses in the order the API returns them

The list queries fold both join tables into the row, so the list can be grouped by person or by folder without a second query.

Timestamps are stored as UTC text in RFC3339 format.
Task start and due dates and the tracked date on a timelog keep the strings without a time zone that the API returns.

Search goes through tasks_fts, an index that SQLite triggers keep in step with the tasks table (see: https://sqlite.org/fts5.html ).
It cannot drift, no matter which code path writes a task.
Descriptions have their HTML stripped before they are stored, and the stripped text is indexed.

The scopes table lists the followed spaces and projects, plus one row for the user's own tasks.
Each row remembers how far the last sync got.
Unfollowing a scope clears that memory, so following it again later pulls it from the start instead of asking only for the changes since the day it was dropped.
The pins table lists the folders the sidebar can narrow itself to, local state that never goes to Wrike.
The outbox table holds the queued writes, and for thirty days the sent and discarded ones, and is described below.

## Getting changes from Wrike

The first sync fetches everything in the followed scopes.
After that the engine polls, every 60 seconds by default, and there is a key for a manual refresh.
A poll asks Wrike only for tasks whose updatedDate changed since the last sync.
The task search API supports that filter directly, so a poll with nothing new costs one small request per scope.
A change of the followed set asks the engine for a full cycle, the kind the refresh key asks for: a newly followed scope is pulled from the start, and the sweep at the end of such a cycle removes the tasks of an unfollowed scope, which is why a scope followed again later starts from scratch as well.
The task search also lists subtasks, including those with no folder of their own, and carries each task's super tasks, dependency ids and attachment count.
Comments, timelogs and dependencies are synced only for tasks the user recently viewed or touched, not for the whole account.
The dependencies are among them because adding or removing one on Wrike moves the updatedDate of neither task, so the poll would never see the change.
The task's own dependencies endpoint answers every edge of the task with both ends, so one read also takes a removed edge off the other end and adds a new one to it.
At the end of a cycle the engine reads the dependencies of the tasks that list an edge the cache does not have yet, two hundred tasks at most per cycle, so a first pull of a large plan catches up over a few cycles.
A read that Wrike refuses forgets that task's dependency ids rather than the task, the pull and the sweep are what decide a task exists, and the ids come back with the task's next pull.
Every cycle also pulls the user's own timelogs for the current week and the eight weeks before it, on top of that per-task pull.
That pull stops at an empty page, because Wrike answers an empty window with a page token and refuses the token on the next request.
The store rows in that range are replaced as a whole, so a deleted or moved entry disappears too.
The first day of the window is written to the meta table, and the timesheet marks a week before it as not synced rather than showing it empty.

## Sending changes back

Every write becomes a row in the outbox: operation type, entity id, a JSON payload, state (pending, in flight, failed, sent, discarded), attempt count and the last error.
The row and the matching change to the local cache are written in one transaction.
The UI shows the change at once, and the queue can never disagree with the cache.
The engine sends rows in order.
A write waits while an earlier write on the same entity is backing off after a failure, where the entity is the task for an update, a comment or a time entry create, and the time entry for its own update or delete.
Two changes to one task reach Wrike in the order they were made across sync cycles, and a command queued behind such a write says which write it waits for and why that one failed.
A write Wrike rejected holds nothing, the writes after it go out, and a retry of it from the changes screen lands after them.
When a write succeeds, the temporary local row is swapped for the version the server returned, again in one transaction.
A status change also applies the workflow group to the local task alongside the custom status id, so the lists sort and filter it as done right away.
The drain sends Wrike only the custom status id and lets it derive the group.

A row is not deleted when it has been sent or discarded.
It stays for thirty days, marked sent or discarded with the time, and the sweep that removes the older ones runs inside the transaction that completes or discards a row.
The row also carries the values the write replaced, read in the enqueue transaction before its own update overwrites the cache row, which is the last moment they exist: the old title, description, status, importance or dates block of a task update, the old hours, day and note of a time entry edit, the whole entry for a delete, and the mirror of an assignee or folder change.
It names the task it belongs to, with the title at the time, and the front end that queued it, the interface or a command.
The changes screen reads these rows next to the queue through one list.
The queries that work the queue, the due pick, the blocker, the marks and the counts, filter on the three queue states and never see the history.

A created task has no id until Wrike answers, so the store gives it a local one, the outbox row's number behind a `local:` prefix, the same way a queued comment or time entry gets its row.
Until then the row shows the first Active status of the default task workflow of the folder's space, which the sync stores with the space, so the row keeps its place when the answer lands.
Writes queued on that task name the local id and are not sent until the create has landed.
The completion then swaps the local row for the server's and points the comments, time entries and queued writes at the real id, in one transaction.
The deletion sweep and the thread refresh never see a local id: the first would prune the task as one Wrike no longer lists, the second would ask Wrike for its comments and get a 404.

A command sends its write right away: it queues the row and runs one drain pass with a deadline, then reads the row back to report sent, queued or rejected.
Two processes can therefore drain one outbox, a running TUI and a command.
The claim on a row is atomic, a row is marked in flight only while it is still pending, so no row is sent twice by two drainers.
An advisory lock file next to the database covers what the claim cannot: the reset of in-flight rows, which would resend a row a command is mid-way through, and the order of two rows on one task.
Every drain pass takes the lock, puts back to pending the rows a process that died mid-send left in flight, and then sends.
The kernel releases the lock when its holder dies, so a crash never leaves it taken.
A lock the file system refuses is logged once and the pass continues without it.
A command that cannot take the lock inside its deadline leaves its write queued.
The deadline bounds the wait and the start of a row, a create already sent runs to Wrike's answer because a cut off create may have landed and would be sent again by the next drain.
The client never retries a POST after a network or server error, it does after a rate limit, so a create can wait up to a minute and a half.

A description edit is the one write that sends a whole field, and the field is HTML while the editor shows markdown.
The UI cuts the stored HTML into top level blocks, a block element or a run of text between two line breaks, and hands the editor one piece of markdown per block.
On save, a block whose markdown is still in the file keeps the bytes it came with, only the blocks the user changed are translated back, and markup that markdown cannot express stays in the file as HTML tags.
A description given to a command goes through the same split and merge, and a create converts it whole.
So an untouched paragraph never changes on the server, whatever it holds.

After a network error the engine waits before the next attempt, and the wait grows with every failure.
A 429 response, meaning the rate limit was hit, is treated the same way.
This is what the Wrike documentation asks for, retries with growing waits.
It puts the limit at around 400 requests per minute (https://developers.wrike.com/faq/ ).

## Conflicts

Comments can only be added, so they cannot conflict.
A timelog is only ever edited by the person who created it.
It can still be locked or approved in a timesheet later, and then Wrike rejects the edit.
The client exposes both flags, so the UI can refuse the change up front instead of queueing a write that is going to fail.

Task edits send only the fields the user changed.
Two people editing different fields of the same task both keep their change.
If they edit the same field, the last write wins, which is also what the web application does in practice.
A description edit is that case with a longer window, since the whole field is sent when the editor closes.

A create sent again because the answer was lost or not understood, a network error, a server error or a response that does not decode, can land twice when the first attempt had reached Wrike.
The second copy shows up with the next pull and is deleted in the web application, the same ambiguity a comment or a time entry has.

Some writes fail for good: the task was deleted on the server, a permission was revoked, or the API rejects the write.
Those rows move to the failed state.
The changes screen lists them, together with the rows still retrying after a failure, and offers a retry or a discard.
The engine cannot tell a failure that passes from one the write earns on every attempt, a garbled answer or a server error for that one payload, and such a row holds the later writes on its entity, so the view is where it is seen and discarded.
The status bar always shows the pending and failed counts, so nothing fails without the user seeing it.

## Error handling

The network is treated as unreliable by default.
A request that fails on the network or on Wrike's side puts the app in offline mode, which shows in the status bar.
A request Wrike rejects with a client error shows as a failing sync instead, the log line carries the request and the reason.
Reads keep coming from the cache and writes keep going to the queue.
The engine reconnects on its own, waiting longer between attempts each time (exponential backoff).
A 401 is the exception: the client asks its token source once for a renewed token, a browser session refreshes and a permanent token has nothing to renew.
A refresh Wrike refused pauses sync and asks for a new sign in, a refresh that could not run, the token endpoint unreachable for example, leaves the request to the retry with the session intact.
A scope that Wrike rejects, because access was revoked or the project was deleted, is skipped with a log line and the other scopes keep syncing.
Logs go to a file through log/slog, never onto the screen.
Every local change is a single SQLite transaction, so a crash cannot leave the cache half written.
The rows the store creates on its own, queued writes and their optimistic cache rows, are stamped from a clock the store holds, so the tests can run on a fixed time.

## Files on disk

The app follows the XDG base directory convention on both macOS and Linux , because the audience is developers who expect `~/.config`.
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` override the defaults.

- config: `~/.config/wrikery/config.toml`
- database: `~/.local/share/wrikery/wrike.db`, cache and outbox in one file, deleting it resets the cache
- logs: `~/.local/state/wrikery/wrikery.log`, the previous run's log is kept as `wrikery.log.1`, two instances at once share the name and the first one's later lines end up in the `.1` file.
  A demo run writes `demo.log` in the same directory and rotates nothing.
- API token: the system keychain (macOS Keychain, Linux Secret Service), falling back to `~/.config/wrikery/token` (mode 0600) on machines without one
- browser session: a second keychain item with the access token, the refresh token, the expiry, the host and the app, falling back to `~/.config/wrikery/oauth.json` (mode 0600), and `~/.config/wrikery/auth.lock` held while it is renewed
- `WRIKERY_TOKEN` in the environment overrides the stored sign in, keychain and files both

## Testing

Each module is tested on its own, along the boundaries above:

- `pkg/wrike` against a local test HTTP server that serves hand written fixtures shaped like the documented responses
- `internal/store` against a real SQLite database in a temporary file, migrations included
- `internal/syncer` against a fake client and a real store in a temporary file
- `internal/ui` flows with teatest

The sync scenarios cover offline, rate limits, rejected writes, tasks deleted on the server and resuming an interrupted sync.
One more test drives the real client against a local fixture server end to end.

CI runs golangci-lint and the tests on Linux and macOS through GitHub Actions, with cgo disabled like the build.
The same job checks that go.mod and go.sum are tidy, and two more jobs run the tests under the race detector with cgo on and run govulncheck.
Releases are single static binaries for Linux and macOS on amd64 and arm64.
`make cross` builds all four.
A pushed `v*` tag runs goreleaser through GitHub Actions: it builds the four archives, publishes the GitHub release and pushes a Homebrew cask to `mrcne/homebrew-tap`.
