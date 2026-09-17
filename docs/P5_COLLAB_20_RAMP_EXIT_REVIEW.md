# P5-COLLAB-20 Ramp and rollback/exit review

Status: **IN PROGRESS — PREPARATION ONLY**

Updated: 2026-09-17

## 1. Current decision

P5-COLLAB-20 has started at the local preparation gate. This checkpoint does not authorize a
provider mutation, tenant ramp, rollback, production action, or shared-staging action.

The local contract is intentionally fail-closed:

- liveActionsAuthorized=false;
- providerMutationAuthorized=false;
- production and shared staging are outside the authorization boundary;
- the next environment, candidate, tenant allowlist/count, and approval identity remain unset;
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

| Stage | Scope                                 | State     | Gate                                                        |
| ----- | ------------------------------------- | --------- | ----------------------------------------------------------- |
| R0    | Global force-off, zero tenant         | Completed | Static and provider force-off evidence                      |
| R1    | Exact-one internal tenant             | Completed | 2 documents, 10 connections, 64 MiB, 600 operations/minute  |
| R2    | Private alpha                         | Completed | P5-COLLAB-19 60-minute soak, drills, cleanup, owner closure |
| R3    | Bounded disposable private-alpha ramp | Blocked   | Separate exact authorization packet is required             |

R3 has no default tenant count. Before a live action, the authorization packet must bind:

1. exact disposable-private-alpha environment and target fingerprint;
2. full candidate SHA and hashed tenant allowlist;
3. exact tenant count and per-tenant quotas;
4. hold duration of at least 3,600 seconds;
5. named approver and timestamp;
6. provider-observed evidence locations;
7. ready live and rollback executors.

The first R3 hold point cannot exceed the already-proven per-tenant profile: 2 documents,
10 connections, 64 MiB, and 600 operations/minute. ADR-0037 absolute maxima are ceilings, not
authorization to raise the initial ramp.

## 5. Kill-switch and rollback policy

The allowed degradation path is enabled -> read_only -> off. Manual force-off remains available.
The local pure evaluator produces a deterministic automatic decision:

| Decision  | Trigger                                                                                                                                                                      |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| off       | one-authority violation; portability/recovery failure; cross-tenant leak; divergence; data loss; security/privacy incident; any unbilled charge; expired read-only recovery  |
| read_only | readiness fails twice; checkpoint persistence fails; quota rejection rises continuously; accepted free cap reaches 75%; accessibility regression; provider-exit review fails |
| enabled   | all required observations are healthy                                                                                                                                        |

This evaluator is policy evidence only. It is not a live provider executor. P5-COLLAB-20 cannot
enter R3 until the executor can apply the decision and the rollback path has been tested on the
exact authorized disposable target.

Rollback completion must prove whiteboard off, runtime not ready, zero active document/edit
connection, portable last-good artifact readability, and zero synthetic database/B2 residue.

## 6. Exit-review matrix

| Review        | Current state           | Required live evidence                                                   |
| ------------- | ----------------------- | ------------------------------------------------------------------------ |
| License       | Pending live validation | Pins/notices unchanged and no incompatible dependency drift              |
| Runtime       | Pending live validation | Exact candidate/deploy binding, readiness, latency, convergence, cleanup |
| Cost          | Pending live validation | 0 USD, free-cap usage below 75%, no automatic upgrade/autoscale          |
| Security      | Pending live validation | tenant isolation, grant/revoke, privacy-safe logs, secret scan           |
| Accessibility | Pending live validation | retained matrix remains applicable or fresh regression is attached       |
| Provider exit | Pending live validation | portable export/readback, restore, force-off and recovery remain valid   |

No review can be marked PASS from conversation history alone. Reused evidence requires an explicit
validity rationale; changed code/topology/format/security semantics require fresh evidence.

## 7. Automated preparation evidence

Implemented:

- scripts/p520-ramp-exit-contract.mjs: fail-closed preparation/authorization validator and automatic
  hold-point evaluator;
- scripts/p520-ramp-exit-contract.test.mjs: preparation, authorization, scope, quota, automatic
  decision and secret-rejection tests;
- scripts/run-p520-local.mjs: local-only aggregate runner.

Run: pnpm test:collaboration:p520

Current result: PASS, including 7/7 P5-COLLAB-20 contract tests plus inherited force-off and
bounded-canary static guards.

## 8. Remaining gates

- [ ] Receive a separate exact authorization for R3 on a disposable private-alpha target.
- [ ] Bind exact candidate, environment fingerprint, hashed allowlist, tenant count and quotas.
- [ ] Implement and test the live decision executor and rollback executor without logging secrets.
- [ ] Run the provider-observed hold and capture fresh license/runtime/cost/security/a11y/exit review.
- [ ] Execute rollback/cleanup and confirm final whiteboard force-off plus zero residue.
- [ ] Record exact completion candidate, supported profile, residual risks and deferred work.

Until every item passes, P5-COLLAB-20 remains IN PROGRESS and Phase 5 is not closed.
