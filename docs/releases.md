# npm releases

Lerna manages versions, `CHANGELOG.md`, Git release commits and tags for the
optional `@reah/mcp` client. It manages the existing root package; no workspace
or source-directory move is required. Hosted plugin manifests have their own
version and are not changed by an npm release.

## Prerequisites

Use Node.js 24.15.0 and the committed npm lockfile for release development.
The installed client's runtime minimum remains Node.js 22.16.0. Lerna is a
development dependency and is not installed by consumers.

Before the first public release:

1. Confirm that Reah controls the npm `@reah` organization and can publish
   `@reah/mcp` as a public package.
2. Configure an npm trusted publisher for GitHub owner `ReahPlatform`, repository
   `reah-mcp`, workflow `publish-npm.yml`. Follow npm's current setup requirements;
   an initial authenticated publish may be needed before package settings exist.
3. For that initial publish, use the same checked release tag and run
   `npm run release:publish` from an authenticated maintainer environment. Never
   commit an npm token. Subsequent CI releases use OIDC without a stored token.

The release job uses a GitHub-hosted runner because npm trusted publishing does
not support the repository's self-hosted check runner. A private GitHub repository
can use trusted publishing, but npm does not generate provenance for it.
See [npm trusted publishers](https://docs.npmjs.com/trusted-publishers/) and
[Lerna OIDC publishing](https://lerna.js.org/docs/recipes/oidc-trusted-publishing).

## Prepare a version

Start from a clean, current `main` checkout after the client changes have merged:

```sh
git switch main
git pull --ff-only origin main
npm ci --ignore-scripts
npm run release:version
```

The version command runs lint, tests and both builds before Lerna changes files.
Lerna uses Conventional Commits to recommend a version and generate the changelog.
Use `fix: ...` for fixes, `feat: ...` for features, and a `BREAKING CHANGE:` footer
for incompatible changes in commits that reach `main`, including squash commits.
Review the proposed version before accepting it. For an explicit patch release,
including the first release when no MCP tags exist:

```sh
npm run release:version -- patch --force-publish
```

Lerna updates `package.json`, `package-lock.json`, `lerna.json` and
`CHANGELOG.md`, creates a local release commit and a tag such as `mcp-v0.1.1`.
It does not push or upload to npm. Inspect the commit and changelog, then push
the release commit and its tag through the repository's approved Git workflow:

```sh
git show --stat HEAD
git show HEAD:CHANGELOG.md
git push --atomic origin main mcp-v0.1.1
```

Use the actual tag created by Lerna. The `mcp-v` prefix keeps client release tags
separate from hosted plugin releases. Do not move or reuse a published tag or npm
version; fix an incorrect release with a new version.

## Publish a checked tag

On GitHub Actions, select **Publish npm package**, run the workflow on `main`,
and enter the existing tag, for example `mcp-v0.1.1`.

The workflow verifies that the tag belongs to `main` and matches both package
and Lerna versions. It installs locked dependencies, runs lint, tests and builds,
and inspects the package contents before `lerna publish from-package` uploads
the public package to the official npm registry. Publishing is serialized and
does not run on ordinary pushes, pull requests or tag creation.

`from-package` skips versions already on npm, allowing a completed upload to be
checked again without changing versions. If a run fails with an uncertain upload
result, inspect npm and the job log before retrying.

After publishing, verify the version and executable from a clean environment:

```sh
npm view @reah/mcp version --registry=https://registry.npmjs.org
npx --yes @reah/mcp@0.1.1 --help
```

No npm release has been performed merely by adding this configuration. Do not
advertise an available package until the registry verification succeeds.
