# Go client releases

Go module versions use root SemVer tags such as `v0.1.0`. Hosted plugin
manifests keep their own version; a client release does not change them.
Do not reuse a tag or overwrite a release archive after distribution.

From a clean checkout of the intended `main` commit on macOS with Xcode command
line tools, Go and Python installed:

```sh
make lint
make test
make build
make release-go VERSION=v0.1.0
```

Review the binaries and checksums in `dist/go/v0.1.0`. When authorized to release,
create and push the matching `v0.1.0` tag, then run **Release Go client** on main
with that existing tag. The workflow verifies the tag is on main, runs checks,
builds macOS/Linux/Windows clients for AMD64/ARM64 with CGO disabled, verifies
SHA-256 checksums, and creates a **draft** GitHub release. Review and publish the
draft separately. No release happens on an ordinary push.

The build job runs on macOS to compile the universal native CryptoKit helper.
Both macOS executables embed that helper using the `secureenclave` build tag;
Linux and Windows builds omit it and reject Secure Enclave requests explicitly.
The helper is part of the Go executable, so archive checksums and signed update
metadata cover it too. No separate helper download or runtime compiler is used.
macOS login requires hardware keys by default and offers no software override.
Plain `go build`/`go install` builds omit the native component and cannot create
new macOS connections; use `make build` or the full release binary. Verify hardware
profiles against an upgraded macOS executable before publishing; a virtual CI
runner can validate compilation and contracts, not physical hardware behavior.

Go downloads require no npm organization or token. This repository is private,
so its source and release downloads require GitHub repository access. Do not
promise public `go install` or public release links until distribution is made
public. Compiled binaries may be shared through an approved distribution channel
without requiring Go or Node on the user's machine.

## Signed automatic update channel

The same repository will provide public binary downloads after it is made
public. The stable channel is fixed to:

```text
https://github.com/ReahPlatform/reah-mcp/releases/latest/download/update.json
```

Before the first official release, configure these GitHub Actions values:

- Repository variable `REAH_MCP_UPDATE_PUBLIC_KEY`: base64-encoded 32-byte
  Ed25519 public key, embedded in every release executable.
- Repository secret `REAH_MCP_UPDATE_SIGNING_SEED`: base64-encoded 32-byte
  Ed25519 seed generated and retained securely by the release owner. Never
  commit it, put it in build arguments, or distribute it with the client.

The workflow fails if the trust root is missing or the signing key does not
match. The private seed is available only to the manifest signing step, after
compilation. No GitHub or Reah credentials are required by the updater. While the
repository is private, unauthenticated update downloads will fail safely; local
MCP service continues to work. Publication and repository visibility changes
remain separate owner actions.

For a local release build with updates enabled, export the public key and
`REAH_MCP_UPDATE_URL` set to the channel URL above, then run `make release-go`.
Run `go run scripts/sign_update.go dist/go/v0.1.0` in an environment with the
signing seed and matching public key. Without this configuration, local builds
remain usable but do not auto-update. Do not distribute those builds as official
automatically updating releases.

Release artifacts include six archives, six raw executable assets, SHA256SUMS,
`update-manifest.json` and signed `update.json`. The envelope base64-encodes the payload and signature. The signature covers the exact
decoded payload bytes with the `reah-mcp-update-v1` domain prefix. Metadata binds
a stable version, publication/expiry times, and each platform's immutable HTTPS
URL, byte size and SHA-256 digest. New downloads require a valid signature and
unexpired metadata; cache activation rechecks authenticity and executable bytes.
The signer verifies the same schema and validity rules as the client.

Review the **draft** release and publish it as GitHub's latest stable release
only after all assets are present. Never overwrite executables for an existing
version or mark an older version as latest. Release metadata expires after 30
days; if no new client release is planned, refresh only the publication and
expiry timestamps in `update-manifest.json`, re-sign with the same key, and
replace only those two metadata assets on the latest release before expiry.
Never change its version, executable URLs, hashes or sizes during refresh.
Existing cached binaries keep working offline after expiry. Changing the trust
root requires users to manually install a newly trusted launcher; this updater
does not implement automatic key rotation.
