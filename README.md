# Reah MCP

Give AI assistants read-only access to Reah financial data: balances, accounts,
transactions, settled cashflow, card spending, invoices, and treasury positions.
The service validates queries locally and uses the upstream identity's Reah
permissions to access data.

Includes a Go MCP server and plugin configurations for Claude Code, Codex, and
Cursor. The API schema is pinned to the Reah revision dated October 18, 2026.

## Run locally

Requires Go 1.25.5+ and a Reah Agent bearer credential (`reah_agent_...`) with
access to the target entity. The current GraphQL Gateway bearer authentication
path accepts Agent credentials; an ordinary browser session or a Google login
token is not a substitute.

```sh
make build
install -m 755 bin/reah-mcp /usr/local/bin/reah-mcp
export REAH_GRAPHQL_URL='https://YOUR-REAH-GRAPHQL-HOST/graphql'
export REAH_ENTITY_ID='YOUR_ENTITY_ID'
read -rs REAH_ACCESS_TOKEN
export REAH_ACCESS_TOKEN
reah-mcp
```

The last command starts the stdio MCP server. Logs go to stderr. Supply
credentials through the process environment; keep them out of plugin manifests
and Git. Replace expired credentials and restart the server. Browser cookies and
automatic token renewal are not used.

Configure a client after making the three environment variables available to its
process:

```json
{"mcpServers":{"reah":{"command":"reah-mcp"}}}
```

Desktop clients launched from Finder generally do not inherit terminal exports.
Set the variables in the client's MCP environment settings, or launch the client
from the configured terminal.

## Install the plugin

This repository includes Claude Code, Codex, and Cursor / Agent Plugins manifests
and a `reah-mcp` query skill. Install the binary and configure its environment
first. Installation also requires access to this private GitHub repository.

Claude Code:

```text
/plugin marketplace add ReahPlatform/reah-mcp
/plugin install reah@reah-marketplace
```

Codex:

```sh
codex plugin marketplace add ReahPlatform/reah-mcp
```

Then select Reah in `/plugins`. Cursor can use the root plugin files. For local
development, copy the repository to `~/.cursor/plugins/local/reah` and reload the
window. These configurations launch the local server; they do not point to an
undeployed hosted service.

## Self-hosted HTTP

```sh
export REAH_MCP_HTTP_TOKEN="$(openssl rand -hex 32)"
reah-mcp -transport http -listen 127.0.0.1:8080
```

The Streamable HTTP endpoint is `/mcp`. Every MCP request requires
`Authorization: Bearer <REAH_MCP_HTTP_TOKEN>`. This is a separate client access
credential. `REAH_ACCESS_TOKEN` is used only for upstream GraphQL calls. Provide
an HTTPS reverse proxy for remote access. See
[examples/http.mcp.json](examples/http.mcp.json).

An instance uses one fixed upstream credential and entity. Clients holding its
HTTP token share that identity, so unrelated users and entities require isolated
instances. The HTTP token is not an OAuth access token.

## Hosted authorization design

The proposed hosted experience is **Add Reah → sign in → choose a business →
authorize**. It reuses Reah login and the Agent credential lifecycle, with a
separate OAuth authorization grant and enforced read-only delegation.

The implementation plan, API contracts, data model, rollout, and acceptance
criteria are in [Hosted OAuth design](docs/hosted-oauth-design.md).
This is a design proposal; hosted OAuth and its proposed URLs are not deployed
by this repository's current implementation.

## Read-only tools

| Tool | Data |
| --- | --- |
| `reah_catalog` / `reah_describe_type` | Query catalog, entity scope, fields, arguments, and units |
| `reah_get_entity` / `reah_list_balances` | Entity overview and balances paginated by instrument |
| `reah_list_wallets` / `reah_list_banking_accounts` | Wallets, bank accounts, and balances |
| `reah_list_transactions` / `reah_list_settled_movements` | Transaction lifecycle and settled cashflow coverage |
| `reah_list_contacts` / `reah_list_invoices` | Contacts and invoices |
| `reah_list_approvals` / `reah_list_members` | Approval records and entity members |
| `reah_list_card_accounts` / `reah_list_card_transactions` | Card accounts, masked cards, and card transactions |
| `reah_list_treasury_positions` / `reah_list_statements` | Treasury positions and existing statement jobs |
| `reah_list_assets` / `reah_search` | Asset catalog and financial object search |
| `reah_query` | Custom GraphQL queries within the allowed read schema |

