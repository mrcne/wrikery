# Contributing

This is a small project with a small scope.
Read [docs/overview.md](docs/overview.md) for what the app does and does not do before proposing a feature.

## Build and check

`make build` builds the binary with cgo off, `make test` runs the tests and `make lint` runs golangci-lint.
`make check` runs both, and CI runs the same on Linux and macOS, plus a race run, a tidy check and govulncheck.
golangci-lint is not part of the module, install the pinned version once with `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.3`.
`./wrikery --demo` runs the app on built in sample data with no Wrike account, which is the fastest way to look at a UI change.

## Changes

One topic per pull request.
A commit subject starts with a type, `feat`, `fix`, `chore`, `docs`, `refactor`, `test` or `build`, followed by an imperative sentence, for example `fix: reject an empty comment`.
Tests assert behavior, the UI tests run against a seeded temporary store, and golden files are updated with `go test ./internal/ui -update`.
A choice of technology or of a way of working goes through a decision record in [docs/adr/](docs/adr/), one file per decision, the existing records show the shape.
[docs/overview.md](docs/overview.md) describes what the app does and [docs/architecture.md](docs/architecture.md) how, keep both current when behavior changes.
Comments explain why rather than what, and text in the repository stays plain ASCII.
