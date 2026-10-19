# Hosted Reah MCP authorization

Status: proposed implementation plan. Reviewed on October 19, 2026.
No hosted OAuth endpoints described here are implemented or deployed by the
current reah-mcp release. Lifetimes and new URLs below are proposed defaults.

## Product decision

Deliver this flow: **Add Reah → sign in → choose a business → authorize → return
to the client**. Existing Reah users reuse their normal login and MFA. A client
receives a dedicated, revocable read authorization for one user and one entity.
Users never copy browser cookies or Agent credentials into the client.

Reuse the Agent identity and credential lifecycle for internal execution. Add a
standard OAuth authorization server around a new durable MCP grant. A browser
session proves consent; it is not the long-lived client authorization. An Agent
browser session is unnecessary for financial API reads.

The first release supports the existing 19 tools, one entity per connection,
explicit consent, background access with rotating refresh tokens, and connection
management. Additional entities require separate connections. Payments, signing,
approvals, file writes, report generation, and Agent execution remain unavailable.
Authorization bookkeeping is separate from financial write access.

## Verified starting point

| Repository | Reviewed remote main | Relevant behavior |
| --- | --- | --- |
| reah-backend | `30cb7640056b031823e714de75d78009f9783f95` | Google/Slack login OAuth, human/Agent sessions, Agent credential issuance and revocation, live runtime requester checks |
| reah-web | `a06d0d5741f1abb2f6a6ca6bb58a070ce02ac7ca` | Existing login/MFA, entity memberships, strict login continuation handling, entity settings |
| reah-graphql-gateway | `1365870085587213cb7ed294fff1073cdeded0af` | Cookie authentication or Agent-only bearer authentication, resolver permission checks, HTTP integration routes |
| reah-mcp | `603994c96a4bcbecd79fa6593cd95ad896d74f39` | Fixed upstream credential/entity, shared HTTP secret, local read schema and AST validation |

Source evidence:

