# Signing schema and wire fixture

`internal/client/approval.descriptor.bin` is the pinned binary `FileDescriptorProto` for
Conductor's `reah-conductor/v1/approval_details.proto`. It was extracted from the
reviewed generated descriptor at Reah MCP commit
`d164b0f46d3f0d48f829b7bc76261c66cfdb6ca8`, before retirement of the TypeScript
client. Its SHA-256 is `ae0422d6806073e971fec97a8c5dfcd47bbd5b1ddb86f6dc6571a2f776ab58ae`.
The embedded descriptor is a standalone input to the Go signer; source builds
and releases do not require Node, npm or generated TypeScript files.

The descriptor omits dependency names. `loadApprovalDescriptor` restores the
`google/protobuf/timestamp.proto` dependency from Go's protobuf registry before
constructing the dynamic messages. Keep protobuf field numbers, enum values,
oneof structure and deterministic serialization compatible with Conductor and
Reah Web. Do not regenerate against an unrelated latest schema as part of a
routine client release.

`internal/client/testdata/send-v2.base64` contains deterministic protobuf bytes produced by the reviewed
TypeScript/Web signing implementation for the fixed transfer in
`TestApprovalWireAndSignature` in `internal/client/client_test.go`. It contains synthetic identifiers
and 15.25 USDC in minor units, with no credentials. The Go test compares its
encoded intent to this independently produced fixture. When the signing contract
changes, review the upstream schema and update the descriptor, intent adapter
and cross-implementation fixture together.

`testdata/convert-v4.base64` and `testdata/convert-minimum-v4.base64` were
independently generated with Reah Web's existing approval protobuf schema and
`@bufbuild/protobuf` 2.10.2 (`create`, `toBinary`, `timestampFromDate`), following
`packages/signature/approval/convertIntent.ts`. They cover exact-input and
minimum-output v4 wallet conversion, fixed synthetic identities, nonce and
timestamps. `TestConvertWebWireFixtures` verifies byte-for-byte compatibility,
including the source/destination CAIP-19 assets, addresses, funding bucket, gas
payer and receipt floor. The pinned descriptor is unchanged.


`testdata/bank-internal-v1.base64` was independently generated with the existing
Reah Web approval protobuf schema and `@bufbuild/protobuf` 2.10.2 (`fromJson`,
`create`, `toBinary`, `timestampFromDate`). It follows
`packages/signature/approval/bankingInternalTransferIntent.ts`: decode the complete
frozen Send v1 details and sign them unchanged. The synthetic fixture binds a
10 USD Banking transfer (1000 cents, 10000000 settlement atomic units at six
decimals), existing UUID instrument/network identities, distinct Banking accounts, Gas Station
attribution and fixed intent identity/timestamps. `TestBankTransferWebWireFixture`
compares deterministic bytes and verifies the signature. The pinned descriptor
is unchanged. This existing-Ledger fixture intentionally has no `asset`; local
CAIP-19 settlement context is never inserted into signed details. It verifies
approval intent compatibility. Bank execution separately requires a compatible
Gateway/Hodor private exact-request binding and retains the existing Web trust
boundary for Ledger transaction semantics; the fixture does not prove deployment,
execution or settlement.
