# Local client protocol

The authenticated Gateway owns tool names, business schemas, descriptions,
availability and canonical approval encoding. The client holds credentials,
verifies fixed protocol bindings and signs. It has no business-tool allowlist,
financial protobuf descriptor, chain-address library or transaction builder.

Tool `_meta` declares the transport contract:

```json
{
  "reah.com/local": {
    "version": 1,
    "mode": "signed",
    "signing": "approval-prepared-v1",
    "inputSchema": {
      "type": "object",
      "additionalProperties": false,
      "properties": {"entityId": {"type": "string"}}
    },
    "description": "Additional server-owned business guidance."
  }
}
```

| Mode | Client behavior |
| --- | --- |
| `proxy` | Forward the advertised tool, including non-read-only preparation tools. No device signing. |
| `internal` | Hide the protocol helper from the Agent. |
| `signed` | Follow a supported fixed signing protocol; hide `_proof`, `requestSignature` and `signatures`. |

Unmarked tools must be annotated read-only and have no signing fields. Duplicate
names reject discovery. Unknown versions and protocols remain hidden; they never
fall back to unsigned forwarding. Metadata is accepted only from the configured,
OAuth-authenticated HTTPS endpoint, with redirects blocked. It cannot select a
new destination, signing method, algorithm or provider verification key.

## Approval preparation: `approval-prepared-v1`

1. The Agent follows the live public `inputSchema` and descriptions. It chooses
   business terms, obtains quotes and checks fees/funds using advertised tools.
   Preparation-only data uses the fixed `preparationContext` property, whose
   contents and business validation are entirely server-owned.
2. The client obtains a live private `reah_signing_context` matching its device
   key, resource and selected entity.
3. The client signs a short-lived `reah.mcp.operation.v1` proof covering the actual
   business tool and all original arguments, then calls the fixed private helper
   `reah_prepare_approval_signature` with `tool`, `arguments` and `_proof`.
4. Gateway verifies the proof, current signing binding, allowed operation and
   configured spend limits. Its typed adapters prepare the existing Conductor
   `SignedApprovalIntent` without signing or submitting an approval or payment.
5. The response requires `protocol`, `resource`, `connectionId`, `userId`,
   `entityId`, `publicKey`, `policyProfile`, `tool`, `requestId`, `issuedAt`,
   `expiresAt`, `arguments`, `input` and `payloadBytes`. The client checks the
   identity, original arguments, stable nonce, fresh issue time and credential
   deadline. `input` must equal the original arguments with only
   `preparationContext` removed. Additional outer annotations, including
   `financialState`, are descriptive JSON within the response's 192 KiB limit.
   The client does not interpret them or copy them into signing input, operation
   proofs or submission arguments. Adding descriptive annotations does not
   require a client update.
6. `payloadBytes` is bounded canonical base64 and stays opaque to the client.
   The client does not decode an approval protobuf or duplicate financial,
   quote, funds or spend-limit validation. Gateway, Ledger and Hodor retain
   their typed business/intent checks, including validation of the actual signed
   payload. The authenticated Gateway owns encoding the Agent's exact business
   terms; the client checks only the surrounding request and credential binding.
7. The client signs the exact canonical base64 approval payload, adds
   `requestSignature`, and submits the original tool with a new operation proof
   covering every final argument. It never exposes a payload-signing tool to the
   Agent. A lost submission response requires reconciliation of the original
   request/approval; it does not automatically reprepare or resubmit.

Adding a business operation or changing its encoding within this contract is a
Gateway change. Changing the protected envelope fields, cryptography or credential
security requires a versioned client protocol update. Deploy compatible Gateway
and client versions together; older business-specific signing protocols are not
accepted by this client. Read-only operation remains available independently.

## Execution: `turnkey-execution-v1`

The client preserves the original entity, approval ID and optional business
selected from the live Gateway schema. It signs an operation proof and invokes
that tool. A signing continuation must contain Hodor-authenticated exact provider
bytes, the expected organization, a supported provider signing operation and a
fresh timestamp. Verification keys remain pinned locally. Each continuation
retains the original business/approval and signs only verified requests.

There are at most eight continuation rounds and 16 requests per round. Repeated
request IDs stop for reconciliation. No arbitrary provider signing, automatic
submission retries or polling until settlement are provided. Existing downstream
permissions, approval rules, quota reservations, request binding and replay
protection remain mandatory. This protocol does not change Web/App flows.

## Business capabilities

Use `tools/list` and `reah_catalog.write_operations` for the current connection;
server schemas are authoritative. Gateway owns preparation adapters for Wallet
send, protected crypto convert, V2 Banking crypto deposit/withdraw, internal
Banking transfers, bank-funded fiat payouts and Smart Balance Add/Withdraw.
Unsupported financial shapes remain unavailable on the server. The client does
not duplicate their field lists, units, routing rules or business restrictions.

Financial credentials still require explicit user-confirmed browser enrollment.
A read OAuth grant or local key alone is not signing permission. Preparation and
approval creation do not prove execution or settlement. Preserve request and
approval IDs and reconcile uncertain outcomes using advertised status tools.
