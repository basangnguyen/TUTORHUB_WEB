# P5-COLLAB-17 force-off staging acceptance

Status: **VERIFY**

Date: 2026-08-23

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

## Shared-staging and exact browser gate

These steps remain **NOT RUN** and require a green exact candidate plus explicit user authorization:

1. publish the exact candidate and require GitHub Verify/Security PASS;
2. report disposable Neon/B2 evidence before touching shared staging;
3. forward shared staging idempotently to `41 false`, provision exact ACL and confirm the deployment
   still carries `FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false`;
4. deploy the exact candidate without creating or admitting a collaboration runtime;
5. in physical Chrome and Edge, verify authenticated force-off/concealment, disabled UI and 503
   behavior, no provider connection or retry storm, keyboard/focus/Axe/NVDA behavior, and retained
   reconnect/recovery semantics;
6. leave no test room, grant, snapshot, export/import job or B2 artifact, and record a final read-only
   cleanup snapshot with the feature still force-off.

## Exit checklist

- [x] Local static, Core API, web and P5-COLLAB-16 regression gates PASS.
- [x] Disposable runner validation and secret-safe boundaries PASS.
- [ ] Exact candidate GitHub Verify/Security PASS.
- [ ] Disposable migration/ACL/database/B2 gates PASS at final `41 false`.
- [ ] Shared staging is forwarded only after the disposable report and remains deployment force-off.
- [ ] Exact physical Chrome/Edge authorization/convergence/recovery/accessibility matrix PASS.
- [ ] Post-test cleanup snapshot PASS with no provider/test residue.

## Current decision

P5-COLLAB-17 is **VERIFY**. The local candidate is green and keeps classroom whiteboards force-off.
No disposable provider gate, shared-staging migration, deploy or exact staging browser acceptance has
been claimed in this checkpoint.
