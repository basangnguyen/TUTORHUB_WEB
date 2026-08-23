# P5-COLLAB-16 failure, outage and provider-exit acceptance

Status: **VERIFY**

Date: 2026-08-23

## Scope and safety boundary

- Validate failure, sustained-outage, latency, credential-rotation, recovery and provider-exit behavior
  against the production control-plane, readiness, canonical-authority and artifact modules.
- Reuse the retained disposable-provider evidence from P5-COLLAB-01/07/08/09; do not claim that a new
  live 600-second outage was run for this candidate.
- Do not read `.env*.local`, connect shared staging, migrate PostgreSQL, deploy Render, rotate a live
  credential or enable the production whiteboard in this task.
- Keep the production whiteboard force-off until P5-COLLAB-17 exact staging acceptance.

## Published failure contract

| Condition                                       | New room                                                             | Existing room                                                                         | Durable-content rule                              |
| ----------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------- |
| Control authority unavailable or over timeout   | Fail closed; no grant/admission                                      | Stay bounded by the last admitted authority and move to reconnect/read-only           | Do not advance the durable checkpoint             |
| PostgreSQL/control dependency unhealthy         | Fail closed                                                          | Read-only while last-good content is safe; otherwise `off`                            | RPO is the last verified durable artifact         |
| B2 unavailable                                  | New durable work fails closed                                        | Current in-memory room may continue only inside the published dirty/checkpoint budget | Never replace or delete last-good artifact        |
| Credential revoked/rotating                     | Old credential fails after overlap; new work uses current credential | Bounded overlap only for verification/recovery                                        | Never log either credential                       |
| Recovery cannot prove scope/checksum/generation | Fail closed                                                          | Remain read-only/off                                                                  | Quarantine the artifact; no generation swap       |
| Cost cap or containment trigger                 | Force-off                                                            | Read-only/export first, then off if required                                          | Preserve PostgreSQL rows and verified B2 versions |

Private-alpha recovery objective remains RPO no worse than the last verified artifact and an
operator-driven RTO target of at most five minutes. The earlier disposable recovery measurement was
`RTO_MS=3096`; it is historical evidence, not a new measurement from this local candidate.

## Candidate changes

- Add an exact runtime test that aborts a slow control-authority exchange and proves new-room grant
  exchange fails closed.
- Exercise the production readiness coordinator through `enabled -> read_only -> off`, including
  readable existing-room behavior and write/admission rejection.
- Verify artifact binding-key overlap, old-key rejection after rotation and a provider-independent
  generation rebuild from a last-good portable artifact while preserving the semantic hash.
- Add one root runner that aggregates runtime, control/Neon/B2 outage regressions, portable client
  recovery and Core API collaboration authority tests.

## Automated evidence

Command:

```text
pnpm.cmd test:collaboration:p516
```

Result on 2026-08-23:

- Whiteboard runtime: **4 files / 20 tests PASS**.
- Control, Neon and B2 outage regression: **8/8 PASS**.
- Collaboration client portable export/reconnect: **2 files / 9 tests PASS**.
- Core API collaboration mode/grant/recovery authority: **PASS**.
- Aggregate result: **P5-COLLAB-16 local gates PASS**.

Full repository verification:

```text
pnpm.cmd verify
```

Result on 2026-08-23: **PASS in 35.9 seconds**, including formatting, generated OpenAPI drift,
security gates, lint, TypeScript checks, unit/integration tests, production builds, Storybook, client
bundle scan, Go tests and `go vet`.

Full repository verification:

```text
pnpm.cmd verify
```

Result on 2026-08-23: **PASS in 35.9 seconds**, including formatting, generated OpenAPI drift,
security gates, lint, TypeScript checks, unit/integration tests, production builds, Storybook, client
bundle scan, Go tests and `go vet`.

The aggregate includes these boundaries:

1. control-authority latency timeout and fail-closed new-room exchange;
2. exact disposable 600-second confirmation guards for control, Neon and B2 drills;
3. existing-room `read_only` fallback and terminal `off` containment;
4. last-good artifact verification, corrupt/unsafe quarantine and B2 retry recovery;
5. portable scene export/reconnect and provider-independent next-generation restore;
6. binding-key overlap/revoke behavior without exposing a credential;
7. Core API feature mode, grant and recovery authority regression.

## Provider-backed evidence retained from earlier gates

- P5-COLLAB-01 Gate F.3 retained three disposable 600-second outage drills for control authority,
  Neon and B2, plus control/B2 credential rotation and post-redeploy recovery.
- P5-COLLAB-07/08 retained immutable B2 artifact, corrupt quarantine, exact last-good restore and
  generation-fence evidence.
- P5-COLLAB-09 retained feature/quota/kill-switch gates, the private-alpha profile and owner sign-off.

These retained results are used as provider evidence because P5-COLLAB-16 changes validation only; it
does not change the schema, provider topology or credential consumers. P5-COLLAB-17 will run the exact
candidate staging gates before any enablement decision.

## Provider-exit decision and runbook

- Exit triggers: sustained dependency failure outside RTO, integrity failure, credential compromise,
  provider quota/cost risk, or inability to meet the accepted RPO.
- Primary on-call, security incident owner and cost owner: Bá Sáng. Backup on-call: Duy Mạnh.
- Immediate containment: `read_only`; escalate to `off` when safe read/export cannot be guaranteed.
- Migration path: verify the immutable portable artifact, rebuild the canonical Y.Doc outside the old
  provider, stage a server-derived next generation, then atomically swap generation/revoke authority.
- Target operator migration/recovery time for the private alpha: at most five minutes when the
  last-good artifact and replacement runtime are available.
- Rollback: keep the old generation fenced and immutable, never dual-write authorities, and return to
  the previous verified artifact only through another validated generation swap.

The owner/profile approval recorded by P5-COLLAB-01 and P5-COLLAB-09 remains applicable: one free
Render instance, no HA/multi-region, cold start accepted, hard cap `0 USD`, Object Lock disabled and
whiteboard force-off on quota risk.

## Current decision

All P5-COLLAB-16 pre-staging gates are green and the candidate is **VERIFY**. No migration, shared
staging write, provider mutation or deploy is required. To move to **DONE**, publish the exact
candidate to `origin/main`, obtain GitHub Verify/Security PASS and record those run IDs here. The next
rollout task remains blocked until that closure.
