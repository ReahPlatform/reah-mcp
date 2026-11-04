# Local MCP client

The local client is a standalone Go executable. It serves Reah's
remote catalog over stdio and keeps OAuth tokens and the P-256 private key in
the operating system credential store. Running it does not require Node.js,
npm, or an npm account. The hosted read-only connection still requires no local
program at all.

## Install

From a checkout, use Go 1.25 or newer:

```sh
go build -trimpath -o bin/reah-mcp ./cmd/reah-mcp
./bin/reah-mcp --help
```

Users with repository access can install from `main`:

```sh
GOPRIVATE=github.com/ReahPlatform/reah-mcp go install github.com/ReahPlatform/reah-mcp/cmd/reah-mcp@main
```

For repeatable installations, replace `main` with an existing Go client release
tag such as `v0.1.0`; this example does not announce an available release. Ensure
`$(go env GOPATH)/bin` is in your PATH. This GitHub repository is currently private:
Git credentials must already allow access. `GOPRIVATE` bypasses the public Go
module proxy and checksum service; it does not grant access to the repository.
No private Reah service modules are needed to build the executable.

Maintainers can build six standalone archives for macOS, Linux and Windows
(ARM64 and AMD64) with `make release-go VERSION=v0.1.0`. Once a checked GitHub
release exists, users may download the matching archive, verify it against
`SHA256SUMS`, unpack it and put `reah-mcp` on their PATH. Downloading a binary
does not require Go. Private releases also require repository access. A public
binary distribution would need a separate publishing decision.

A desktop OS credential store is required: Keychain on macOS, an unlocked Secret
Service session on Linux, or Credential Manager on Windows. There is no
plaintext fallback. The Go keyring library uses the operating system's storage
facilities (including the system `security` helper on macOS). A first-use OS
credential-store prompt may appear; subsequent access follows the OS policy.

```sh
reah-mcp login --profile default
```

The login command registers this local client, opens Reah in your browser, and
waits up to five minutes for consent. Sign in, select one or more businesses and
an access duration, then authorize. The temporary callback listener binds only
to `127.0.0.1` on an ephemeral port. It checks state and exchanges the code with
PKCE S256 before showing success. Browser cookies and Web signing keys are never
copied into the local client.

Register the installed executable in your AI client's stdio configuration. Use
an absolute executable path if the host does not inherit your shell PATH:

```json
{
  "mcpServers": {
    "reah-local": {
      "command": "reah-mcp",
      "args": ["serve", "--profile", "default"]
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
The Gateway owns the live tool names, schemas, descriptions and availability.
The local client does not keep a business-tool allowlist. Legacy read-only tools
work without extra metadata; preparation and signed tools declare the supported
local protocol through tool `_meta`. Unknown protocol versions are hidden rather
than being forwarded unsigned. The supported signing formats are wallet transfer intents (`approval-send-v2`)
and verified execution continuations (`turnkey-execution-v1`). The Go adapter
also preserves the optional financial `business` discriminator on execution.
The `approval-financial-v1` adapter additionally supports EVM wallet swaps and
cross-chain crypto converts using v4 intents, in exact-input or minimum-output
mode, plus existing asset-absent Banking internal Send v1 quotes paid by Gas
Station and Web-equivalent bank execution through compatible Gateway/Hodor
servers. External bank payouts, bank-paid internal gas, Points, Smart Balance,
scheduled and exact-output shapes remain closed. A new tool using an already supported
format does not require a local release.

The local client retains OAuth, OS credential storage, exact intent construction,
operation proofs and verified Turnkey signature continuation. It does not pick
accounts/networks, calculate amounts or orchestrate balance/fee queries. The
Agent follows the live tool descriptions for that workflow. Signing-format,
credential-storage or local security changes still require a client update.
See [the local protocol](local-protocol.md) and [tool reference](tools.md).

## Reconnect and disconnect

```sh
reah-mcp status --profile default
reah-mcp login --profile default
reah-mcp logout --profile default
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
`Reah MCP Go` OS credential-store namespace. `~/.reah-mcp-go` contains only
coordination files and locks, never credentials. The program does not read
Codex's MCP credential store. If migrating from the retired npm client, disconnect
the old connection in Reah Settings and log in once with Go. The old and new
clients use separate vaults and locks. Do not copy tokens or device private keys.

## Signing boundary

