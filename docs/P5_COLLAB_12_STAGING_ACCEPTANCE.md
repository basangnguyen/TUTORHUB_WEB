# P5-COLLAB-12 convergence, history, undo and reconnect acceptance

Status: **DONE**

Date: 2026-08-23

## Candidate scope

- The production `CanonicalExcalidrawAuthority` is exercised through 25 deterministic offline cycles with
  three actors. Every cycle delivers concurrent updates in different orders, repeats one update, and must
  finish with the same semantic hash on every peer.
- Actor-local `Y.UndoManager` history is exercised after remote updates arrive. Undo removes only the
  selected actor's latest semantic edit, retains remote work, and redo restores only that local edit.
- The real Hocuspocus WebSocket runtime alternates two authenticated actors through ten disconnect/edit/
  reconnect cycles. Online and offline edits must be present on both clients after every reconnect.
- PostgreSQL provider-authority guard, runtime session reservation races and checkpoint compaction remain
  part of the aggregate. A duplicate authority fails closed, only one incompatible scope wins an admission
  race, and compacted state preserves its causal watermark without consuming live actor-local undo.
- No schema migration, Neon or B2 disposable credential, shared-staging write or deployment is required for
  this local deterministic test slice. Production whiteboard remains force-off.

## Local gate

Run without loading any `.env*.local` file:

```text
pnpm test:collaboration:p512
```

The aggregate must prove:

1. concurrent offline edits converge after duplicate and out-of-order delivery;
2. actor-local undo/redo never removes a remote actor's independent edit;
3. restored provider state has the exact final semantic hash and causal watermark;
4. two real WebSocket clients converge after ten alternating reconnect cycles;
5. duplicate provider authority and incompatible session scope fail closed;
6. checkpoint compaction retains convergence, tombstones and live history isolation.

Local result (2026-08-23): focused aggregate PASS `26/26` across five files. Repository-wide `pnpm verify`
also PASS with formatting, OpenAPI, security `52/52`, lint, strict typecheck, web/runtime tests and builds,
Storybook, bundle security, Go test and vet green. The first full run reached the Go step and hit only the
host sandbox's default Go-cache write denial; rerunning the same command with `GOCACHE` inside the workspace
completed successfully.

## Closure gates

- [x] Add deterministic production canonical-authority convergence/history soak.
- [x] Add real Hocuspocus alternating reconnect soak with distinct authenticated actors.
- [x] Aggregate split-brain, session-race and checkpoint invariants.
- [x] Run focused gate and full `pnpm verify`.
- [x] Complete final diff and no-secret review.
- [x] Exact candidate `0b4ee83` pushed to `origin/main`; GitHub Verify `32607940566` and Security
      `32607940492` PASS.
- [x] Mark P5-COLLAB-12 `DONE`; no shared-staging migration or deployment was performed for this test slice.