List tools accept `arguments` for GraphQL arguments and optional `fields` for
result selections:

```json
{"arguments":{"first":100,"after":"PREVIOUS_CURSOR"},"fields":"transactions { id humanId type state } cursor"}
```

Settled movements require an explicit time window:

```json
{"arguments":{"settledFrom":"2026-10-01T00:00:00Z","settledTo":"2026-11-01T00:00:00Z","first":100}}
```

Pass a settled movement response's `nextPageToken` as `arguments.after`. Other
connections generally return `cursor`. Each card account has a separate nested
`cards.cursor`; select subsequent pages through `fields`, for example
`cards(first:100, after:"...")`. A call does not automatically retrieve every
page. Search matches do not establish a complete financial dataset.

For custom queries, declare and use `$entityId: ID!`. The server supplies the
configured entity ID:

```graphql
query Cash($entityId: ID!) {
  entity(id: $entityId) {
    id
    totalBalance { usdAmount priceVersion }
    treausryPositionAccountSummary { currentValueInUSD averageAPY }
  }
}
```

The `treausry...` spelling is part of the upstream API. Use `reah_describe_type`
to inspect `Query`, `Entity`, or a result type before selecting additional fields.

## Data semantics and boundaries

- Returns raw GraphQL `data` and `errors`, preserving decimal strings, cursors,
  settled coverage, and exclusion counts. Missing amounts are not converted to
  zero. Partial errors set MCP `isError: true` while preserving returned data.
- Monetary units follow the upstream fields. Decode `usdValue.amountMinor`
  using `scale`, and `signedAmountSmallest` using `instrumentDecimals`. Read
  field descriptions before comparing treasury values, card limits, or balances.
  Use exact decimal arithmetic.
- Ordinary transaction lists include multiple lifecycle states. Use settled
  movements to analyze actual cashflow; raw transactions cannot substitute for
  a settled ledger.
- Reuses the Agent's read API contracts and pinned schema. Agent normalization
  helpers, financial calculation policies, spam-safe balance normalization,
  evidence admission, LLM execution, Temporal workflows, browser automation,
  and approval execution are outside this service. `totalBalance` is an upstream
  observation, not the Agent's normalized balance summary.
- Parses and validates GraphQL before network access. Only one query operation
  and an explicit set of read roots are allowed. Every root entity argument
  must match the configured scope, including aliases and fragments. The schema
  excludes mutations, subscriptions, identity lifecycle operations, Agent
  conversations, and card PAN/CVV access. Upstream services enforce permissions
  and resource ownership.
- Limits queries to 64 KiB, 2,000 expanded selections, and 16 field levels.
  Explicit page sizes must be 1–200. Responses are limited to 8 MiB and requests
  time out after 30 seconds. Redirects are not followed.
- Statement tools read existing jobs. Generating statements or requesting
  download URLs requires upstream mutations and is not exposed.

## Development

```sh
make lint
make test
make build
```

Checks cover all business tools, MCP and HTTP authentication, rejected queries
before network access, entity scope, pagination, precision, partial errors,
response limits, and redirects. Tests use a local mock GraphQL service; live
integration testing requires environment-specific credentials.

[docs/upstream.json](docs/upstream.json) records the pinned `reah-agent`
[commit](https://github.com/ReahPlatform/reah-agent/commit/474fda4a771289782c5f9e793a4f0c33e7076f79)
and SHA-256 hashes of its schema sources. After updating the pin, run
`make schema` to extract allowed fields and reachable types from the specified
commit in the adjacent `../reah-agent` checkout. Hash mismatches fail generation.
Raw mutation schemas are not packaged. This service does not use a database,
sqlc, or copied sqlc output.
