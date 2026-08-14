# P4-07 — Host/co-host/TA moderation acceptance

Status: `VERIFY`

Date started: 2026-08-13

This ledger is the source of truth for moving P4-07 through `IN PROGRESS -> VERIFY -> DONE`.
Classroom media remains deployment-force-off throughout implementation. No shared-staging migration,
rollout or deploy is authorized by this document.

## 1. Locked contract

- PostgreSQL/Core API owns lock, effective room role and participant lifecycle authority.
- The browser sends only an opaque `participant_key`; provider and internal participant identifiers
  never cross the API boundary.
- Dynamic co-host assignments are scoped to one active RoomInstance and never update class or
  organization roles.
- Lock blocks new admission/join paths without ejecting active participants.
- Remote microphone mute is supported; remote unmute is deliberately absent.
- Remove is terminal for the current ParticipantSession and preserves the explicit restore barrier.
- Provider operations run after the database transaction and expose an explicit bounded reconcile
  state. A retryable provider failure must never be described as provider success.
- Safety-admin recovery requires a bounded reason and audit; it does not grant normal room access.

## 2. Local implementation gates

- [x] ADR-0030 and OpenAPI describe the exact moderation and provider-effect boundary.
- [x] Forward migration `000033_media_moderation_commands` has safe forward/down static tests,
      RoomInstance-scoped co-host assignments, durable provider-effect receipts and PUBLIC zero ACL.
- [x] Exact official/member-owned host, co-host, TA, attendee and safety-admin matrix is table-tested.
- [x] Core API reauthorizes exact tenant/source/room/target and enforces expected versions plus stable
      idempotency for lock/unlock, promote/demote, mute and remove.
- [x] The LiveKit adapter exposes mute-only and remove operations; it has no remote-unmute entry point.
- [x] Signal/lobby/credential/moderation use one effective room-role resolver.
- [x] UI renders only server-projected operations and has loading, forbidden, stale, provider-pending,
      reconcile-required and terminal states without optimistic provider success.
- [x] Keyboard, focus return, destructive confirmation, 320 CSS px, 200% zoom, reduced motion,
      forced colors and Axe tests pass.
- [x] Focused Go/TypeScript/Playwright tests and full local `pnpm verify` pass.
- [x] Diff/privacy/security scan finds no secret, provider identity, raw error, media/chat content or
      direct browser-to-provider moderation call.

### Local evidence — 2026-08-13

- ADR-0030, OpenAPI, generated client, Core API, PostgreSQL repositories and web UI implement one
  exact contract for lock/unlock, instance co-host promotion/demotion, remote mute, remove and end.
- Provider effects commit durable receipts before transport convergence. A bounded runtime reconciler
  claims work with `FOR UPDATE SKIP LOCKED`, lease/CAS and bounded retry; the original actor remains
  immutable audit metadata and is not required to stay active for recovery.
- PostgreSQL cross-instance actor/room/operation-family rate limits fail closed and return exact
  `429`/`Retry-After`. Read-only idempotent replay is evaluated before consuming quota.
- End-room transport failure returns only the typed `503` committed-business projection; browser
  reconciliation accepts it only when status, space and exact fields match. Generic `503` remains an
  error. Migration down is guarded with SQLSTATE `55000` while active dynamic co-hosts or unresolved
  required effects exist.
- Focused Go packages PASS; PostgreSQL integration-tag compile and vet PASS. API client tests PASS
  `7/7` files, `52/52` tests; web tests PASS `66/66` files, `424/424` tests. Deterministic P4-07
  Playwright/Axe fixture PASS `4/4` before the final backend-only security hardening; the affected
  end-convergence client path has dedicated unit coverage afterward.
- Full `pnpm verify` PASS with lint, typecheck, tests, production builds, Storybook, client-bundle and
  GitHub Actions security checks, all Go tests and vet. `go vet -tags=integration`, `api:check`,
  `format:check` and `git diff --check` also PASS.
- The isolated LiveKit harness PASS against the authorized test resource for real microphone mute,
  participant removal, room deletion and idempotent NotFound replay. Retryable failure and durable
  reconciliation remain proven by deterministic PostgreSQL/fake-provider integration; no real
  provider outage was induced.
- The ignored local environment file was loaded through an exact key allowlist in each test process;
  no secret value was displayed or logged. Neon disposable and isolated LiveKit were contacted while
  shared staging and deployment remained untouched. Both media capabilities remain deployment-force-off.

## 3. Disposable database/provider gates

