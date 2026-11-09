# Hosted tool reference

The service exposes 20 fixed, read-only tools. Use the live MCP input schemas
and `reah_catalog` for the deployed contract. All business tools below require
`entityId` from `reah_list_entities`; the two discovery tools accept `{}`.
Arguments are flat JSON objects with no extra keys, `fields`, or GraphQL text.

| Tool | Read | Additional arguments |
| --- | --- | --- |
| `reah_list_entities` | Authorized business IDs and names | None; no `entityId` |
| `reah_catalog` | The 18 business reads, input schemas, reviewed fields, units, and coverage guidance | None; no `entityId` |
| `reah_get_entity` | Business identity, state, type, and current valuation | None |
| `reah_list_balances` | Instrument balances | `first`, `after` |
| `reah_list_wallets` | Wallet roster and valuations; capped at 100 without continuation | None |
| `reah_list_banking_accounts` | Bank account roster and valuations; no account numbers or credentials | None |
| `reah_get_transaction` | One business-owned transaction with sender/recipient parties and current saved-contact matches | Required `transactionId` |
| `reah_list_transactions` | Transaction lifecycle, sender/recipient addresses, networks, and current saved-contact matches; not a cashflow ledger | `first`, `after` |
| `reah_list_settled_movements` | Settled movements recorded in Reah, valuations, exclusions, and coverage flags | Required `settledFrom`, `settledTo`; optional `first`, `after`, `includeTreasury`, `includeSpam`, `excludeInternalActivity`, `excludeSmallBalances`, `transactionIds` |
| `reah_list_contacts` | Contact IDs, names, types, and saved blockchain addresses with networks and labels | `first`, `after` |
| `reah_list_invoices` | Invoice metadata and amounts | `first`, `after` |
| `reah_get_invoice` | One invoice owned by the selected business | Required `invoiceId` |
| `reah_list_approvals` | Approval lifecycle summaries; no decisions or execution | `first`, `after` |
| `reah_list_cards` | Masked Rain cards and spending limits; no PAN or CVV | `first`, `after` |
| `reah_list_card_accounts` | Card account roster and valuations | None; use `reah_list_cards` for cards |
| `reah_list_card_transactions` | Card activity metadata; monetary amounts are excluded | `first`, `after` |
| `reah_list_treasury_positions` | Latest at most 200 positions and their bounded summary; no continuation | None |
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

To inspect a settled movement's sender or recipient, use its `transactionId`:

```json
{"entityId":"ID_FROM_ENTITY_LIST","transactionId":"TRANSACTION_ID_FROM_MOVEMENT"}
```

Pass this to `reah_get_transaction`. It returns `fromTo.from` and `fromTo.to`
parties, including addresses, instrument/network IDs and a nullable `contact`
with ID, name and type. `reah_list_contacts` also returns `chainAddresses`
(address, label and network) for direct comparison. These are current saved
address-book matches, not verified historical ownership. Grouped address parties
return the first matching contact only; do not attribute the whole group to it.
A null contact or an empty search result does not establish that no contact
exists. Use settled movements, not party metadata, for monetary totals.

Statement export tools are not exposed. Existing Web statement features are
separate from this MCP catalog.

## Financial USD preparation

On a compatible signed Gateway, `reah_get_usd_withdrawal_context` reads exact
account/rail `settlementAddress`, `targetInstrumentId` and
`approvalDetailsVersion` from Ledger's canonical USD request derivation. The
legacy target can differ from Banking settlement backing. Copy the version into
signed details `v`; unsupported/provider-neutral routes stop without an approval.
`reah_get_contact_bank_metadata` resolves an exact owned saved contact/account
and ACH/Fedwire rail into normalized USD `recipientTerms`, including both
addresses and stored holder names/classification. Both tools require an existing
financial credential and use the authenticated live catalog; hosted read-only
connections do not gain these tools or execution authority.
