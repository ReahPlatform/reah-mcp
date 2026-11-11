# Local MCP client

The local client is a standalone Go executable. It serves Reah's
remote catalog over stdio and keeps OAuth tokens and device-key material in
the operating system credential store. Supported Macs automatically use Secure
Enclave for new keys; unsupported Macs, Linux and Windows use software keys in
their OS credential stores. Hardware keys keep
the private scalar in hardware and store only a wrapped device-bound reference
in Keychain. Running it does not require Node.js,
npm, or an npm account. The hosted read-only connection still requires no local
program at all.

## Install

From a checkout on macOS, use Go 1.25 or newer and Xcode command line tools:

```sh
make build
./bin/reah-mcp --help
```

On Linux and Windows, build with `go build -trimpath -o bin/reah-mcp ./cmd/reah-mcp`.
Users on those systems with repository access can also install from `main`:

```sh
GOPRIVATE=github.com/ReahPlatform/reah-mcp go install github.com/ReahPlatform/reah-mcp/cmd/reah-mcp@main
```

For repeatable installations, replace `main` with an existing Go client release
tag such as `v0.1.0`; this example does not announce an available release. Ensure
`$(go env GOPATH)/bin` is in your PATH. This GitHub repository is currently private:
Git credentials must already allow access. `GOPRIVATE` bypasses the public Go
module proxy and checksum service; it does not grant access to the repository.
No private Reah service modules are needed to build the executable.

On macOS, `make build` also compiles and embeds a universal CryptoKit helper.
Building this variant requires Xcode command line tools; running the resulting
binary does not. Plain `go build` and `go install` omit the native component:
on macOS they can use existing software-key profiles for read/signing operations,
but new login fails explicitly until a full macOS build is installed. They never
create a new software key as a substitute. Prefer the complete macOS release
binary when one is available.

Maintainers on macOS can build six standalone archives for macOS, Linux and Windows
(ARM64 and AMD64) with `make release-go VERSION=v0.1.0`. Once a checked GitHub
release exists, users may download the matching archive, verify it against
`SHA256SUMS`, unpack it and put `reah-mcp` on their PATH. Downloading a binary
does not require Go. Private releases also require repository access. Automatic downloads become available when this repository and its releases
are public; the updater does not send GitHub credentials or Reah OAuth tokens.

## Automatic updates

Official stable release binaries contain a fixed GitHub Releases update URL and
an Ed25519 verification key. During `serve`, the client checks at most once per
24 hours, including failed attempts, downloads a newer stable version in the
background, and verifies its signed metadata, SHA-256 digest and exact size.
Checks run at startup and daily while the connection stays open, using a separate
unauthenticated HTTPS client. Background download failures are reported
on stderr and do not interrupt the running MCP service. New sessions use the newest verified
cached version; running sessions keep their original executable. Reconnect the
MCP connection to activate a downloaded update. The configured executable path
stays the same on macOS, Linux and Windows.

```sh
reah-mcp update --check     # Verify availability without downloading a binary
reah-mcp update             # Stage the latest stable release for the next start
reah-mcp serve --no-auto-update
reah-mcp serve --pin-version v0.1.0
```

`REAH_MCP_AUTO_UPDATE=off` also disables background downloads. Already staged
updates still apply. To freeze the running version, use `--pin-version` or
`REAH_MCP_VERSION` with an exact installed stable version. A pin must match the
launcher or a previously downloaded, verified release; an unavailable pin fails
explicitly. Manual installation is required for versions never cached locally.
Remove the pin before staging a new update. Source builds and `go install`
builds do not embed the official update trust root and do not auto-update;
install an official binary release to opt in.

