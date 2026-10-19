# Repository conventions

- Write documentation, comments, tool descriptions, errors, plugin metadata,
  examples, and commit / pull request descriptions in English.
- Preserve upstream API identifiers and enum values exactly, including existing
  spelling. Do not translate machine-readable contracts.
- Keep implementation plans and architecture proposals outside this repository
  unless the user explicitly requests publication.
- Run `make lint`, `make test`, and `make build` before opening or updating a pull
  request.
- Keep MCP read-only. Enforce entity scope and the closed schema before sending
  upstream requests; never add identity, signing, or financial write operations.
