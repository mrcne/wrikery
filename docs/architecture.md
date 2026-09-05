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
- `internal/sync` is the only module that knows both sides.
  It pulls changes from Wrike into the store and sends the writes waiting in the outbox back to Wrike.
  It depends on a small interface that covers only the part of the client it needs, so its tests run against a fake.
- `internal/ui` is the bubbletea application.
  It reads from the store, writes user actions to the store and the outbox, and never talks to the network.
  When sync changes the store, the UI hears about it through a bubbletea message and refreshes.
- `internal/config` loads the TOML config file and resolves the paths listed at the end.

Data flows in one line: ui <-> store <-> sync <-> `pkg/wrike` <-> Wrike API.

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

After a network error the engine waits before the next attempt, and the wait grows with every failure.
A 429 response, meaning the rate limit was hit, is treated the same way.
This is what the Wrike documentation asks for, retries with growing waits.
It puts the limit at around 400 requests per minute (https://developers.wrike.com/faq/ ).

## Files on disk

The app follows the XDG base directory convention on both macOS and Linux , because the audience is developers who expect `~/.config`.
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` override the defaults.

- config: `~/.config/wrikery/config.toml`
- database: `~/.local/share/wrikery/wrike.db`, cache and outbox in one file, deleting it resets the cache
- logs: `~/.local/state/wrikery/wrikery.log`
- API token: the system keychain (macOS Keychain, Linux Secret Service), falling back to a file with restricted permissions on machines without one