Each login generates a fresh P-256 key for that connection and keeps its private
key in the OS vault. Ordinary login remains read-only. To opt into credential
preparation on a server that advertises support:

```sh
reah-mcp login --enroll-key --profile default
reah-mcp status --profile default
# Resume after an interrupted proof submission:
reah-mcp enroll-key --profile default
```

The public key is fixed in the OAuth request before browser consent. Backend
binds it to the authenticated user, connection and selected businesses. The
client checks the Hodor proof's purpose, server, client, public key, connection,
businesses and expiry, then signs its exact UTF-8 bytes using SHA-256/P-256 and
a 64-byte IEEE P1363 signature. It sends only the signature; it never sends the
private key or exposes an arbitrary signing tool. Repeated preparation uses the
same challenge and does not extend its lifetime.

**Successful possession proof means `awaiting_provider`, not an active Turnkey
credential.** When discovery advertises credential registration, login opens the
Reah registration page. An enterprise owner reviews the connection, local public
key, selected businesses and expiry, then confirms registration for all listed
businesses. Web reuses its verified browser login key to stamp each separate
Turnkey registration request. Expired or unavailable browser verification requires
sign-in again; it is not bypassed. The existing per-step Passkey path remains
available as an alternative.

This separately consents to transfer initiation and execution under current
business permissions and approval policy; the OAuth `reah.read` grant alone
cannot move money. Each business has an independent MCP identity and restricted
policies. The browser login key and OTP proof never enter the local MCP program;
the MCP private key never enters the browser. Unknown registration outcomes stop
for reconciliation rather than automatically recreating identities.

After registration, the local client exposes:

- `reah_transfer`: creates a single-recipient wallet transfer approval. Supply
  source instrument-account, wallet, network and instrument IDs, source and
  recipient addresses, the canonical CAIP-19 asset, and a positive integer string
  in the asset's smallest unit. For example, 1 USDC with 6 decimals is `1000000`.
  Wallet balances support `GAS_SOURCE_FROM_WALLET` and `GAS_SOURCE_GAS_STATION`. The selected gas payer is included in the signed intent.
  Use a stable UUID `requestId` for the intended transfer.
- `reah_execution`: executes that transfer approval once its existing approval
  policy is satisfied. It does not approve a pending request. Supply `entityId`
  and the original `approvalId`. The local program verifies Hodor's signed
  request, signs its exact Turnkey body and submits the signature continuation.
  A successful request is not proof of chain settlement.

For wallet swaps, crypto converts and internal Banking transfers, create a separate explicit financial grant:

```sh
reah-mcp login --enroll-key --policy-profile financial-operations-v1 --profile convert
```

The server must support explicit profile enrollment, and an owner must review
and register this new credential. A wallet-only credential cannot convert.
Follow the authenticated conversion schema, including its local-only
`convertContext`, and obtain a fresh quote, principal balance and the selected
payer's gas balance before submitting. Amounts and minimum receive are strings in
standard asset units; do not reuse transfer minor-unit amounts. Minimum-output
binds the maximum source budget and the target receipt floor. Retain the original
request and approval identities, follow normal approval policy, then execute with
`business: crypto_convert`. Request success does not prove settlement; reconcile
the original approval and actual transactions. See [the local protocol](local-protocol.md)
for exact context fields and supported scope.

For internal Banking transfers, obtain a fresh quote, both owned settlement
identities, actual available source funds and the Gas Station balance first.
Follow the live schema and supply its local-only `bankTransferContext`. The
adapter creates an approval by signing the entire frozen `approvalDetailsJson`,
checking both Banking IDs, addresses, instrument/network/wallet identities,
exact principal in USD cents and settlement atomic units, and Gas Station
attribution. Local CAIP-19 asset metadata and decimals help verify the selected
ERC20 settlement and exact 1:1 USD rescaling; the existing signed quote itself
uses UUID identities and contains no `asset`. The client never inserts local
asset metadata into signed details and rejects a populated future asset field.
The quote expiry gates submission; the admitted approval retains its normal
approval window. No bank-paid gas split, external payout or arbitrary protobuf
family is signed. Retain stable `requestId`, `quoteId` and original approval
identities and reconcile that same operation after an unknown outcome.

