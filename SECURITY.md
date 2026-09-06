# Security

wrikery stores a Wrike API token so it can act on your behalf.
On macOS the token goes into the Keychain, on Linux into the Secret Service keyring, and on a machine without either into `~/.config/wrikery/token` with mode 0600.
It leaves your machine only in requests to Wrike and is never written to the log.

If a token leaked, revoke it in Wrike in the API app it was created for, then create a new one.
`wrikery --logout` removes the stored token, and the next start asks for the new one.

To report a problem in the app itself, use GitHub's private vulnerability reporting on this repository (the Security tab, "Report a vulnerability") instead of a public issue.
