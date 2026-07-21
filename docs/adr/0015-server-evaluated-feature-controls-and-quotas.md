# ADR 0015: Server-evaluated tenant feature controls and quotas

- Status: Accepted
- Date: 2026-07-20
- Scope: P2-09 and later tenant-scoped expansion controls

## Context

TutorHub already authorizes tenant and class actions on the server, but Phase 2 has no
single source for disabling an expansion path or limiting tenant consumption. UI-only
flags, process-local counters, and a count followed by an insert would be bypassable by
direct API calls or concurrent requests. The existing invitation limiter is also local
to one process and keys untrusted traffic by `RemoteAddr`, which is insufficient once
Cloudflare Pages proxies requests to the public Render origin.

P2-09 needs feature and quota controls without introducing billing, a new service, a
paid flag provider, or Redis before measured alpha load justifies them.

## Decision

TutorHub adds an `internal/modules/featurecontrol` boundary to the Go modular monolith.
The module owns the typed catalog, effective-value evaluation, tenant override
repository, PostgreSQL enforcement helpers, capability projection, and admin service.
Identity and classroom call its narrow enforcer interface; they do not read control
tables directly.

The initial feature catalog is `membership_invitations`, `class_management`, and
`class_invite_links`. The initial quotas are `members`, `active_classes`, and
`invite_creations_per_hour`. The latter is shared by successful membership-invitation
and class invite-link creation.

Existing Phase 2 paths default to enabled so the migration is backward compatible.
Defaults are conservative and bounded in the typed catalog. Unknown keys are rejected
and fail closed. Feature disabling blocks expansion paths while retaining safe cleanup
and history paths: create/accept membership invitations; create/activate/restore
classes; and create/join class invite links are guarded, while read, revoke, archive,
leave, and audit remain available.

Effective values use this precedence, from strongest to weakest:

1. a validated deployment emergency guardrail, which may only force a feature off or
   lower a quota ceiling;
2. an active tenant override;
3. the compiled catalog default.

A tenant override can never exceed the catalog hard bound or bypass a deployment
guardrail. P2-09 exposes tenant override administration only to an active organization
administrator through the new `tenant.manage_features` policy permission. Platform
operators own deployment guardrails. A product-facing platform-admin identity and API
remain deferred and require a later ADR.

The effective value and the tenant-configured edit value are distinct. A deployment
guardrail may clamp the effective value without rewriting the catalog default or tenant
override. An administrative read therefore returns `configured_enabled` and
`configured_limit` in addition to the effective fields; update forms must initialize
and compare their draft against those configured fields. This prevents an unrelated
aggregate update from persisting a temporary deployment clamp as a tenant override.

Migration `000012` stores normalized tenant feature overrides, tenant quota overrides,
an aggregate tenant control revision for optimistic concurrency, and fixed quota
windows. Absence of an override row means catalog default; migrations do not seed one
row per tenant. Override changes lock the tenant and revision, reauthorize the actor
from persisted membership, use compare-and-swap versioning, and append audit plus
outbox facts in the same transaction.

All governed mutations evaluate controls inside their existing PostgreSQL transaction.
Capacity paths lock the tenant before authoritative count/check/write so concurrent
accept or activation cannot exceed a limit. Draft class creation does not consume the
active-class quota; `draft -> active` and restore to the prior active state do. Pending
membership invitations do not consume member capacity; acceptance does. Lowering a
limit below current usage is allowed and does not delete or suspend existing data, but
it blocks the next expansion.

The invite creation quota uses a tenant fixed-window row and consumes only in the same
transaction as a successful create. Rollback therefore does not consume capacity.
PostgreSQL is the shared coordination store for private alpha; the port permits a later
Redis implementation without changing domain callers.

The public read model is
`GET /api/v1/tenants/{tenant_id}/capabilities`. It is available to an active member of
that same active tenant, is `no-store`, and returns effective typed features,
same-tenant usage/limit values, tenant-control operation availability and bounded
reasons. The `operations` projection answers only whether the effective feature and
quota controls allow an operation; it is not an authorization decision. The web must
combine it with the actor and resource permissions already projected by the identity
and classroom APIs, and every mutation reauthorizes on the server.
Only an actor allowed to manage overrides additionally receives the configured edit
values; the deployment guardrail source and ceiling remain operator-only and are not
exposed. The web treats the effective projection as advisory display state, uses only
configured fields to build an override mutation, and fails closed while the response
is loading or unavailable. Direct API enforcement remains authoritative. The admin
mutation is an aggregate feature-control endpoint with CSRF and expected-version
protection.

Disabled features return a typed `403 feature_disabled`; exhausted capacity returns
`409 quota_exceeded`; invite-rate exhaustion returns `429 quota_exceeded` with
`Retry-After`. Evaluator/storage failure is `503` and fails closed. Quota rejection
metrics use bounded catalog key and operation labels and never tenant, user, email,
token, or IP values.

Cloudflare must replace client-address headers and attach a short-lived HMAC-signed
edge context covering version, timestamp, method, path, and canonical client prefix.
Core API accepts a configured clock skew no greater than five minutes; the staging
default is two minutes and startup fails if the configured value exceeds the bound.
Render trusts the forwarded prefix only after validating that context; otherwise it
falls back to the direct peer address. Anonymous preview/accept/join abuse protection
uses a shared PostgreSQL window keyed by a domain-separated hash of limiter version,
purpose, and canonical prefix, never raw IP or token. Storing purpose in a separate
column is not a substitute for binding it into the digest because the same prefix must
not be correlatable across purposes through an identical bucket hash. The existing
bounded local limiter may remain only as an optional first line.

## Consequences

- Flags, quota values, authorization, usage, and HTTP/UI behavior share typed keys.
- Cross-instance concurrent mutations are serialized by PostgreSQL and remain within
  quota without introducing another provider.
- Capability cache keys must contain tenant ID and be purged on workspace switch.
- Manager edit state must use configured fields, never deployment-clamped effective
  values.
- New expansion features must declare a catalog key, default, hard bounds where
  relevant, enforcement points, capability projection, audit behavior, metrics, and
  tests.
- PostgreSQL window rows require bounded cleanup/retention. Redis remains optional
  until load or latency evidence requires it.
- Billing plans, entitlements purchased by a customer, automatic provider upgrades,
  and platform-admin product workflows are explicitly outside P2-09.

## Alternatives rejected

- Frontend, `localStorage`, or OIDC-claim flags: bypassable and stale.
- Handler-only checks: do not protect other callers or concurrent database mutations.
- Environment-only tenant flags: cannot safely represent independent tenant choices.
- Untyped JSON control blobs: weak validation and poor migration/query guarantees.
- Count then insert without a shared transaction lock: races across requests/instances.
- Audit/outbox rows as usage counters: append history and delivery state are not quota
  coordination primitives.
- A SaaS flag provider, Redis, microservice, or billing subsystem in Phase 2: adds cost
  and operational boundaries without current evidence.
