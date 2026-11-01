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
  and continue the original operation once. No arbitrary signing, automatic
  submission retries, or loops until settlement are provided.

The Go adapter preserves the optional `business` discriminator for financial
execution. It does not infer it from an approval ID. The Go client currently
hide `approval-financial-v1`: those business intent formats require a separate
reviewed adapter and are never downgraded to unsigned forwarding.

Both formats obtain a live `reah_signing_context` matching the local key and
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
