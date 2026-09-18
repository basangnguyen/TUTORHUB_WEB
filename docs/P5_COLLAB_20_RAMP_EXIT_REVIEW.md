# P5-COLLAB-20 Ramp and rollback/exit review

Status: **VERIFY — LIVE RUNTIME LATENCY GATE FAILED; FORCE-OFF/CLEANUP PASS**

Updated: 2026-09-18

## 1. Current decision

The owner authorized the corrected two-stage R3 workflow for exact candidate
`4412bbdad87e425b5cdba00a05abcd8cf6b30008` on the inherited disposable target and existing
exact-two tenant manifest. Exact-target deploy/adoption, initial `off`, provider preflight, two
provider-observed 3,600-second holds, the full drill matrix and mandatory rollback/cleanup were
executed. Both holds failed only the runtime artifact latency gate, so no completed six-review
packet was issued and the task cannot move to `DONE`.

The corrected local contract is intentionally fail-closed:

- preparation packets keep liveActionsAuthorized=false and providerMutationAuthorized=false;
- an owner-approved packet uses `authorized-pending-live-validation` and remains exact-target only;
- `DONE` is impossible until all six fresh live reviews are `passed` with evidence references;
- production and shared staging are outside the authorization boundary;
- the live target environment and approval identity remain unset;
- the private preparation packet binds the proposed candidate, inherited target fingerprint and
  exact-two tenant allowlist hash/count without storing raw tenant UUIDs;
- the whiteboard starts and remains off;
- a live ramp is ineligible until both the live executor and rollback executor are ready.

## 2. Verified inherited baseline

The starting baseline is the closed P5-COLLAB-19 provider run:

| Item                | Verified baseline                                                          |
| ------------------- | -------------------------------------------------------------------------- |
| Task                | P5-COLLAB-19 — DONE                                                        |
| Provider candidate  | 22ebfe1bfecc95d782ee35f4a8049c32f25fdc50                                   |
| Control deploy      | dep-dal159ek1f9s73db1bcg                                                   |
| Runtime deploy      | dep-dal15k6k1f9s73db2730                                                   |
| Final ledger        | 42 false                                                                   |
| Whiteboard          | off; runtime not ready; 0 document; 0 edit connection                      |
| Soak/drills/cleanup | Provider-observed 60 minutes, full drill matrix, final zero residue — PASS |

The local preparation runner rechecks only the inherited static force-off and bounded-canary
guardrails. It does not repeat the already-passed P5-COLLAB-19 live soak or drill matrix because
there is no provider or product-semantic drift at this checkpoint.

## 3. Immutable architecture and supported profile

The ramp must preserve ADR-0034 and ADR-0037:

- Excalidraw 0.18.1 is the editor projection;
- Yjs 13.6.27 is the sole document/history/undo authority;
- Hocuspocus 4.6.0 is the collaboration transport;
- PostgreSQL remains control-plane metadata, not an operation/history writer;
- B2 remains immutable artifact storage, not an operation/history writer;
- supported profile remains FREE_PRIVATE_ALPHA: one Render Free instance in Singapore, no Redis,
  no HA, no autoscaling, hard cost cap 0 USD.

Paid multi-instance production topology remains deferred and has not been provisioned.

## 4. Ramp ladder and hold points

| Stage | Scope                                   | State     | Gate                                                        |
| ----- | --------------------------------------- | --------- | ----------------------------------------------------------- |
| R0    | Global force-off, zero tenant           | Completed | Static and provider force-off evidence                      |
| R1    | Exact-one internal tenant               | Completed | 2 documents, 10 connections, 64 MiB, 600 operations/minute  |
| R2    | Private alpha                           | Completed | P5-COLLAB-19 60-minute soak, drills, cleanup, owner closure |
| R3    | Exact-two disposable private-alpha ramp | Blocked   | Corrected candidate needs a new exact-SHA authorization     |

The first R3 hold is fixed at exactly two tenants: the smallest tenant-count increase after the
exact-one internal canary. The actual tenant UUIDs are confined to an ignored private manifest.
Before a live action, the authorization packet must bind:

1. exact disposable-private-alpha environment and target fingerprint;
2. full candidate SHA and hashed tenant allowlist;
3. exact tenant count and per-tenant quotas;
4. hold duration of at least 3,600 seconds;
5. named approver and timestamp;
6. two-stage review state: pending during the authorized live window, then six fresh evidence locations before completion;
7. ready live and rollback executors.

