# Reah MCP

Connect AI assistants to Reah financial data through a hosted, read-only MCP
service. Sign in with your Reah account in the browser, choose one or more
businesses, and authorize the connection.

**Default environment: staging, for integration testing.** All packaged MCP
configurations use:

```text
https://platform.stg.reah.com/mcp
```

The Gateway runs the MCP service and executes predefined GraphQL reads through
existing business resolvers. This repository distributes client configurations,
installation instructions, an optional analysis skill, and an optional
[local Go client](docs/local-client.md). The hosted connection requires
no local runtime, Agent credential, or manually configured bearer token. The
optional local client also supports explicitly authorized transfer and execution
when the matching server features are enabled; see its signing boundary.

The local client is written in Go and distributed as a standalone executable.
It handles stdio MCP, OAuth, secure credential storage and local signing.
Build from source or use a published GitHub binary release; no Node.js or npm
runtime is required. See [local installation](docs/local-client.md).

## Connect

Use a client with remote Streamable HTTP MCP and OAuth support. Add the MCP URL
in the client, then complete the Reah browser login and authorization page. The
client discovers the authorization endpoints and registers itself automatically.
Do not open the authorization endpoint directly: it requires parameters supplied
by the client's OAuth flow.

### Codex

```sh
codex mcp add reah-stg --url https://platform.stg.reah.com/mcp
```

Follow the authorization URL shown by Codex if the browser does not open. To
start sign-in again for an existing entry:

```sh
codex mcp login reah-stg
```

Restart the chat or reload its MCP connection if tools added during the chat do
not appear yet.

### Claude Code

```sh
claude mcp add --transport http reah-stg https://platform.stg.reah.com/mcp
```

