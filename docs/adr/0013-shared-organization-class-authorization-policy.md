# ADR 0013: Shared organization and class authorization policy

- Status: Accepted
- Date: 2026-07-16
- P2-05 amendment: 2026-07-19

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

Starting in P2-05, organization `student` and `guest` grant only tenant-scoped
access. They do not grant `class.view`, `session.join`, `media.publish`, or
`chat.send` across every class in the tenant. Those permissions come from an
active persisted enrollment for the target class. Organization `org_admin` and
`teacher` remain global class managers, while `classes.owner_user_id` remains the
implicit owner role. Classroom reads resolve this state authoritatively for each
request and expose a server-derived viewer projection; session-supplied class
roles are never trusted.

`enrollment.leave` remains in the complete `org_admin` permission union and is
also granted through every class role. The domain capability and repository still
require a persisted active enrollment for the actor, so an organization admin or
implicit owner without that enrollment cannot self-leave. The action remains
state-eligible for an archived class so an enrolled member can leave without
restoring the class. `enrollment.manage` also remains state-eligible on archived
classes so managers can inspect and revoke invitation artifacts; domain transition
guards still reject new enrollment, invite creation, suspend, or remove operations
unless the class is active.

## Consequences

- Permission constants and role mappings have one source of truth and table-driven
  tests.
- Identity can still return organization-scoped `permissions`; class-scoped
  permissions are resolved only with the target class and current enrollment.
- Adding a role or action requires a policy change, matrix documentation, OpenAPI
  update, and tests; unknown values have no permissions.
- Class-scoped enforcement becomes stricter once enrollment persistence is wired,
  so an unenrolled student cannot enumerate class detail or obtain a media token.
- Class list filtering and class detail/media authorization share the same
  owner/enrollment projection, reducing permission drift between modules.
- A static test rejects reintroduction of local permission helpers in domain modules.

## Alternatives rejected

- Keep checks in handlers: easy initially, but duplicates security behavior and is
  difficult to audit.
- Put permissions only in OIDC claims: claims become stale after membership changes
  and cannot safely express active tenant/class resource state.
- Use a remote policy service now: adds network availability and operational cost
  before the modular monolith needs that boundary. The interface permits a later
  adapter if scale requires it.
