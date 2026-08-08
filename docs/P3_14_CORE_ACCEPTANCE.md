# P3-14-CORE Core Exit acceptance

- Status: `VERIFY`
- Date: 2026-08-08
- Database: no migration; disposable and shared staging remain at `28 false`
- Scope: runnable Phase 3 lane only; this document does not close full P3-14 or Phase 3

## Exit boundary

P3-14-CORE is the minimum checkpoint that allows Phase 4 Classroom Media work to start.
It confirms that the runnable calendar, poll, conversation, message, file-transfer, Class Files,
Home/search and offline/quota slices remain usable, authorized, tenant-scoped and accessible.
It does not activate worker-driven notification, email, poll delivery or file processing.

The full Phase 3 exit remains open until the carry-over register below is closed and a separate
`PHASE_3_COMPLETION.md` is signed off.

## Runnable-lane evidence matrix

| Slice | Accepted evidence before this checkpoint | Fresh P3-14-CORE regression evidence |
| --- | --- | --- |
| P3-02A/B/C | Calendar shell, recurrence/conflict, working hours, attendee/free-busy and RSVP are `DONE`; see `P3_02A_STAGING_ACCEPTANCE.md`, `P3_02B_STAGING_ACCEPTANCE.md` and `P3_02C_STAGING_ACCEPTANCE.md`. | Calendar production-route Playwright covers one-time/recurrence/RSVP, keyboard, Axe, 200% zoom, forced colors, visual baselines and performance budgets. |
| P3-02D-A | Poll/StudyMeeting schema/API/UI, capability lifecycle, privacy, quota and manual lifecycle are `DONE`; see `P3_02D_A_STAGING_ACCEPTANCE.md`. | Disposable PostgreSQL rechecks the shared StudyMeeting/ClassSession owner-time barrier and exact runtime ACL without granting audit/outbox reads. |
| P3-06/07A | Direct/class conversation, persistent messages and unread/read/reload are `DONE`; P3-07A evidence is in `P3_07A_STAGING_ACCEPTANCE.md`. | Fresh Go/web/security and PostgreSQL integration suites cover idempotency, tenant/class scope and foreign-resource concealment. |
| P3-08/09 | Metadata, intent/finalize and exact-version direct B2 single/multipart transfer are `DONE`; see `P3_08_STAGING_ACCEPTANCE.md` and `P3_09_STAGING_ACCEPTANCE.md`. | Content integration and live feature-control projection are rechecked while `file_uploads` remains deployment-disabled. |
| P3-11A | Gate-off Class Files transfer UI is `DONE`; see `P3_11A_STAGING_ACCEPTANCE.md`. | Browser/accessibility regression confirms the runnable UI states without activating upload or processing. |
| P3-12 | Home dashboard and authorization-filtered PostgreSQL search are `DONE`; see `P3_12_STAGING_ACCEPTANCE.md`. | Home/search route remains in the exact Browser E2E and tenant-isolation suite. |
| P3-13 | Scoped drafts, bounded retry and full feature/quota catalog are `DONE`; see `P3_13_STAGING_ACCEPTANCE.md`. | Feature-control integration and live Admin projection recheck the effective disabled ceilings and bounded catalog. |

## Fresh hardening found by the checkpoint

1. The Calendar toolbar had gained Messages and Availability Poll actions after its original
   visual baselines. At some desktop/tablet widths the final display-preference action could be
   clipped. The action group now wraps inside the calendar main region, and Playwright asserts
   every visible action remains within that region before taking desktop/tablet/mobile snapshots.
2. Classroom integration tests were still reading append-only `audit_events`/`outbox_events`
   through the exact Core API runtime pool. The production ACL is intentionally insert-only.
   Business mutations continue to use the runtime pool; audit/outbox assertions now use the
   migration-owner pool supplied only to the integration test process. No runtime grant changed.
3. A forced StudyMeeting/ClassSession two-writer race exposed an advisory-lock/class-row lock-order
   deadlock. ClassSession creation now acquires the shared tenant/user owner-time lock before the
   class row lock. The focused PostgreSQL concurrency test passed three consecutive executions and
   still requires exactly one writer to commit while the loser returns the typed schedule conflict.

## Automated gates

- [x] Format, generated-contract, lint, typecheck, web unit tests, builds, Storybook, local security
      guards, Core API Go tests and `go vet` pass on the working candidate.