The first R3 hold point cannot exceed the already-proven per-tenant profile: 2 documents,
10 connections, 64 MiB, and 600 operations/minute. ADR-0037 absolute maxima are ceilings, not
authorization to raise the initial ramp.

Core API supports this path only when FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARD_RAMP is explicitly
true together with the global whiteboard flag and exactly two canonical tenant UUIDs. The ramp flag
defaults to false; one or three tenants, ramp without whiteboard enable, and all invalid UUID lists
fail configuration validation. Both tenants receive the same low-quota profile. The P5-COLLAB-18
exact-one path remains unchanged.

## 5. Kill-switch and rollback policy

The allowed degradation path is enabled -> read_only -> off. Manual force-off remains available.
The local pure evaluator produces a deterministic automatic decision:

| Decision  | Trigger                                                                                                                                                                      |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| off       | one-authority violation; portability/recovery failure; cross-tenant leak; divergence; data loss; security/privacy incident; any unbilled charge; expired read-only recovery  |
| read_only | readiness fails twice; checkpoint persistence fails; quota rejection rises continuously; accepted free cap reaches 75%; accessibility regression; provider-exit review fails |
| enabled   | all required observations are healthy                                                                                                                                        |

The evaluator is now wired into a fail-closed executor core. The core validates an authorized
packet, exact candidate SHA, target fingerprint and private exact-two manifest binding before an
adapter can be called. It deploys in `off`, applies only the evaluated mode, emits a redacted receipt
and makes rollback traverse `read_only -> off`. The allowlisted Render adapter is implemented and
mock-tested, but exact-target live proof remains pending, so P5-COLLAB-20 cannot enter R3 yet.
The live runner provisions one provider document per tenant and performs a negative cross-tenant
grant probe before workload execution. The control plane now derives an exact tenant-to-document
mapping from the sorted allowlist and rejects `tenant_document_mismatch`; the previously authorized
`b278bab` candidate lacked this binding and was never deployed.

Rollback completion must prove whiteboard off, runtime not ready, zero active document/edit
connection, portable last-good artifact readability, and zero synthetic database/B2 residue.

## 6. Exit-review matrix

| Review        | Current state                  | Required live evidence                                                     |
| ------------- | ------------------------------ | -------------------------------------------------------------------------- |
| License       | Evidence PASS; packet withheld | Pins unchanged at the exact candidate; dependency/lock scope did not drift |
| Runtime       | **FAIL**                       | Both holds exceeded artifact p95 2,500 ms; all other runtime gates passed  |
| Cost          | Evidence PASS; packet withheld | 0 USD, no optional burst/autoscale                                         |
| Security      | Evidence PASS; packet withheld | exact-two isolation, grant/revoke and drills passed                        |
| Accessibility | Evidence PASS; packet withheld | collaboration UI scope unchanged; accessibility notice retained            |
| Provider exit | Evidence PASS; packet withheld | export/readback, restore, force-off, recovery and cleanup passed           |

No review can be marked PASS from conversation history alone. Reused evidence requires an explicit
validity rationale; changed code/topology/format/security semantics require fresh evidence.

## 7. Automated preparation evidence

Implemented:

- scripts/p520-ramp-exit-contract.mjs: fail-closed preparation/authorization validator and automatic
  hold-point evaluator;
- scripts/p520-ramp-exit-contract.test.mjs: preparation, authorization, scope, quota, automatic
  decision and secret-rejection tests;
- scripts/p520-authorization-packet.mjs: generates a preparation-only packet from the current
  commit without credentials or live authorization; its materializer imports only allowlisted
  SHA/deploy-ID fields from the P5-COLLAB-19 binding, confines output below tmp/p5-collab-20 and
  refuses to overwrite an existing packet;
- scripts/p520-tenant-allowlist.mjs: binds an exact-two canonical UUID manifest to a new
  preparation packet using only an order-independent SHA-256 and tenant count; raw tenant UUIDs
  remain only in the ignored private manifest, output is confined below tmp/p5-collab-20, existing
  output is never replaced, and live/execute flags are rejected;
- scripts/p520-ramp-dry-run.mjs: reads only bounded JSON inputs below tmp/p5-collab-20, validates
  their hashes and emits a redacted decision receipt; live/execute flags are rejected;
- scripts/p520-ramp-dry-run.test.mjs: packet, path confinement, redaction, secret rejection,
  healthy/degraded/off decision and live-flag tests;
