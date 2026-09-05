# 003: Local first architecture with an outbox

Status: Accepted

## Context

The two project goals are speed and working offline.
If the app calls the Wrike API while the user moves through it, every keystroke costs a round trip.
Without a connection there would be nothing to show at all.

## Options considered

1. Direct API calls with an in-memory cache.

- Simple, and fast enough while online. A cold start is slow and offline gives you nothing.

2. A read through cache with online only writes.

- Browsing offline works.
- Writes fail the moment the connection drops, which is exactly the moment someone wants to log time from a train.

3. Local first with an outbox.

- The UI reads only from SQLite.
- A background sync engine pulls remote changes and pushes local writes queued in an outbox table.
- A write lands in the local view at once and reaches Wrike later.

## Decision

Local first with an outbox.

Pull sync is incremental.
The engine polls the task search API with an updatedDate cursor, every 60 seconds by default, plus a manual refresh.
Writes are queued in the outbox and sent in order, with growing waits after failures.

The app sends only the fields the user changed.
Edits to different fields of the same task therefore merge without trouble.
Edits to the same field resolve as last write wins.
Some writes fail for good: the task was deleted, a permission was revoked, the API rejected them.
Those show up in a sync issues view with retry or discard.

## Consequences

- every screen renders from local data, so navigation is instant and browsing offline just works
- writes feel instant and survive a connection drop
- the price is a sync engine with some complexity: cursors, backoff, an outbox state machine and a conflict story, all of it needing thorough tests
- data is eventually consistent, at most about a minute stale, with a manual refresh available
