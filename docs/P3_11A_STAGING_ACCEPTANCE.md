# P3-11A Class Files transfer-core acceptance

- Status: `VERIFY`
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
- [ ] Exact candidate CI/security.
- [ ] Deployed feature-off acceptance for Teacher/Student, keyboard/focus and Axe/NVDA.

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

## Deferred boundary

P3-10 owns exact-version stream hash, malware scan, metadata extraction and durable worker
transitions. P3-11B owns real processing/rejected/thumbnail UX. Neither is claimed by this
task, and `file_uploads` must remain off until those safety gates close.
