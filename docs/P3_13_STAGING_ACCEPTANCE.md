# P3-13 Offline/retry drafts and Phase 3 quota closure acceptance

- Status: `VERIFY`
- Date: 2026-08-08
- Database: no migration; shared and disposable schema remain `28 false`

## Accepted scope

- Persist only the organization-admin feature/quota override draft in `sessionStorage`, scoped
  to actor and tenant, limited to 16 KiB and eight hours.
- Never persist message content, poll availability/participant data, capability tokens, signed
  URLs, file handles, checksums or provider metadata.
- Purge TutorHub drafts on successful save, explicit reload, logout, HTTP 401/session expiry and
  workspace/principal switch without deleting unrelated browser storage.
- Automatically retry an opted-in mutation at most once and only for network/HTTP 5xx failures
  when the request already contains a stable server-enforced idempotency key. Never auto-retry
  4xx, quota, conflict, authorization or validation errors.
- Keep full B2 file transfer on explicit manual retry because signed capabilities and multipart
  provider state can expire independently of the API idempotency key.
- Derive bounded quota-rejection metric labels from the complete compiled Go quota catalog;
  unknown runtime labels are ignored.

## Automated gates

- [x] ADR-0029 accepted; no database or provider dependency added.
- [x] Draft schema/scope/TTL/size/cleanup tests and session/workspace purge regression tests.
- [x] Stable idempotency-key retry tests, including one 503 replay with the same message key.
- [x] Feature/quota catalog coverage and bounded quota metric tests across scheduling, poll,
      message and file domains.
- [x] Full exact-tree local `pnpm verify`.
- [x] Neon disposable feature-control PostgreSQL integration and schema-version check.
- [ ] Exact candidate CI/security and deployed Teacher/Admin staging acceptance.

## Local and disposable evidence

- `pnpm verify` PASS on the exact local tree with workspace-local `GOCACHE`: format, generated
  contract, lint, typecheck, builds, Storybook, security/bundle checks, 55 web files/262 tests,
  all Core API Go tests and `go vet`.
- `go test -count=1 -tags=integration ./services/core-api/internal/modules/featurecontrol` PASS
  against Neon disposable using the migration URL for owner setup and the exact runtime pool for
  business operations. Runtime could mutate through the intended API path but could not read
  append-only audit/outbox tables; the test now checks those facts only when its role has SELECT
  rather than widening runtime ACL.
- Disposable migration ledger remained `28 false`; no migration, rollback, shared-staging write,
  deploy or feature activation was performed.
- An earlier diagnostic committed two synthetic feature-control fixture tenants/users and their
  append-only audit/outbox facts on the disposable branch before the runtime-ACL mismatch was
  understood. They remain disposable-only because deleting audit history or bypassing the
  append-only trigger would weaken the gate. The branch is retained; dropping the disposable
  branch after acceptance is the cleanup boundary.
- Existing quota/rate-window maintenance remains bounded and uses `SKIP LOCKED`; lowering a quota
  changes future enforcement and does not delete historical business data.

## Retry and privacy matrix

1. Network or HTTP 5xx: one automatic retry only for an opted-in request carrying its stable
   idempotency key.
2. HTTP 4xx, including 409 and 429: no automatic retry; typed Problem Details remain visible.
3. Message, poll response/finalize/create and Study Meeting create: eligible because their
   request contracts already carry stable idempotency identifiers.
4. B2 single/multipart transfer: manual retry only; no provider capability is persisted.
5. Feature/quota draft: same-tab reload restores only the scoped typed configuration; another
   actor or tenant cannot read it, and boundary cleanup removes it.

P3-13 remains `VERIFY` until the exact candidate passes CI/security and the deployed Admin
same-tab recovery, logout/workspace purge, quota rejection and retry/privacy acceptance matrix.
