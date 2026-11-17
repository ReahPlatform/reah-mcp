# Gateway preparation fixture

`prepared-approval.json` is copied byte for byte from
`ReahPlatform/reah-graphql-gateway/internal/mcp/tools/testdata/prepared-approval.json`.
The Gateway test `TestPreparedApprovalMatchesActualClientFixture` produces the
expected response using its real `approval.Prepare` encoder and
`approvalPreparationResult` response projector, which also serves the production
preparation handler. Update both repository copies together when the protected
contract changes.

SHA-256: `97b66bc244716ab9e7562777a1bd33c567494a82d065cefb27d1b62b74c8713f`.

All identities, addresses, amounts and timestamps are synthetic. The fixture
contains a public key and no private key or live credential. Client tests generate
an ephemeral key and replace only the unsigned public-key echoes in the context
and prepared response; the Gateway-produced approval payload remains unchanged.
The real client validator signs those exact bytes, and the tests verify the
signature and reject altered protected bindings. Descriptive annotations never
enter signing input or final operation proofs.
