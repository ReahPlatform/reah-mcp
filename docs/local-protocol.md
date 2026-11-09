# Local client protocol

The authenticated Gateway owns the business catalog, descriptions and schemas.
The local process implements credential custody and versioned signing formats.
It never exposes an arbitrary signing tool or accepts model-provided signing
material. This protocol is an additive extension to MCP tool `_meta`:

```json
{
  "reah.com/local": {
    "version": 1,
    "mode": "signed",
    "signing": "approval-send-v2"
  }
}
```

| Mode | Behavior |
| --- | --- |
| `proxy` | Forward the advertised tool unchanged, including non-read-only preparation tools. No local key is used. |
| `internal` | Hide this protocol helper from the model. |
| `signed` | Use the declared supported signing format; hide `_proof`, `requestSignature` and `signatures` from model inputs. |

Tools without this metadata are accepted only as annotated read-only tools,
without signing fields. Duplicate names reject the catalog. Unknown versions,
modes and signing formats are hidden and cannot be called through the proxy.
Metadata is trusted only from the configured, OAuth-authenticated HTTPS endpoint;
it cannot add a signing format, replace verification keys or select a destination.
The local client also rejects model-supplied signing fields on calls.

Supported signing formats:

- `approval-send-v2`: validate and construct the exact Web-compatible send-intent
  protobuf from the model's explicit terms. Sign that intent and a short-lived
  operation proof binding the connection, resource, entity, actual tool name and
  all submitted arguments. The schema of this intent is a security contract;
  supporting a different intent type requires a client update.
- `turnkey-execution-v1`: sign the operation proof and invoke the advertised tool.
  If the response requests signatures, validate the server signature over exact
  provider bytes, target organization, operation type and timestamp, then stamp
  and continue the original operation for at most eight verified rounds, with at
  most 16 requests per round. Repeated request IDs stop for reconciliation. Every
  round retains the original entity, approval and business. No arbitrary signing,
  automatic submission retries, or loops until settlement are provided.

- `approval-financial-v1`: the supported input shape is Wallet/Smart Balance crypto conversion
  through Web-compatible v4 intents. Wallet same-chain swaps and cross-chain EVM
  converts support `EXACT_INPUT` and `MINIMUM_OUTPUT`. Smart Balance supports
  `EXACT_INPUT` ERC-20 funding from Ethereum/Base to Tempo only, with a verified
  owned synchronous ERC-4626/allocator vault; no Smart same-chain swap,
  `MINIMUM_OUTPUT` or `EXACT_OUTPUT`. Points, scheduled and `EXACT_OUTPUT` intent families remain unsupported.
  Smart Balance uses explicit `balanceSource=SMART_BALANCE` and an owned
  `smartBalanceVaultId`; absence means Wallet and never an automatic fallback. Capability
  selection uses the authenticated schema, not a business-tool name allowlist.

The conversion schema adds a local-only `convertContext`: stable UUID `requestId`,
`originNetworkId`, `destinationNetworkId`, `sourceAsset`, `destinationAsset`,
`originTokenAddress`, `destinationTokenAddress`, `originAddress`, `sourceDecimals`
and `destinationDecimals`. The Agent must obtain these explicit identities from
verified reads; the proxy does not discover accounts or infer chains. Native
assets require an explicit `slip44` identity and empty token address; ERC-20
identities must match the explicit contract. The public `destinationAddress` is
required. Amounts and the positive receipt floor must fit asset precision.
Minimum-output requires `requestedTargetAmount` to equal `minimumReceive`.
Context enters the signed intent but is removed from Gateway arguments. The
operation proof binds the actual tool and every upstream argument, including
that signed intent. The local adapter never accepts arbitrary payload bytes.

Conversions require live `financial-operations-v1` credentials. Request one with
`login --enroll-key --policy-profile financial-operations-v1`; this creates a new
connection and requires explicit user-confirmed registration. The default stays
`wallet-transfer-v1`. The selected financial profile must appear exactly in the
frozen possession proof; a server lacking explicit profile enrollment fails
closed. Existing wallet credentials are never silently expanded.

The Go adapter preserves the optional `business` discriminator for execution;
use `crypto_convert` for conversion approvals. It does not infer business from
an approval ID.

All formats obtain a live `reah_signing_context` matching the local key and
entity before use. This helper is private, including on legacy gateways.
Business tool names are bound into proofs but are not compiled into the client.

The Agent uses tool descriptions to discover sources, resolve ambiguity, obtain
fee/balance estimates and stop when funds are insufficient. The proxy does not
choose gas payers or automatically call `reah_estimate_transfer`. Prompt guidance
is not a security boundary: existing server permissions, approval rules and
preparation/execution balance checks remain necessary.

Deploy the additive Gateway metadata before the new local client. Existing local
clients ignore metadata and keep their previous behavior. A new client against
an older Gateway still supports read-only tools, but hides unmarked signed and
preparation tools; upgrade the server and refresh `tools/list` or reconnect.
Adding a public tool that reuses a supported format requires only a Gateway
catalog change. New cryptographic formats or security/credential behavior still
require updating the local program.


### Internal Banking quote adapter

An authenticated `approval-financial-v1` schema whose only business fields are
required `entityId`, `quoteId` and `requestId` receives `bankTransferContext`.
Tool names are not used to select this adapter. The context requires:

- `approvalDetailsJson`, `expiresAt`: the selected quote's exact frozen protobuf
  JSON and RFC3339 expiry. Unknown or duplicate protobuf fields fail closed.
