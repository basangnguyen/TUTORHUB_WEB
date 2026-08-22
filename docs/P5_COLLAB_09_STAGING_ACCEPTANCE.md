# P5-COLLAB-09 feature/quota/operations acceptance

Status: **DONE**

Date: 2026-08-22

## Candidate delivered

- ADR-0037 defines the server-owned feature dependency, quota clamp, bounded telemetry and runtime modes.
- `classroom_whiteboards` is compiled default-off and may be deployment force-off.
- Document, connection, storage and operation quotas are tenant-scoped and capped by deployment maxima.
- Runtime mode `read_only` preserves read/list/export while rejecting mutation and clamping grants to
  `view`; mode `off` conceals public whiteboard surfaces.
- Runtime telemetry exposes fixed capabilities, outcomes, dependencies and rejection reasons only.
- Migration `000041` extends typed feature/quota constraints without enabling the feature or changing ACL.
- Retained integration tests now treat clean ledger `41 false` as latest while the historical P5-07/P5-08
  evidence remains recorded at `40 false`.

## Local verification

The candidate must pass before disposable access:

```text
pnpm --filter @tutorhub/whiteboard-runtime typecheck
pnpm --filter @tutorhub/whiteboard-runtime test
go test ./internal/modules/featurecontrol ./internal/modules/collaboration ./internal/httpapi ./internal/config ./cmd/api
pnpm api:check
pnpm lint
pnpm typecheck
```

Current local result:

- whiteboard runtime typecheck PASS; Vitest `123 passed`, `2 skipped` on 20 files;
- Go feature-control/collaboration/HTTP/config/API packages PASS;
- generated OpenAPI client drift check PASS;
- repository lint/typecheck PASS and web Vitest `456/456` PASS;
- migration safety, bounded telemetry, noisy-tenant quota and read-only/off focused tests PASS;
- full `pnpm verify` PASS, including format, security, build, Storybook, all Go tests and `go vet`.

No `.env*.local` value is read, printed or committed by the local gate.

## Disposable result — 2026-08-22

- Exact owner/runtime/collaboration-worker role and endpoint preflight PASS; credentials remained hidden.
- Forward-only migration and idempotency PASS: `37 false -> 41 false -> 41 false`; the final ledger remains
  `41 false`. No rollback was run.
- PostgreSQL integration PASS for compiled default-off, deployment force-off, concurrent document quota,
  storage quota and tenant isolation.
- Focused runtime service gates PASS: Vitest `30/30` across connection/operation quota, read-only/off policy
  and bounded telemetry.
- The first storage-quota probe found a real candidate defect: the query referenced a nonexistent
  `whiteboard_snapshots.quarantined_at` column. Both artifact admission and tenant capability/quota
  projection now conservatively count every retained snapshot, preventing retained B2 data from bypassing
  the tenant storage limit; the exact database gate and full repository verification passed after the fix.
- Task-owned fixtures were cleaned up. The disposable branch is retained for review; shared staging and
  production were not changed.

## Disposable Neon gate required for DONE

Create ignored `.env.p5-collab-09-disposable.local` with values kept secret:

```text
P5_COLLAB_09_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_09_DISPOSABLE_ONLY
DATABASE_MIGRATION_URL=<direct owner URL for the P5-COLLAB-09 disposable branch>
DATABASE_POOL_URL=<pooled tutorhub_runtime URL for the same branch>
DATABASE_COLLABORATION_URL=<direct tutorhub_collab_worker URL for the same branch>
```

The exact gate will:

1. verify all URLs point to the same disposable branch and distinct intended roles;
2. run forward migration from the clean disposable baseline (`37` through `41`) to
   `41 false`, then rerun idempotently at `41 false`, with no rollback;
3. prove compiled default-off and deployment force-off cannot be bypassed by a tenant override;
4. prove document and storage concurrent quota claims fail closed and stay tenant-isolated;
5. prove runtime connection/operation quota exhaustion for tenant A does not block tenant B;
6. prove `read_only` preserves projection/list/export but blocks lifecycle/snapshot/restore, and `off`
   conceals whiteboard routes;
7. verify final ledger remains `41 false` and clean up only task-owned fixtures.

Do not migrate shared staging, deploy or delete the disposable branch before this report is reviewed.

## Remaining closure gates

- [x] Run the disposable Neon gate above and record boolean/bounded evidence only.
- [x] Review the complete diff and local source secret scan.
- [x] Commit/push after explicit authorization; exact final candidate `983ada6` is on `origin/main`.
- [x] GitHub Verify `32586930775` and Security `32586930710` PASS.
- [x] Mark P5-COLLAB-09 DONE. Production remains force-off; no shared-staging migration/deploy was run.

The first pushed candidate exposed one retained media ACL integration expectation at ledger `40`; it was
aligned with the already-reviewed latest ledger `41`, focused Go tests and commit hooks passed, and the
final exact candidate passed all three Verify jobs plus Security.
