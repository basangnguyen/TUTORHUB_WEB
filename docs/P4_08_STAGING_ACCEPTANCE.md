# P4-08 — Persistent in-room chat acceptance

Status: `VERIFY`

Date started: 2026-08-14

This ledger is the source of truth for moving P4-08 through
`IN PROGRESS -> VERIFY -> DONE`. Migration and database authority are proven on a retained Neon
disposable branch before any shared-staging or deployment action.

## 1. Locked contract

- `room` is a third kind in the canonical `conversations` aggregate; `messages` and
  `message_receipts` remain the only persistent message authority.
- One MediaSpace has at most one room conversation. P4-09 recovery instances reuse that conversation
  and never fork history.
- PostgreSQL/Core API is authoritative. LiveKit DataChannel remains disabled and is not a content
  source, persistence path or success signal.
- Official room history follows current `class.view`. StudyMeeting history follows current owner or
  active explicit MediaSpace membership. Foreign/inaccessible resources are concealed.
- New writes require source permission, MediaSpace `open`, one active RoomInstance and a current
  ParticipantSession in `admitted|joining|connected|reconnecting`.
- `left|removed|failed`, source revoke, room end or cancellation blocks new writes immediately.
  Exact idempotent replay may still return the already committed message without creating a row.
- Message content is absent from audit, outbox, telemetry and application logs.

## 2. Local implementation gates

- [x] ADR-0013/0025 amended before implementation.
- [x] Forward migration `000034_persistent_room_conversations` extends the canonical aggregate with
      tenant-composite FK, partial uniqueness, exact shape and PUBLIC zero privilege.
- [x] OpenAPI, generated client, Core API and web shell use one room-conversation contract.
- [x] Write authorization locks current source, MediaSpace, RoomInstance and ParticipantSession state;
      terminal state wins a concurrent blocked send.
- [x] Existing sanitization, pagination, unread/read, quota and stable client-message idempotency are
      reused without a parallel content store.
- [x] Classroom drawer has labelled trigger/title, focus return, loading/empty/error/forbidden/retry,
      read-only projection and long-content wrapping.
- [x] Full local verify gate set, Go integration-tag compile/vet and final diff/privacy scan PASS.

### Local evidence — 2026-08-14

- Focused Go conversation/httpapi packages PASS. The P4-08 PostgreSQL integration suite compiles with
  the integration build tag.
- API generation check, web lint and typecheck PASS. All web tests PASS `67/67` files and `430/430`
  tests, including canonical room ensure, read-only history and accessible drawer coverage.
- The complete verify gate set PASS: formatting, generated contract, local/E2E infrastructure,
  GitHub Actions and client-bundle security, lint, typecheck, tests, production builds, Storybook,
  all Go tests and Go vet. Integration-tag conversation compile/vet and `git diff --check` also PASS.
- Neon disposable database gates PASS on 2026-08-15. No shared staging migration, push, deploy or
  rollback has been run for P4-08.

## 3. Disposable database gates

Required ignored local file: `.env.p4-08-disposable.local` with exactly:

- `DATABASE_MIGRATION_URL`: direct, non-pooled `neondb_owner` URL for the retained disposable branch.
- `DATABASE_POOL_URL`: pooled `tutorhub_runtime` URL for the same branch/database.

Values must be loaded by exact key allowlist inside the same PowerShell process that runs each gate.
Never print the variables, parsed URL, role, endpoint or password.

- [x] Read-only owner preflight PASS at ledger `33 dirty=false`, direct-owner/pooled-runtime boundary.
- [x] Forward-only `33 false -> 34 false -> 34 false`; no rollback.
- [x] Exact conversation runtime/PUBLIC and media dependency ACL PASS.
- [x] Canonical concurrent room creation yields one row and one allowlisted creation audit.
- [x] Official/member-owned read/write matrix, tenant concealment and source/member revoke PASS.
- [x] Participant removal versus blocked send barrier PASS; terminal actor creates no message.
- [x] Exact replay after room end returns the committed row, while a new send is read-only.
- [x] Message content is absent from audit/outbox and the final read-only snapshot remains
      `34 dirty=false`.
- [x] Disposable branch is retained after every database gate passed.

### Disposable evidence — 2026-08-15

- Owner preflight PASS at `ledger=33 dirty=false`; the direct owner and pooled runtime connections
  resolved to distinct roles on the same disposable database without exposing connection metadata.
- Migration 000034 applied forward-only and an immediate `pnpm db:migrate` rerun reported the schema
  up to date, proving `33 false -> 34 false -> 34 false` with no rollback.
