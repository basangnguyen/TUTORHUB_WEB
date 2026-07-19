# ADR 0014: Append-only tenant audit log

- Status: Accepted
- Date: 2026-07-19
- Scope: P2-07 and later sensitive tenant operations

## Context

P2-02 through P2-06 already write domain facts to the transactional outbox, but the
outbox is not an audit store. Delivery bookkeeping intentionally updates outbox rows,
its payloads have different shapes, and it cannot represent denied, failed, or
idempotent attempts. TutorHub needs a tenant-scoped history that links an actor and a
request to sensitive changes without retaining tokens, raw sessions, or unnecessary
personal data.

The HTTP `X-Request-ID` may be supplied by a client and is therefore useful for
correlation but not safe as a unique or idempotency key. Some operations also commit a
business transaction before building their response, and roster bulk operations commit
items independently. An HTTP status alone consequently cannot decide whether a
mutation happened.

## Decision

TutorHub introduces an `audit_events` table and an `internal/modules/audit` boundary in
the existing Go modular monolith.

- Audit rows are append-only and tenant-owned. The runtime path exposes insert and
  select only; database triggers reject update, delete, and truncate as defense in
  depth. Migration owners remain able to administer the schema, so this is runtime
  immutability rather than cryptographic WORM storage.
- A row contains tenant, user or system actor, an imperative action, resource type and
  optional ID, `succeeded`, `denied`, or `failed` outcome, request ID, a server-generated
  request-instance ID, occurrence time, privacy-reduced network/device hints, and a
  bounded redacted metadata object.
- A user actor references an authoritative application user but does not require a
  membership in the target tenant. This is intentional for invitation acceptance:
  the server can resolve a valid invitation's tenant before the actor joins it. The
  target tenant must come from that server-side resolution, never from client input;
  an unresolved token remains a structured security/application log only.
- `request_id` links the row to structured application logs. The UUID request-instance
  ID distinguishes reused client request IDs and groups independently committed bulk
  items; it is not returned by the public API.
- A changed successful mutation appends its audit row in the same database transaction
  as the business change and outbox event. Failure of this audit insert rolls the
  business transaction back.
- Authenticated no-op successes, denials, and domain failures are appended after the
  business transaction using a separate best-effort transaction. Before doing so the
  recorder checks the request-instance ID, so it does not mislabel a mutation whose
  transaction committed but whose response projection later failed. Independently
  committed bulk items additionally bind this check to a server-owned target user ID,
  so repeated actions in one request remain distinct without creating contradictory
  success/failure rows after an ambiguous commit response.
- If PostgreSQL itself is unavailable, a failed attempt cannot be durably written. The
  primary response is preserved and an `audit_write_failed` structured log with the
  same request ID is the residual fallback. Pre-authentication, invalid-CSRF, and
  pre-tenant failures remain security/application logs because no authoritative tenant
  and actor scope exists yet.
- Automatic expiry uses a system actor. Bulk roster operations emit one audit result per
  target item, including `internal_failure` for the aborting item and `not_attempted`
  for remaining targets, and share the request-instance/request IDs.
- Metadata is produced only by server-owned typed helpers. It is bounded and may contain
  enum transitions, effect codes, field names, or coarse failure reasons. It must not
  contain invitation/code tokens or hashes, cookies, raw session IDs, email addresses,
  names, descriptions, request bodies, SQL, stack traces, or raw errors.
- Organization administrators receive `audit.view`. The query API reloads the active
  tenant membership authoritatively, always predicates by tenant ID, and uses an opaque
  filter-bound keyset cursor ordered by `(occurred_at, id)` descending. There is no
  update or delete API.
- The initial API is `GET /api/v1/tenants/{tenant_id}/audit-events`. The path tenant must
  equal the verified active tenant; it never overrides session scope. Responses are
  `no-store` and support time, action, resource, and outcome filters.
- Export and retention are ports, not public mutation endpoints. P2-07 supplies disabled
  retention and paged export interfaces; production retention, legal erasure,
  partitioning, and a dedicated maintenance role are decided in Phase 8.

The audit action catalog describes intent (`tenant.update`,
`class.enrollment.update_role`) while outbox event types remain past-tense domain facts
(`tenant.updated`, `class.enrollment.role_changed`). A centralized mapping keeps the
two taxonomies explicit.

## Consequences

- Audit success has the same atomicity as sensitive business data and the outbox.
- Tenant isolation, authorization, pagination, and redaction can be tested independently
  from event delivery.
- Adding a sensitive mutation requires an action catalog entry, a transactional success
  mapping, a failure/no-op attempt boundary, and tests.
- Runtime immutability depends on using a non-owner, non-superuser application role.
  Role grants remain an infrastructure provisioning responsibility because provider
  role names are environment-specific.
- Audit rows for an archived tenant remain retained but are not visible through the
  active-tenant API. Recovery or platform export is deferred with the Phase 8 policy.
- Restrictive foreign keys preserve audit history and must be revisited together with
  the production privacy-erasure policy before hard deletion is introduced.

## Alternatives rejected

- Reuse `outbox_events`: rejected because rows are mutable delivery state, existing
  payloads are not a stable redacted projection, and failures/no-ops have no row.
- Infer audit outcome only from HTTP status: rejected because post-commit projection
  failures and per-item bulk commits make that inference incorrect.
- Trust `X-Request-ID` as unique: rejected because callers may reuse it.
- Make tenant nullable to capture anonymous failures: rejected for P2-07 because it
  weakens the tenant boundary and would require a separate platform-audit authority.
- Add a remote audit service now: rejected because it adds a distributed availability
  dependency before the modular monolith needs independent scaling.
