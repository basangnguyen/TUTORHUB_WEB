# P3-11A Class Files transfer-core acceptance

- Status: `DONE`
- Date: 2026-08-08
- Activation: `file_uploads` remains fail-closed/off
- Database: no new migration; shared schema remains `28 false`

## Accepted scope

- `GET /api/v1/classes/{class_id}/files` provides a bounded, tenant/class-cursor-bound
  projection. Ready files are visible to current class viewers; non-ready metadata remains
  limited to the creator or a current upload manager.
- The server projects `can_upload`, `can_download` and `can_retry_upload`; the browser never
  derives file authority from a role label.
- Class Files renders loading, empty, retryable error, concealed forbidden and feature-off
  states. Active content is never previewed; ready downloads still use the existing
  attachment-only, immutable-version capability.
- Transfer UI supports worker-based SHA-256, direct single PUT with progress/retry and
  multipart progress/resume within the current browser session. Client request IDs and
  provider selectors remain memory-only.
- Query keys contain tenant and class. Workspace switch purges the cache; class mutation and
  roster/role mutation invalidate the affected class-file projection.

## Automated gates

- [x] OpenAPI generated-client check and API-client tenant/class binding tests.
- [x] Go service/HTTP tests for cursor scope, nullable pagination and viewer projection.
- [x] Web tests for gate-off, pending/ready/forbidden states, checksum and fail-closed
      provider-version handling.
- [x] Go content + HTTP unit tests and web TypeScript/lint checks.
- [x] Neon disposable content suite: exact runtime ACL, tenant isolation, Teacher/Student
      list visibility, ready download, archived-class retry denial and multipart regression.
- [x] Full exact-tree `pnpm verify`.
- [x] Exact candidate `73467ae665c5aa26a901585f59d41fa32eeff585`: Verify
      `31247921859` and Security `31247921851` PASS, including Browser E2E,
      quality/integration, CodeQL, Trivy, container and secret scan.
- [x] Cloudflare Pages and Render deploy `dep-d9rechlbedkc73bj80v0` are live on the exact
      candidate; direct Render and same-origin Pages health/readiness/status are `6/6` HTTP
      `200` with `Cache-Control: no-store`.
- [x] Deployed Teacher/Student feature-off, workspace-switch, roster-role and class
      archive/restore cache invalidation acceptance.
- [x] Accessibility closeout: owner explicitly approved the exact-candidate automated
      Axe/keyboard/accessibility-tree evidence as the P3-11A substitute for manual NVDA.

## Live acceptance matrix

1. Confirm deployment still reports `file_uploads.enabled=false` for Teacher and Student.
2. Teacher opens one active class: Class Files loads, upload action explains the safety gate,
   no file picker or provider URL is exposed, and empty/error retry states are reachable.
3. Student opens the same class: no upload management control is rendered. A ready fixture,
   when one is explicitly available, exposes only attachment download; non-ready metadata is
   concealed.
4. Switch workspace, archive/restore the class and change a roster role; verify no previous
   tenant/class file row remains visible from cache.
5. Run keyboard-only focus and Axe/NVDA checks on loading, empty, forbidden and feature-off
   states. Do not enable uploads or create a real end-user object during this gate.

## Live evidence 2026-08-08

- Teacher opened active class `P2081744`: Class Files loaded the empty state and explicitly
  announced that uploads are temporarily disabled. No file input, upload control, provider
  URL or signed selector was present.
- Student opened the same class: the panel remained download-only, rendered no upload
  management control and exposed no provider URL. No ready fixture or end-user object was
  created for this gate; ready-download authority remains covered by the disposable
  PostgreSQL integration suite.
- Switching Student from the P2-08 workspace to P2-12 redirected away from the old class and
  left no previous class-file row visible. The original workspace was restored afterwards.
- The synthetic Student enrollment was changed `student -> teaching_assistant -> student`.
  Both mutations returned success and the Class Files query reloaded without stale metadata.
- Archived fixture `P306A0804` was restored and archived again. The active projection showed
  the feature-off upload notice; the final archived projection removed upload/retry authority.
  The fixture ended in its original archived state.
- Exact-candidate Browser E2E/Axe and keyboard/focus checks passed in CI. The deployed DOM
  exposes a named Class Files region, ordered headings and a live feature-off status.

## Accessibility exception and sign-off

On 2026-08-08 the owner explicitly approved automated accessibility evidence in place of the
manual NVDA check after NVDA quick-navigation did not respond in the embedded browser. The
substitute evidence is the exact-candidate Browser E2E/Axe gate, keyboard/focus assertions,
live accessibility-tree inspection and Teacher/Student feature-off projections above.

This exception is scoped only to the P3-11A gate-off slice. It does not waive manual screen
reader acceptance for P3-11B, upload activation, pilot/public release or future UI changes.
With this recorded exception, every P3-11A exit gate is closed and the task is `DONE`.

## Deferred boundary

P3-10 owns exact-version stream hash, malware scan, metadata extraction and durable worker
transitions. P3-11B owns real processing/rejected/thumbnail UX. Neither is claimed by this
task, and `file_uploads` must remain off until those safety gates close.
