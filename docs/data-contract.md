# Reah read data contract

The MCP schema is a reviewed subset of Reah's read API. A field or argument is
public only when it has an explicit contract in `internal/schema/contracts.go`.
The generated schema contains no other upstream fields. Removing a field from
the contract removes it from queries and `reah_describe_type`; its upstream
existence does not make it available through MCP.

Contracts document meaning, raw units, fixed or companion scale, currency or
asset identity, null behavior, required companion selections, and evidence.
Structural fields such as IDs, cursors, statuses, and record collections have
business descriptions without pretending to carry monetary units. Introspection
and private identity or card-secret fields are not discovery surfaces.

## Reading values

Every business read and `reah_query` returns:

- `data`: original upstream values. Money stays in its original decimal string
  encoding; cursors, coverage, exclusion counts, and nulls remain unchanged.
- `errors`: original GraphQL errors, when present. MCP `isError` is true for
  partial GraphQL errors, even when some data is usable.
- `field_semantics`: contracts for the selected fields, with exact `Type.field`
  references and response paths. Paths use JSON Pointer syntax; `*` denotes an
  array item. Aliases change paths, not the underlying field's meaning.
- `normalized_values`: exact conversions keyed by JSON Pointer into `data`.
  Use `amount`, `currency` and `unit`. `raw` preserves the original numeric
  string, while `source_scale` records the source conversion exponent. Do not
  scale `amount` a second time. APYs also include a ready-to-display `percent`.
- `read_coverage`: acquisition status for source-bounded collections and
  aggregates, keyed by JSON Pointer. `source_limit` records the upstream cap;
  `returned_items` is present only when an actual list can be counted. This is
  separate from amount availability and external ingestion coverage.

For example, a balance response with raw `data.entity.totalBalance.usdAmount`
equal to `"10000"` includes this normalized entry:

```json
{
  "/entity/totalBalance/usdAmount": {
    "raw": "10000",
    "amount": "100",
    "unit": "usd_dollars",
    "currency": "USD",
    "source_scale": 2,
    "status": "available"
  }
}
```

`amount` is canonical decimal text, not locale formatting. Display USD `"100"`
as `$100.00` if desired; do not divide it by 100 again. Fractional cents, large
integers, tiny asset amounts and negative values retain exact precision. No
floating-point arithmetic or rounding participates in conversion.

## Financial units

| Field | Raw contract | Normalized contract |
| --- | --- | --- |
| `Balance.usdAmount` | USD cents, possibly fractional | USD dollars; divide raw value by 100 |
| `Balance.nativeAmount` | Major asset units | Same numeric amount with aggregation asset identity; no second decimal shift |
| Treasury position / summary `*InUSD` fields exposed by MCP | USD cents, possibly fractional | USD dollars |
| `TreasuryPositionAccount.currentValueInAssets` | Major underlying asset units | Same amount, identified by `vault.underlyingInstrument.asset.symbol` |
| `Card.limitAmountMinor` | Integer USD cents over `limitFrequency` | USD dollars; not available balance |
| `SettledMovement.signedAmountSmallest` | Signed integer instrument units | Divide by `10^instrumentDecimals`, retaining `assetSymbol` and instrument identity |
| `SettledMovementMoney.amountMinor` | Integer minor units in `currency` | Divide by `10^scale` |
| `InvoiceAmountSummary.amountDue` | Major units of `currency` | Same amount; not proof of receipt |
| `InvoiceAmountSummary.amountDueUsd` | Frozen publish-time USD dollars | Same dollar amount; never divide by 100 |
| APY fields | Decimal fractions, e.g. `0.05` | `amount: "0.05"`, `percent: "5"`; never multiply `percent` again |

`Vault.apy` is current product APY. `TreasuryPositionAccountSummary.averageAPY`
is a position-weighted average of product APYs over the bounded upstream subset,
not realized position performance. Negative product rates remain negative.

