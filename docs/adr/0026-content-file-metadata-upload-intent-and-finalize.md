# ADR 0026: Content file metadata, upload intent and finalize authority

- Status: Accepted
- Date: 2026-08-07
- Scope: P3-08 file metadata, upload intent and finalize core

## Context

TutorHub needs class file metadata before browser-to-object-storage transfer, scanning,
thumbnail generation and end-user sharing can be introduced safely. The first slice must
reserve tenant capacity, make ambiguous retries deterministic and establish a server-owned
file lifecycle without proxying file bodies through the Core API or trusting metadata
reported by an untrusted browser.

Backblaze B2 is already the accepted private object store. Presigned transfer is P3-09 and
processing is P3-10, so P3-08 must expose a useful contract without making pending or
unscanned content downloadable.

## Decision

### Authority and scope

- PostgreSQL is authoritative for file identity, class ownership, quota reservation and
  lifecycle. B2 is authoritative for the stored object's immutable upload evidence.
- P3-08 supports class-scoped source files only. Personal drive, folders, sharing,
  download, preview, multipart transfer, processing jobs and deletion are out of scope.
- The API exposes `POST /api/v1/files/upload-intents`, `GET /api/v1/files/{file_id}` and
  `POST /api/v1/files/{file_id}/finalize`. Upload intent creation and finalize require
  CSRF; every route binds `X-TutorHub-Expected-Tenant-ID`.
- Upload and finalize require current authoritative `file.upload` permission on an active
  class. Metadata reads require `file.view`; a file that is not `ready` is visible only to
  its creator or a current actor with upload authority. Missing, inaccessible and
  foreign-tenant IDs are concealed as `404`.
- The `file_uploads` feature switch is off when the object-store runtime prerequisite is
  unavailable. The switch blocks new intent/finalize mutations but does not hide already
  committed metadata.

### Intent, keys and idempotency

- A valid intent contains a class ID, bounded display filename, normalized declared media
  type, expected byte length, lower-case SHA-256 and a client request UUID. The server
  generates both the file ID and opaque object key; raw filenames never form storage keys.
- The object key is private implementation data and is not projected by P3-08 APIs.
  P3-09 will return a narrowly scoped, short-lived transfer capability instead.
- `(tenant_id, creator_user_id, client_request_id)` is unique. A request fingerprint binds
  every semantic input. An identical retry returns the original record without consuming
  quota or rate capacity; a changed payload returns a typed `409`.
- Pending intents expire after 15 minutes. Expired pending reservations are reclaimed
  transactionally on a later create. Their rows become deleted tombstones so the original
  idempotency key cannot silently create a new object.

### Lifecycle and finalize proof

- The state machine reserves `pending -> uploaded -> processing -> ready/rejected`.
  P3-08 performs only `pending -> uploaded`; later transitions belong to P3-10.
  Deletion/retention uses separate tombstone fields and never masquerades as a processing
  state.
- Finalize accepts no client assertion of stored size, MIME, checksum, ETag or version.
  The Core API issues an object metadata request to B2 and requires exact expected size,
  normalized content type, SHA-256 and a non-empty immutable version ID.
- Missing or unverifiable provider checksum/version fails closed. A mismatch leaves the
  file pending and returns a typed conflict. Provider failure returns a retriable service
  error without mutating PostgreSQL.
- The network request occurs outside a database transaction. The repository then locks and
  reauthorizes the file/class, compares the still-current pending expectations, persists
  the immutable storage proof and atomically moves bytes from reserved to committed.
  An already-uploaded replay is successful only for the same persisted proof.
- Files are not shareable or downloadable until `ready`. P3-08 therefore never issues a
  download URL and does not publish a worker/outbox event.

### Quota, privacy and database privileges

- Tenant controls add `files_per_tenant`, `file_bytes_per_tenant`,
  `single_file_bytes` and `file_upload_intents_per_hour`. A per-tenant usage row tracks
  file count plus reserved and committed bytes under the existing tenant advisory lock.
  Expired pending rows release count and reserved bytes exactly once.
- Upload intent rate is serialized in PostgreSQL and identical idempotent replay does not
  consume the limit.
- Filenames, object keys and checksums never enter logs, errors, audit/outbox payloads or
  metrics. API responses expose only bounded business metadata needed by the authenticated
  actor.
- Migration `000026` uses tenant-composite foreign keys, revokes `PUBLIC`, and leaves
  environment-specific Core API role provisioning separate. Runtime roles receive only
  the exact table/column privileges required by repository statements and never table
  ownership, `DELETE`, `TRUNCATE`, `REFERENCES` or `TRIGGER`.

## Consequences

- P3-09 can add direct browser transfer without changing metadata authority or allowing
  the browser to choose an object key.
- P3-10 can consume uploaded rows and advance processing state without exposing unscanned
  content.
- Finalize depends on B2 returning trustworthy SHA-256 and version metadata for the exact
  upload mechanism. That behavior must be proven during P3-09 provider acceptance; until
  then the endpoint intentionally fails closed.
- Pending-intent cleanup is lazy in this slice. A scheduled cleanup may be added later with
  the same row locks and accounting invariants.

## Alternatives rejected

- Trusting size, MIME or checksum supplied to finalize by the browser: forgeable and does
  not prove what is stored.
- Returning the raw object key: unnecessarily exposes storage layout and encourages clients
  to construct provider requests outside an issued capability.
- Using filename as an object key: collision-prone, reveals user-controlled data and makes
  rename semantics unsafe.
- Reserving quota only after upload: permits unbounded orphan uploads and races hard-cap
  enforcement.
- Holding a database transaction open while calling B2: extends locks across an unreliable
  network boundary.
- Marking files ready at finalize: bypasses the P3-10 scanning and metadata safety gate.
