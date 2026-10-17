# Security

wrikery stores a sign in so it can act on your behalf: a permanent Wrike API token, or the token pair of a sign in through the browser.
On macOS it goes into the Keychain, on Linux into the Secret Service keyring, and on a machine without either into `~/.config/wrikery/token` or `~/.config/wrikery/oauth.json` with mode 0600.
A browser session also holds the client id and secret of the API app it was made with, since a renewal needs them.
It leaves your machine only in requests to Wrike and is never written to the log.

If a permanent token leaked, revoke it in Wrike in the API app it was created for, then create a new one.
The access token of a browser session runs out within an hour on its own, and Wrike revokes both of its tokens when you revoke them in the app's settings or reset your password.
`wrikery --logout` signs out, whichever way the sign in happened, and the next start asks for a new one.

To report a problem in the app itself, use GitHub's private vulnerability reporting on this repository (the Security tab, "Report a vulnerability") instead of a public issue.
