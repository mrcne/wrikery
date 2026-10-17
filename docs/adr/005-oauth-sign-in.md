# 005: OAuth 2.0 sign in next to the permanent token

Status: Accepted, supersedes 004

## Context

A permanent token never expires, grants full access to the account and cannot be limited by scopes.
Wrike OAuth 2.0 gives an access token that lives an hour, a refresh token that rotates on every renewal and scopes that limit what a token can do.
Its authorization code flow needs a client secret for the code exchange and for every renewal (possibly PKCE).
The app is open source, so a secret would have to be shipped in public binary.

## Options considered

1. The permanent token only, the decision of 004.

- One code path and nothing to register.
- No expiry and no scopes.

2. OAuth 2.0 with an app registered for wrikery, its id and secret in the binary.

- The first sign in needs no registration.
- The secret is public, which RFC 8252 section 8.5 treats as expected for a native app, and every installation depends on that one app.

3. OAuth 2.0 with an app the user or the company registers, next to the permanent token.

- No secret in the binary.
- Every user or company registers an API app in Wrike first, and the token stays the path for everybody else.

## Decision

The third option for now.
The browser sign in shows when API app is set in the config (id, secret). Nothing changes for an installation without one.
The client takes its token from a source, so the engine, the first run probe and the commands do not know which kind they hold.
The source renews a session before it expires and after a 401, under a lock file shared by the interface and the commands, and adopts a pair the other process renewed.
The scope requested is `wsReadWrite`, every endpoint the client calls accepts it.

## Consequences

- two sign in paths
- a company registers an app once and hands out its id, with the secret in the config or pasted at first run, and sees the number of its users in Wrike's App activity
- a renewal in one process is picked up by the other through the keychain, so two processes never end up with a dead refresh token
- an app shipped in the binary is a possibility for later (as a default the config overrides)
