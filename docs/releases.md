# Go client releases

Go module versions use root SemVer tags such as `v0.1.0`. Hosted plugin
manifests keep their own version; a client release does not change them.
Do not reuse a tag or overwrite a release archive after distribution.

From a clean checkout of the intended `main` commit:

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

Go downloads require no npm organization or token. This repository is private,
so its source and release downloads require GitHub repository access. Do not
promise public `go install` or public release links until distribution is made
public. Compiled binaries may be shared through an approved distribution channel
without requiring Go or Node on the user's machine.
