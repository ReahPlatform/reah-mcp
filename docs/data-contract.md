# Hosted financial data contract

The Gateway owns tool schemas, fixed GraphQL reads, authorization, and exact
decimal normalization. The live tool definitions and `reah_catalog` describe the
deployed contract. This client plugin does not create additional fields or
perform a second normalization pass.

## Result structure

Business reads return the same JSON object in MCP `structuredContent` and the
JSON text content block:

```json
{
  "status_code": 200,
  "graphql": {
    "data": {
      "entity": {
        "totalBalance": {"usdAmount": "12345.67", "priceVersion": "example"}
      }
    }
  },
  "normalized_values": {
    "/graphql/data/entity/totalBalance/usdAmount": {
      "amount": "123.4567",
      "currency": "USD"
    }
  },
  "normalization_unavailable": {},
  "field_semantics": {
    "Balance.usdAmount": "USD cents including fractional cents; divide by 100 exactly. Valuation, not spendable cash."
  },
  "read_coverage": {
    "note": "Follow every available cursor. Preserve raw GraphQL coverage, valuation status and exclusions. Source errors and nulls are unavailable, never zero."
  }
}
```

The example shows selected fields. Raw data, errors, and extensions remain under
`graphql`. Normalization maps use JSON Pointer paths into that object, starting
with `/graphql/data/`. `field_semantics` is a map of financial contract notes,
not a complete GraphQL schema. Read the live catalog for fixed output fields.
Money entries contain decimal-string `amount` and `currency`; they do not include
`source_scale`. Native asset entries instead carry `amount`,
`unit: "major_asset_units"`, and a note to use the raw companion asset identity.
APY entries carry `percent` and `unit: "percent"`.

`reah_list_entities` returns `{"entities":[{"id":"...","name":"..."}]}`;
`reah_catalog` returns `tools`, `field_semantics`, and `instructions`. These two
discovery results do not use the financial envelope above. Failed calls may
instead return an `error` object with MCP `isError: true`.

## Financial units

| Raw field | Meaning and conversion | Interpretation |
| --- | --- | --- |
| `Balance.usdAmount` | USD cents, including fractional cents; divide by 100 exactly | Valuation, not spendable cash |
| Treasury position and summary `*InUSD` fields | USD cents; divide by 100 exactly | Bounded position population; do not add overlapping positions and summaries |
| `Balance.nativeAmount` | Already major asset units | Use the companion asset or instrument; no further decimal scaling |
| `TreasuryPositionAccount.currentValueInAssets` | Already major underlying asset units | Identify the asset through the vault's underlying instrument |
| `InvoiceAmountSummary.amountDue` | Already major units of companion `currency` | Outstanding obligation, not proof of cash receipt |
| `InvoiceAmountSummary.amountDueUsd` | Already USD dollars | Empty legacy values and null are unavailable, never zero |
| `Card.limitAmountMinor` | Integer USD cents; divide by 100 | A spending limit over `limitFrequency`, not an account balance |
| `SettledMovement.signedAmountSmallest` | Signed integer divided by `10^instrumentDecimals`, in `assetSymbol` | A settled posting is not automatically external cashflow |
| `SettledMovementMoney.amountMinor` | Integer divided by `10^scale`, in companion `currency` | Preserve valuation availability and status |
| `Vault.apy` | Product APY as a decimal fraction; multiply by 100 for percent | Current product yield, not realized position performance |
| `TreasuryPositionAccountSummary.averageAPY` | Weighted product APY as a decimal fraction; multiply by 100 for percent | Applies to the bounded summary |

Use `normalized_values` for calculations and display without applying these
conversions again. For example, raw `"0.05"` APY becomes
`{"percent":"5","unit":"percent"}`. Preserve decimal precision with decimal
arithmetic and retain signs; do not introduce binary floating-point rounding.
Never sum different native assets or currencies as if they shared a unit.

Card activity amounts and position `currentAPY` are absent from the fixed
selections because their semantics cannot be established reliably. Masked card
limits and card activity metadata remain available. Do not infer spending totals
from activity metadata or treat missing fields as zero.

## Missing values and errors

A null or empty source value has no normalized value. Its path appears in
`normalization_unavailable` with a reason. Missing settled-movement USD valuation
is also recorded as unavailable. Invalid financial decimals, missing required
normalization companions, GraphQL errors, missing data, or unsuccessful execution
status mark the MCP result `isError: true`; interpretable partial data may remain.
Never report such a result as an unqualified successful total.

## Pagination and coverage

| Source | Coverage rule |
| --- | --- |
| Cursor-based tools | Follow every available cursor with unchanged filters; retain raw coverage, watermarks, and exclusions |
| Wallets | At most 100; no continuation; completeness is `not_proven` |
| Treasury | Latest at most 200 matching positions, ordered by creation time and ID descending; positions and summary have no continuation and completeness is `not_proven` |
| Assets | Full entity catalog; no `first`, `after`, or synthetic cursor |
| Search | Bounded discovery matches; not a complete financial population |
| Settled movements | Records posted in Reah within the requested time range. Preserve coverage flags, data timestamp, watermarks, valuation status, selection flags, and exclusion counts even after all pages are read |

A successful response and an exhausted cursor do not prove that all upstream
records have been ingested. State material gaps, source limits, and filters in
analysis. Ordinary transaction lifecycle records and approval completion do not
substitute for settled movements. Existing statement export metadata does not
grant file access or permission to generate a statement.
