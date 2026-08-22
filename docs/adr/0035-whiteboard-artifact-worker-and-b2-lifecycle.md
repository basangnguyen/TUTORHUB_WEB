# ADR 0035: Whiteboard artifact worker and B2 lifecycle

- Status: Accepted
- Date: 2026-08-22
- Scope: P5-COLLAB-07
- Depends on: ADR-0027, ADR-0034, P5-COLLAB-02..06

## Context

Core API currently owns whiteboard lifecycle and snapshot catalog, while the collaboration runtime owns
the exact Yjs document state. Snapshot/export endpoints intentionally fail closed because neither process
alone can safely create and publish an artifact. Restoring catalog metadata without first validating and
staging the provider state would also violate the generation-swap invariant in ADR-0034.

P5-COLLAB-07 must add a bounded workflow without making PostgreSQL or B2 a second live document writer,
without putting board content in SQL/logs, and without giving the browser B2 credentials.

## Decision

### Control and data boundaries

- Core API authorizes tenant/document/generation/idempotency and writes a durable artifact command to
  PostgreSQL. It never serializes or parses a live Yjs document.
- A Node worker co-located with the selected one-instance whiteboard runtime claims commands by lease. It
  is the only component in this slice that reads provider checkpoints, validates Yjs/portable scene bytes,
  and talks to B2 with server credentials.
- B2 stores immutable opaque blobs. PostgreSQL stores bounded catalog/command metadata plus exactly one
  size-limited crash-recovery checkpoint per document generation; it stores no operation/history stream.
  Checksums, byte counts, versions, causal watermarks and opaque object coordinates are retained there.
- The browser never supplies an object key, bucket, endpoint, provider document name or restore target
  generation.

### Command lifecycle

Commands are `snapshot`, `export`, `restore` or `import_validate` and move through
`pending -> processing -> succeeded|failed|quarantined`. Claims have an owner, random lease token,
deadline and attempt cap. Expired leases can be reclaimed with `FOR UPDATE SKIP LOCKED`; completion is
fenced by the exact lease token. `(tenant_id, actor_user_id, idempotency_key)` is unique and a reused key
with a different request fingerprint fails closed.

Snapshot/export endpoints remain asynchronous (`202`). Restore keeps its existing synchronous contract,
but the Core API first enqueues/reuses a restore preparation command and waits for a short bounded period.
The worker must validate the source artifact and stage the target-generation checkpoint before Core API
performs the existing atomic current-generation/revoke-generation swap. A timeout returns provider
unavailable and leaves the current generation unchanged.

### Artifact envelope and B2 publication

The worker produces versioned envelopes with:

- exact engine/schema/format version;
- provider state and portable canonical Excalidraw scene;
- tenant/document/generation scope binding protected by a retained HMAC verification key ID;
- SHA-256 checksum, semantic hash, byte/object/file counts and Yjs state-vector watermark;
- allowlisted service provenance only.

Limits from ADR-0034 are enforced before upload: envelope 32 MiB, provider state 20 MiB, portable scene
16 MiB, 2,000 elements and 256 files. Object keys contain a random 192-bit value and no tenant, user,
document, email or display name. After PUT, the worker performs exact HEAD plus versioned GET verification
of bucket/key/version, metadata, byte length and checksum. Only then may it publish the snapshot catalog
row and complete the command.

### Import, restore and quarantine

Portable imports use the canonical portable-scene parser and reject unsupported versions, excessive
depth/size/counts, prototype-pollution keys, traversal/archive tricks, active HTML/SVG/script content,
unsafe data URLs and every external URL/fetch. An incompatible, corrupt or scope-mismatched artifact is
recorded as `quarantined` with a bounded reason code; raw bytes and content-derived error strings are never
stored or logged.

Restore verifies the exact catalog binding and B2 object version before decoding. It creates a new
provider checkpoint for a server-derived generation. It never mutates the old generation in place and it
does not swap control-plane authority until checkpoint staging succeeds.

### Retention and maintenance

Expired artifacts are claimed in bounded batches through a dedicated maintenance function using
`FOR UPDATE SKIP LOCKED`. The function only creates fenced purge work; the B2 worker deletes the exact
version and then the maintenance path removes catalog metadata. A failed B2 deletion is retryable and does
not silently erase the recovery catalog. Active restore commands retain an exact source snapshot;
terminal restore commands permit that foreign key to be redacted through `ON DELETE SET NULL` after
retention purge without weakening target-generation or provider-document binding. The collaboration
worker role receives only checkpoint,
snapshot and command columns, while a distinct maintenance role receives only the exact purge function
`EXECUTE` privileges. The Core API runtime role cannot mutate checkpoints, publish snapshots or purge.

### Activation

Core API command production and the runtime worker each have an explicit default-off switch. Production
whiteboard remains force-off through P5-COLLAB-17. This ADR authorizes local/disposable implementation and
tests, not shared-staging migration, deployment or feature enablement.

## Consequences

- Snapshot/export can survive process restarts and are idempotent, but Free private alpha still has one
  worker and accepts cold-start latency.
- PostgreSQL stores queue/catalog metadata but never board content; B2 is durable blob storage, not live
  collaboration authority.
- Restore has a bounded synchronous wait until a later API-contract task may expose a fully asynchronous
  restore command.
- Orphan uploads/checkpoints are possible after a process crash; reconciliation and fenced retention make
  them recoverable without publishing corrupt authority.
- Shared staging requires forward migration, exact ACL provisioning, disposable Neon/B2 gates and explicit
  owner permission in a later step.
