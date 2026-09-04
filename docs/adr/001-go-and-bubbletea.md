# 001: Go and bubbletea

Accepted, 2026-09-01.

## Context

I'm building a terminal client for Wrike, for macOS and Linux.
It should to feel fast, ship as a single binary and stay easy to maintain.
The choice of language and TUI framework shapes everything else, so it comes first.

## Options considered

1. Rust with ratatui.

- It has the strongest TUI ecosystem at the moment (gitui, yazi, atuin).
- It gives the highest performance ceiling, no garbage collector and small static binaries.
- The cost is slower development, a harder start for contributors and more ceremony in everyday code.

2. Go with bubbletea.

It means the charmbracelet stack: bubbletea for the event loop, bubbles for ready made components,
lipgloss for styling, glamour for markdown rendering.

- A simple language, fast builds, easy cross compilation and many potential contributors.
- Control over rendering is a step below ratatui and the runtime has a garbage collector.
- At the scale of one user's cache neither matters in practice.

3. Others.

TypeScript with Ink, or Python with Textual, are the fastest to prototype with.
Both need a runtime on the user's machine, start visibly slower and would make the speed goal a constant fight.
Ruled out early.

## Decision

Go with bubbletea, plus bubbles, lipgloss and glamour.
Readability, maintainability and speed of development won over the higher performance ceiling of Rust.
The performance we give up is not something a user of this app would notice.

Task descriptions come from Wrike as HTML. They are converted with html-to-markdown and rendered with glamour.

## Consequences

- single static binaries for all four platform targets, easy releases
- fast iteration and a codebase that contributors can pick up quickly
- the Elm style update loop needs discipline in a multi pane app, so UI state is split into one model per screen
- the tooling follows from the choice: golangci-lint, the standard testing package, teatest for UI tests, goreleaser for releases