Banking internal execution requires a compatible authenticated Gateway catalog
that advertises `business: bank_internal_transfer`. Gateway must obtain Hodor's
private acknowledgment binding the original pending raw request to the original
signed intent and approval before exposing its signing challenge. The client
verifies the server-authenticated exact body and signs it through the existing
execution protocol, retaining the original business, approval and request IDs.
Hodor checks the exact stamp, live financial key, Conductor authorization and
fixed approval journal before execution. Unknown outcomes require reconciliation
of that original operation, without automatic retries or replacements.

This bank path trusts Ledger's transaction semantics at the same boundary as
Web; it does not claim the independent constructed-transaction comparison used
for supported wallet and convert paths or an independent original-attempt
binding. No Ledger or quote changes are required. Server deployment and an
actual completed bank E2E are separate prerequisites; client support or approval
completion never proves settlement. Schema mappings and preparation are stored
and executed in the client, without remote templates or a server-preparation RPC.

The model never supplies signatures, private keys or provider request bodies.
The program signs an exact connection/entity/operation/arguments proof and a
Web-compatible protobuf transfer intent. Execution additionally verifies the
pinned environment-specific Hodor public key. The protobuf schema is pinned from
Conductor `approval_details.proto` and
embedded in the executable. Its provenance and the Web-compatible wire fixture
are documented in [the signing fixtures](signing-schema.md).
Building and validating the signer does not require a TypeScript generator.

Deploy matching Hodor, Backend and Gateway contracts and migrations before
enabling their separate enrollment, provider-registration and transfer-execution
flags. All flags default off. The hosted plugin still needs no local process;
financial calls require the registered local key in addition to OAuth.

Disconnect immediately blocks new Gateway calls. Backend durably retries Hodor
revocation; Hodor rejects revoked keys before submitting or coapproving provider
requests and asynchronously removes provider credentials. Requests already
submitted cannot be undone. Neither failed submissions nor refreshes with an
unknown outcome are retried automatically. Inspect the original request/approval
before taking any recovery action; do not create a replacement transfer.

## Checks

`make lint`, `make test` and `make build` check both the hosted plugin and local
client. Local integration tests exercise browser callbacks, MCP initialization
and read calls, rotating refresh, changed business selection and revocation
against a simulated OAuth/Gateway service. Financial tests cover exact protobuf
intent signing, the two-stage execution protocol, signed provider requests,
argument tampering, missing credentials and ambiguous outcomes. They do not access your OS vault or
real financial data. A live staging login is a separate manual verification.

The hosted plugin must not be changed to an unpublished local executable.

## Choosing a transfer source

After credential registration, use `reah_list_entities` and `reah_list_wallets`
to select the business, wallet, blockchain account and instrument account.
The wallet read returns exact IDs, addresses, network metadata and decimals;
`normalized_values` at each instrument path supplies its reviewed `caip19`. Do
not guess internal IDs or infer native assets from missing token contracts.
For example, 15.25 USDC at 6 decimals is the integer string `15250000`.
Only identified EVM sources are listed in this version. Omitted sources are not
signable through this discovery flow; balances and gas are checked when the
transfer is prepared. Call `reah_transfer`, then `reah_execution` for its original
approval ID once the existing approval policy is satisfied.

## Network and gas selection

Use `reah_list_wallets` to select the exact wallet asset and network. If
multiple networks match and the user did not select one, ask; an EVM address
alone does not identify its network. Convert the requested amount using the
source decimals, then call `reah_estimate_transfer` with the source IDs, recipient
and amount in minor units. It uses the same fee estimator as Reah Web.

The estimate returns two gas options: wallet fees in native-token major units
with a USD equivalent, and Gas Station fees in USD dollars. Existing Reah buffers
are included. Balance checks use the standard-wallet and Gas Station snapshots also used by
Web. For native-token sends, wallet gas requires amount plus fee; Gas Station
requires enough USD balance. The Agent must request a fresh estimate for the exact transfer terms before
submission, check `assetBalance.sufficient` and the selected gas option
`sufficient`, and stop to ask the user when either is false or unavailable.
The local proxy does not issue this query automatically. These instructions are
not an authorization boundary; existing server-side preparation and execution
checks remain authoritative. These are snapshots, not
reservations or fee caps; preparation and execution check again and can reprice.
The preparation query may initialize an empty Gas Station record, like Web; it
never funds that record and is advertised as non-read-only and non-destructive. Do not silently change
the network or gas payer. Explicitly pass the selected `gasSource` to
`reah_transfer`; the local program signs it together with the transfer details.
The hosted read-only connection does not gain these credential-gated tools.