- `test:integration:conversation:p408` PASS: exact conversation runtime/PUBLIC ACL, canonical
  concurrent room creation, same-tenant source authority, foreign concealment, source/member revoke,
  participant-removal write barrier, exact replay after room end and content privacy all passed.
- The Neon harness was corrected to issue one parameterized SQL command per prepared statement and
  to include the required P4-06 roster sequence. The final ACL snapshot checks the exact dependency
  columns used by the repository rather than incorrectly requiring table-level SELECT.
- Final read-only snapshot PASS at `ledger=34 dirty=false`, with `room_conversations=2` and
  `retained_enabled_media_overrides=2` from the isolated acceptance fixtures. The disposable branch
  remains retained; shared staging and deployment remain untouched.

Safe execution order after explicit authorization:

1. Load only the two URL keys and run
   `TestPostgresP408DisposableOwnerPreflight` with
   `P4_08_OWNER_PREFLIGHT=I_UNDERSTAND_P4_08_OWNER_PREFLIGHT_ONLY`.
2. Clear the preflight confirmation. Set
   `P4_08_DISPOSABLE_CONFIRM=I_UNDERSTAND_P4_08_DISPOSABLE_ONLY` and run
   `pnpm test:integration:conversation:p408`. This applies migration 000034 forward-only and runs
   exact ACL plus authority/concurrency/privacy gates.
3. Run `pnpm db:migrate` again with only the two URL keys to prove idempotency.
4. Clear the write confirmation. Run `TestPostgresP408DisposableFinalSnapshot` with
   `P4_08_FINAL_SNAPSHOT_CONFIRM=I_UNDERSTAND_P4_08_FINAL_SNAPSHOT_READ_ONLY`.

## 4. Candidate, shared staging and live gates

Run only after the disposable result is reported and accepted.

- [x] Stage reviewed candidate only; exclude `.env*.local`, `.tmp-gocache/` and generated junk.
- [x] Commit/push direct to `main`; exact GitHub Verify and Security PASS.
- [x] With separate explicit authorization, shared owner preflight, forward-only 33 -> 34,
      idempotent rerun, exact ACL and read-only postflight PASS.
- [x] Render and Cloudflare deploy the exact verified SHA.
- [x] Live feature-off/privacy/accessibility/no-side-effect acceptance PASS without temporarily
      enabling classroom media.
- [x] PROJECT_STATE, Phase 4 backlog and this ledger record exact evidence; P4-08 becomes `DONE`.

### Candidate/shared/live evidence — 2026-08-15

- Exact candidate `fd2c3fc70f7e32c252523367e8aa56e8b466b810` PASS GitHub Verify
  `31858451744` and Security `31858451822`. The reviewed stage excluded local environment and cache
  files; the two media-suite compatibility hotfixes only advanced inherited integration assertions
  to the P4-08 ledger and did not change production authority.
- Shared owner preflight PASS before mutation at `33 dirty=false`. Shared staging advanced
  forward-only through `33 false -> 34 false -> 34 false`; the immediate second migrate was
  idempotent. Exact conversation runtime/PUBLIC ACL and final read-only snapshot PASS with
  `room_conversations=0` and `enabled_media_overrides=0`.
- Render deployment `dep-d9vsqjojo6nc73d6f6n0` reached `Live` on the exact candidate. Cloudflare
  Pages deployment `d64338e9-8116-4021-8f5e-90e261868ecb` completed successfully on the same SHA.
- Direct Render and Pages health/readiness/status plus anonymous room-conversation/message privacy
  probes passed `10/10`: expected `200`/typed `401`, `no-store`, request ID, no Set-Cookie and no
  sensitive body fields.
- Authenticated Organization Admin confirmed both media capabilities remained off. A synthetic
  MediaSpace prejoin was concealed without chat, device or LiveKit resources. Workspace and
  concealment pages had `53/53` and `11/11` named exposed controls; each had one `main`, `h1` and
  `nav`, with zero duplicate IDs and zero console warnings/errors.
- Post-live exact ACL/read-only snapshot remained `ledger=34 dirty=false`,
  `room_conversations=0`, `enabled_media_overrides=0`. No capability was temporarily enabled, no
  rollback ran and the disposable branch remains retained. P4-08 moved
  `IN PROGRESS -> VERIFY -> DONE`; physical/manual/device/load/outage gates remain
  `UNVERIFIED — P4-11`.

## 5. Prohibited actions

- No rollback of migration 000034 on disposable or shared staging.
- No deletion of the retained disposable branch before all database gates pass.
- No shared staging migration or deploy before disposable evidence is reported.
- No logging of database URLs, credentials, provider identifiers or message content.
