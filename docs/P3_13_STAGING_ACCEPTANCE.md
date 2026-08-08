# P3-13 Offline/retry drafts and Phase 3 quota closure acceptance

- Status: `DONE`
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
- [x] Exact candidate CI/security and Cloudflare Pages deployment.
- [x] Render exact-candidate deployment and live Admin staging acceptance.

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

## Candidate evidence

- Exact source candidate `25a323ad17c65ae402dd65d33cc7727132f748b1` passed Verify
  `31260418987` and Security `31260418985`.
- Verify passed Quality/integration, Browser E2E and local environment smoke. Security passed
  CodeQL Go/TypeScript, secret scan, repository vulnerability scan and Core API container scan.
- The first candidate exposed one stale Browser E2E assertion: it expected a manual retry button
  after one network failure. The final E2E now proves the automatic second POST returns 201 and
  carries the exact same `client_message_id`; the final Browser E2E job passed.
- Cloudflare Pages check `93110366172` passed for the exact source candidate. Render deployment
  `dep-d9rjdnf10e5c7387mh60` reached `Live` on the exact full SHA
  `25a323ad17c65ae402dd65d33cc7727132f748b1` in 1m44s.

## Live staging evidence

- Direct Render `/health`, `/ready`, `/api/v1/status` and Pages-proxied `/api/health`,
  `/api/ready`, `/api/v1/status` all returned HTTP 200 with `Cache-Control: no-store` (6/6).
- Anonymous direct and Pages-proxied tenant capability reads returned HTTP 401 with
  `Cache-Control: no-store` and `Referrer-Policy: no-referrer` (2/2).
- A deployed organization-admin session displayed the complete effective feature/quota catalog.
  Changing `message_sends_per_hour` from 5000 to 5001 without saving created the scoped draft;
  a same-tab reload restored 5001 and the draft status.
- Switching KMA -> P2-08 Alternate -> KMA removed that draft and restored the server value 5000.
  Repeating with an unsaved value 5002, logging out and signing in again also restored 5000 with
  no draft status. No feature/quota save, shared-database mutation or quota exhaustion fixture was
  performed.
- Browser console warning/error capture was empty. The live catalog/purge checks combine with the
  exact candidate Browser E2E, unit and disposable PostgreSQL tests for 4xx/409/429 no-retry,
  one bounded network/5xx retry using the same idempotency key, typed quota rejection and bounded
  privacy-safe metric labels.

## Retry and privacy matrix

1. Network or HTTP 5xx: one automatic retry only for an opted-in request carrying its stable
   idempotency key.
2. HTTP 4xx, including 409 and 429: no automatic retry; typed Problem Details remain visible.
3. Message, poll response/finalize/create and Study Meeting create: eligible because their
   request contracts already carry stable idempotency identifiers.
4. B2 single/multipart transfer: manual retry only; no provider capability is persisted.
5. Feature/quota draft: same-tab reload restores only the scoped typed configuration; another
   actor or tenant cannot read it, and boundary cleanup removes it.

P3-13 is `DONE`: the exact candidate is Live, public and anonymous privacy probes pass, and the
deployed non-destructive Admin acceptance plus exact candidate CI/disposable evidence closes the
draft, quota, retry and privacy matrix without changing shared configuration.
