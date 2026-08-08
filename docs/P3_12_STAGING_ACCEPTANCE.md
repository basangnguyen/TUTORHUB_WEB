# P3-12 Home dashboard and PostgreSQL search acceptance

- Status: `DONE`
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
- [x] Exact candidate CI/security and deployed acceptance.

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
- No migration was added. During the disposable gate, the branch remained at schema `28 false`
  and no rollback, shared-staging migration or deployment was performed.
- The local Docker-backed whole-product E2E stack was unavailable on this host. Exact candidate
  CI Browser E2E passed and is the authority for that existing suite.

## Candidate and live evidence

- Exact candidate `1c73c52782ca8b4139af0802bbace8e82b0c288b` passed Verify
  `31256747702` and Security `31256747681`. Quality/integration, Browser E2E, local smoke,
  CodeQL Go/TypeScript, Gitleaks, Trivy repository and Core API container checks all succeeded.
- Cloudflare Pages check `93101270357` succeeded for the exact SHA. Render deployment
  `dep-d9rhu4ugekts739t6ssg` checked out the full SHA and reached `Live`.
- Direct Render and same-origin Pages health/readiness/status probes were `6/6` HTTP 200 with
  `Cache-Control: no-store`.
- Unauthenticated recent-file and search probes against direct Render and Pages were `4/4`
  HTTP 401 with `no-store`, `no-referrer` and `nosniff`.
- Teacher and Student Home both showed their authorized upcoming sessions. The unavailable
  notification projection exposed only its local retry while session, message and file cards
  remained usable.
- Teacher and Student found the authorized P3-06 class conversation and its message route.
  Teacher workspace switch from P2-08 to P2-12 immediately removed the old result and replaced
  Home counts/sessions with the new projection. Student search for a foreign P2-08 workspace
  term returned no result.
- Neither live workspace currently contains a ready file, so live acceptance verified the
  ready-file empty state without creating shared staging data. Ready-only visibility, pending/
  deleted concealment and foreign-tenant file isolation remain covered by the passing Neon
  disposable integration gate.
- At a 375 px content viewport, Home had no horizontal overflow and main/search remained visible.
  The live accessibility tree preserved skip link, regions, headings, lists and named controls;
  console warning/error count was zero. Exact-build Playwright keyboard/Axe passed with zero
  `<main>` violations, and candidate Browser E2E succeeded.
- No migration, shared-database mutation, rollback or feature activation was required. Shared
  schema remains `28 false`.

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

All six items are closed by the combined live, exact-build Playwright/Axe and disposable
PostgreSQL evidence above. P3-12 moved `VERIFY -> DONE` on 2026-08-08.