- [x] Calendar production-route Playwright passes 15/15, including updated visual baselines,
      keyboard/Axe, 200% zoom, forced colors and performance budgets.
- [x] The owner-time concurrency regression passes 3/3 on Neon disposable.
- [x] Full disposable PostgreSQL package matrix passes with schema `28 false` and no rollback.
- [ ] Exact candidate Verify, Security, Browser E2E and Cloudflare Pages checks pass.
- [ ] Exact Render deployment, 6/6 public smoke, 2/2 anonymous privacy smoke and authenticated
      effective feature-gate checks pass.

The local host does not have Docker, so the general Browser E2E webServer cannot start locally.
Calendar production-route E2E runs directly and passes locally; the exact candidate Browser E2E
job remains the authoritative container-backed gate.

## Staging baseline before exact-candidate deployment

- Direct Render `/health`, `/ready`, `/api/v1/status` and Pages-proxied `/api/health`, `/api/ready`,
  `/api/v1/status` returned HTTP 200 with `Cache-Control: no-store` (6/6).
- Anonymous direct and Pages-proxied tenant capability reads returned HTTP 401 with
  `Cache-Control: no-store` (2/2).
- The authenticated organization-admin projection showed both `Tải tệp lên` and
  `Thông báo trong ứng dụng` as effective `Đang tắt`; the browser console had no warning/error.
- These are baseline observations only and must be repeated on the exact candidate before `DONE`.

## Deferred carry-over register

| Task | Current state | Owner | Dependency | Disabled guard / current boundary | Open gate |
| --- | --- | --- | --- | --- | --- |
| P3-03A/P3-03B | `VERIFY` / `DEFERRED/VERIFY` | Project owner | Durable host, dedicated worker role and exact worker grants | Render Free Web Service is Core API only; no keep-alive/cron/laptop substitute | Non-spin-down worker; startup ACL probe; crash/reclaim, duplicate and DLQ acceptance on PostgreSQL |
| P3-04 activation | `VERIFY` | Project owner | P3-03B | `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY=false` and `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS=false` | Controlled canary with worker role/grants, crash/reclaim and end-user activation acceptance |
| P3-CAL-02 | `VERIFY` | Project owner | AWS SES account/region plus owned sending domain | Renderer/sink/SES adapter remains isolated; no business-delivery runtime | SES sandbox/account/quota, verified event ingress, SPF/DKIM/DMARC, suppression and Gmail/Outlook/Apple interoperability |
| P3-05A | `DEFERRED/TODO` | Project owner | P3-02C, P3-CAL-02, P3-03B, P3-04 | No session email/ICS/reminder consumer is registered | Durable invitation/update/cancel/reminder delivery, stable UID/SEQUENCE and external RSVP security |
| P3-02D-B | `DEFERRED/TODO` | Project owner | P3-02D-A, P3-03B, P3-04 | Poll deadline is stored but lifecycle remains manual; no worker auto-close/fan-out | Auto-close, roster snapshot/fan-out, retry/idempotency and privacy-safe delivery acceptance |
| P3-05B | `DEFERRED/TODO` | Project owner | P3-02D-B, P3-05A | No Poll/StudyMeeting delivery adapter is active | Provider-backed poll lifecycle delivery without changing poll ownership or capability boundaries |
| P3-07B | `DEFERRED/TODO` | Project owner | P3-07A, P3-03B, P3-04 | Message persistence works; in-app notification effective state remains off | Durable message-notification consumer, duplicate/crash recovery and preference acceptance |
| P3-10 | `DEFERRED/TODO` | Project owner | P3-03B, P3-09 | `FEATURE_CONTROL_DISABLE_FILE_UPLOADS=true`; uploaded files cannot become ready through an untrusted client claim | Exact-version stream hash, malware scan, metadata/thumbnail processing and stale-version safety |
| P3-11B | `DEFERRED/TODO` | Project owner | P3-10, P3-11A | Class Files activation/upload remains fail-closed | Processing/thumbnail/rejected UX, sharing/download state matrix and end-user activation acceptance |

## Current decision

P3-14-CORE remains `VERIFY` until exact candidate CI, deployment and repeated live acceptance
are green. No deferred item is treated as passed, no
asynchronous side effect is activated, and full P3-14/Phase 3 remain `TODO`.
