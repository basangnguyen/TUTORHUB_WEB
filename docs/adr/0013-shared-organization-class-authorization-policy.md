# ADR 0013: Shared organization and class authorization policy

- Status: Accepted
- Date: 2026-07-16

## Context

Phase 1 derived organization permissions inside the identity repository, while the
classroom and media modules repeated string-based permission checks. That approach
preserved tenant isolation but could drift as class enrollment, roster and role
transitions are added in Phase 2. It also did not represent organization role and
class role as separate scopes.

## Decision

TutorHub uses one deny-by-default `internal/policy.Authorizer` for identity,
classroom and media.

- Organization roles are `org_admin`, `teacher`, `student`, and `guest`.
- Class roles are `owner`, `co_teacher`, `teaching_assistant`, and `student`.
- Effective permissions are the deterministic union of active roles in the active
  tenant and the target class only.
- Authorization input contains actor, active membership, active tenant, resource
  tenant, optional resource class, action, and resource state.
- Resource state restrictions are evaluated after role permissions.
- Missing permission is exposed as 403. A missing or cross-scope resource is
  concealed as 404 to limit identifier enumeration.
- HTTP handlers never decide roles. They build access context from the verified
  server-side principal; modules ask the shared policy and repositories retain
  tenant-scoped queries.

The organization matrix initially preserves Phase 1 behavior. P2-05 and P2-06 will
load persisted class enrollment roles into the same policy input without changing
the module interface.

## Consequences

- Permission constants and role mappings have one source of truth and table-driven
  tests.
- Identity can still return the existing `permissions` list while using the same
  engine as classroom and LiveKit media authorization.
- Adding a role or action requires a policy change, matrix documentation, OpenAPI
  update, and tests; unknown values have no permissions.
- Class-scoped enforcement becomes stricter once enrollment persistence is wired,
  but no Phase 1 endpoint behavior changes in P2-00.
- A static test rejects reintroduction of local permission helpers in domain modules.

## Alternatives rejected

- Keep checks in handlers: easy initially, but duplicates security behavior and is
  difficult to audit.
- Put permissions only in OIDC claims: claims become stale after membership changes
  and cannot safely express active tenant/class resource state.
- Use a remote policy service now: adds network availability and operational cost
  before the modular monolith needs that boundary. The interface permits a later
  adapter if scale requires it.
