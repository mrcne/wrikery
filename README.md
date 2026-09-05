# Wrikery (Wrike TUI)

Status: early development, no release yet.
Build from source with `make build` and run `./wrikery`, or `./wrikery --demo` to look around without a Wrike account.

A fast terminal client for Wrike (Unofficial).
Browse tasks, read descriptions, add comments, change statuses, log time and check your timesheet without leaving the terminal.

## Why

The Wrike web UI is nice but slow for quick checks.
This tool stores a local copy of tasks, so browsing and searching is instant, and it works offline.

## Features

- browse the spaces and projects selected to follow
- instant full text search for everything cached
- add comments, change status, assignee and dates
- log time and review week in a timesheet view
- offline mode: reads come from the local cache, writes are queued and synced later

## Docs

The project description is in [docs/overview.md](docs/overview.md) and the technical design in [docs/architecture.md](docs/architecture.md).
Technology decisions are recorded in [docs/adr/](docs/adr/).