- scripts/p520-ramp-executor.mjs: provider-independent live-decision and rollback executor core with
  exact packet/candidate/fingerprint/tenant binding, initial-off deployment ordering, monotonic safe
  mode selection, redacted receipts and mandatory `read_only -> off` rollback verification;
- scripts/p520-ramp-executor.test.mjs: fake-adapter proof for no-call-before-authorization, exact
  binding, initial-off deployment, enabled/off decisions, UUID-free receipts, rollback ordering and
  failed force-off verification containment;
- scripts/p520-render-adapter.mjs: no-CLI adapter locked to the exact two P5-COLLAB-19 disposable
  Render services, inherited target fingerprint, main branch, Singapore Free profile and auto-deploy
  off; it accepts only the two P5-COLLAB-20 control keys plus runtime build ID, deploys the exact
  candidate, applies mode through the authenticated control endpoint and verifies readiness/metrics;
- scripts/p520-render-adapter.test.mjs: fully mocked provider proof for exact service cardinality,
  target/fingerprint drift rejection, three-key environment allowlist, initial-off/exact-two tenant
  enforcement, deploy/mode verification and credential-redacted public/error surfaces;
- scripts/p520-provider-fixture.mjs: deterministic exact-two fixture mapper and transactional
  provision/verify/cleanup/destroy lifecycle with no identifier output;
- scripts/p520-live-runner.mjs: exact-confirmation two-stage packet materializer, deploy/preflight,
  negative cross-tenant probe, rollback and final ledger/zero-state cleanup orchestration;
- services/whiteboard-runtime/p520-provider-soak.mjs: exact-two wrapper around the inherited
  provider-observed 3,600-second soak/drill harness, bound to P5-COLLAB-20 private artifacts;
- scripts/p520-live-review.mjs: fail-closed finalizer that requires a fresh valid provider report,
  exact binding/preflight, unchanged dependency pins and unchanged accessibility scope before it can
  create the six evidence-bound PASS reviews; it was intentionally not run because runtime failed;
- scripts/p519-live-control.mjs: optional P5-COLLAB-20 exact-two tenant allowlist, initial-off mode and
  R3 quota profile plus exact tenant-to-document binding while preserving P5-COLLAB-19 behavior;
- scripts/check-p520-ramp-guard.mjs: static guard for default-off, exact-two config and low-quota
  server wiring while preserving the exact-one canary path;
- scripts/run-p520-local.mjs: local-only aggregate runner.

Run:

```powershell
pnpm p5-collab-20:packet
pnpm p5-collab-20:prepare-packet
pnpm p5-collab-20:bind-tenants -- --packet tmp/p5-collab-20/authorization.json --tenants tmp/p5-collab-20/tenants.json --output tmp/p5-collab-20/authorization-bound.json
pnpm p5-collab-20:dry-run -- --packet tmp/p5-collab-20/authorization.json --observation tmp/p5-collab-20/observation.json
pnpm test:collaboration:p520
```

Create `tmp/p5-collab-20/tenants.json` with a local editor so the real UUIDs do not enter terminal
history or chat/log output. The ignored manifest shape is:

```json
{
  "schemaVersion": "p5-collab-20-tenant-allowlist-v1",
  "tenantIds": ["<canonical-uuid-1>", "<canonical-uuid-2>"]
}
```

The packet command writes only to standard output. The prepare-packet command atomically creates
the ignored private tmp/p5-collab-20/authorization.json file and will not replace it. Its proposal
binds the current source commit, inherited P5-19 target fingerprint/deploy IDs, low-quota profile
and 3,600-second hold. Tenant allowlist hash/count, owner approval and fresh review evidence remain
unset until separately supplied. The tenant binder can materialize only the allowlist hash/count;
it does not set authorization, live readiness, review PASS state, or provider mutation permission.
The dry-run command cannot mutate a provider.

Current local result: PASS, including 57/57 P5-COLLAB-20 contract/dry-run/binder/executor/control/
adapter/fixture/live-runner/finalizer tests,
targeted Core API config/guardrail tests, plus inherited force-off and bounded-canary static guards.
P5-COLLAB-19 regression also remains PASS after the generic fixture parameterization.

Exact-two binding checkpoint on 2026-09-17:

- the P5-COLLAB-19 disposable target and clean `42 false` ledger were revalidated before mutation;
- exactly two dedicated synthetic tenants, users, active org-admin memberships and active
  `classroom_whiteboards` private-alpha enrollments were created transactionally;
