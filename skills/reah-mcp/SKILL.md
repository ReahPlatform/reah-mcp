---
name: reah-mcp
description: Read and analyze Reah balances, wallets, banking, transactions, settled cashflow, cards, contacts, invoices and treasury through the read-only Reah MCP tools.
metadata:
  tools: reah_catalog,reah_describe_type,reah_query,reah_get_entity,reah_list_balances,reah_list_wallets,reah_list_banking_accounts,reah_list_transactions,reah_list_settled_movements,reah_list_contacts,reah_list_invoices,reah_list_approvals,reah_list_card_accounts,reah_list_card_transactions,reah_list_treasury_positions,reah_list_statements,reah_list_members,reah_list_assets,reah_search
---

Use the connected Reah MCP server for the configured entity. Start with
`reah_catalog` to see scope and return types. Use `reah_describe_type` when a
field, argument, unit, or enum needs explanation; its schema is local and pinned.
Only reviewed public fields and arguments are exposed. Never invent a field or
assume that the upstream API's broader schema is available through this server.

Business read tools take `arguments` and optional `fields`. Put GraphQL field
selections inside `fields`, without the outer result braces. Required input
arguments are documented by the parent type in the catalog. For `reah_query`,
declare `$entityId: ID!` and use it for every root entity argument; the server
supplies its value. Other enterprise scopes are unavailable.

For balances and allocation, read the relevant wallet, banking, card and treasury
surfaces. `totalBalance` is a raw API observation, not the Agent's normalized
spam-safe summary. Do not add overlapping asset/wallet/instrument totals together.

For cashflow, burn or period receipts, prefer `reah_list_settled_movements` with
explicit RFC3339 `settledFrom` and `settledTo`. Confirm the user's timezone and
resolve relative dates from the current date. Ordinary transaction lists include
pending/failed/internal activity and are not a settled cashflow ledger. Preserve
the selected filters, Treasury/spam/internal/small-balance exclusions, valuations
and coverage. Do not equate absent external ingestion proof with incomplete Reah
query acquisition; missing pages, unposted partial transactions, or unavailable
valuation still constrain the answer.

Follow `cursor` or `nextPageToken` by passing it to `arguments.after`; keep the
original filters and time window unchanged. For cards, each account has its own
nested `cards.cursor`; use `fields` to select the next page of that connection.
If pagination stops early, report the bounds of the data actually read. Search
matches are not a complete population, even if the result includes a total.

Every read returns `field_semantics` and `normalized_values`. Use normalized
`amount` and `currency` as exact decimal strings for display and calculations.
Paths are JSON Pointers into raw `data`, with array indices; metadata paths use
`*` for array items. `source_scale` describes the raw-to-major-unit conversion.
Never divide normalized amounts again. Never substitute zero for missing/null
or explicitly unavailable amounts.

Raw balance and Treasury USD values are cents, including fractional cents;
invoice `amountDueUsd` is already dollars. Native balances and Treasury asset
values are already major asset units. Settled postings use their explicit scale
or instrument decimals. Custom queries must select the required currency,
scale, and underlying-asset companions, even when they are aliased or fragmented.

APYs are decimal fractions: raw `0.05` means 5%. Normalized `percent` is already
multiplied by 100; do not multiply it again. Position `currentAPY` is Utilized
APY, while `vault.apy` is current product APY. Summary `averageAPY` is weighted
product APY, not average Utilized APY. Do not substitute one rate for another.
Use exact decimal arithmetic and cite the window, filters and data timestamp.

Tools preserve raw GraphQL data and errors alongside the unit contract. A response with `isError: true` may
contain partial data; explain the limitation before making aggregate claims.
Use the card's `last4` in user-facing reports.

The server exposes no payments, approvals, edits, report generation, uploads,
browser automation, or card PAN/CVV reads. It also does not expose the Agent's
financial helper calculations or evidence-admission pipeline. Use the available
reads to answer the request; describe unsupported actions as unavailable.