The original binary remains a launcher. It delegates before parsing commands,
so `--version`, help, new CLI arguments and the version reported to the Gateway
all come from the selected release. Updates are shared
across credential profiles within the current OS user account. Only executable
files, signed public metadata, a timestamp and a lock are stored under the OS
user configuration directory's `Reah/mcp-updates` folder. Credentials and signing
keys remain in the OS credential store. Cached signatures and executable hashes
are checked again before each launch. A cache integrity or activation error stops
startup instead of falling back to an older executable. Repair the cache or
manually install a verified release; an explicit `--pin-version` matching the
original launcher or an intact cached release permits a deliberate rollback.
No operation is replayed if an updated
process fails.

Downloaded releases cannot roll back the highest installed signed version.
Metadata expires after 30 days by default to bound replay of an old channel;
an already installed authentic binary remains usable offline after metadata
expiry. Uninstalling the cache also removes its rollback history. For an explicit
rollback, pin a previously verified cached release. Cached executables are
retained because another MCP session may still be running them. The update
signing key is fixed in the launcher: key rotation requires a manually installed
release. See [release setup](releases.md) for the signing and publishing steps.

A desktop OS credential store is required: Keychain on macOS, an unlocked Secret
Service session on Linux, or Credential Manager on Windows. There is no
plaintext fallback. The Go keyring library uses the operating system's storage
facilities (including the system `security` helper on macOS). A first-use OS
credential-store prompt may appear; subsequent access follows the OS policy.

```sh
reah-mcp login --profile default
```

## macOS Secure Enclave keys

On macOS, login automatically checks native Secure Enclave availability. Supported
Macs use a hardware key; explicitly unsupported Macs use the original software
P-256 key stored in Keychain. No storage selector is needed. A probe failure or
a key-creation error does not trigger software fallback or replace existing
credentials. Use a macOS binary built with `make build` or a release containing
this feature. All Apple silicon Macs have Secure Enclave;
Intel availability depends on the security chip and OS. The helper targets macOS
12 or newer and checks CryptoKit availability at runtime rather than using a
model-name allowlist. Virtual machines may not expose hardware access.

```sh
reah-mcp login --profile enclave \
  --enroll-key --policy-profile financial-operations-v1
reah-mcp status --profile enclave --check
reah-mcp serve --profile enclave
```

Existing software-key profiles remain usable until a new login. Login always
replaces the key and OAuth grant; on a supported Mac it generates a hardware key
even if the previous grant used a software key. On an unsupported Mac it keeps
using software storage; that private key remains exportable to the client process.
Software private keys cannot be imported
into Secure Enclave, so migration requires fresh browser consent and, for
financial access, user-confirmed credential registration. Use a separate profile
to trial hardware without replacing an existing connection. Staging trials must also specify
`--server https://platform.stg.reah.com/mcp` on every command.

The hardware key uses P-256 and the existing enrollment and signing protocols;
it does not require a different Gateway tool catalog. It is created with
`WhenUnlockedThisDeviceOnly` and `privateKeyUsage`, without `userPresence` or
biometric requirements. The helper disables authentication UI. There is no
Touch ID confirmation for each signature; an unavailable or locked key fails
explicitly instead of prompting, exporting a key, or falling back to software.
The OS credential-store policy can still prompt when accessing Keychain.

Only CryptoKit's opaque, device-bound `dataRepresentation` is saved in Keychain,
alongside this profile's OAuth credentials and public key. No PEM private key is
saved for this backend. The embedded helper receives the wrapped reference and
validated payload on stdin; it returns only a public key/reference during
creation or a signature. Its temporary executable contains no credentials and
is removed after use. Go verifies signatures against the exact request and
enrolled public key before encoding DER or P1363 for existing protocols.

`status` reports `keyStorage` and `keyExportable` without exposing the reference
or opening hardware. `status --check` verifies server authorization, not whether
the Mac is currently unlocked. OAuth read access, refresh and logout do not
require the signing key; an unsupported client build cannot sign with a saved
hardware key. Ordinary client upgrades retain the existing vault record and
restore the wrapped reference in a fresh native process. Old clients that only
understand PEM keys cannot use a hardware profile; use a compatible build.

