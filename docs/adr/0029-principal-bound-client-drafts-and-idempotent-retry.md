# ADR 0029: Principal-bound client drafts and idempotent mutation retry

- Status: Accepted
- Date: 2026-08-08
- Scope: P3-13 offline/retry drafts and Phase 3 quota closure

## Context

TutorHub already exposes offline/error states and server-side idempotency for selected Calendar,
Availability Poll, message and file mutations. Client form state is otherwise memory-only, while
blindly persisting every form or enabling generic mutation retry would retain private content and
could replay non-idempotent operations.

The feature-control catalog has also expanded across scheduling, polls, messages and files. The
admin projection already exposes the typed values, but quota rejection metrics still recognize
only the three original Phase 2 keys. P3-13 must close these gaps without adding Redis, a service
worker, a provider, billing semantics or a database migration.

## Decision

TutorHub stores only explicitly allowlisted, non-sensitive drafts in browser `sessionStorage`.
Every record is versioned, size-bounded, expires after eight hours and is scoped by actor ID,
tenant ID, draft kind and resource ID. Invalid, oversized, expired or scope-mismatched records
are deleted. Storage failures degrade to memory-only editing.

The first persisted draft is the organization administrator's feature/quota override form. It
contains only typed boolean and integer configuration plus the optimistic version. TutorHub does
not persist message content, poll participant/availability data, capability tokens, signed URLs,
file handles, checksums or provider metadata. A successful save or explicit reload deletes the
draft. Logout, an unauthorized session boundary and workspace/principal switch purge all
TutorHub draft records before the next principal can render.

Automatic mutation retry is opt-in. A shared predicate permits at most one retry for a network
failure or HTTP 5xx response, and only callers whose request already carries a stable,
server-enforced idempotency key may use it. HTTP 4xx, quota rejection, conflict, validation and
authorization failures are never retried automatically. Full file transfer remains manual retry:
although upload intent is idempotent, the operation crosses expiring B2 capabilities and
multipart provider state.

The Go feature-control catalog is the single bounded label source for quota rejection metrics.
Metrics allocate one counter per compiled quota key, ignore unknown labels and never use tenant,
user, IP, token or free-form values. Existing typed Problem Details and `Retry-After` behavior
remain authoritative. Existing maintenance SQL continues to delete only expired quota/rate-limit
windows in bounded `SKIP LOCKED` batches; quota lowering never deletes historical business data.

## Consequences

- A same-tab reload can recover an unsaved admin control draft without making it durable across
  browser sessions or principals.
- New persisted draft kinds require an explicit schema, validator, sensitivity review, scope and
  purge test; a generic arbitrary-value storage API is not exposed to feature code.
- New quota keys automatically appear in the bounded metric label set when added to the typed Go
  catalog, and tests fail if catalog/metric coverage diverges.
- Mutations without an idempotency key keep manual retry even when the server operation is often
  naturally idempotent.

## Alternatives rejected

- Persist every form in `localStorage` or IndexedDB: retains private content too broadly and can
  cross browser sessions on shared devices.
- Persist message, availability, token or signed-URL state: violates the P3-13 privacy boundary.
- Enable React Query mutation retry globally: can duplicate non-idempotent side effects.
- Add a service worker/offline mutation queue: expands lifecycle, credential and conflict risk
  beyond the Core Exit requirement.
- Add Redis, a metrics SaaS or a new quota service: no measured alpha load justifies it.
