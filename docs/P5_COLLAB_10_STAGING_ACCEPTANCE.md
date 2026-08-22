# P5-COLLAB-10 authorization and tenant-isolation acceptance

Status: **VERIFY**

Date: 2026-08-23

## Candidate scope

- Current server authority drives the owner/teacher/teaching-assistant/student/guest projection and every
  lifecycle decision; removed or inactive memberships fail before repository access.
- Unknown or mixed forged organization roles fail closed instead of silently collapsing to attendee.
- Snapshot pagination now uses a bounded opaque cursor scoped to tenant, actor, document, current
  generation and page limit. A cursor cannot be replayed across any of those boundaries.
- PostgreSQL snapshot reads retain exact `tenant_id + document_id` predicates and use deterministic
  `(created_at, id)` keyset pagination.
- Existing strict HTTP request decoding continues to reject caller-supplied provider document names,
  object keys and other unknown authority fields.
- P5-COLLAB-10 adds no schema migration. Its separately authorized migration gate may apply only the
  retained forward migrations from clean `37 false` to `41 false`; rollback is never run.

## Local verification

The local candidate covers:

1. organization admin, owner, teacher, teaching assistant, student, guest, removed and inactive matrix;
2. explicit `view/edit/present` projection plus lifecycle allow/conceal behavior;
3. forged role rejection before repository access;
4. tenant/principal/document/generation/limit cursor binding and malformed cursor rejection;
5. HTTP cursor forwarding and response contract;
6. OpenAPI generated-client drift, integration-tag compile and repository-wide verification.

Commands:

```text
go test ./services/core-api/internal/modules/collaboration ./services/core-api/internal/httpapi
go test -tags=integration -run '^$' ./services/core-api/internal/modules/collaboration
node --test scripts/run-p510-disposable.test.mjs
pnpm api:check
pnpm verify
```

No `.env*.local` value is read, printed or committed by the local gate.

Final local result after disposable acceptance (2026-08-23): all commands above PASS. The repository-wide `pnpm verify` completed
with format/OpenAPI drift/security `52/52`/lint/typecheck/web `456/456`/build/Storybook/Go test/vet green;
the Go cache was kept under the workspace. Final diff check is clean and the source scan found no real
credential (only synthetic runner fixtures and documented placeholders).

First authorized disposable preflight (2026-08-23): exact owner/runtime role and same-branch checks PASS,
then the runner stopped safely after observing ledger `37 false` instead of required `41 false`. No
migration, rollback, fixture write or database gate ran. A clean `41 false` disposable branch is still
required.

Authorized follow-up (2026-08-23): retained forward-only migrations PASS
`37 false -> 41 false -> 41 false`. The exact PostgreSQL authorization/tenant-isolation aggregate then
PASS at `41 false`, including scoped pagination, cross-tenant document/snapshot/export/restore concealment,
forged/inactive authority rejection, tenant/principal idempotency isolation and fixture cleanup. A typed
snapshot seed parameter issue (`42P08`) was found by the gate and fixed before the final green rerun. No
rollback or shared-staging write ran; the disposable branch remains available for review.

## Disposable Neon gate required for DONE

Create ignored `.env.p5-collab-10-disposable.local` with values kept secret:

```text
P5_COLLAB_10_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_10_DISPOSABLE_ONLY
DATABASE_MIGRATION_URL=<direct neondb_owner URL for one disposable branch>
DATABASE_POOL_URL=<pooled tutorhub_runtime URL for the same branch and database>
```

The aggregate authorization gate requires clean ledger `41 false`. If the branch is still at clean
`37 false`, run the retained forward-only migration gate first under separate explicit authorization.
P5-COLLAB-10 does not need a collaboration-worker credential, Backblaze B2 credential or a new schema
migration.

Run only after explicit disposable-access authorization:

```text
pnpm test:integration:collaboration:p510
```

The exact gate will:

1. authenticate as exact owner and runtime roles without displaying either URL;
2. prove both roles point to the same disposable database and confirm ledger `41 false`;
3. exercise scoped snapshot pagination through the real runtime repository;
4. conceal cross-tenant document, snapshot, export and restore attempts;
5. reject forged roles and inactive membership;
6. prove the same opaque idempotency key remains isolated across tenants and principals while a changed
   replay for the same principal conflicts;
7. clean up only P5-COLLAB-10 fixtures and leave the disposable branch available for review.

## Remaining closure gates

- [x] Run the exact disposable Neon gate and record bounded PASS/FAIL evidence only.
- [x] Run final diff/no-secret review and full `pnpm verify` on the local candidate.
- [ ] Commit/push only after explicit authorization and verify GitHub Verify/Security.
- [ ] Mark P5-COLLAB-10 `DONE`; do not migrate shared staging or deploy for this test slice.
