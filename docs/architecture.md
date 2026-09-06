# Architecture

One rule shapes the whole design: the UI never waits for the network.
Everything on screen is read from a local SQLite database.
A sync engine runs in the background, keeps that database up to date with Wrike and sends local changes back.

## Modules

The code is split into four parts with strict boundaries, plus a small config package:

- `pkg/wrike` talks to the Wrike REST API v4: typed requests and responses, the auth header, paging, retries and rate limits.
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
- `internal/config` loads the TOML config file and resolves the paths listed at the end.

Data flows in one line: ui <-> store <-> syncer <-> `pkg/wrike` <-> Wrike API.

`cmd/wrikery` wires the parts together.
It loads the token through `internal/auth`, builds the client and the engine, starts the bubbletea program and forwards engine events into it as messages.
The UI gets a few callbacks (refresh, wake the outbox, verify a token), so it never imports the client or the engine.
A new token rebuilds the client and the engine, because the client holds the token.
Wrike serves accounts from more than one data center under a different host, so the first run probes the known hosts and keeps the one that accepted the token in the store meta table.
Later runs read that host back instead of probing again, and the `host` config key overrides both when set.

## What gets cached

On the first run the user picks which spaces and projects to follow.
Only these are synced and searchable, which keeps the database small and the first sync short even on a larger Wrike account.
The user own tasks are always included, and anything outside the followed set can still be fetched on demand while online.

Cached entities: tasks with descriptions, folders and projects, spaces, contacts, comments, timelogs and workflows.
Workflows are there because custom statuses come from them.
Search over task titles and descriptions uses FTS5, the full text search built into SQLite.

## The database

One SQLite file holds everything, with a table for each cached entity.
Most of the mapping is direct, so only three parts need explaining:

- tasks keep their dates in flat columns, with task_responsibles and task_parents as join tables
- folders keep the project fields inline, with folder_children for the tree
- workflows keep custom_statuses in the order the API returns them

Timestamps are stored as UTC text in RFC3339 format.
Task start and due dates and the tracked date on a timelog keep the strings without a time zone that the API returns.

Search goes through tasks_fts, an index that SQLite triggers keep in step with the tasks table (see: https://sqlite.org/fts5.html ).
It cannot drift, no matter which code path writes a task.
Descriptions have their HTML stripped before they are stored, and the stripped text is indexed.

The scopes table lists the followed spaces and projects, plus one row for the user's own tasks.
Each row remembers how far the last sync got.
The outbox table holds the queued writes and is described below.

## Getting changes from Wrike

The first sync fetches everything in the followed scopes.
After that the engine polls, every 60 seconds by default, and there is a key for a manual refresh.
A poll asks Wrike only for tasks whose updatedDate changed since the last sync.
The task search API supports that filter directly, so a poll with nothing new costs one small request per scope.
Comments and timelogs are synced only for tasks the user recently viewed or touched, not for the whole account.

## Sending changes back

Every write becomes a row in the outbox: operation type, entity id, a JSON payload, state (pending, in flight, failed), attempt count and the last error.
The row and the matching change to the local cache are written in one transaction.
The UI shows the change at once, and the queue can never disagree with the cache.
The engine sends rows in order.
When a write succeeds, the temporary local row is swapped for the version the server returned, again in one transaction.
A status change also applies the workflow group to the local task alongside the custom status id, so the lists sort and filter it as done right away.
The drain sends Wrike only the custom status id and lets it derive the group.

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

Some writes fail for good: the task was deleted on the server, a permission was revoked, or the API rejects the write.
Those rows move to the failed state.
A sync issues view lists them and offers a retry or a discard.
The status bar always shows the pending and failed counts, so nothing fails without the user seeing it.

## Error handling

The network is treated as unreliable by default.
A failed request puts the app in offline mode, which shows in the status bar.
Reads keep coming from the cache and writes keep going to the queue.
The engine reconnects on its own, waiting longer between attempts each time (exponential backoff).
A 401 is the exception: sync pauses and asks for a new token instead of retrying.
A scope that Wrike rejects, because access was revoked or the project was deleted, is skipped with a log line and the other scopes keep syncing.
Logs go to a file through log/slog, never onto the screen.
Every local change is a single SQLite transaction, so a crash cannot leave the cache half written.

## Files on disk

The app follows the XDG base directory convention on both macOS and Linux , because the audience is developers who expect `~/.config`.
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` override the defaults.

- config: `~/.config/wrikery/config.toml`
- database: `~/.local/share/wrikery/wrike.db`, cache and outbox in one file, deleting it resets the cache
- logs: `~/.local/state/wrikery/wrikery.log`
- API token: the system keychain (macOS Keychain, Linux Secret Service), falling back to `~/.config/wrikery/token` (mode 0600) on machines without one
- `WRIKERY_TOKEN` in the environment overrides the stored token, keychain and file both

## Testing

Each module is tested on its own, along the boundaries above:

- `pkg/wrike` against a local test HTTP server that serves hand written fixtures shaped like the documented responses
- `internal/store` against a real SQLite database in a temporary file, migrations included
- `internal/syncer` against a fake client and a real store in a temporary file
- `internal/ui` flows with teatest

The sync scenarios cover offline, rate limits, rejected writes, tasks deleted on the server and resuming an interrupted sync.
One more test drives the real client against a local fixture server end to end.

CI runs golangci-lint and the tests on Linux and macOS through GitHub Actions, always with cgo disabled.
Releases are single static binaries for Linux and macOS on amd64 and arm64.
`make cross` builds all four.
