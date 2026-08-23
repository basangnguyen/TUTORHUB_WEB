# P5-COLLAB-13 snapshot, import, export and restore acceptance

Status: **VERIFY**

Date: 2026-08-23

## Candidate scope

- The production artifact envelope is exercised as the immutable export/import boundary. The gate verifies
  the exact artifact SHA-256, signed scope binding, semantic document hash and restored Yjs state.
- Corrupt JSON, unsupported format versions, oversize artifacts and signed active-content payloads must be
  quarantined. None may call the restore staging operation.
- A transient B2 read outage must produce a bounded retry. Recovery may stage only the last artifact that
  passes exact object-version, checksum, binding, scope and semantic verification.
- PostgreSQL retains the atomic generation authority. The disposable gate re-runs the concurrent restore
  generation swap and verifies that one transaction wins while stale generation writers are fenced.
- Existing P5-COLLAB-07 artifact lifecycle coverage remains authoritative for two concurrent `SKIP LOCKED`
  purge claimants, bounded retry and exact-version B2 deletion.
- This test slice does not bind the browser `import_validate` command to an end-user upload flow. Browser
  upload intent and public import UI remain deferred; this task must not be described as a completed public
  whiteboard import feature.
- No new P5-COLLAB-13 migration exists. Owner authorization on 2026-08-23 permits the isolated disposable
  branch to replay already-committed migrations from `37 false` to clean `41 false`; no rollback,
  shared-staging write or deployment is authorized, and production whiteboard remains force-off.

## Local deterministic gate

Run without loading any `.env*.local` file:

```text
pnpm test:collaboration:p513
node --test scripts/run-p513-disposable.test.mjs
```

Current result:

- P5-COLLAB-13 matrix: **7/7 PASS**.
- Production envelope/object-store/worker regression: **8/8 PASS**.
- Secret-safe disposable runner validation: **3/3 PASS**.
- Full repository `pnpm verify`: **PASS**; the whiteboard-runtime aggregate finished with **136 passed**
  and **2 skipped** tests.
- Disposable migration replay: **PASS**, `37 false -> 41 false -> 41 false`.
- Exact collaboration-worker ACL provision and PostgreSQL control-plane gates: **PASS**.
- Neon/B2 aggregate: **PASS twice consecutively** at final ledger `41 false`.
- Last-good provider recovery: **PASS**, `RPO=last_verified_artifact`; observed RTO was `2654 ms`
  and `2472 ms` across the two final aggregate runs.

The local matrix proves:

1. export bytes retain their exact SHA-256 and semantic hash through a fresh-generation restore;
2. corrupt, incompatible, oversize and malicious artifacts are quarantined with zero restore staging;
3. a B2 unavailable result is retryable and a later verified read restores the last-good semantic hash;
4. stale current-generation jobs fail before an artifact is written or published.

## Disposable environment

Create ignored file `.env.p5-collab-13-disposable.local`. It must contain the same secret-safe boundary as
P5-COLLAB-07:

```text
DATABASE_MIGRATION_URL=<owner direct URL on one disposable Neon branch>
DATABASE_POOL_URL=<tutorhub_runtime pooled URL on that branch>
DATABASE_COLLABORATION_URL=<dedicated collaboration worker direct URL on that branch>
DATABASE_POLL_MAINTENANCE_URL=<maintenance direct URL on that branch>
B2_ENDPOINT=<credential-free HTTPS S3 endpoint>
B2_REGION=<B2 region>
B2_BUCKET=<private disposable versioned bucket>
B2_KEY_ID=<bucket-scoped disposable key ID>
B2_APPLICATION_KEY=<bucket-scoped disposable application key>
P5_COLLAB_13_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_13_DISPOSABLE_ONLY
```

The runner loads only the allowlisted file values in the child process. It never prints URL credentials,
database passwords or B2 keys. Owner, runtime, worker and maintenance must be four distinct roles on one
branch/database; owner and worker/maintenance are direct endpoints while runtime is pooled.

## Disposable gates

Run in order:

```text
pnpm test:integration:collaboration:p513 -- .env.p5-collab-13-disposable.local preflight
pnpm test:integration:collaboration:p513 -- .env.p5-collab-13-disposable.local migration
pnpm test:integration:collaboration:p513 -- .env.p5-collab-13-disposable.local database
pnpm test:integration:collaboration:p513 -- .env.p5-collab-13-disposable.local provider
pnpm test:integration:collaboration:p513 -- .env.p5-collab-13-disposable.local all
```

Required evidence:

1. preflight reports the starting ledger, and the explicitly authorized disposable bootstrap reports
   `37 false -> 41 false -> 41 false` before any acceptance gate runs;
2. the PostgreSQL concurrency gate has one restore generation winner and one stale loser;
3. immutable B2 binding, corrupt quarantine, fresh-generation restore and final semantic hash pass;
4. two purge claimants use `SKIP LOCKED`, provider failure retries, last-good stays readable and exact cleanup
   removes only eligible object versions;
5. aggregate rerun leaves the migration ledger at `41 false` and leaves no disposable fixture rows/objects.

## Current decision

Local/full-repository gates and the exact Neon/B2 disposable aggregate are green. The owner-authorized
forward replay reached `41 false` without rollback; exact worker ACL provision, PostgreSQL concurrency,
immutable B2 round-trip, quarantine, purge/retry and last-good recovery all passed. A failed first rerun
exposed stale fixture cleanup; the candidate now removes only its `p502-*` disposable tenant graph in
dependency order, and two consecutive aggregate reruns passed with cleanup zero.

P5-COLLAB-13 remains in `VERIFY` only until final diff/no-secret review, explicit commit/push and GitHub
Verify/Security pass. Shared staging was not migrated, no deployment was performed, and production
whiteboard remains force-off.