- zero whiteboard documents were created and no Render/B2/provider action was executed;
- `tmp/p5-collab-20/tenants.json` and the bound preparation packet are ignored private artifacts;
- postflight reports exactly two active enrollments, a valid hash/count binding,
  `liveRampAllowed=false` and `providerMutationAuthorized=false`, without logging UUIDs or secrets.

### Live R3 validation checkpoint on 2026-09-18

- Exact candidate `4412bbdad87e425b5cdba00a05abcd8cf6b30008` was bound to the approved
  disposable fingerprint and exact-two private manifest. The exact Control and Runtime candidate
  was deployed once; later rolling redeploy recovery detected the singleton authority collision and
  safely adopted the already-live exact candidate instead of expanding capacity or cost.
- Initial mode `off`, evaluator-selected enablement, exact-two negative isolation preflight and
  provider readiness all passed. Production and shared staging were not touched.
- Hold 1 ran the full 3,600-second provider workload and drill matrix. Join/reconnect/convergence/
  acknowledgement/artifact p95 were `1229/1474/664/441/2926 ms`. Only artifact p95 exceeded the
  `2500 ms` gate. Mandatory recovery rollback and cleanup passed.
- The output-path defect that placed the redacted summary outside the intended private tmp directory
  was fixed and regression-tested before the one authorized retry.
- Hold 2 again ran the full 3,600-second provider workload and drill matrix. Join/reconnect/
  convergence/acknowledgement/artifact p95 were `1349/1793/587/444/6480 ms`. The evaluator reported
  exactly one error category: `artifactP95Ms`. Soak cleanup verified zero database/runtime/B2
  residue.
- Final rollback traversed `read_only -> off` and verified runtime not ready with zero documents
  and edit connections. Final cleanup destroyed the synthetic fixture and verified ledger
  `42 false`, whiteboard `off`, zero synthetic tenants/documents and no residual activity.
- No threshold was relaxed, no migration was rolled back, no additional paid capacity was created,
  and no six-review completion packet was materialized. A future completion attempt requires a new
  decision after diagnosing artifact latency; the current authorized retry budget is exhausted.

### Local artifact measurement correction on 2026-09-18

- The failed runs exposed a measurement defect: the inherited harness collected only four artifact
  samples, so nearest-rank p95 was always the maximum. Each sample correctly covered the immutable
  existence check, upload and verified read-back, but one transient B2 outlier decided the gate.
- The P5-COLLAB-20 forward contract v2 now requires exactly 20 artifact create/read-back samples and
  20 separately timed restore/reuse samples. The artifact threshold remains `2500 ms`; it was not
  relaxed. Restore RTO now uses the restore/reuse path instead of the create path.
- The six-review finalizer rejects a report unless both exact sample counts are present and the
  inherited provider evaluation passes. P5-COLLAB-19 retains its historical four-sample default.
- Local gates passed: P5-COLLAB-20 `57/57`, P5-COLLAB-19 regression PASS, whiteboard runtime
  `152 passed / 2 skipped`, plus runtime lint, typecheck and build.
- This checkpoint made no provider call, did not change the live mode and did not authorize another
  hold. A new exact candidate and a new owner authorization are still required for live proof.

## 8. Remaining gates

- [x] Receive two-stage authorization for candidate `b278bab`; reject that candidate locally after
      finding the missing tenant-to-document binding, without a provider call.
- [x] Receive a new exact-SHA authorization for the corrected disposable R3 candidate.
- [x] Supply the private exact-two tenant manifest and materialize its hash/count binding.
- [x] Bind exact candidate and environment fingerprint in the separately approved live packet.
- [x] Implement and test the fail-closed live-decision/rollback executor core without logging secrets
      or tenant UUIDs in receipts.
- [x] Connect the executor to an allowlisted adapter for exactly the two disposable Render services
      and prove it against mocks before any authorized live action.
- [x] Exercise the adapter on the separately authorized exact disposable target and capture redacted
      deploy/mode verification receipts.
- [x] Run two provider-observed 3,600-second holds and the full drill matrix; both fail only the
      artifact latency gate, so the six-review completion packet remains blocked.
- [x] Execute rollback/cleanup and confirm final whiteboard force-off plus zero residue.
- [x] Diagnose the four-sample p95 defect and implement exact 20-sample create plus restore evidence.
- [ ] Obtain a new exact-candidate authorization and prove the corrected artifact gate in a fresh
      provider-observed completion attempt.
- [ ] Record exact completion candidate, supported profile, residual risks and deferred work.

Until every item passes, P5-COLLAB-20 remains `VERIFY` and Phase 5 is not closed.
