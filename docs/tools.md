# Hosted tool reference

The service exposes 21 fixed, read-only tools. Use the live MCP input schemas
and `reah_catalog` for the deployed contract. All business tools below require
`entityId` from `reah_list_entities`; the two discovery tools accept `{}`.
Arguments are flat JSON objects with no extra keys, `fields`, or GraphQL text.

| Tool | Read | Additional arguments |
| --- | --- | --- |
| `reah_list_entities` | Authorized business IDs and names | None; no `entityId` |
| `reah_catalog` | The 19 business reads, input schemas, reviewed fields, units, and coverage guidance | None; no `entityId` |
| `reah_get_entity` | Business identity, state, type, and current valuation | None |
| `reah_list_balances` | Instrument balances | `first`, `after` |
| `reah_list_wallets` | Wallet roster and valuations; capped at 100 without continuation | None |
| `reah_list_banking_accounts` | Bank account roster and valuations; no account numbers or credentials | None |
| `reah_list_transactions` | Transaction lifecycle metadata, not a cashflow ledger | `first`, `after` |
| `reah_list_settled_movements` | Settled posting facts, valuations, exclusions, and ingestion coverage | Required `settledFrom`, `settledTo`; optional `first`, `after`, `includeTreasury`, `includeSpam`, `excludeInternalActivity`, `excludeSmallBalances`, `transactionIds` |
| `reah_list_contacts` | Contact IDs, names, and types | `first`, `after` |
| `reah_list_invoices` | Invoice metadata and amounts | `first`, `after` |
| `reah_get_invoice` | One invoice owned by the selected business | Required `invoiceId` |
| `reah_list_approvals` | Approval lifecycle summaries; no decisions or execution | `first`, `after` |
| `reah_list_cards` | Masked Rain cards and spending limits; no PAN or CVV | `first`, `after` |
| `reah_list_card_accounts` | Card account roster and valuations | None; use `reah_list_cards` for cards |
| `reah_list_card_transactions` | Card activity metadata; monetary amounts are excluded | `first`, `after` |
| `reah_list_treasury_positions` | Latest at most 200 positions and their bounded summary; no continuation | None |
| `reah_list_statement_exports` | Existing export job metadata; no creation, download URL, or file access | `first`, `after` |
| `reah_list_tasks` | Pending tasks assigned to the authenticated user; no complete history or continuation | None |
| `reah_list_members` | Member IDs, names, emails, and joining dates | `first`, `after` |
| `reah_list_assets` | Full asset catalog; instrument decimals take precedence over asset defaults | None; no pagination |
| `reah_search` | Bounded matches across permitted accounts, transactions, approvals, and contacts | Required search-text `query`; optional `limit` |

## Argument rules

IDs are nonempty strings of at most 256 characters, without surrounding
whitespace. Pagination `first` defaults to 100 and accepts 1–100, or 1–200 for
settled movements. `after` is an opaque cursor of at most 2,048 characters;
omit it on the first page rather than sending null. Pass the response's `cursor`
or settled movement `nextPageToken` as the next call's `after`. Retain the same
business, filters, and time window across pages.

Settlement bounds are required RFC3339 timestamps with a timezone offset, with
`settledFrom` earlier than `settledTo`. Resolve the user's timezone before
choosing bounds. Optional settlement flags default to false. `transactionIds`
accepts at most 200 IDs: omitted or null applies no transaction-ID filter, while
an empty array intentionally selects no transactions. Search `query` is 1–1,024
characters of nonblank text, never GraphQL. Search `limit` defaults to 100 and
accepts 1–100. Omit unsupported parameters rather than sending null.

## Example sequence

First call `reah_list_entities` with `{}`. Resolve the intended business from
its `entities` array, then call `reah_list_balances` with:

```json
{"entityId":"ID_FROM_ENTITY_LIST","first":100}
```

For settled cashflow, call `reah_list_settled_movements` with an explicit window:

```json
{
  "entityId": "ID_FROM_ENTITY_LIST",
  "settledFrom": "2026-10-01T00:00:00+08:00",
  "settledTo": "2026-11-01T00:00:00+08:00",
  "first": 100
}
```

These are tool argument objects, not direct REST request bodies. The MCP client
handles the protocol envelope and OAuth. Use the [data contract](data-contract.md)
when interpreting the result. Exhausting pages does not establish ingestion
completeness; preserve source coverage and exclusions.