A reference copied to another Mac cannot restore the private key. Losing,
erasing or replacing the original device requires revoking the connection in
Reah and registering a new key. Logout revokes the server grant and removes the
local reference; server revocation also protects against an old reference
restored on the original device. This device key is an authorization credential,
not a wallet recovery key. Hardware protection reduces private-key extraction;
malware able to run as the user may still request signatures on that Mac. There
is no hardware-attestation guarantee to the server. Browser consent, live
permissions, approval policies, transaction limits and expiry remain required.
An existing hardware profile never downgrades to software, including on an
unsupported Mac or another operating system. Unlock or restore access to the
original Mac, or explicitly revoke the connection and register a new profile.
The hosted read-only MCP connection remains available without a local key.

For an isolated, offline physical-device test from a checkout:

```sh
make prepare-enclave
REAH_MCP_TEST_SECURE_ENCLAVE=1 go test -tags secureenclave \
  -run '^TestSecureEnclaveHardware$' -v ./internal/client
```

This opt-in test creates and removes a separate OS-vault entry, checks restored
keys across native processes and both signature formats, and rejects altered
references and public keys. It does not use existing profiles, contact Reah or
create a financial authorization or transaction. CI compiles the macOS helper
but does not claim hardware validation on virtual runners.

## Browser login

The login command registers this local client, opens Reah in your browser, and
waits up to five minutes for consent. Sign in, select one or more businesses and
an access duration, then authorize. The temporary callback listener binds only
to `127.0.0.1` on an ephemeral port. It checks state and exchanges the code with
PKCE S256 before showing success. Browser cookies and Web signing keys are never
copied into the local client.

For an Agent coordinating login, use `login --no-browser --json`. Output is
newline-delimited JSON with named stages: `oauth_consent_required` contains the
OAuth URL; `oauth_connected` confirms token storage. If `--enroll-key` was
requested, a **second** `credential_registration_required` stage contains the
credential registration URL. Open each URL for the user at its corresponding
stage. Keep the login command running until the OAuth callback completes.
Continuation URLs are private; do not post them in issues, shared transcripts or
logs. `--no-browser` suppresses automatic browser launching, not the second step.
The final `local_status` event is saved progress, not proof of financial readiness.
If automatic browser launching fails, the printed URL remains usable while the
command waits; starting another login is unnecessary.

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
The `approval-financial-v1` adapter additionally supports EVM Wallet swaps and
cross-chain crypto converts using v4 intents, in exact-input or minimum-output
mode. Smart Balance is restricted to exact-input ERC-20 funding from Ethereum/Base
to Tempo with an owned synchronous ERC-4626/allocator vault. It also supports existing asset-absent Banking internal Send v1 quotes paid by Gas
Station and Web-equivalent bank execution through compatible Gateway/Hodor
servers. Distinct adapters also support USD ACH/Fedwire withdrawals and owned
Smart Balance Add/Withdraw. Smart Balance-funded transfers require the financial
profile and an exact vault ID. Bank-paid internal gas, Points, scheduled and
exact-output shapes remain closed. A new tool using an already supported
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
reah-mcp status --check --profile default
reah-mcp enroll-key --no-browser --json --profile default
reah-mcp login --profile default
reah-mcp logout --profile default
```

Status without `--check` reads only the OS credential store. It reports local
credential presence, access-token expiry, the public device key, a `phase`, and
`nextAction` with a command and argument array preserving the exact server,
profile and requested signing policy. It does not prove server readiness.

`status --check` verifies authenticated catalog access, reads credential status
and validates the live signing context for **every enrolled business**, including
the connection ID, public key, policy profile and expiry. It can refresh OAuth
but never starts or executes a payment, registers provider credentials, revokes a
grant, or generates a replacement key. The server's short registration window
can expire while already registered signing bindings remain active. The check
tests those bindings instead of treating enrollment `expired` as proof that they
are unusable. Incomplete registrations after that window closes require new
user consent; `enroll-key` cannot extend the window. A failed check retains local credentials unless refresh-token
consumption is uncertain. A failed network check is not evidence that consent
must be repeated.

`readOnlyStatus` and `signingStatus` are independent. `signing_ready` requires
verified bindings for all enrolled businesses and supported signed tools in the
live catalog. Partial registration lists each business separately. Registered
credentials with no compatible signed tools produce
`server_capability_unavailable`; reauthorizing does not enable a server feature
flag or add protocol support. Readiness is a snapshot and does not override
permissions, transaction limits, approval rules, balance or fee checks.

The local stdio catalog always includes `reah_local_connection_status`, even
before login or when remote tool discovery fails. The Agent can call it with
`{"check":true}` and follow `nextAction`. This is a local connection diagnostic,
not an additional hosted business tool. Remote catalog failures leave only this
diagnostic visible; the live check reports the error instead of presenting an
empty catalog as a successful connection. No tokens or private keys are returned.

Use `enroll-key` to resume an interrupted signing registration. It completes any
missing possession proof and opens the current connection's registration page,
preserving its grant, device key and businesses. With `--no-browser --json`, the
Agent receives a labeled URL to open for the user. Verify completion with
`status --check`; a successful `enroll-key` invocation only means the registration
page was handed off, not that the user approved it.

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

The default server is production: `https://platform.reah.com/mcp`. Use matching
`--server` and `--profile` arguments for every command and the host configuration.
Server URLs and profiles isolate clients, credentials and keys. Existing staging credentials are not
migrated or reused: run `reah-mcp login` to authorize production. To manage a
staging connection explicitly, pass `--server https://platform.stg.reah.com/mcp`
with its original `--profile` to every command, including `status` and `logout`.
Tokens and private keys live under the
`Reah MCP Go` OS credential-store namespace. `~/.reah-mcp-go` contains only
coordination files and locks, never credentials. The program does not read
Codex's MCP credential store. If migrating from the retired npm client, disconnect
the old connection in Reah Settings and log in once with Go. The old and new
clients use separate vaults and locks. Do not copy tokens or device private keys.

