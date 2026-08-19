# P5-COLLAB-01 — Excalidraw canonical authority contract

> **Checkpoint:** Gate B `DONE`
> **Date:** 2026-08-19
> **Scope:** isolated `apps/whiteboard-spike`; no production route, database, shared staging or deploy

## 1. Exact candidate boundary

- `@excalidraw/excalidraw@0.18.1` is the editor/renderer projection only.
- `yjs@13.6.27` `Y.Doc` is the sole canonical document and causal-history authority for one exact
  `{tenantId, documentId, generation}`.
- `@hocuspocus/provider@4.6.0` and `@hocuspocus/server@4.6.0` are the exact Gate B transport/provider
  candidate. They replicate the canonical `Y.Doc`; they do not introduce a second document/history.
- Excalidraw internal history must not be wired as a competing undo authority in collaborative mode.
  Product undo/redo is exposed only by `CanonicalExcalidrawAuthority` and its actor-scoped
  `Y.UndoManager`.
- This selection is enough to test the model and protocol adapter. Hosting region, durable store,
  HA/drain, backup, operational owner and TCO remain Gate F and ADR-0034 remains `Proposed`.

## 2. Canonical schema v1

The adapter is implemented in
`apps/whiteboard-spike/src/excalidraw/canonicalAuthority.ts`.

| Canonical root | CRDT type | Meaning |
| --- | --- | --- |
| `tutorhub.excalidraw.metadata.v1` | `Y.Map` | schema version and exact tenant/document/generation binding |
| `tutorhub.excalidraw.page.v1` | `Y.Map` | page id, name and background color |
| `tutorhub.excalidraw.elements.v1` | `Y.Map<actor/element, envelope>` | actor-scoped revision/tombstone register |
| `tutorhub.excalidraw.element-order.v1` | `Y.Array<elementId>` | deterministic Excalidraw z-order |
| `tutorhub.excalidraw.files.v1` | `Y.Map<fileId, stable JSON>` | supported Excalidraw image-file metadata/payload |

Supported element types are `rectangle`, `ellipse`, `diamond`, `line`, `arrow`, `freedraw`, `text`,
`image` and `frame`. The adapter preserves JSON fields for the supported element/file subset, including
text-container links, arrow bindings and image references. Page background maps to
`appState.viewBackgroundColor`; page id/name remain TutorHub page metadata.

Each element envelope contains actor id, element id, monotonic revision, tombstone and the supported
element JSON. Resolution selects the highest revision then actor id deterministically. Concurrent
values remain addressable: undoing one actor's winning value reveals the remote value instead of
deleting the element. The CRDT array preserves z-order and projection ignores stale order entries.

The semantic hash is deterministic FNV-1a 64 over stable canonical JSON. It includes element order,
so a z-order change changes the hash. This hash is only for fast semantic/convergence comparison; the
immutable snapshot integrity gate must use a cryptographic checksum later in Gate D.

## 3. Transaction and undo contract

- Every local adapter instance owns a non-serializable origin object containing its authenticated
  actor id. Only that exact origin is tracked by its `Y.UndoManager`.
- Bootstrap and remote/provider transactions use untracked origins. Remote actor changes therefore
  do not enter another actor's undo stack.
- One adapter operation is one Yjs transaction and one undo item; capture is explicitly stopped at
  operation boundaries.
- Element state is an actor-scoped multi-value register and z-order is a CRDT array. Concurrent edits
  converge under deterministic revision/actor resolution. Undoing a local edit removes/restores only
  that actor's slot, preserving both a remote version of the same element and unrelated remote work.
- Restore is not undo. Restore must create/swap a new generation through the later control-plane gate.

## 4. Fail-closed boundary

Current hard limits are 2,000 elements, 256 files, depth 20, 16 MiB canonical JSON, 4 MiB raw update,
128-character identifiers and 12 MiB for any single JSON string. A remote update is first applied to
a probe `Y.Doc`; exact scope and canonical scene validation must pass before it reaches the live doc.

Errors expose a bounded code only: authority initialization/scope errors, invalid/corrupt/schema,
duplicate/unsupported/element/file limits, depth/document size, storage corruption and corrupt/update
size. Payload content is not included in the error string or logs.

## 5. Evidence at this checkpoint

- Scene ↔ canonical round-trip PASS for shape, text/container binding, arrow binding, image/file and
  page background/id/name.
- Deterministic semantic hash PASS, including z-order sensitivity.
- Two `Y.Doc` concurrent edits and same-element conflict PASS; actor-local undo/redo preserves remote
  work.
- Two-way offline changes, duplicate/out-of-order delivery, resync and compacted full-state restore
  converge to the same hash.
- Exact Hocuspocus provider integration PASS for bootstrap, concurrent edits, actor-local undo and
  offline/reconnect using the Excalidraw canonical adapter.
- Two real Chromium pages, each rendering its own Excalidraw instance, PASS concurrent actor changes,
  equal canonical hash/rendered count, Teacher undo preserving Student state and convergent redo.
- Projection updates are suppressed from feeding back as local transactions; internal Excalidraw
  history is cleared, Ctrl/Cmd+Z routes to canonical undo and all upstream fonts are served locally.
- Gate C full-suite regression exposed a concurrent projection race. The adapter now commits only the
  actor-local delta against the last rendered baseline and holds remote projection during a local
  imperative update; two-browser convergence/undo/redo stress passes `5/5` without serializing actors.
- Unsupported, duplicate, cyclic, too-deep, over-element-limit, over-update-limit, corrupt update,
  wrong-scope and corrupt stored page inputs fail closed.

Reproduction:

```powershell
pnpm.cmd --filter @tutorhub/whiteboard-spike lint
pnpm.cmd --filter @tutorhub/whiteboard-spike typecheck
pnpm.cmd --filter @tutorhub/whiteboard-spike test
pnpm.cmd --filter @tutorhub/whiteboard-spike e2e:excalidraw
pnpm.cmd --filter @tutorhub/whiteboard-spike security:excalidraw-bundle
```

## 6. Gate B closure

Gate B is complete. The candidate remains isolated and production stays force-off. Authorization,
revoke and abuse controls subsequently passed Gate C; durable snapshot/restore is Gate D; load/
accessibility and exact self-hosted operations remain Gates E/F. These later gates must not infer PASS
from this model-layer closure.