- `sourceBankingAccountId`, `targetBankingAccountId`, `fromAddress`, `toAddress`,
  `walletId`, `networkId`, `instrumentId`, `asset`, `decimals`: quoted UUID
  identities and independently verified local Banking settlement metadata. The
  CAIP-19 ERC20 `asset` and precision remain local; they are absent from the
  signed quote and are never inserted into it. The custody `walletId` comes
  from the frozen quote and is checked against the source account by Ledger.
- `amountCents`, `amountAtomic`, `gasFeeCents`, `totalDebitCents`: exact quote
  terms. Amounts are integer strings; `decimals` is an integer. Source principal
  and debit are equal for Gas Station gas. Atomic principal equals USD cents
  multiplied by `10^(decimals-2)` under the internal USD settlement contract.

The accepted frozen details contain only Send v1, exactly one Banking sender
and one distinct Banking recipient, matching addresses/instrument/principal,
and Gas Station gas with no Banking payer ID or fee output. Every populated
field outside this closed contract is rejected. The complete parsed protobuf
is signed unchanged with `requestId` as intent nonce. Context stays local; the
operation proof binds the upstream `entityId`, `quoteId`, `requestId` and exact
request signature. A populated `asset` in the frozen Send v1 quote is rejected
as a different contract. Execution requires a compatible authenticated Gateway
catalog advertising `bank_internal_transfer` and Hodor's private exact-request
binding acknowledgment before Gateway returns the signing challenge. The client
verifies the authenticated exact provider body and preserves the original
business, approval and request IDs through the existing execution continuation.
Hodor rechecks live financial credentials, the exact stamp, signed Conductor
authorization and a fixed approval journal before custody submission and receipt
replay. Unknown outcomes must be reconciled without replacement or automatic
retry.

This bank path trusts Ledger's transaction semantics as Web does. It neither
changes existing Ledger quotes nor claims independent constructed-transaction
comparison or original-attempt binding. Wallet and convert guards remain
separate. Client support does not establish deployment, actual execution or
settlement. This adapter does not cover bank-paid internal gas. USD payouts use
the distinct protocol below; other fiat shapes remain unsupported locally.
Schemas, mappings and preparation stay local in the client; no remote templates
or server-preparation RPC are used.


### USD and Smart Balance protocols

`approval-usd-withdraw-v1`, `approval-smart-add-v1` and
`approval-smart-withdraw-v1` select distinct strict input adapters, independent
of business tool names. All require a live `financial-operations-v1` credential.
The local-only `financialContext` contains a stable UUID `requestId` and
`approvalDetailsJson` (protobuf `ApprovalDetails` JSON, never provider bytes).
Unknown fields, direction changes and mismatched business terms are rejected.
The context is removed before dispatch; the exact generated intent and every
submitted business field enter the operation proof. The local write catalog
filters out request/execution adapters this binary cannot expose.

For Add/Withdraw, obtain `approvalDetailsJson` from the matching
`reah_quote_add_smart_balance` / `reah_quote_withdraw_smart_balance` result. Replay
its normalized `assetAmount` in underlying major units, owned wallet/vault,
instrument account, network/instrument and three Treasury account IDs. The
client signs v1 of the matching oneof, explicit gas payer and submitted notes.
Gas estimates are advisory and are not frozen into the signed terms. These
preparations can provision the wallet-attached hierarchy and have no quote
ID/expiry. Check `depositEnabled`/`redeemEnabled`, freshness, principal and gas
balances before requesting approval. `totalBalance` overlaps Wallet and Smart
Balance; never add all three. Add and Withdraw retain opposite directions.

For USD, the context additionally requires `targetInstrumentId` and
`settlementAddress` from `reah_get_usd_withdrawal_context` for the exact account
and ACH/Fedwire rail. Copy its `approvalDetailsVersion` into details `v`; the
legacy Dakota target may differ from Banking settlement backing, so
`reah_get_bank_account_metadata` is not a substitute. Supply the complete v1/v2 `bankingUsdWithdrawal` details using those identities, a positive
integer USD-cent amount, lowercase rail, resolved bank/holder names, account
number/type, routing number, both addresses and any saved contact IDs. Manual
recipients must match every submitted bank/address field; saved recipients
must match the verified contact and bank-account IDs. Obtain normalized saved
`recipientTerms` from `reah_get_contact_bank_metadata` for the exact contact,
bank-account and rail. Copy these terms unchanged, including both addresses,
holder classification and stored first/last names. Never substitute Contact.name
or infer holder type from Contact.type; the Gateway mirrors Ledger normalization.
Missing route or recipient metadata means unavailable, not permission to guess.
The settlement address is a destination, not custody signing authority.

Gateway compares the signed terms against its private original canonical
approval before binding Ledger's returned raw request IDs. USD uses one immutable signer group of at most 16 backing-source digests;
Smart-funded send uses one immutable signer group; Add/Withdraw permit at most two groups
with distinct approval slots. Hodor authenticates the exact original intent,
live financial credential, Conductor execution authorization and provider body,
and retains the existing quota, journal and unknown-outcome fences. This is the
Web trust boundary: Ledger constructs raw business transactions; the client
does not reconstruct raw digests. Reconcile the original approval after any
unknown outcome. These protocols require coordinated Gateway/Hodor/Ledger
versions and do not establish deployment or successful provider settlement.
