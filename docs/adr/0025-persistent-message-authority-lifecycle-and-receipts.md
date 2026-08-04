# ADR 0025: Persistent message authority, lifecycle and receipts

- Status: Accepted
- Date: 2026-08-04
- Scope: P3-07A persistent message, unread/read core

## Context

P3-06 created direct and class conversation containers but deliberately left message
content, unread state, realtime transport and notification delivery out of scope. P3-07A
must add durable messaging without turning LiveKit DataChannel, browser memory, an
outbox consumer or a future realtime connection into the source of truth.

The first message slice also needs deterministic retry behavior, bounded storage and
send rates, tenant isolation, archived-class history, and a lifecycle that does not
leak private content into logs, audit records or operational metadata.

## Decision

### Authority and scope

- PostgreSQL commits reached through the authenticated Core API REST endpoints are the
  only source of truth for messages and read state.
- P3-07A supports only the existing `direct` and `class` conversation kinds. Group,
  session, attachment, search, typing, presence and moderation workflows are not added.
- Every request binds `X-TutorHub-Expected-Tenant-ID`. Every mutation also requires
  CSRF and reauthorizes the conversation, actor membership and relevant class state in
  the same database transaction.
- Direct content writes require the actor to be a persisted participant and both direct
  participants to have active user and tenant membership state. Class content writes
  require authoritative `chat.send` on an active class. Existing readable history
  remains available under the P3-06 read policy; archived classes are read-only.
- Missing, foreign-tenant or inaccessible conversation/message identifiers are concealed
  as `404`. A visible but read-only conversation returns `409` for content mutation.

### Persistence, ordering and idempotency

- Migration `000025` owns `messages` and `message_receipts`. All foreign keys are
  tenant-composite and `PUBLIC` receives no privilege.
- A message is ordered by a positive, conversation-local server `sequence`. Send locks
  the conversation row, checks idempotency, then allocates the next sequence in the same
  transaction. List uses an opaque keyset cursor bound to tenant and conversation and
  returns newest-first pages; `created_at` is still database-generated display time.
- Send accepts only `client_message_id` UUID and normalized plain-text `content`.
  `(tenant_id, sender_user_id, client_message_id)` is unique. Same key, conversation and
  original normalized payload returns the existing message; a different payload or
  conversation returns a typed `409` without mutation.
- A successful first insert advances `conversations.updated_at`. Idempotent replay does
  not advance ordering, quota or any side effect.

### Lifecycle and privacy

- Content is plain text with 1-4,000 Unicode characters and at most 16 KiB UTF-8 after
  CRLF normalization and trimming. The HTTP JSON body is capped separately.
- Authors may edit or delete only their own active message while the conversation still
  permits content writes. Both operations use `expected_version`; stale writes return
  `409`.
- Delete clears stored content and leaves a non-editable tombstone with `deleted_at`.
  Tombstone rows remain for ordering, receipts and idempotency. P3-07A does not introduce
  an automatic retention purge; organization retention/export/erasure operations need a
  separate reviewed lifecycle before activation.
- Message content never enters audit events, outbox rows, logs, metrics, errors or
  cursors. P3-07A emits no notification/outbox event. IDs and bounded lifecycle metadata
  may be used in future reviewed events, but never message text.

### Read state and bounded controls

- One `message_receipts` row per tenant/conversation/user stores the last-read message
  and sequence. Upsert advances only when the requested sequence is newer, so concurrent or
  out-of-order requests cannot move the marker backward.
- Unread counts exclude the viewer's own messages and are bounded at 100; the response
  states whether more unread messages exist. Opening history does not imply peer-facing
  “seen by” semantics.
- The existing `conversations` feature emergency-off blocks new send/edit/delete but does
  not hide committed history or prevent a viewer from advancing their own read marker.
  An identical replay of an already committed send is treated as a read of that resource
  and may still return `200` after current read authorization; it performs no mutation.
- Tenant controls add `messages_per_tenant` and `message_sends_per_hour`. Tombstones count
  toward the storage hard cap. A transactionally maintained per-tenant message counter,
  updated under the same tenant advisory lock as quota changes, makes hard-cap enforcement
  O(1); request paths must not scan the full message table while holding that lock. New
  sends also use a PostgreSQL-serialized actor limit of 60 messages per minute. Rate
  rejection is `429` with bounded `Retry-After`; storage exhaustion is a typed quota
  conflict. Idempotent replay consumes neither limit.

### API and delivery boundary

- `GET/POST /api/v1/conversations/{conversation_id}/messages` lists or sends.
- `PATCH/DELETE /api/v1/conversations/{conversation_id}/messages/{message_id}` edits or
  tombstones an author-owned message with optimistic versioning.
- `POST /api/v1/conversations/{conversation_id}/read` advances the viewer marker.
- P3-07A uses explicit refresh/reload over REST. SSE, WebSocket, LiveKit DataChannel,
  offline persistence, typing/presence and message notification delivery remain P3-07B
  or later work and are not activated by this ADR.

## Consequences

- Retry after a network ambiguity is safe without storing sent messages in browser
  storage or creating duplicate rows.
- Authorization changes take effect on the next transaction; class roster snapshots are
  not copied into message storage.
- Read markers and older-page pagination remain monotonic under concurrency, while the
  unread projection stays bounded.
- Column-level runtime grants are needed for `conversations.updated_at`; message and
  receipt tables need only their exact read/insert/update paths and never delete,
  truncate, reference, trigger or ownership privileges.
- Realtime delivery can later publish committed IDs without changing persistence
  authority or exposing content to the outbox.

## Alternatives rejected

- LiveKit/DataChannel as history: delivery is transient and cannot prove durable commit.
- Client timestamps or client-provided participant/sender identity: forgeable and prone
  to cross-tenant cache mistakes.
- Offset pagination: unstable when new messages arrive during older-history loading.
- Hard delete: breaks stable ordering, read markers and deterministic retry.
- Redis-only rate limits: managed Redis is not selected and would make the current
  runnable slice depend on new infrastructure.
- Enabling notifications/realtime now: crosses the P3-07B durable-worker boundary.