These gates ran under explicit user authorization with secret-safe loading from an ignored local file.

- [x] Owner preflight confirms the disposable branch, ledger `32 false`, distinct direct owner and
      pooled runtime endpoints, and both media features effective false.
- [x] Forward-only `32 false -> 33 false -> 33 false`; no rollback.
- [x] Exact runtime/PUBLIC ACL and dependency ACL pass after reprovisioning.
- [x] Lock/join, lock/admit, role/credential, remove/rejoin and end/credential races fail closed.
- [x] Idempotent replay, conflicting fingerprint, tenant concealment and audit/outbox exact-once pass.
- [x] Isolated LiveKit mute/remove/delete success and idempotent NotFound replay pass with no secret or
      raw provider value in output. Retryable failure and durable reconcile pass through deterministic
      adapter/PostgreSQL gates; no real provider outage is inferred.
- [x] Final disposable snapshot stays `33 false`, features effective force-off and no unsafe unresolved
      provider effect or unexpected side effect.

### Disposable evidence — 2026-08-13

- Secret-safe owner preflight PASS with three distinct principals on one Neon disposable database.
  Forward migration and idempotent rerun finish at `32 false -> 33 false -> 33 false`; no rollback was
  executed and the disposable branch remains retained.
- Exact Core API runtime, PUBLIC, maintenance and dependency ACL provisioning PASS. The full media
  PostgreSQL regression command passes all `9/9` programs, including P4-07 authority/concurrency plus
  retained P4-02/P4-04/P4-06 lifecycle, lobby, credential/webhook and signal gates.
- PostgreSQL proves official/member-owned authority, tenant concealment, bounded rate limit,
  idempotent/conflicting replay, exact audit/outbox and fail-closed lock/join, lock/admit,
  role/credential, remove/rejoin and end/credential races. The durable-effect single-winner probe is
  isolated from the intentionally retained global fixture queue with transactional row locks.
- Read-only postflight PASS at `33 false`: exact ACL remains intact, deployment guardrails make both
  media features effective false, unsafe unresolved effects are `0`. It reports retained audit
  evidence without deleting it: `14` enabled test overrides, `6` role assignments, `45` moderation
  receipts, `4` synthetic `end/pending` fixture receipts and `21` moderation rate rows.
- The isolated LiveKit resource gate PASS for connected microphone publish, server mute and replay,
  participant remove and replay, plus room delete and replay. Retryable/outage behavior remains
  deterministic evidence here and the sustained real-provider outage drill stays deferred to P4-11.
- Shared staging, candidate push, CI and deployment were not run in this disposable stage.

### Candidate hardening evidence — 2026-08-14

- Full `pnpm verify` PASS again after the shared-runner hardening. Integration-tag compile/vet and
  `git diff --check` also PASS.
- The exact P4-07 Playwright/Axe file PASS `4/4`; the earlier apparent timeout was the Windows sandbox
  blocking Playwright's Vite teardown, not a fixture, UI or Axe failure.
- A separate P4-07 shared owner-preflight/ACL/final-snapshot harness now requires fresh action-scoped
  confirmations, rejects disposable/stale confirmations and keeps preflight/final outside the ACL
  provisioner read-only. Static security tests and integration-tag compile PASS; the harness has not
  been executed and shared staging remains untouched.
- Final candidate review closed two client fail-closed gaps: provider-backed participant operations
  now reject a `none` effect instead of announcing provider success, and remote mute rotates its
  idempotency key only after confirmed `applied` so a later self-unmute can be muted again. Focused
  regression tests PASS `19/19`; pending/retryable/permanent or uncertain outcomes retain the key.

## 4. Shared staging and live gates

Run only after the disposable report is accepted and the user explicitly authorizes forwarding.

- [ ] Candidate is committed/pushed to `main`; GitHub Verify and Security pass.
- [ ] Shared owner preflight, forward-only migration, exact ACL provision and read-only postflight pass.
- [ ] Render and Cloudflare deploy the exact verified commit.
- [ ] Public/anonymous privacy, authenticated feature-off, accessibility and no-side-effect acceptance
      pass without temporarily enabling classroom media.
- [ ] PROJECT_STATE, Phase 4 checklist and this ledger record exact evidence and P4-07 becomes `DONE`.

## 5. Deferred gates

Physical browser/device coverage, 25/50 participant provider load, sustained outage and optional
effects remain `UNVERIFIED — P4-11`; P4-07 must not infer them from deterministic tests.
