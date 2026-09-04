# 002: SQLite as the local store

Status: Accepted

## Context

The app is local first. Every read comes from a local cache and every write goes through a local outbox.
The store therefore needs transactions, queries and full text search.
I also want it to be a single file on disk, easy to inspect and easy to delete.

## Options considered

1. Flat JSON files.

Simple to start with and painful soon after. No transactions, no queries, no search.
I would end up writing a bad database of my own.

2. An embedded key value store, bbolt or badger.

- Transactions come with it.
- Queries and full text search do not. Both would have to be written by hand over the key space.
- Adding a search index like bleve next to it would work, but then there are two stores to keep in step.

3. SQLite with the CGO driver (mattn/go-sqlite3).

- The standard choice and the fastest one.
- CGO is the price: cross compiling for the four platform targets stops being a plain go build.

4. SQLite with the pure Go driver (modernc.org/sqlite).

- No CGO, so all four targets build from one machine.
- Slower than the CGO driver. For a single user cache with a few thousand rows nobody notices.
- FTS5 is included.

5. A client-server database, PostgreSQL or MySQL.

- Transactions, queries and full text search, all of it mature and well understood.
- It also asks the user to install a server and keep it running, for a terminal client that caches their own tasks.
- ADR 001 mentions a single binary. This is the opposite of that.

## Decision

SQLite through modernc.org/sqlite.
FTS5 provides the full text search over task titles and descriptions.
Schema changes go through SQL migrations embedded in the binary.
A small loop over embed.FS runs them in order, there is no migration framework.

## Consequences

- one database file in the XDG data dir holds both the cache and the outbox, deleting it resets the app
- writes are transactional, a crash cannot leave the local state half written
- instant search is one of the main features and FTS5 gives it for very little work
- if the pure Go driver ever turns into a bottleneck, swapping in the CGO driver is a small and contained change
