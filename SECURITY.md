# Security

wrikery stores a sign in so it can act on your behalf: a permanent Wrike API token, or the token pair of a sign in through the browser.
On macOS it goes into the Keychain, on Linux into the Secret Service keyring, and on a machine without either into `~/.config/wrikery/token` or `~/.config/wrikery/oauth.json` with mode 0600.
A browser session also holds the client id and secret of the API app it was made with, since a renewal needs them.
It leaves your machine only in requests to Wrike and is never written to the log.

If a permanent token leaked, revoke it in Wrike in the API app it was created for, then create a new one.
A browser session is revoked in Wrike under Authorized Apps on your profile page, where every app you allowed is listed with its date, and the next sync asks for a new sign in.
Its access token runs out within an hour on its own, and a password reset revokes every token too.
`wrikery --logout` signs out, whichever way the sign in happened, and the next start asks for a new one.

To report a problem in the app itself, use GitHub's private vulnerability reporting on this repository (the Security tab, "Report a vulnerability") instead of a public issue.

## Checking a release

Releases are built by goreleaser in GitHub Actions from the tag, the workflow is `.github/workflows/release.yml`.
The binaries are built with cgo off and `-trimpath`, which makes them reproducible, see the last step.
Releases after v0.6.0 carry, next to the archives, `checksums.txt` with a signature, an SBOM per archive and a build provenance attestation for every archive.
Earlier releases have the checksum file only.
Each step below stands on its own, run the ones you need, with the tag and the archive you downloaded in place of `v0.7.0` and `wrikery_0.7.0_darwin_arm64.tar.gz`.

1. Checksums: `shasum -a 256 --check --ignore-missing checksums.txt` in the directory with the archive, or `sha256sum --check --ignore-missing checksums.txt` on Linux.
   It proves the archive is the one the checksum file names.
2. The signature on the checksum file:

   ```
   cosign verify-blob \
     --certificate-identity https://github.com/mrcne/wrikery/.github/workflows/release.yml@refs/tags/v0.7.0 \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     --bundle checksums.txt.sigstore.json checksums.txt
   ```

   It proves the checksum file was written by the release workflow of this repository for that tag, through a keyless Sigstore signature, and needs no account anywhere.
3. The provenance of an archive: `gh attestation verify wrikery_0.7.0_darwin_arm64.tar.gz --repo mrcne/wrikery`.
   It shows the workflow run, the commit and the runner that built the archive.
   `gh` needs a GitHub login for this.
4. The tag: `curl -fsS https://github.com/mrcne.gpg | gpg --import`, then `git tag -v v0.7.0` in a clone.
   The key's fingerprint is `2B01 4743 3879 F97A 99EF 202F C6DF BFA4 8D03 6D87`, compare it with what `gpg` prints for the imported key.
   It proves the tag was made by the maintainer, whoever ran the release.
5. The SBOM: `wrikery_0.7.0_darwin_arm64.tar.gz.sbom.json` is SPDX JSON and lists every Go module in the binary with its version.
   `go version -m wrikery` prints the same list from the binary itself, and `govulncheck -mode=binary wrikery` checks it against the Go vulnerability database.
6. Rebuilding the binary:

   ```
   git clone https://github.com/mrcne/wrikery
   cd wrikery
   git checkout v0.7.0
   CGO_ENABLED=0 GOTOOLCHAIN=$(sed -n 's/^toolchain //p' go.mod) go build -trimpath -ldflags "-s -w -X main.version=v0.7.0 -X main.commit=$(git rev-parse --short HEAD)" -o wrikery ./cmd/wrikery
   shasum -a 256 wrikery
   ```

   The `toolchain` line of go.mod names the Go version the release was built with, and `go version -m` prints it for the binary from the archive, in its first line.
   The hash is the same as that of the `wrikery` file in the archive for your system.
   `GOOS=darwin GOARCH=amd64` in front of the build command gives the binary for an Intel Mac, and `GOOS=linux GOARCH=amd64` the one for Linux, where `sha256sum wrikery` prints the hash.
   The tree must be a clean git checkout: the Go toolchain stamps the commit and whether the tree was modified into the binary, and a source archive without `.git` gives different bytes.
   The release workflow runs this comparison itself before it publishes.
