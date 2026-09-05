# 004: Permanent API token for authentication

Status: Accepted

## Context

The app has to authenticate against the Wrike REST API.
It is open source, so anything shipped inside the binary is public, credentials included.

## Options considered

1. A permanent access token.

- Every Wrike user can generate one in their profile.
- The app stores it and sends it as a bearer header. Many developer CLI tools work this way.

2. OAuth2.

- A nicer login through the browser.
- It also means registering a Wrike app and shipping its client id and secret inside an open source binary.
- Awkward and fragile.

3. Both.

- Two code paths and two sets of documentation to maintain from the first day. Ruled out early.

## Decision

Permanent access token only.
On the first run the user pastes the token and the app verifies it.
The token then goes into the system keychain through go-keyring, so macOS Keychain or Linux Secret Service.
A machine without a keychain falls back to a file with restricted permissions.

## Consequences

- no server infrastructure, and no secrets in the repo or in the binary
- setup asks the user to visit their Wrike profile once, which is fine for a developer audience
- OAuth2 can still be added later if the audience widens
