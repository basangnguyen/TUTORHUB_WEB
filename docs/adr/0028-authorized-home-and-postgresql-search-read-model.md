# ADR 0028: Authorized Home and PostgreSQL search read model

- Status: Accepted
- Date: 2026-08-08
- Scope: P3-12 Home dashboard and basic search

## Context

TutorHub already has authoritative Calendar, Notification, Conversation and Content read
paths. Home needs a small cross-module summary, while basic search must never turn a matching
row into an authorization bypass or expose message/file content as a snippet. A remote search
provider would add another tenant-scoped data copy before PostgreSQL has shown that it is
insufficient.

## Decision

- Home keeps independent queries and cache entries for upcoming Calendar items, notification
  unread count, bounded Conversation unread projection and recent ready files. Failure of one
  module degrades only its card and each card has its own retry action.
- Existing Calendar, Notification and Conversation services remain authoritative. P3-12 adds
  only a small `discovery` read model for recent ready files and cross-resource search.
- `GET /api/v1/home/recent-files` returns at most ten ready, non-deleted file metadata rows
  from classes the current actor can view. It never returns object keys, checksums, provider
  selectors or signed URLs.
- `GET /api/v1/search` searches normalized PostgreSQL text for class sessions,
  conversations and ready files. Results contain only title, safe resource label, identifiers
  and timestamps. Message bodies, file contents, session descriptions, participant lists,
  email addresses and generated snippets are outside the projection.
- Every SQL branch binds `tenant_id` and actor identity, rechecks active tenant/membership/user
  state in the same read transaction and applies the same class/direct-conversation boundary
  as the source module. Foreign or inactive resources are absent rather than distinguishable.
- Search text is trimmed, lower-cased, two to one hundred Unicode code points, and matched
  with PostgreSQL `position` so `%` and `_` are literal characters. Per-request and
  per-resource limits are bounded; the endpoint is read-only and `no-store`.
- Search uses existing PostgreSQL tables and indexes for the private-alpha data size. No
  extension, FTS materialization, Elasticsearch or vector store is added. Query latency and
  row growth must provide evidence before a forward indexing/search-provider decision.
- Client cache keys include tenant and user. Workspace switch therefore cannot reuse a Home
  or search projection from another principal.

## Consequences

- P3-12 introduces no migration and needs no new database grants; the runtime role only reads
  columns it already needs for the source modules.
- The initial search is deliberately metadata-only and does not search message bodies or file
  contents. Rich snippets require a separate privacy design.
- Very large tenants may eventually need expression/trigram/FTS indexes or a dedicated search
  service, but that change is driven by measured PostgreSQL plans and latency, not assumption.

## Alternatives rejected

- One monolithic Home endpoint: a single module timeout would make unrelated cards fail.
- Browser-side authorization/filtering: unauthorized metadata would already have crossed the
  trust boundary.
- Searching message bodies or file contents now: increases privacy and indexing scope without
  a P3-12 requirement.
- Adding Elasticsearch or a vector database: premature operational and consistency cost.
