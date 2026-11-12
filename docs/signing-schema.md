# Signing schema ownership

Gateway owns financial approval encoding in `internal/mcp/approval`, using its
pinned Conductor Go schema. The Go client distributes no business protobuf
schema or descriptor and does not reconstruct transfers, conversions or payouts.

The client checks the stable `SignedApprovalIntent` outer wire fields only:
`details` (field 1, opaque nonempty bytes), `entity_id` (2),
`initiator_user_id` (3), `nonce` (4), `expires_at` (5), `domain` (6), and
`issued_at` (7). It rejects missing, duplicate, unknown or mistyped outer fields
and checks identities, timestamps and the request binding before signing.
See [the local protocol](local-protocol.md) for the authenticated preparation flow
and its trust boundary.

Independent Web wire fixtures and business encoding tests now live alongside
Gateway's preparation adapters. They cover Wallet Send v2, exact-input and
minimum-output Convert v4, frozen internal Banking Send v1 and bank-funded fiat
v15. Updating a business encoding requires reviewing those server-side fixtures
and existing Ledger/Hodor validation, without regenerating a client descriptor.
Client tests cover the fixed envelope, exact signatures, binding rejection,
execution verification and uncertain-outcome behavior.