- [Agent credentials and runtime delegation](https://github.com/ReahPlatform/reah-backend/blob/30cb7640056b031823e714de75d78009f9783f95/internal/svc/agent.go#L402).
- [Agent browser sessions revalidate their parent credential](https://github.com/ReahPlatform/reah-backend/blob/30cb7640056b031823e714de75d78009f9783f95/internal/svc/auth.go#L412).
- [Agent role permissions include write capabilities](https://github.com/ReahPlatform/reah-backend/blob/30cb7640056b031823e714de75d78009f9783f95/internal/permission/system_user_groups.go#L124).
- [Gateway bearer authentication requires an Agent identity](https://github.com/ReahPlatform/reah-graphql-gateway/blob/1365870085587213cb7ed294fff1073cdeded0af/internal/middleware/auth.go#L77).
- [Resolver permission and self-access checks](https://github.com/ReahPlatform/reah-graphql-gateway/blob/1365870085587213cb7ed294fff1073cdeded0af/internal/graph/resolver/permission_directive.go#L16).
- [Login completion hooks](https://github.com/ReahPlatform/reah-web/blob/a06d0d5741f1abb2f6a6ca6bb58a070ce02ac7ca/apps/dashboard/src/hooks/useHandleLoginSuccess.ts).
- [Login redirect restrictions](https://github.com/ReahPlatform/reah-web/blob/a06d0d5741f1abb2f6a6ca6bb58a070ce02ac7ca/apps/dashboard/src/lib/handleLoginSuccessFlow.ts).

Existing StartOAuth/FinishOAuth are Reah's identity-provider login flow. They do
not implement external client authorization, refresh rotation, or MCP discovery.
The implemented Gateway bearer path specifically expects an Agent credential.

## Service ownership and endpoints

Use the existing dashboard origin for browser authorization. Repository deployment
configuration identifies `https://platform.reah.com` and
`https://platform.stg.reah.com` as the dashboard/GraphQL origins. Confirm edge
routing during implementation. Reserve `https://mcp.reah.com/mcp` and
`https://mcp.stg.reah.com/mcp` for hosted MCP; these are proposed addresses.

| Owner | Responsibilities |
| --- | --- |
| reah-backend | OAuth protocol engine, client policy, authorization requests, grants, tokens, consent/session validation, internal Agent credential exchange, revocation and audit |
| reah-graphql-gateway | Public OAuth HTTP facade, browser consent/management facade, dedicated internal GraphQL read endpoint, delegated identity and query enforcement |
| reah-web | Existing login/MFA continuation, consent screen, personal connections and enterprise controls |
| reah-mcp | Protected-resource discovery, per-request authentication, internal credential exchange, existing read tools and query limits |
| Deployment configuration | DNS/TLS, edge routing, network/service identity, secrets, release flags, monitoring |

The OAuth protocol engine belongs in backend, beside its transactional storage.
Use a mature Go OAuth library; [Fosite](https://github.com/ory/fosite) is the
preferred candidate for the implementation spike. Its storage hooks support
authorization codes, PKCE and refresh rotation. Pin a reviewed version and prove
transaction and resource-binding behavior before implementation. MCP discovery,
client metadata documents, and Reah consent policy still need explicit adapters;
library adoption alone does not prove MCP compatibility.

Gateway translates bounded HTTP parameters to typed backend RPCs. Backend may
adapt those fields to the library's HTTP-oriented request API using the configured
canonical issuer. Return typed OAuth results/errors to Gateway. Do not distribute
the library's database transaction across multiple Gateway storage RPCs, expose
backend directly to the internet, or trust an arbitrary inbound Host as issuer.

| Public endpoint | Purpose |
| --- | --- |
| `mcp.reah.com/mcp` | Streamable HTTP resource |
| `mcp.reah.com/.well-known/oauth-protected-resource/mcp` | Resource discovery |
| `platform.reah.com/.well-known/oauth-authorization-server` | Issuer discovery |
| `platform.reah.com/oauth/authorize` | Validate client request and open consent |
| `platform.reah.com/oauth/token` | Authorization code exchange and refresh |
| `platform.reah.com/oauth/revoke` | Client token revocation |
| `platform.reah.com/auth/authorize?request_id=...` | Web consent screen |
| `platform.reah.com/oauth/requests/{id}` | Cookie-authenticated consent details |
| `platform.reah.com/oauth/requests/{id}/decision` | Cookie-authenticated approval/denial POST |
| `platform.reah.com/oauth/connections` | Cookie-authenticated personal connection management |

Authorize/token/metadata routes must not pass through the existing Agent bearer
middleware: token endpoint Authorization headers may authenticate OAuth clients.
Use explicit route middleware. Consent/management routes require a real human
browser session. The internal `/internal/mcp/graphql` route requires both MCP
service identity and a dedicated internal credential, and is not edge-routable.

Discovery follows [RFC 9728](https://www.rfc-editor.org/rfc/rfc9728) and
[RFC 8414](https://www.rfc-editor.org/rfc/rfc8414). For an unauthenticated MCP
request, return 401 with:

```http
WWW-Authenticate: Bearer resource_metadata="https://mcp.reah.com/.well-known/oauth-protected-resource/mcp", scope="reah:read"
```

Resource metadata identifies that exact resource, the platform issuer, and
`reah:read`. Issuer metadata advertises authorization_code/refresh_token,
response type code, S256, supported client authentication, and issuer response
identification. Only advertise a registration endpoint if it is implemented.

## User journey and authorization state

```mermaid
sequenceDiagram
    participant C as MCP client
    participant M as Reah MCP
    participant W as Reah web
    participant G as Gateway
    participant B as Backend
    C->>M: Connect without token
    M-->>C: 401 and discovery metadata
    C->>G: Authorize with client, resource, state, PKCE
    G->>B: Begin authorization
    B-->>G: Validated request ID
    G-->>W: Open consent; sign in if needed
    W->>G: Load request and eligible businesses
    G->>B: Validate active human session
    W->>G: Approve selected business
    G->>B: Atomically approve and issue code
    G-->>C: Registered callback with code, state, iss
    C->>G: Exchange code and PKCE verifier
    G->>B: Consume code; create grant tokens
    G-->>C: MCP access token and optional refresh token
    C->>M: MCP request with access token
    M->>B: Validate and exchange for internal read credential
    M->>G: Internal query with service identity and read credential
    G->>B: Validate delegation and current permissions
    G-->>M: Authorized GraphQL result
    M-->>C: Read-only tool result
```

1. Validate client, redirect URI, response type, resource and scopes before any
   browser redirect. Require S256 PKCE for every client. Store the immutable
   request with a random ID and a browser-binding cookie. A malformed redirect
   URI produces a local error page, never a redirect to the unvalidated address.
2. If logged out, resume through an internal same-origin URL containing only
   the request ID. Preserve this continuation through email, Google, Passkey and
   MFA flows. Login completion must run the authorization continuation before
   the existing authorizedEntityId/overview/entity-selection redirects; merely
   supplying redirectUri does not cover every current Passkey path.
3. Load the consent request from backend. Bind it to the authenticated user and
   active session on first authenticated load. Account switching invalidates
   that binding and requires a fresh review; never approve a page rendered for
   another user. Losing the initiating browser cookie requires restarting.
4. Show the requesting application's identity, signed-in account, one eligible
   business, accessible data categories, background access duration, and the
   statement that the connection cannot move funds or change Reah data.
5. On approval POST, verify CSRF, browser binding, session ownership, active human
   session type, completed MFA, current membership, connection permission,
   enterprise policy, and any applicable account security restrictions. Derive
   the user from stored session state. Recompute consent capabilities server-side.
6. Atomically move pending → approved and create an authorization code. Repeated
   clicks cannot create multiple grants. Bind the code to the selected entity,
   user, client, exact callback, resource, PKCE challenge and consent policy.
   Denial moves pending → denied and returns access_denied to the validated
   callback. Expired or consumed requests require a fresh authorization.
7. The client exchanges the code once, presenting the same client, callback and
   resource plus the matching verifier. Code consumption and initial access /
   refresh issuance commit together. Return state unchanged and emit iss on the
   authorization response. Access/refresh tokens never enter a browser URL.
8. The client stores its tokens and refreshes in the background. An invalid grant
   requires a new authorization flow. Website sign-out does not disconnect an
   intentionally persistent connection; explain this on the consent screen.

PKCE verification follows [RFC 7636](https://www.rfc-editor.org/rfc/rfc7636).
The resource is bound during authorization and token exchange, following
[RFC 8707](https://www.rfc-editor.org/rfc/rfc8707). Production and staging use
different issuers, clients, credentials, cookies, stores and service keys.

## Reusing Agent credentials without inheriting Agent authority

The long-lived object is an MCP grant, not an existing runtime session. Reuse
Agent token generation/hash storage, profile state checks, expiration, revocation,
and last-used tracking for a short-lived internal credential.

Extend agent_credentials with origin `mcp`, `mcp_grant_id`, and an explicit
audience for the internal read endpoint. Require a grant and finite expiration
for this origin. Existing human/runtime row constraints remain valid. Use a
different token prefix, such as `reah_mcp_read_`, so old Agent bearer parsers
cannot accidentally authenticate it. Do not return these internal credentials to
the external client or expose them through general Agent credential management.

Reuse the entity's canonical Agent identity for execution attribution. A narrowly
authorized backend helper may ensure this identity exists; it must never grant
the human agent:edit or agent:execute. A disabled Agent is not silently recreated.
For this proposal, disabling the enterprise Agent also blocks MCP execution;
enterprise settings must describe that consequence.

Add separate service authorization for MCP exchange; the existing runtime HMAC
key and RPC allowlist are not shared with MCP. Only the MCP service can exchange
an external MCP access token for a short-lived internal read credential. Backend
derives grant/user/entity/client from the token. Client-supplied user IDs or an
MCP service key alone cannot mint authority for an arbitrary user.

The internal authentication response contains both identities:

```text
execution_agent_user_id, authorizing_user_id, entity_id,
grant_id, client_id, credential_id, policy_version,
permission_ceiling, audience, expires_at, parent_access_token_id
```

Gateway uses authorizing_user_id for business permissions, ownership, self-read
checks, and downstream acting-user context. It records the Agent and grant as
delegation metadata. Auth method is `mcp_delegated`, with no human browser session
or signing grant. A human user ID does not satisfy a browser-only operation.

Every Gateway request checks the parent access token/grant, current user state,
membership and enterprise policy as well as credential expiry. Revoking any
parent invalidates internal credentials without waiting for their short expiry.
Normal AuthenticateAgentToken, CreateAgentBrowserSession and the public GraphQL
route explicitly reject MCP-origin credentials. Conversely MCP rejects normal
Agent tokens, browser cookies and Google tokens.

External OAuth tokens are scoped to the MCP resource. Do not forward one as an
ordinary GraphQL bearer. Exchange it for the independently scoped internal
credential; retain the existing two-credential separation for self-hosted mode.
This follows the audience boundary in the
[MCP authorization specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization).

## Permission policy

Effective access is the intersection of:

```text
the current user's permissions and resource ownership
∩ the permission ceiling confirmed at consent
∩ the grant's entity and versioned read policy
∩ the enterprise's current connection policy
```

Use one initial resource scope, `reah:read`. Consent describes all data categories
it can expose, including contacts and member details where permitted. Scope does
not grant an underlying business permission. `offline_access` is an
authorization-server scope for refresh issuance, not a required resource scope.
New domain-specific scopes can follow when field-level policy mappings exist.

Add `mcp:connect` for self-delegation and `mcp:manage` for enterprise connection
control. Proposed defaults: Owner/Admin have both; Member/Viewer have connect.
Cardholder and KYB Collaborator are excluded from the first release rather than
implicitly upgraded to financial access. Custom roles require explicit connect
permission. No agent:execute prerequisite: a Viewer can delegate existing reads.
These are new proposed permissions, not existing code behavior.

The backend snapshots eligible business read permission slugs at consent.
Permission loss reduces access immediately. A later role promotion cannot expand
the stored ceiling; expanding access needs a fresh consent. Bind grants to a
versioned field policy so a release adding sensitive fields does not silently
expand old grants. Restrictive policy updates apply immediately.

Both MCP and Gateway validate the complete AST against the closed read schema,
including nested fields, aliases, fragments, root entity arguments, operation
count, pagination and complexity. Gateway checks entity boundaries again when
resolving resource IDs, parent objects and permission paths. Resource ownership
stays enforced downstream. Self-read shortcuts must pass delegation policy first.
Fields without permission directives require an explicit reviewed policy entry;
absence of a directive never means unrestricted MCP access.

The generic reah_query tool follows the same rules as named tools. Filtering
tools/list is a usability feature, not authorization. Preserve the current query,
response, timeout, precision and partial-error behavior. No mutations,
subscriptions, identity/session operations, signing, PAN/CVV, or Agent task data.

## State and persistence

Keep durable OAuth state in backend Postgres. Redis may hold rate limits and
disposable caches; losing Redis must not reactivate a code or refresh token.

| Record | Required fields |
| --- | --- |
| oauth_clients | client ID, registration mechanism, exact redirect URIs, authentication method, allowed resources/scopes, trusted display metadata, metadata fingerprint, status |
| oauth_authorization_requests | opaque ID, browser-binding hash, original client parameters/state, PKCE challenge, resource, bound user/session, selected entity, policy/permission ceiling, decision, code hash, expiry, consumed time |
| mcp_grants | ID, user ID, entity ID, client ID, execution Agent ID, scopes, permission ceiling, policy version, consent/auth times, absolute expiry, revocation reason/time, security version, last-used time |
| oauth_tokens | token signature/hash, kind, grant ID, parent/family linkage, resource, scope, issuance/expiry, used/revoked times, OAuth library session metadata |
| agent_credentials additions | MCP grant ID, audience, parent access-token ID; origin-specific integrity constraints |

Store opaque token material as library-compatible signatures/hashes, not raw
credentials. Authorization requests need bounded OAuth parameters, not browser
cookies or full incoming HTTP requests. Restrict stored state and callback data
to auth-service access and retention; do not include them in telemetry.

Lock/CAS authorization requests and refresh token rows within a database
transaction. Code redemption and token issuance are atomic. Refresh rotates both
credentials atomically; an already-used refresh token revokes its grant and
descendants. Multiple client instances must serialize refresh. If a successful
response is lost, retrying the used refresh token requires reconnecting; do not
weaken replay detection with an unbound retry window. Keep consumed token
tombstones long enough to detect replay throughout the grant lifetime.
These measures follow the rotation guidance in
[OAuth Security BCP](https://www.rfc-editor.org/rfc/rfc9700).

Proposed lifetimes:

| Item | Lifetime |
| --- | --- |
| Pending authorization request | 10 minutes |
| Single-use authorization code | 60 seconds |
| External access token | 10 minutes |
| Refresh token | 7 days idle, bounded by grant expiry |
| Grant | 30 days absolute; reconnect after expiry |
| Internal read credential | At most 60 seconds and no later than parent access expiry |

No cross-request positive permission/revocation cache in the initial release.
Request-local batching is allowed. A short internal-credential cache can be
keyed by access-token digest and grant, but never skips live parent checks.
Fail closed if authorization services are unavailable. Expired internal rows may
be purged after audit retention requirements are met. Backend migrations and
repository generation must retain `sqlc v2.3.3-wicked-fork`.

## Backend API contracts

Names below are proposed RPC contracts; implementation must preserve the trust
boundaries even if names change.

| RPC group | Contract and authority |
| --- | --- |
| BeginMCPAuthorization | Validates OAuth client parameters and browser binding; returns request ID / internal continuation |
| GetMCPAuthorizationRequest | Real stored human session; returns sanitized client details, eligible entities, consent capabilities and CSRF binding |
| DecideMCPAuthorization | Session, request ID, entity ID, approve/deny and CSRF proof; derives user and rechecks policy; returns validated redirect/code result |
| ExchangeMCPOAuthToken | Bounded OAuth form and client authentication; handles code/refresh atomically; returns standard OAuth response/error |
| RevokeMCPOAuthToken | Authenticates the owning client; revokes the relevant grant/tokens; unknown token is a successful no-op |
| ExchangeMCPAccessToken | MCP service-authenticated; validates external audience/token/grant and issues the internal read credential |
| AuthenticateMCPReadCredential | Gateway service-authenticated; validates parents and live access; returns delegated principal and policy |
| ListMyMCPConnections / RevokeMyMCPConnection | Real human session; requester owns the grant; revoke is idempotent |
| ListEntityMCPConnections / RevokeEntityMCPConnection / SetEntityMCPPolicy | Current entity mcp:manage; no access to secret token values |

Protocol errors remain standard invalid_request, invalid_client, invalid_grant,
invalid_scope, unsupported_grant_type and access_denied. Apply
[RFC 7009](https://www.rfc-editor.org/rfc/rfc7009) client revocation semantics.
Invalid/expired MCP access tokens return HTTP 401; insufficient resource scopes
return 403 with a scope challenge. Business data permission failures retain the
existing structured tool error behavior. Dependency outages return a retriable
service error without accepting unauthenticated traffic.

## Web implementation

Add a standalone auth/authorize route rather than an entity dashboard route.
It must work before choosing an active business and resume after login/MFA.
Reuse existing login components and completion hooks. Add a typed authorization
continuation branch and tests for every login method. Existing Google callback
state remains dedicated to Google login; do not overload it as MCP consent state.

Reuse business picker presentation, but authorize from freshly fetched backend
eligibility. Do not use activeEntityId/localStorage as authority, or invoke the
full entity-entry/signing flow solely to approve read access. Account Passkey and
MFA validation required by normal login still complete normally.

Proposed screen copy:

```text
Connect Reah to {application}
Signed in as {email}
Business: {business picker}

Allow this application to read the Reah data available to you:
Balances and accounts; transactions and spending; invoices and contacts;
treasury positions; approval records and business members, where permitted.

This connection cannot move funds, approve payments, or change Reah data.
It can access this business in the background for up to 30 days.
You can disconnect it at any time in Settings → Connected apps.

[Cancel] [Authorize read-only access]
```

Render actual granted categories for the chosen role, not every possible
category. Show the application's registered identity/domain; fetched client names
and icons are untrusted display content, never a Reah verification badge.
Ordinary active human sessions can approve. Pending MFA, recovery, view-as, and
Agent sessions cannot create or expand a durable grant. Browser requests use
CSRF protection, strict origin validation, no-store responses, frame blocking,
and a restrictive referrer policy. Keep tokens out of localStorage, analytics,
error trackers, browser history and page URLs.

Personal Settings → Connected apps shows application, business, granted
categories, authorization/expiry/last-use times, status and Disconnect. Add
enterprise controls under entity settings for administrators to disable new or
existing connections and revoke grants. Account recovery, account disablement
and an explicit security action to revoke all connections invalidate grants.
Normal browser logout only terminates that browser session.

## MCP runtime and client registration

Add a separate hosted OAuth mode. Its startup rejects fixed user tokens,
REAH_ENTITY_ID and the shared HTTP secret. Do not silently fall back to the
self-hosted handler when OAuth configuration is missing. Fixed mode remains an
explicit isolated deployment option.

Create an immutable principal for every HTTP request. Existing tools must obtain
the entity, capabilities and upstream credential from that request context.
Never mutate a global Gateway client or server entity. Check every request,
including initialize, tools/list and notifications; a transport session ID is
not authentication. Initially use stateless Streamable HTTP/JSON responses as
today. Apply identity-aware quotas and separate OAuth endpoint abuse limits.
Any later SSE/resumption cache must bind to the same grant and principal.

Discovery GETs and valid CORS preflights are public; MCP execution is not. For
browser-based clients, explicitly allow reviewed client origins, required MCP
headers and methods, and expose WWW-Authenticate. Validate Origin on actual
requests, use bearer authentication without cross-site cookies, and deny other
origins. Do not copy the isolated HTTP handler's blanket cross-origin protection
unchanged into hosted mode or replace it with unrestricted credentialed CORS.
Protect internal service calls with authenticated encrypted transport and a
dedicated service identity, in addition to private ingress routing.

Support client pre-registration and Client ID Metadata Documents. Treat Dynamic
Client Registration as an optional compatibility adapter, enabled only if the
tested target clients need it. The current
[MCP specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
prefers metadata documents and retains DCR for backward compatibility.
Use reviewed client allowlists for the initial rollout, including reviewed
metadata-document URLs. No universal client secret embedded in desktop plugins.

Validate exact registered redirects, with only standards-defined native loopback
port handling. No wildcard domains or arbitrary callback URLs. Fetch approved
client metadata with HTTPS, bounded response/time limits, redirect controls and
private/link-local address denial after DNS resolution and at connection time.
Apply the same rules to referenced keys/assets or do not fetch them. Metadata
changes require revalidation; pin the relevant fingerprint to the request.
These are application controls informed by
[MCP security guidance](https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices).

Update hosted plugin manifests only after endpoints and client compatibility
pass staging. Clients connect directly to the remote URL and manage OAuth.
An optional local OAuth bridge is a separate compatibility task, not a hidden
dependency of the hosted design. Capture the exact tested client/version;
existing stdio manifests do not prove hosted OAuth support in any client.

## Implementation and release plan

| Step | Deliverable | Completion evidence |
| --- | --- | --- |
| 1. Protocol/client spike | Pin OAuth library; prove typed backend adapter, PKCE, resource binding, client registration, refresh transaction behavior | Integration tests against the selected library plus each target client's actual discovery/redirect behavior |
| 2. Backend state and delegation | Additive migrations, RPCs, permission catalog, grant lifecycle, isolated MCP service authorization and internal Agent credentials | Real Postgres concurrency/replay tests; existing Agent credential/session tests remain green |
| 3. Gateway boundaries | Explicit OAuth routes, consent facade, internal GraphQL route and delegated permission enforcement | Public/Agent/browser credential crossover tests; AST, entity, field and self-read tests |
| 4. Web experience | Continuation, consent, business selection, personal and admin connection settings | Email/Google/Passkey/MFA end-to-end tests, deny/retry/logout/account-switch tests |
| 5. Hosted MCP | Per-request principals, discovery, exchange, remote configuration | Concurrent multi-user/entity tests and MCP transport conformance checks |
| 6. Staging and limited rollout | DNS/TLS, ingress, separate secrets, allowlisted clients/entities and observability | Real client connect/query/refresh/revoke evidence; rollout flags remain available |
| 7. Production release | Publish hosted connection instructions and compatible plugin configuration | Staged rollout with successful authorization, read, rotation and revocation monitoring |

Each repository change gets its own checked PR with explicit contract versions
and deployment dependencies. Deploy additive database/backend changes before new
Gateway consumers. Upgrade all replicas to reject MCP credentials in normal
Agent/session paths before enabling issuance. Deploy the guarded Gateway, then
MCP, then web. Enable new consent last, first for allowlisted accounts/clients.

Use separate flags for new grants and existing-grant execution. A UI or issuance
rollback may leave existing connections working; an emergency execution kill
switch denies every hosted read. Never roll backend back to a version that can
misinterpret MCP-origin credentials. Leave additive columns in place during
rollback. Revoke affected grants if a policy defect is found.

Audit consent/denial, token rotation/reuse, revocation, internal exchange and
data access by grant/client/user/entity/request ID. Exclude tokens, codes, raw
financial responses and callback query strings. Monitor authorization failure
rates, token/replay errors, permission denials, query latency, cross-entity
rejections and revocation failures. Define load-derived rate limits before broad
rollout; do not invent production quota values from the current local server.

## Acceptance criteria

- Add Reah in each supported client, complete login/MFA if needed, choose a
  business, approve, and run a read without copying a credential.
- Existing login sessions skip the login screen but still receive explicit
  client/business consent. Authorization never lands unexpectedly on Overview.
- Denial, expiration, account switching and interrupted login issue no tokens.
- Wrong client, redirect, issuer, PKCE verifier or resource fails exchange.
  A code can succeed only once, including concurrent requests and process crashes.
- Refresh rotates atomically. Reuse revokes the grant and internal descendants.
  Absolute expiry cannot be extended by refresh. Unknown revoke tokens are safe.
- A Viewer gets only existing reads. Loss of membership or permissions takes
  effect on the next read. Promotion does not expand the consent ceiling.
- Recovery, view-as, Agent and pending-MFA sessions cannot approve grants.
- External MCP, internal MCP, normal Agent and browser credentials are rejected
  outside their designated audience/route. No signing/browser session can be
  minted from an MCP credential.
- Mutations, forbidden nested fields, alternate entity IDs, alias/fragment
  bypasses and cross-entity resource IDs are rejected. Generic queries receive
  the same checks as named tools.
- Two users in different businesses can query concurrently through one hosted
  process without identity, catalog, cursor cache or result leakage.
- Disconnect, admin disable, user disable and account recovery stop the next
  authorization check. Already delivered client data cannot be recalled;
  in-flight requests are bounded by the existing request timeout.
- Metadata fetches cannot reach internal networks or unapproved redirects.
  Consent resists CSRF and framing; telemetry contains no secrets or finance data.
- Backend/Gateway outage fails closed. Existing isolated stdio/HTTP mode and
  existing Agent execution remain functional.
- DNS/TLS and all three target clients are verified in staging before hosted
  URLs appear in release manifests. Record client versions and known limitations.

The only deployment-time values still to resolve are the reserved DNS/ingress,
the reviewed OAuth library version, actual client identifiers/redirects and
metadata URLs, and operational quota values established by testing. They do not
change the product flow, grant model or permission boundary above.
