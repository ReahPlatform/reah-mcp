# Local MCP client

The optional Node.js client serves Reah tools over stdio. The existing hosted
plugin remains the default. This package is under local development and has not
been published to npm; do not assume an `npx` package is available yet.

## Run from a checkout

Use Node.js 22.16 or newer with a desktop OS credential store. Linux requires an
unlocked Secret Service session. macOS uses Keychain; Windows uses Credential
Manager. There is no plaintext credential fallback.

```sh
npm ci --ignore-scripts
npm run build
node dist/client/cli.js login --profile default
```

The login command registers this local client, opens Reah in your browser, and
waits up to five minutes for consent. Sign in, select one or more businesses and
an access duration, then authorize. The temporary callback listener binds only
to `127.0.0.1` on an ephemeral port. It checks state and exchanges the code with
PKCE S256 before showing success. Browser cookies and Web signing keys are never
copied into the local client.

Register the built executable in your AI client's local stdio configuration,
substituting the absolute checkout path:

```json
{
  "mcpServers": {
    "reah-local": {
      "command": "node",
      "args": ["/ABSOLUTE/PATH/reah-mcp/dist/client/cli.js", "serve", "--profile", "default"]
    }
  }
}
```

The same command and argument list work with stdio-capable Codex, Claude Code
and Cursor clients. Run login before enabling the connection. Reload the MCP
connection if the host previously loaded it without credentials. Login is
separate from server startup so a browser consent step cannot exhaust the host's
MCP initialization timeout.

First ask for the authorized businesses and confirm the returned names and IDs.
The local client forwards the live reviewed tool schemas, descriptions and
results. It does not duplicate financial conversions or accept arbitrary
GraphQL. Financial writes and unreviewed tools are excluded even if a later
Gateway release adds them. See the [tool reference](tools.md).

## Reconnect and disconnect

```sh
node dist/client/cli.js status --profile default
node dist/client/cli.js login --profile default
node dist/client/cli.js logout --profile default
```

Status reports local credential presence, access-token expiry and the public
device key. It does not prove that a connection is still valid on the server.
Login disables the old local credentials and revokes the previous connection
before opening new consent. The same profile reuses its registered client ID.
All tools reload credentials on each HTTP request, so running local clients use
the new grant instead of a cached bearer token. A request already in flight may
still finish during reconnection; verify the newly authorized business list
before continuing.

Refresh is automatic, serialized across processes sharing a profile, and saves
rotated credentials in the OS vault. An interrupted refresh requires a new login;
the client never retries a refresh token whose consumption is uncertain. Logout
revokes the whole connection and removes the local credentials and device key.
If revocation cannot be confirmed, local access stays disabled and the command
directs you to disconnect the app in Reah Settings.

The default server is `https://platform.stg.reah.com/mcp`. Use matching `--server`
and `--profile` arguments for every command and the host configuration. Profiles
isolate clients, credentials and keys. Tokens and private keys live under the
`Reah MCP Local` OS credential-store namespace. `~/.reah-mcp` contains only empty
coordination files and transient locks; it contains no credentials. The program
does not read Codex's MCP credential store or invoke the macOS `security` CLI.

## Signing boundary

Each login generates a fresh P-256 key for that connection and keeps its private
key in the OS vault. Ordinary login remains read-only. To opt into credential
preparation on a server that advertises support:

```sh
node dist/client/cli.js login --enroll-key --profile default
node dist/client/cli.js status --profile default
# Resume after an interrupted proof submission:
node dist/client/cli.js enroll-key --profile default
```

The public key is fixed in the OAuth request before browser consent. Backend
binds it to the authenticated user, connection and selected businesses. The
client checks the Hodor proof's purpose, server, client, public key, connection,
businesses and expiry, then signs its exact UTF-8 bytes using SHA-256/P-256 and
a 64-byte IEEE P1363 signature. It sends only the signature; it never sends the
private key or exposes an arbitrary signing tool. Repeated preparation uses the
same challenge and does not extend its lifetime.

**Successful possession proof means `awaiting_provider`, not an active Turnkey
credential.** This release does not register credentials with Turnkey or enable
transfers, approval execution or any financial mutation. The existing internal
Web-compatible approval-signing primitive is not exposed to MCP callers. The
normal hosted plugin remains read-only and needs no local process.

Deploy the Hodor preparation service, matching Backend and Gateway contracts,
and enable their separate credential-preparation flags before using this option.
An unsupported server fails before replacing the current connection. A failed
proof after successful OAuth leaves read-only access available; retry `enroll-key`
within the preparation deadline, or start a new login after expiry. Disconnecting
the OAuth grant blocks all subsequent preparation requests. Provider-side
credential activation and revocation remain separate follow-up work.

## Checks

`make lint`, `make test` and `make build` check both the hosted plugin and local
client. Local integration tests exercise browser callbacks, MCP initialization
and read calls, rotating refresh, changed business selection and revocation
against a simulated OAuth/Gateway service. They do not access your OS vault or
real financial data. A live staging login is a separate manual verification.

Future npm releases should be installed at an explicitly pinned version. The
hosted plugin must not be changed to an unpublished local executable.
