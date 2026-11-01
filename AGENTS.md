# Repository conventions

- Write documentation, comments, tool descriptions, errors, plugin metadata,
  examples, and commit / pull request descriptions in English.
- Preserve upstream API identifiers and enum values exactly, including existing
  spelling. Do not translate machine-readable contracts.
- Keep implementation plans and architecture proposals outside this repository
  unless the user explicitly requests publication.
- Run `make lint`, `make test`, and `make build` before opening or updating a pull
  request.
- This repository distributes hosted MCP connection settings, portable skills,
  and optional Go and TypeScript stdio clients. Prefer the Go binary for new local installations.
  The Gateway owns OAuth, entity authorization, predefined GraphQL execution,
  tool schemas, and financial normalization. The local client follows the authenticated Gateway catalog and versioned signing
  protocols without a business-tool name allowlist. Keep account/network selection
  and balance/fee query orchestration in server-owned tool instructions for the Agent;
  do not duplicate GraphQL or financial normalization in the local proxy.
- Store local OAuth credentials and device keys in the OS credential store only.
  Never expose an arbitrary signing tool or enroll keys through Web session APIs.
  Device keys authorize transfers only after explicit owner-confirmed browser registration and live server validation.
- Default plugin connections to staging until production is explicitly approved.
  Never package credentials, bearer headers, fixed entity IDs, or login sessions.
- Keep the hosted plugin read-only. Document only tools, fields, units, and arguments verified
  against the Gateway catalog. Its live tool schemas are authoritative; do not
  introduce arbitrary GraphQL, field selections, or generic signing. The current server financial catalog is limited to reah_transfer and reah_execution;
  new tools may reuse a supported signing protocol without a client name allowlist. The unsigned reah_estimate_transfer preparation tool may initialize an empty Gas Station through the existing Web resolver; it must not be advertised as read-only.
- Keep the analysis skill general. Put tool reference details in documentation.
