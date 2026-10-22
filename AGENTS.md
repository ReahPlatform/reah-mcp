# Repository conventions

- Write documentation, comments, tool descriptions, errors, plugin metadata,
  examples, and commit / pull request descriptions in English.
- Preserve upstream API identifiers and enum values exactly, including existing
  spelling. Do not translate machine-readable contracts.
- Keep implementation plans and architecture proposals outside this repository
  unless the user explicitly requests publication.
- Run `make lint`, `make test`, and `make build` before opening or updating a pull
  request.
- This repository distributes hosted MCP connection settings and portable skills.
  The Gateway owns OAuth, entity authorization, predefined GraphQL execution,
  tool schemas, and financial normalization. Do not add a second local server.
- Default plugin connections to staging until production is explicitly approved.
  Never package credentials, bearer headers, fixed entity IDs, or login sessions.
- Keep MCP read-only. Document only tools, fields, units, and arguments verified
  against the Gateway catalog. Its live tool schemas are authoritative; do not
  introduce arbitrary GraphQL, field selections, or financial write tools here.
- Keep analysis skills general. Put tool reference details in documentation;
  preserve the browser skill's progressive loading and separate website session.
