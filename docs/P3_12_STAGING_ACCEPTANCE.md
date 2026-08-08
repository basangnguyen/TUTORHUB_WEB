# P3-12 Home dashboard and PostgreSQL search acceptance

- Status: `VERIFY`
- Date: 2026-08-08
- Database: no migration planned; shared schema remains `28 false`

## Accepted scope

- Home independently loads upcoming authorized sessions, unread notification/message counts
  and recent authorized ready files with bounded requests.
- PostgreSQL search returns metadata-only session, conversation and ready-file results from
  resources the actor can currently access.
- Partial Home failure degrades one card with a local retry action. Cache scope is bound to
  tenant and user and is purged by the existing principal-generation boundary.
- No message/file-content search, private snippet, external search engine or vector store.

## Automated gates

- [x] OpenAPI generated-client contract and tenant-header binding tests.
- [x] Go validation/HTTP tests for authentication, scope change, bounds and no-store headers.
- [x] PostgreSQL integration tests for Teacher/Student/direct/class authorization, foreign
      tenant concealment, inactive membership and literal wildcard handling.
- [x] Web loading/empty/error/partial-degrade/search/keyboard/Axe tests.
- [x] Full exact-tree local `pnpm verify`.
- [ ] Exact candidate CI/security and deployed acceptance.

## Local and disposable evidence

- `pnpm verify` PASS on the exact local tree: format, generated contract, lint, typecheck,
  API-client/Go/web tests, build, Storybook and security/bundle checks. Web result: 53 files,
  255 tests PASS.
- `go test -count=1 -tags=integration ./services/core-api/internal/modules/discovery` PASS
  against Neon disposable. The suite covers active/inactive membership, Teacher/Student,
  direct/class conversation authority, foreign-tenant concealment, ready-file visibility and
  literal `%`/`_` search terms.
- Production web build plus `home-p312.spec.ts` Playwright PASS for independent-card degradation,
  expected-tenant headers, keyboard focus, empty search and Axe with zero violations in `<main>`.
- No migration was added. The disposable branch remains at schema `28 false`; no rollback,
  shared-staging migration or deployment was performed.
- The local Docker-backed whole-product E2E stack was unavailable on this host. Exact candidate
  CI remains the authority for that existing suite before `VERIFY -> DONE`.

## Live acceptance matrix

1. Teacher and Student Home show only their authorized upcoming sessions and ready files.
2. A forced failure of one Home provider leaves the other cards usable and exposes a named
   retry action only for the failed card.
3. Search an accessible session, class conversation and ready filename; each opens only a
   currently authorized destination.
4. Search foreign-tenant, inactive-class-membership, pending-file and message-body-only terms;
   none appear and response metadata contains no private snippet/provider selector.
5. Switch workspace and verify old Home/search data disappears before the new projection.
6. Run keyboard-only focus order, screen reader/Axe and narrow viewport acceptance.
