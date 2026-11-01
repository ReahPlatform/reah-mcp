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
