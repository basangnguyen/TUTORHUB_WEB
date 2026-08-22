# ADR 0036: Whiteboard reconnect, compaction and generation recovery

- Status: Accepted
- Date: 2026-08-22
- Scope: P5-COLLAB-08
- Depends on: ADR-0034, ADR-0035, P5-COLLAB-05, P5-COLLAB-07

## Context

The Hocuspocus/Yjs data plane already reconnects with Yjs state-vector sync, PostgreSQL stores one bounded
latest checkpoint per generation, and the artifact worker can restore a verified immutable B2 snapshot
into a new generation. P5-COLLAB-08 must make those pieces one deterministic recovery policy without
adding an operation log, changing canonical authority, or allowing cached clients to write after a
close/revoke/restore fence.

## Decision

### Reconnect and resume watermark

- Yjs state vectors are the resume watermark. A reconnect performs the normal differential sync; full
  state is used only when the remote state vector cannot satisfy the missing update range.
- Yjs update idempotency is the duplicate-delivery boundary. Replayed and out-of-order updates may be
  applied more than once, but the canonical scene and final state vector must converge.
- The browser keeps the same in-memory `Y.Doc` during transient network loss so unsent local updates and
  actor-local undo remain available. It does not copy scene content into REST, storage, logs or URL state.
- A bounded authority close is terminal. The browser discards the memory-only grant, refetches the Core API
  projection and creates a new provider session only from the newly projected generation/revoke fence.

### Checkpoint compaction

- Compaction is copy-on-write. Runtime rebuilds the encoded state in an isolated `Y.Doc`, replays that
  candidate into a second probe document and requires the exact same state vector before persistence.
- Compaction never replaces the live `Y.Doc` and never touches its undo manager. Therefore it cannot turn
  remote changes into local undo entries or silently clear the active actor-local undo stack.
- Corrupt, empty, oversized or causally divergent candidates fail closed with bounded reason codes. The
  previous durable checkpoint remains authoritative because PostgreSQL replacement happens only after
  validation succeeds.
- PostgreSQL continues to hold exactly one current checkpoint per generation. It is not an operation,
  audit, history or undo store.

### Recovery and generation fence

- A current-generation checkpoint failure makes runtime readiness red and closes affected authority; the
  runtime does not silently load another generation or a guessed object.
- Last-good recovery uses the P5-COLLAB-07 artifact workflow: verify exact B2 version/envelope/scope,
  quarantine corrupt or incompatible input, stage a server-derived next-generation checkpoint, then swap
  `current_generation` and increment `revoke_generation` atomically.
- Runtime authority revalidation closes old leases with a bounded terminal reason. Cached grants,
  provider connections and pending old-generation work cannot cross the generation/revoke fence.
- Private-alpha recovery objectives are measured from the last successful checkpoint/snapshot watermark.
  The accepted target is RPO no worse than the last verified durable artifact and operator-driven RTO of
  five minutes; later soak and browser matrices may tighten these values.

## Consequences

- No migration or new provider is required for P5-COLLAB-08.
- Transient reconnect remains fast and offline-capable, while authority changes deliberately require a
  fresh Core API projection and one-time grant.
- Automatic cross-generation fallback is forbidden; an operator or authorized restore command selects
  the verified last-good artifact.
- P5-COLLAB-12/13 still own the wider multi-browser, load and artifact recovery matrices. P5-COLLAB-08
  owns the core invariants and focused deterministic tests only.

## Rejected alternatives

- Persisting every Yjs update in PostgreSQL: creates a second history authority and unbounded retention.
- Replacing the live document during compaction: destroys active local undo semantics and risks lost edits.
- Reusing a cached grant after restore/revoke: violates the generation fence.
- Loading the most recent B2 object automatically after corruption: object recency is not authorization or
  scope proof and can silently widen RPO.
