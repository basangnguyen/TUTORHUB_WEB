# P5-COLLAB-17 force-off staging acceptance

Status: **DONE**

Date: 2026-08-23

Closure: 2026-08-24

## Scope and safety boundary

- Prove that classroom whiteboards remain deployment force-off across configuration, feature
  evaluation, authenticated HTTP routes, web fallback and the retained collaboration recovery path.
- Prepare an exact disposable Neon/B2 runner for the candidate before any shared-staging write.
- Do not read or print `.env*.local` values. The runner reports only gate state and migration ledger.
- Do not rollback a disposable database. Shared staging may be forwarded only after the disposable
  report is green and the user explicitly authorizes that step.
- Keep `FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false` in every deployment covered by this
  acceptance. P5-COLLAB-17 validates force-off behavior; it does not authorize a canary or enablement.

## Candidate changes

- Add a static guard that verifies the environment example, Core API configuration default,
  deployment guardrail, feature catalog default, disabled web state and HTTP 503 fail-closed mapping.
- Prove a tenant override cannot bypass the deployment-level whiteboard force-off.
- Exercise all fourteen authenticated whiteboard lifecycle, grant, snapshot, import/export and
  restore routes while the service is absent. Each route must return the private
  `whiteboard_unavailable` problem after authentication, without tenant/document disclosure.
- Add one local aggregate runner covering the exact force-off gates plus the P5-COLLAB-16
  failure/outage/provider-exit regression.
- Add a secret-safe disposable runner that validates four same-branch PostgreSQL roles and a scoped
  disposable B2 bucket before migration, exact ACL and provider tests.

## Local automated evidence

Commands:

```text
pnpm.cmd test:collaboration:p517
node --test scripts/run-p517-disposable.test.mjs
pnpm.cmd verify
```

Result on 2026-08-23:

- Static deployment force-off guard: **PASS**.
- Core API `cmd/api`, `internal/httpapi` and `featurecontrol` packages: **PASS**.
- Web `ClassroomWhiteboardTool` force-off/error contract: **1 file / 6 tests PASS**.
- P5-COLLAB-16 regression: runtime **20/20**, outage guards **8/8**, collaboration client **9/9**
  and Core API collaboration authority **PASS**.
- Disposable environment validator: **3/3 PASS**, including missing confirmation and cross-branch or
  duplicate-role rejection.
- Full repository verification: **PASS**. The initial Go phase could not write the default Windows
  build cache inside the sandbox; the same complete `go test ./services/core-api/...` and
  `go vet ./services/core-api/...` gates passed with a writable temporary `GOCACHE`.

The earlier P5-COLLAB-15 physical Chrome/Edge + NVDA matrix remains historical evidence. A
supplemental local Edge headless constrained-mode run observed an existing forced-colors Axe
`color-contrast` finding and is not counted as a P5-COLLAB-17 pass. The required P5-COLLAB-17 exact
physical evidence will be collected against the deployed force-off staging candidate.

## Disposable Neon/B2 gate

The ignored local file is `.env.p5-collab-17-disposable.local`. It must contain:

```text
DATABASE_MIGRATION_URL=
DATABASE_POOL_URL=
DATABASE_COLLABORATION_URL=
DATABASE_POLL_MAINTENANCE_URL=
B2_ENDPOINT=
B2_REGION=
B2_BUCKET=
B2_KEY_ID=
B2_APPLICATION_KEY=
P5_COLLAB_17_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_17_DISPOSABLE_ONLY
```

Requirements:

1. all four PostgreSQL URLs target one disposable Neon branch and four exact roles;
2. the B2 key is scoped to one disposable private bucket;
3. migration starts at clean `37 false` or already-current `41 false`;
4. forward migration is run twice and must finish `41 false -> 41 false` idempotently;
5. exact authorization/tenant, artifact-worker ACL, control-plane PostgreSQL and B2 artifact gates
   pass; final ledger remains `41 false`;
6. no rollback and no shared-staging connection occurs in this gate.

Commands, in order:

```text
node scripts/run-p517-disposable.mjs .env.p5-collab-17-disposable.local preflight
node scripts/run-p517-disposable.mjs .env.p5-collab-17-disposable.local migration
node scripts/run-p517-disposable.mjs .env.p5-collab-17-disposable.local all
```

## Disposable Neon/B2 result

The exact candidate `637e8b509352d2f9481e099212d384123940e839` passed GitHub Verify run
`32649917938` and Security run `32649917980`. The disposable gate was completed before any
shared-staging write:

- migration finished at `41 false` and a second run preserved `41 false` idempotently;
- exact runtime, collaboration-worker and maintenance ACL gates passed;
- authorization/tenant and database integration gates passed;
- isolated B2 artifact/recovery gates passed with `RTO_MS=1853` and
  `RPO=last_verified_artifact`;
- no rollback was run.

## Shared-staging, deploy and browser result

After the disposable report and explicit authorization:

1. the `tutorhub_collab_worker` credential was provisioned and synchronized without printing or
   logging its value;
2. shared staging advanced `37 false -> 41 false -> 41 false`; the second forward was idempotent and
   exact ACL/read-only audits passed;
3. Render service `srv-d9c1tmmrnols73dkl5g0` deployed exact candidate
   `637e8b509352d2f9481e099212d384123940e839` as deploy `dep-da5igfgu01pc73fca160`;
4. Render logs confirmed `whiteboard control plane is deployment-force-off`; no collaboration
   runtime was admitted and the deployment retained
   `FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false`;
5. direct Render `/health`, `/ready` and `/api/v1/status`, plus the Pages proxy equivalents, returned
   `200` with the expected no-store behavior;
6. an authenticated Teacher session showed no whiteboard tool or entry point in the active classroom;
   the browser produced no whiteboard provider connection, reconnect or retry activity;
7. unauthenticated whiteboard probes failed at authentication with `401` and privacy headers
   (`no-store`, `no-cache`, `no-referrer`, `nosniff`). The private authenticated `503
   whiteboard_unavailable` mapping for all fourteen routes is covered by the exact Core API tests;
   direct authenticated API navigation was not counted because the browser client blocked it;
8. the completed P5-COLLAB-15 physical Chrome/Edge + NVDA matrix remains the regression evidence for
   named controls, semantic fallback, keyboard/focus, reconnect/failure announcements, forced colors,
   reduced motion and 200% zoom;
9. the final read-only cleanup snapshot returned `41 false`, zero whiteboard documents, zero
   whiteboard relation rows, zero snapshot bindings and zero `wb/` B2 objects.

## Exit checklist

- [x] Local static, Core API, web and P5-COLLAB-16 regression gates PASS.
- [x] Disposable runner validation and secret-safe boundaries PASS.
- [x] Exact candidate GitHub Verify/Security PASS.
- [x] Disposable migration/ACL/database/B2 gates PASS at final `41 false`.
- [x] Shared staging was forwarded only after the disposable report and remains deployment force-off.
- [x] Authenticated staging concealment and retained physical Chrome/Edge + NVDA regression PASS.
- [x] Post-test cleanup snapshot PASS with no provider/test residue.

## Current decision

P5-COLLAB-17 is **DONE**. The exact candidate, disposable providers, shared staging, force-off Render
deployment, authenticated browser concealment and final cleanup snapshot are green. Classroom
whiteboards remain deployment force-off. P5-COLLAB-18 internal canary requires a separate explicit
authorization and is not enabled by this closure.