Currency and scale companions are mandatory in custom selections; the server
verifies the actual returned values too.
For native balances, select aggregation `__typename`, Asset `symbol`, and
Instrument `asset { symbol }`. For Treasury asset amounts, select the vault's
underlying asset symbol. Aliased and fragmented companions work normally.

## Fields intentionally omitted

`CardTransaction.amountMinor` is absent from the public SDL, discovery, and
default selections. The current producer mixes USD card spending and USDC
deposit/withdrawal amounts from different metadata paths without an explicit
scale. A currency label or the Web's formatting helper does not establish the
scale of every record. Card activity metadata, including currency labels and
pagination, remains available; it does not support monetary spending totals.

`TreasuryPositionAccount.currentAPY` is also absent. The Ledger calculates XIRR
from settled investor cashflows and a terminal marked position value, then applies
a net yield multiplier. It returns zero for missing inputs, unsolved XIRR, and
nonpositive returns. An individual zero therefore cannot distinguish actual zero
yield, missing data, and a negative return. The MCP does not publish that ambiguous
value as an available financial measure. Current vault product APY remains exposed.

These fields are removed at schema generation, rather than rejected only after
the upstream request or hidden only by the optional skill.

## Collection coverage

`Entity.treausryPositionAccounts` returns at most the latest 200 matching
positions, ordered by `created_at DESC, id DESC`. The upstream selection requires
a Treasury account and a nondeleted vault. There is no continuation. Its field
contract publishes `source_limit: 200`, and each read includes coverage such as:

```json
{
  "/entity/treausryPositionAccounts": {
    "status": "not_proven",
    "source_limit": 200,
    "returned_items": 200,
    "reason": "The source limit was reached; older matching positions may be omitted and no continuation is available."
  }
}
```

A successful non-null list with fewer than 200 non-null items has `complete`
coverage for that matching upstream collection only. A full batch, null item,
or partial GraphQL error prevents a completeness claim. A null bounded source
has `unavailable` status. Aliases and fragments preserve these rules and use the
actual response paths.

`Entity.treausryPositionAccountSummary` aggregates the same bounded selection.
Its contributing count is unavailable, so a non-null summary always reports
`not_proven`, without an invented `returned_items`. Its dollar values and weighted
product APY are not guaranteed full-portfolio totals, even when the read succeeds.
Removing this limit requires upstream continuation or complete aggregation;
the MCP does not invent pagination parameters.

`Entity.assets` returns the complete entity asset catalog. The Gateway ignores
upstream `first`/`after` and always returns a null cursor, so those parameters and
`AssetConnection.cursor` are absent from this MCP. Requesting them fails local
schema validation; business tools do not inject a default page size for assets.

## Missing data and reporting scope

A null amount stays null. A null settled valuation object does not create a
zero-valued normalized entry. A legacy empty `amountDueUsd` yields a normalized
entry with `status: "unavailable"` and no `amount`; that legacy behavior is an
explicit source contract. Missing coverage, valuations or pages cannot be
silently replaced with zeros.

Raw amount conversion does not establish spendability, eliminate spam balances,
classify a movement as external cashflow, or make an incomplete page complete.
Do not add overlapping entity, wallet, instrument and Treasury totals. Ordinary
transactions and search matches are not a settled cashflow population. Preserve
settlement windows, timezones, coverage, exclusions and valuation status.

If the upstream service returns an unselected field,
invalid numeric encoding, invalid enum value, or missing unit dependency for a
non-null amount, the tool fails and does not publish that response's data.
These checks detect producer drift; they do not expand the public schema.

## Evidence and maintenance