Open `/mcp` in Claude Code and authenticate the Reah connection. See the
[Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

### Claude custom connector

In Claude's connector settings, add a custom connector with the URL above and
follow the sign-in flow. If asked for an OAuth registration method, choose
automatic registration. Availability depends on the client's plan and workspace
policy. See [Claude's remote connector guide](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp).

### Cursor

Merge the following into your project's `.cursor/mcp.json` or your user-level
`~/.cursor/mcp.json`, then authenticate the connection in Cursor:

```json
{
  "mcpServers": {
    "reah-stg": {
      "url": "https://platform.stg.reah.com/mcp"
    }
  }
}
```

See the [Cursor MCP documentation](https://cursor.com/docs/mcp). The packaged
configuration is also available in [examples/staging.mcp.json](examples/staging.mcp.json).

## Authorization and business selection

The browser authorization page shows the requesting application, the businesses
available to your account, the read scope, and the access duration. Select one or
more businesses and approve once. Each connection belongs to the authorizing
user and client; existing business permissions still apply to reads.

The client stores and refreshes OAuth credentials. Access tokens last at most
15 minutes, capped by the connection's expiry. The selected access duration
bounds the whole connection, including refresh tokens. Refreshing rotates the
refresh token and does not extend that duration or expand access. After expiry,
authorize a new connection. Disconnecting the connection in Reah revokes its
access and refresh tokens. Removing a local client configuration alone does not
revoke the server-side grant.

The assistant first calls `reah_list_entities` to get authorized business names
and IDs. With one business, it uses that ID without asking. With several, it
chooses the business named in the request, or asks if the choice is ambiguous.
Every business tool requires `entityId`, including when only one is available.
The user does not need to copy or type that ID.

For example: “Show the balances for Acme” lets the assistant resolve Acme from
`reah_list_entities`, then call `reah_list_balances` with its ID. For a comparison
across authorized businesses, the assistant makes separate calls for each.

## Tools and financial values

The hosted service provides **20 tools: 18 business reads and 2 discovery tools**.
See the [tool reference](docs/tools.md) for names, parameters, and pagination, and
the [data contract](docs/data-contract.md) for units, normalized results, and
coverage limits. The connected server's tool schemas and `reah_catalog` are the
authoritative runtime contract.

Calls send a tool name and typed arguments to `/mcp`. Query documents and output
fields are fixed by the Gateway; arbitrary GraphQL, custom field selections,
and financial write operations are not exposed. Financial responses preserve
raw GraphQL data and include exact normalized values and coverage information.
Missing valuations are unavailable, never zero. Use normalized values without
converting them a second time.

## Optional plugin and analysis skill

Adding the MCP URL connects tools; it does not install skills. This repository
also packages the portable [`reah-mcp`](skills/reah-mcp/SKILL.md) skill for
financial analysis with read-only MCP tools.

Plugin installation requires access to this GitHub repository. The plugin
already includes the staging MCP connection; choose either plugin installation
or manual MCP setup to avoid duplicate connections.

Claude Code:

```text
/plugin marketplace add ReahPlatform/reah-mcp
/plugin install reah@reah-marketplace
```

Then authenticate the plugin's Reah connection through `/mcp`.

Codex:

```sh
codex plugin marketplace add ReahPlatform/reah-mcp
```

Select Reah in `/plugins`, then authenticate its MCP connection. Cursor can use
this repository's plugin manifests; for local plugin development, copy the
checkout into `~/.cursor/plugins/local/reah` and reload Cursor.
See [Cursor's local plugin guide](https://cursor.com/docs/plugins#test-plugins-locally).

## Migrate from the local server

Version 0.3 replaces the local Go server with the hosted staging connection.
Remove the old `command: reah-mcp` entry before adding the URL or installing the
updated plugin. Remove its environment settings and any manually configured
Authorization header; OAuth now manages the connection. The legacy local server
and its source remain available in Git history.

The hosted API requires `entityId` and uses flat typed arguments. It includes separate card, invoice, transaction-detail and entity discovery
tools. Statement exports, `reah_query` and `reah_describe_type` are not exposed. The former `arguments` and `fields` wrappers are not valid.
Results now retain raw data under `graphql`; normalization paths start with
`/graphql/data/`. Update saved prompts or integrations that relied on the old
format using the [current contract](docs/data-contract.md).

## Validate and package

Maintainers need Go 1.25+, Python 3.12+ and Make:

```sh
make lint
make test
make build
```

Checks cover the Go client, manifests, staging URLs, credential-free configuration,
skill links, and the Codex plugin mirror. Tests exercise OAuth, local signing,
MCP transport, rejected configurations and archive contents. Build writes the
local executable to `bin/reah-mcp` and `dist/reah-0.3.1.zip` with documentation,
manifests, and the analysis skill, resolving the Codex mirror's symlinks into
portable files.
It does not build or deploy a server. Runtime authentication, permissions, and
financial normalization tests belong to the Gateway and Backend services.


The optional local Go client supports internal Banking transfer approvals from
existing Ledger Send v1 quotes paid by Gas Station using a separately registered
financial credential. It preserves the quoted UUID identities and signed fields
without adding a chain asset. Execution requires an authenticated Gateway catalog
advertising `bank_internal_transfer` and a compatible Hodor private binding
acknowledgment. This bank route trusts Ledger transaction semantics as the Web
flow does; wallet and convert construction checks remain separate. Keep the same
business, approval and signing-request identities across continuations, and
reconcile unknown outcomes without retrying or replacing the operation. External
bank payouts and bank-paid internal gas remain outside this adapter. Business
schemas and preparation stay in the client; no remote templates or
server-preparation RPC are used. Client support alone does not establish server
deployment, completed execution or settlement.
See [the local client guide](docs/local-client.md).

The Go client separates OAuth consent from owner-confirmed credential
registration. Agents can use `login --no-browser --json` for labeled continuation
URLs, `enroll-key` to resume the same connection, and `status --check` or the
always-available local `reah_local_connection_status` diagnostic to verify read
access and each enrolled business's signing binding before claiming readiness.
