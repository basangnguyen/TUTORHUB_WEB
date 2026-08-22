# ADR 0037: Whiteboard feature, quota and operations controls

- Status: Accepted
- Date: 2026-08-22
- Scope: P5-COLLAB-09
- Depends on: ADR-0034, ADR-0035, ADR-0036

## Context

The whiteboard control plane, realtime runtime and durable artifact path now exist, but production must
remain force-off until the later rollout slice. Before that rollout, TutorHub needs one server-owned
feature dependency, tenant-scoped limits, bounded operational telemetry and an emergency mode that
preserves reading and export without accepting new edits.

## Decision

### Feature and deployment guardrail

- `classroom_whiteboards` is a typed feature-control key with compiled default `false`.
- Deployment configuration may force the feature off. A tenant override can never bypass that guardrail.
- Core API, artifact workflow and grant exchange all require the server-evaluated feature. Browser state,
  role labels and provider state are not feature authority.

### Tenant quota model

| Quota                                 | Default | Deployment maximum | Enforcement boundary                            |
| ------------------------------------- | ------: | -----------------: | ----------------------------------------------- |
| `whiteboard_documents_per_tenant`     |      10 |                100 | Serializable document creation transaction      |
| `whiteboard_connections_per_tenant`   |      50 |                100 | Grant scope and runtime tenant admission        |
| `whiteboard_storage_bytes_per_tenant` |   1 GiB |             10 GiB | Artifact reservation under tenant advisory lock |
| `whiteboard_operations_per_minute`    |   6,000 |             60,000 | Runtime rolling tenant window                   |

- Tenant overrides may reduce or raise the compiled default only up to the deployment maximum.
- Static global runtime limits remain an additional ceiling. Effective permission is the minimum of the
  server projection and runtime ceiling.
- Dynamic connection and operation accounting is tenant-keyed. Exhausting one tenant's budget rejects
  that tenant with a bounded reason and does not reduce another tenant's budget.
- Artifact storage is reserved before snapshot/export work is queued. The reservation is serialized per
  tenant so concurrent requests cannot individually pass the same remaining capacity.

### Runtime modes and kill switch

`COLLABORATION_RUNTIME_MODE` has three accepted values:

- `enabled`: normal feature-authorized lifecycle, grant, edit and artifact operations.
- `read_only`: document projection, snapshot listing and export remain available; lifecycle mutation,
  snapshot creation and restore are rejected; new grants are clamped to `view`.
- `off`: public whiteboard routes are concealed as not found and runtime readiness reports unavailable.

The incident sequence is `enabled -> read_only -> off` only when containment requires it. Recovery moves
back to `enabled` after authority, PostgreSQL and B2 checks pass. An incident does not use a schema
rollback and never deletes the last verified artifact.

### Metrics and privacy

- Runtime metrics use fixed capability, outcome, dependency and rejection-reason vocabularies.
- Tenant, document, actor, session, lease, provider and artifact identifiers are forbidden as metric
  labels. Document content and provider credentials are forbidden from metrics and logs.
- Operation and connection quota rejections use fixed `operation_quota` and `connection_quota` reasons.
  Raw quota keys are not accepted from network input.

## Consequences

- Migration `000041` extends only typed feature/quota constraints; it does not enable the feature or
  change collaboration ACLs.
- Private alpha keeps a single Render instance and accepts cold start. The hard cost boundary remains
  zero USD; quota exhaustion keeps whiteboard force-off or read-only instead of scaling automatically.
- P5-COLLAB-10 through P5-COLLAB-16 still own the wider authorization, abuse, convergence, load,
  accessibility and outage matrices. P5-COLLAB-17 owns any staging enablement decision.

## Rejected alternatives

- Frontend-only feature flags or limits: easy to bypass and cannot isolate a noisy tenant.
- Tenant/document identifiers in Prometheus labels: unbounded cardinality and privacy leakage.
- One global operation counter: a noisy tenant could deny service to unrelated tenants.
- Hard-off as the only kill switch: prevents safe export and unnecessarily widens recovery impact.
