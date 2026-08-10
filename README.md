# TutorHub V2

TutorHub V2 là phiên bản web-first của hệ sinh thái TutorHub. Dự án được xây dựng mới, còn `D:\Ban_sao_du_an` chỉ là nguồn tham chiếu nghiệp vụ và dữ liệu của TutorHub V1.

- Repository chính thức: [basangnguyen/TUTORHUB_WEB](https://github.com/basangnguyen/TUTORHUB_WEB)
- Thư mục phát triển chuẩn: `D:\TutorHub_V2`
- Nhánh mặc định: `main`; remote chuẩn: `origin`

## Trạng thái

- Phase 0 và **Phase 1 - Engineering Foundation** đã hoàn thành ngày 2026-07-16.
- **Phase 2 - Identity, tenant và class core** đã hoàn thành và được owner sign-off
  ngày 2026-07-22. P2-00 đến P2-12, staging acceptance, application rollback/redeploy
  và exit gate đều đạt.
- **Phase 4 - Classroom Media MVP** đã mở sau khi P3-14-CORE đạt gate. Phase 3
  deferred carry-over vẫn tiếp tục song song. P3-00,
  P3-CAL-00/00B/00C, P3-CAL-01, P3-01, P3-02A, P3-02B và P3-02C đã `DONE`.
  ADR-0019 chấp nhận FullCalendar Standard v7.0.1 qua adapter, recurrence Go bounded
  `366/730/512/2.000/250 ms`, COUNT occurrence-last horizon validation, YEARLY golden,
  Warm Academic theme và manual NVDA gate đã PASS. P3-02C runtime acceptance commit
  `7859c233` đã live trên Render/Cloudflare; Neon staging `21 false`, ACL/runtime-role,
  Calendar E2E `11/11` và ma trận manual/staging đều đạt.
- P3-03A/P3-04 vẫn ở `VERIFY`: schema, fencing/lease, retry/dead-letter, typed registry,
  worker binary, notification projection/API/UI và controlled canary đã có trong
  repository, nhưng chưa có durable worker staging acceptance. Theo quyết định
  2026-07-31, **giữ Render Web Service cho Core API staging/private alpha**; Render Free
  không được dùng làm durable worker. Các gate cần host không spin-down (worker role/
  grants, crash/reclaim, duplicate canary) là `DEFERRED/VERIFY`, không bị coi là đã
  pass do bỏ qua. Hai gate `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY` và
  `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS` vẫn mặc định `false`; không bật
  end-user side effect trước khi durable acceptance đạt.
- ADR-0021 đã `Accepted` để P3-02D xây Native Availability Poll do TutorHub sở hữu:
  active member gồm student có thể tạo poll/Study Meeting của mình; secure public link
  không phải booking và không phụ thuộc When2meet. P3-02D-A đã `DONE` ngày 2026-08-03
  trên exact candidate `8585864`; disposable/shared staging đều ở `23 false`, exact
  database/ACL/concurrency, authenticated browser/API và manual NVDA acceptance đều PASS.
  P3-02D-B delivery/auto-close/fan-out vẫn carry-over.
- P3-06 Direct/class conversation core đã `DONE` ngày 2026-08-04 trên exact candidate
  `756ca60a`; shared Neon ở `24 false`, exact ACL/PostgreSQL, CI/security, authenticated
  direct/class/archive role matrix, keyboard/focus và Playwright/Axe đều PASS. Message
  persistence/unread/read đã hoàn tất ở P3-07A; realtime/notification vẫn thuộc P3-07B.
- P3-07A Persistent message/unread/read core đã `DONE` ngày 2026-08-05 trên exact candidate
  `a21ec385`; shared Neon ở `25 false`, exact ACL/PostgreSQL, CI/security và authenticated
  role/reload/accessibility/log-privacy acceptance đều PASS.
- P3-08 File metadata/upload intent/finalize đã `DONE` ngày 2026-08-08 trên exact candidate
  `6a50c3e4`: ADR-0026, migration `000026`, Go content core, fail-closed B2 metadata
  verification, OpenAPI/generated client, feature/quota controls, disposable
  `25 -> 26 -> 26`, exact ACL/PostgreSQL, CI và Security đều PASS. Theo ranh giới task,
  shared staging vẫn `25 false` và chưa deploy; direct B2 transfer/evidence thuộc P3-09.
- AWS SES đã được owner chọn làm transactional email provider target cho Phase 3.
  P3-CAL-02/ADR-0020 vẫn phải xác minh account/region/sandbox/quota, adapter, webhook và
  deliverability; trước khi có domain chỉ được thử bằng identity cá nhân do owner kiểm
  soát và đã verify trong SES sandbox. Production vẫn chờ domain/DNS cùng
  SPF/DKIM/DMARC, provider-event topology và cross-client matrix; chưa có email runtime.
  Các test local/CI/sandbox vẫn bắt buộc chạy; chỉ các gate phụ thuộc provider/domain
  production mới được ghi `DEFERRED/VERIFY`.
- **Mô hình thực thi Phase 3 (re-baseline 2026-07-31):** Phase 3 được tách thành
  `Core Exit` và `Deferred carry-over`. `Core Exit` bao gồm các luồng có thể xây,
  chạy và kiểm thử trên Render/Neon/B2 hiện tại (trong đó có P3-02D-A poll core,
  conversation/message core và file transfer core). Khi các gate của `Core Exit` xanh,
  có thể bắt đầu Phase 4; điều này **không** đánh dấu umbrella Phase 3 hay P3-14 là
  `DONE`.
- `Deferred carry-over` vẫn được theo dõi và không được bypass: P3-03B durable worker,
  P3-04 activation, P3-CAL-02 live SES/domain/interoperability, P3-05A/B delivery,
  P3-10 và P3-11B processing UX phụ thuộc worker. Cho đến khi các gate này đóng,
  notification/email/reminder và Class Files sharing/processing tới end user vẫn giữ tắt.
- P3-09 Presigned B2 upload/download đã `DONE` ngày 2026-08-08 trên final candidate
  `d6365b5`: migration `000027/000028`, exact-version single/multipart capability,
  disposable/shared Neon `28 false`, exact ACL/PostgreSQL, B2 CORS/lifecycle/provider smoke,
  Verify/Security và Render/Cloudflare live acceptance đều PASS. Live acceptance phát hiện
  deployment guardrail file upload bị thiếu; Render đã được đặt fail-closed và code/`.env.example`
  cũng mặc định disable. Feature tiếp tục off cho tới P3-10. Task runnable tiếp theo là P3-11A
  Class Files transfer-core UI trong trạng thái gate-off. P3-02D-B
  lifecycle delivery, P3-07B realtime/notification và các gate hạ tầng/provider là
  carry-over; chúng có thể tiếp tục chính xác sau khi Phase 4 bắt đầu.
- P3-14-CORE đã `DONE` ngày 2026-08-08 sau fresh full verify, Calendar Playwright
  15/15 và bảy PostgreSQL integration package trên Neon disposable; ledger giữ `28 false`.
  Checkpoint đã sửa Calendar toolbar overflow, giữ exact runtime ACL bằng owner-only test
  assertions và loại deadlock StudyMeeting/ClassSession bằng lock order thống nhất. Carry-over
  register đã có. Exact candidate `f5f1eb3` PASS CI/security và Live trên Render/Pages;
  public/privacy/feature-off/Calendar acceptance đều xanh. Phase 4 được phép bắt đầu; full
  P3-14 và Phase 3 vẫn `TODO`.
- P4-00 architecture/backlog baseline đã `DONE` ngày 2026-08-08. ADR-0030 chốt
  MediaSpace/RoomInstance/ParticipantSession, official/member-owned authority,
  room-instance LiveKit credential, webhook binding, lobby/moderation, privacy và
  feature-off rollout. P4-01 MediaSpace lifecycle/schema/API core đã `DONE` ngày 2026-08-09 trên
  exact candidate `183ca338557fafd6e8fe502d67763bb2a73d9aa0`: disposable/shared forward-only
  `28 false -> 29 false -> 29 false`, exact ACL/PostgreSQL, Verify/Security, Render/Cloudflare và
  authenticated/live feature-off acceptance đều PASS. Chưa gọi LiveKit, không có provider side
  effect và hai media feature vẫn off. P4-02 credential/webhook sau đó cũng đã `DONE` trên exact
  disposable/shared/deploy/live-provider acceptance, ledger giữ `30 false`.
- P4-MEDIA-UX-00 đã `DONE` ngày 2026-08-09 bằng current official Zoom/Meet/LiveKit/browser
  research, V1 reuse/reject audit, prototype cô lập và ADR-0031. `effect=None` là production
  baseline; Track Processors 0.7.2 chỉ là candidate force-off tới browser/device/privacy/performance
  gate; hand/reaction vẫn qua Core API và `CanPublishData=false`.
- P4-03 Prejoin device/network và canonical join-attempt đã `DONE` ngày 2026-08-10 trên exact
  candidate `e49a8cc38f464e3ec56655823bcbb1ee77cbc651`: local/Chromium/disposable/LiveKit provider,
  GitHub Verify/Security, shared exact ACL, Render/Cloudflare exact deploy và live
  public/privacy/authenticated feature-off/no-side-effect acceptance đều PASS. Không có migration;
  shared ledger giữ `30 false`, không rollback.
- P4-04 Lobby/admission/explicit same-tenant invite đang `VERIFY` ngày 2026-08-10. Local candidate
  đã có OpenAPI/generated client, Go service/repository/HTTP, web waiting/moderator/invite UX,
  migration `000031` và full local verification PASS. Neon disposable owner preflight, forward-only
  `30 false -> 31 false -> 31 false`, exact/default ACL cùng PostgreSQL gates PASS. Exact CI/security,
  shared staging, deploy và live acceptance còn `PENDING`; không rollback. Hai media
  feature tiếp tục force-off, `effect=None`, `CanPublishData=false`.
- P5-COLLAB-00 đã được đăng ký làm research spike tương lai cho whiteboard engine,
  document/sync authority, license và realtime topology. Task vẫn `TODO`, không chặn P4-02
  và không cho phép thêm production dependency/runtime trước prototype, evidence và ADR.
- Web MVP nền đã chạy trên staging: Cloudflare Pages -> same-origin `/api/*` -> Go
  Core API trên Render; dữ liệu dùng Neon, file dùng Backblaze B2, media dùng LiveKit
  Cloud và xác thực dùng ZITADEL.
- Exit gate Phase 1 đã đạt cho Verify/Security CI, OIDC/session/logout,
  health/readiness, migration/rollback, B2, LiveKit 2-5 người, webhook idempotent và
  local developer experience.
- Repository hiện do một người duy trì và push trực tiếp `main`; ngoại lệ quản trị
  này được giới hạn trong development/staging/private alpha theo ADR-0012.
- Master Plan web-first 2.4 và backlog Phase 4 là nguồn kế hoạch implementation hiện hành;
  backlog Phase 3 tiếp tục là nguồn cho deferred carry-over/full closure.
- Không sao chép secret, token hoặc cấu hình production từ V1.

## Tài liệu bắt buộc đọc

1. [Quy trình phát triển và checklist](docs/AGENT_COORDINATION.md)
2. [Trạng thái hiện tại](docs/PROJECT_STATE.md)
3. [Kế hoạch tổng thể](docs/MASTER_PLAN.md)
4. [Phạm vi sản phẩm](docs/PRODUCT_SCOPE.md)
5. [Web MVP](docs/WEB_MVP.md)
6. [Bối cảnh hệ thống](docs/SYSTEM_CONTEXT.md)
7. [Mô hình miền và quyền](docs/DOMAIN_MODEL.md)
8. [Bản đồ di chuyển V1](docs/V1_MIGRATION_MAP.md)
9. [Chuẩn bảo mật](docs/SECURITY_BASELINE.md)
10. [Deployment baseline](docs/DEPLOYMENT_BASELINE.md)
11. [Lộ trình giao hàng](docs/DELIVERY_ROADMAP.md)
12. [Backlog Phase 1](docs/PHASE_1_BACKLOG.md)
13. [Biên bản hoàn thành Phase 0](docs/PHASE_0_COMPLETION.md)
14. [Biên bản hoàn thành Phase 1](docs/PHASE_1_COMPLETION.md)
15. [Backlog Phase 2](docs/PHASE_2_BACKLOG.md)
16. [Database foundation và migration runbook](docs/DATABASE.md)
17. [LiveKit spike và smoke-test runbook](docs/LIVEKIT_SPIKE_RUNBOOK.md)
18. [Design system và hướng dẫn sử dụng component](docs/DESIGN_SYSTEM.md)
19. [CI/CD và security runbook](docs/CI_SECURITY.md)
20. [Browser E2E local/staging](docs/E2E_TESTING.md)
21. [P2-12 staging acceptance](docs/P2_12_STAGING_ACCEPTANCE.md)
22. [Biên bản hoàn thành Phase 2](docs/PHASE_2_COMPLETION.md)
23. [Backlog Phase 3](docs/PHASE_3_BACKLOG.md)
24. [Nghiên cứu và thiết kế product/technical tab Lịch](docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md)
25. [Chính sách báo cáo lỗ hổng](SECURITY.md)
26. [ADR-0011: Render cho Core API staging/private alpha](docs/adr/0011-render-core-api-staging.md)
27. [ADR-0012: Direct-main khi một người duy trì](docs/adr/0012-single-maintainer-direct-main-governance.md)
28. [ADR-0013: Shared organization/class authorization policy](docs/adr/0013-shared-organization-class-authorization-policy.md)
29. [ADR-0014: Append-only tenant audit log](docs/adr/0014-append-only-tenant-audit-log.md)
30. [ADR-0015: Server-evaluated feature controls và quotas](docs/adr/0015-server-evaluated-feature-controls-and-quotas.md)
31. [ADR-0016: Idempotent V1 fixture import](docs/adr/0016-idempotent-v1-fixture-import.md)
32. [ADR-0017: Class session scheduling và civil time](docs/adr/0017-class-session-scheduling-and-civil-time.md)
33. [ADR-0018: PostgreSQL leased outbox worker](docs/adr/0018-postgresql-leased-outbox-worker.md)
34. [ADR-0019: Calendar renderer, bounded recurrence và conflict authority](docs/adr/0019-calendar-renderer-recurrence-and-conflict.md)
35. [ADR-0021: Native Availability Poll và member-owned Study Meeting](docs/adr/0021-native-availability-polls-and-member-owned-study-meetings.md)
36. [ADR-0022: Tenant-scoped in-app notification projection và preference](docs/adr/0022-tenant-scoped-in-app-notification-projection.md)
37. [ADR-0023: Calendar working schedule, free/busy và RSVP authority](docs/adr/0023-calendar-working-schedule-free-busy-and-rsvp-authority.md)
38. [ADR-0024: Forward-only security correction cho Availability Poll maintenance purge](docs/adr/0024-forward-maintenance-purge-security.md)
39. [Backlog Phase 4](docs/PHASE_4_BACKLOG.md)
40. [ADR-0030: Authoritative Classroom Media spaces, lifecycle và LiveKit grants](docs/adr/0030-authoritative-classroom-media-spaces-lifecycle-and-livekit-grants.md)
41. [P4-MEDIA-UX-00: Classroom media UX research spike](docs/P4_MEDIA_UX_00_RESEARCH_SPIKE.md)
42. [P4-MEDIA-UX-00: Báo cáo research/evidence](docs/P4_MEDIA_UX_00_RESEARCH_REPORT.md)
43. [ADR-0031: Classroom media UX devices/layout/effects/signals](docs/adr/0031-classroom-media-ux-devices-layout-effects-and-signals.md)
44. [P4-04: Lobby/admission/invite staging acceptance](docs/P4_04_STAGING_ACCEPTANCE.md)
45. [P5-COLLAB-00: Whiteboard engine và collaboration topology research spike](docs/P5_COLLAB_00_RESEARCH_SPIKE.md)

Các quyết định kiến trúc đã chấp nhận nằm trong `docs/adr`.

## Nguyên tắc

- Web-first, API-first, contract-first.
- Modular monolith trước; chỉ tách service khi có số liệu vận hành chứng minh nhu cầu.
- Managed services trước; không dùng Kubernetes trong MVP.
- Multi-tenant và phân quyền được thiết kế từ đầu.
- Secure Exam tiếp tục là sản phẩm native riêng, không giả định trình duyệt web có thể khóa hệ điều hành.
- Di chuyển theo Strangler Pattern, không big-bang rewrite.