The pinned [Agent revision](https://github.com/ReahPlatform/reah-agent/commit/474fda4a771289782c5f9e793a4f0c33e7076f79)
and source hashes are recorded in `docs/upstream.json`. Generation verifies
both the schema and semantic-registry hashes, then copies only public unit
contracts into `internal/schema/source_semantics.json`. It never imports the
full capability registry or admits new fields automatically.

The unit review also follows the producer and its UI consumer:

- [Ledger balance conversion](https://github.com/ReahPlatform/reah-ledger/blob/f4fa1d1d73502ed4fc4e5923f297dfb727d06663/internal/convert/balances.go):
  `BalanceToProto` scales native amounts into major asset units and passes USD
  cents through unchanged.
- [Ledger Treasury production](https://github.com/ReahPlatform/reah-ledger/blob/f4fa1d1d73502ed4fc4e5923f297dfb727d06663/internal/svc/treasury_apis.go):
  `toProtoTreasuryPosition` multiplies USD valuations by 100; asset amounts are
  major units.
- [Web Treasury mapping](https://github.com/ReahPlatform/reah-web/blob/a06d0d5741f1abb2f6a6ca6bb58a070ce02ac7ca/apps/dashboard/src/features/treasury/logical-vaults.ts):
  USD values divide by 100, while fractional APYs multiply by 100 for display.
- [Web card mapping](https://github.com/ReahPlatform/reah-web/blob/a06d0d5741f1abb2f6a6ca6bb58a070ce02ac7ca/apps/dashboard/src/pages/cards/utils/card-maps.ts):
  masked-card spending limits divide by 100 for display; this does not establish
  the units of every card transaction category.
- [Ledger card activity production](https://github.com/ReahPlatform/reah-ledger/blob/c62e0e1124bdef649a3a4b28bbeba082c27bf93c/internal/svc/card_spending_apis.go#L847):
  `cardTimelineDisplayFields` mixes USD spending metadata and USDC funding
  amounts from `from_to`; the ambiguous transaction amount is not exposed.
- [Ledger position APY calculation](https://github.com/ReahPlatform/reah-ledger/blob/c62e0e1124bdef649a3a4b28bbeba082c27bf93c/internal/svc/treasury_apis.go#L1705):
  cashflow XIRR has zero fallbacks and nonpositive-result clamping; the position
  rate is not exposed as an available financial measure.
- [Gateway Treasury list limit](https://github.com/ReahPlatform/reah-graphql-gateway/blob/9b2e65e0d4a6c92d99e02965b322dcd9bf279d94/internal/graph/resolver/entity_treasury_helpers.go#L16)
  and [bounded summary](https://github.com/ReahPlatform/reah-graphql-gateway/blob/9b2e65e0d4a6c92d99e02965b322dcd9bf279d94/internal/graph/resolver/entity.resolvers.go#L1042):
  each uses one `PageSize: 200` request without continuation. The
  [Ledger query](https://github.com/ReahPlatform/reah-ledger/blob/c62e0e1124bdef649a3a4b28bbeba082c27bf93c/internal/repos/treasury_position_accounts/query.sql#L132)
  orders by creation time and ID descending and applies the limit.
- [Gateway asset resolver](https://github.com/ReahPlatform/reah-graphql-gateway/blob/9b2e65e0d4a6c92d99e02965b322dcd9bf279d94/internal/graph/resolver/entity.resolvers.go#L698):
  returns the full entity asset list and ignores pagination arguments.
- [Ledger settled valuation](https://github.com/ReahPlatform/reah-ledger/blob/f4fa1d1d73502ed4fc4e5923f297dfb727d06663/internal/settledmovement/projection.go):
  `Value` produces explicit-scale USD money from fiat or historical prices.
- [Pinned invoice schema](https://github.com/ReahPlatform/reah-agent/blob/474fda4a771289782c5f9e793a4f0c33e7076f79/internal/tools/sandbox/testdata/graphql_schema/invoicing.graphql):
  invoice summaries use major currency units and publish-time frozen USD values.

To add a field, verify its meaning and units, add its explicit contract and
companion requirements, regenerate the public schema, and add source-based
fixtures. Run `make lint`, `make test`, and `make build`. Tests cover all default
business selections, exact conversions, aliases/fragments, nulls, producer drift,
and the absence of unreviewed fields from both schema and discovery.