## Signing boundary

Financial access is optional. Owners, Admins and Operators (the legacy/backend
`Member` role) can select it for their businesses in Reah Web. Viewer, Cardholder
and KYB Collaborator roles cannot select it. Backend requires active membership,
no security cooldown and effective `transaction:initiate` permission; Turnkey
identity, browser credential and policy checks still apply. A role name or local
key alone does not prove signing readiness. Existing approval rules and Agent
transaction limits remain in force; only the business Owner can edit those limits.

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
Reah registration page. An authorized Owner, Admin or Operator (Member) reviews
the connection, local public key, businesses and expiry, then explicitly selects
financial access and confirms registration for the chosen businesses. Web reuses its verified browser login key to stamp each separate
Turnkey registration request. Expired or unavailable browser verification requires
renewed verification on the Reah page; it is not bypassed. The existing per-step Passkey path remains
available as an alternative.

Ordinary OAuth consent does not require enterprise signing access. Credential
registration separately authorizes the new MCP key at Turnkey. If that page
reports `REAUTH_REQUIRED` or expired browser signing access, the Web login session
may still be readable while its signing authorization has expired. Ask the user
to renew authentication on the Reah website, then resume the **same** registration
page. The Go client cannot renew that browser verification, infer its cause from
an incomplete registration alone, or bypass it. Do not run a fresh MCP login or
ask for the user's OTP to repair this Web state. The authorizing user's
confirmation belongs in the browser; per-business protocol requests are not a reason for the Agent to
request a separate interactive verification for each business.

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

The server must support explicit profile enrollment. An authorized user must
review and register this new credential. A wallet-only credential cannot convert.
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
real financial data. A live login is a separate manual verification.

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
