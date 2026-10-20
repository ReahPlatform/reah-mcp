# Reah MCP

Give AI assistants read-only access to Reah financial data: balances, accounts,
transactions, settled cashflow, card activity, invoices, and treasury positions.
The service validates queries locally and uses the upstream identity's Reah
permissions to access data.

Includes a Go MCP server and plugin configurations for Claude Code, Codex, and
Cursor, with skills for financial analysis and user-requested browser actions.
The API schema is pinned to the Reah revision dated October 18, 2026.

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
and two optional skills:

- `reah-mcp` guides financial analysis with the read-only MCP tools. Field
  meanings, units, and normalized values are provided by the server independently
  of the skill.
- [`reah-browser`](skills/reah-browser/SKILL.md) guides user-requested actions
  in the signed-in Reah website,
  such as editing contacts, saving invoice drafts, or preparing a transaction
  for user review. It requires browser tools supplied by the host; this plugin
  does not install a browser controller. Web permissions and the user's task
  govern browser actions; read-only MCP consent does not authorize them. Final
  fund movement and signing are handed to the user.

The browser skill loads guidance progressively: the short entry routes to the
current task, individual guides cover the reviewed UI flow, and payment-method,
approval, or recovery details load only when needed. The same skill instructions
are shared by Claude Code, Codex, and Cursor through their plugin configurations.

Skills are distributed through the plugin or installed as local skill folders.
Adding an MCP server URL alone does not install them. Browser workflows use the
user's web session without exporting it to the MCP server. The MCP tools remain
read-only.

For MCP queries, install the binary and configure its environment first.
Installation also requires access to this private GitHub repository.

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

## Read-only tools

| Tool | Data |
| --- | --- |
| `reah_catalog` / `reah_describe_type` | Query catalog, entity scope, fields, arguments, and units |
| `reah_get_entity` / `reah_list_balances` | Entity overview and balances paginated by instrument |
| `reah_list_wallets` / `reah_list_banking_accounts` | Wallets, bank accounts, and balances |
| `reah_list_transactions` / `reah_list_settled_movements` | Transaction lifecycle and settled cashflow coverage |
| `reah_list_contacts` / `reah_list_invoices` | Contacts and invoices |
| `reah_list_approvals` / `reah_list_members` | Approval records and entity members |
| `reah_list_card_accounts` / `reah_list_card_transactions` | Card accounts, masked cards, and card activity metadata; transaction amounts are not exposed |
| `reah_list_treasury_positions` / `reah_list_statements` | Latest 200 matching Treasury positions with coverage metadata, and existing statement jobs |
| `reah_list_assets` / `reah_search` | Full entity asset catalog without pagination, and financial object search |
| `reah_query` | Custom GraphQL queries within the allowed read schema |

Business tools accept surface-specific GraphQL `arguments` and optional `fields` for
result selections. `reah_describe_type` lists the public arguments and their
defaults; upstream fields and filters outside this contract are not exposed:

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
The asset catalog returns the full collection; `first`, `after`, and `cursor`
are not exposed for assets. Treasury positions and their summary have a fixed
upstream limit of 200 matching positions and no continuation. Read `read_coverage`
before claiming a complete portfolio; following cursors cannot bypass that limit.

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

- Exposes an explicit allowlist of reviewed fields and arguments. Undocumented
  prices, quotes, financial breakdowns, and other unreviewed upstream fields
  are absent from both the packaged schema and discovery results.
- Every read returns raw GraphQL `data` / `errors`, `field_semantics` for its
  selections, exact `normalized_values`, and `read_coverage` for source-bounded
  fields, keyed by JSON Pointer. Raw decimal
  strings, cursors, coverage, and exclusion counts stay intact. Use normalized
  `amount` and `currency` for display/calculation; never apply a second unit
  conversion. `source_scale` describes the conversion from the raw value.
- USD balance and Treasury raw amounts are cents, including fractional cents.
  Invoice `amountDueUsd` is already dollars. Native balances and Treasury asset
  values are already major asset units. Settled movement amounts use their
  explicit `instrumentDecimals` or `scale`. Conversions use decimal strings,
  without floating-point rounding, and preserve negative amounts.
- APYs are decimal fractions: `0.05` means 5%. Normalized `percent` is already
  multiplied by 100. `Vault.apy` is current product APY, and summary `averageAPY`
  is a position-weighted average of product APYs within the bounded summary.
  Position `currentAPY` is absent: upstream XIRR calculations conflate missing,
  unsolved, and nonpositive returns as zero, so availability cannot be established.
- `CardTransaction.amountMinor` is absent from the public schema and discovery.
  The upstream list mixes USD card spending and USDC funding amounts without
  an explicit scale. Activity metadata and its pagination remain available;
  these records alone cannot establish monetary spending totals.
- Treasury lists cover at most the latest 200 matching positions, ordered by
  creation time and ID descending. `read_coverage` marks a full batch as
  `not_proven`; summaries alone always have `not_proven` completeness because
  upstream provides neither a contributing count nor continuation. A successful
  read does not imply a complete portfolio total.
- Custom selections must include the currency, scale, or underlying-asset
  companions documented by their contracts. Aliases, fragments, and conditional
  selections are supported. Unrecognized upstream shapes, units, or enum values
  fail the read instead of publishing ambiguous values. Masked-card spending
  limits use integer USD cents; card transaction
  amounts are not part of this contract.
- Null and legacy unavailable invoice valuations never become zero. Partial
  GraphQL errors set MCP `isError: true` while preserving interpretable data.
  See the [data contract](docs/data-contract.md) for examples and source evidence.
- Ordinary transaction lists include multiple lifecycle states. Use settled
  movements to analyze actual cashflow; raw transactions cannot substitute for
  a settled ledger.
- Reuses the Agent's read API contracts, pinned schema and reviewed unit
  evidence. Financial calculation policies, spam-safe balance normalization,
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

Checks cover all business tools, MCP and HTTP authentication, the public field
allowlist, unit contracts, exact high-precision conversions, aliases/fragments,
nulls, partial errors, entity scope, pagination, response limits, and redirects.
Regression checks also cover mixed USD/USDC card metadata, removal of ambiguous
fields and asset pagination, and Treasury source limits and partial-result coverage.
Tests use a local mock GraphQL service; live
integration testing requires environment-specific credentials.

[docs/upstream.json](docs/upstream.json) records the pinned `reah-agent`
[commit](https://github.com/ReahPlatform/reah-agent/commit/474fda4a771289782c5f9e793a4f0c33e7076f79)
and SHA-256 hashes of its schema sources and semantic registry. After updating
the pin, run `make schema` to extract only reviewed public fields and reachable
types from the specified commit in the adjacent `../reah-agent` checkout. New
upstream fields are never admitted automatically. Hash mismatches, missing
reviewed fields, or argument drift fail generation.
Raw mutation schemas are not packaged. This service does not use a database,
sqlc, or copied sqlc output.
