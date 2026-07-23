# TutorHub V2 - Trạng thái dự án

> Điểm vào nhanh để tiếp tục phát triển. Cập nhật sau mỗi task hoặc thay đổi hạ tầng.

## Snapshot

| Thuộc tính          | Trạng thái                                                                            |
| ------------------- | ------------------------------------------------------------------------------------- |
| Ngày cập nhật       | 2026-07-23                                                                            |
| Repository          | `https://github.com/basangnguyen/TUTORHUB_WEB`                                        |
| Nhánh làm việc      | `main`                                                                                |
| Quy trình           | Một coding agent, commit trực tiếp vào `main`; GitHub dùng để lưu và sao lưu mã nguồn |
| Phase hoàn thành    | Phase 0, Phase 1, Phase 2                                                             |
| Phase hiện tại      | Phase 3 - Daily learning workspace                                                   |
| Task vừa hoàn thành | P3-CAL-00C Calendar implementation-readiness review (docs-only)                       |
| Task hiện tại       | P3-CAL-01 renderer/recurrence/theme spike + ADR-0019 (`READY`)                       |
| Task tiếp theo      | Chạy P3-CAL-01/ADR-0019, sau đó triển khai P3-01 contract-first                      |

## Kiến trúc đang chạy

- Web: React + TypeScript + Vite trên `https://tutorhub-web.pages.dev`.
- Edge/BFF origin: Cloudflare Pages Function proxy `/api/*` tới Core API.
- Core API: Go modular monolith, OCI container trên `https://tutorhub-core-api.onrender.com`.
- Identity: ZITADEL OIDC Authorization Code + PKCE, session cookie phía BFF.
- Database: Neon PostgreSQL; staging tách branch và role runtime/migration.
- Object storage: Backblaze B2, application key tối thiểu quyền.
- Media: LiveKit Cloud; token do backend cấp, webhook được xác minh và lưu idempotent.
- Hugging Face không còn chạy Core API; chỉ là lựa chọn cho dịch vụ AI độc lập sau này.

## Đã hoàn thành

- P1-01 toolchain, monorepo, CI foundation.
- P1-02 React web shell, routing, TanStack Query, i18n và responsive states.
- P1-03 design tokens, UI primitives, Storybook và accessibility baseline.
- P1-04 Go API foundation: config fail-fast, graceful shutdown, structured log,
  request ID, Problem Details, metrics, health/live/ready.
- P1-05 OpenAPI source of truth, generated TypeScript client, PostgreSQL
  migrations, tenant context, outbox và integration tests.
- P1-06 OIDC/BFF authentication, session/CSRF/logout và workspace onboarding.
- P1-06B class vertical slice list/create/detail với authorization và tenant isolation.
- P1-07 LiveKit token service, prejoin, room UI, media controls, reconnect,
  telemetry và webhook receipt idempotent.
- P1-08A Verify/Security pipeline, Gitleaks, Dependency Review, CodeQL, Trivy,
  Dependabot, CODEOWNERS và bundle secret guard.
- P1-08B Cloudflare Pages production deployment, same-origin API proxy, Render
  Core API deployment, provider auto-deploy, health/readiness và rollback smoke.
- P1-09 PostgreSQL/Redis local bằng Compose, migration + seed idempotent,
  `local:setup` và `dev:local` một lệnh cho Windows/Linux.
- P1-10 staging resources: Neon, B2, Cloudflare Pages, Render, ZITADEL và LiveKit.
- Chấp nhận ADR-0011 để thay Hugging Face bằng Render cho Core API staging/private alpha.
- Rà exit gate Phase 1 trên commit `ee597af`: Verify/Security CI, HTTPS staging,
  OIDC, Neon, B2, LiveKit, telemetry và rollback đều đạt.
- Chấp nhận ADR-0012 cho direct-main có kiểm soát trong giai đoạn một người duy trì.
- P2-00: policy engine deny-by-default dùng chung cho identity/classroom/media;
  organization/class role matrix, effective permission, 403/404 concealment,
  OpenAPI enums/error conventions, policy test helpers và static boundary test.
- Chấp nhận ADR-0013 cho mô hình role tổ chức/lớp và authorization policy dùng chung.
- P2-01: profile GET/PATCH, identity list/link/unlink, recent-auth + state/nonce,
  collision protection, last-identity guard, audit/outbox, migration `000006`,
  OpenAPI/generated client và React settings UI có i18n vi/en.
- P2-02: tenant list/detail/create/update/archive, permission `tenant.view`/`tenant.manage`,
  optimistic version, session-context CAS, session/CSRF rotation và migration `000007`;
  success event `tenant.created/updated/archived/switched` được ghi durable qua outbox.
- Workspace UI áp dụng principal mới ngay sau create/switch/archive, hủy và xóa cache
  tenant-scoped để không flash dữ liệu workspace cũ; list/detail/update/archive có query,
  mutation và trạng thái lỗi phù hợp với contract typed.
- P2-03: permission `tenant.manage_members`, migration `000008`, invitation CSPRNG chỉ
  lưu purpose-bound HMAC, TTL/state machine, list/create/revoke và preview/accept bằng
  verified linked identity trong transaction idempotent; accept không tự đổi active tenant.
- Invitation URL giữ token trong fragment, web xóa fragment ngay và chỉ gửi token trong
  JSON POST body; admin UI có list/create/copy-once/revoke, public UI có đủ loading,
  offline, unavailable, mismatch, retry và success states bằng tiếng Việt/Anh.
- P2-04: migration `000009` bổ sung class `timezone`, optimistic `version` và
  `archived_at`; list có status filter cùng opaque keyset cursor, update/archive/restore/
  transfer ownership dùng `expected_version` CAS và transactional outbox.
- Class đi từ draft sang active; archive draft/active và restore đúng trạng thái trước.
  `owner_user_id` là owner implicit cho đến P2-05/P2-06, không tạo enrollment sớm.
  Chỉ `org_admin` hoặc owner có `class.archive`/`class.transfer_ownership`; target
  transfer phải là active member cùng tenant đủ điều kiện tạo lớp và actor phải có
  recent authentication trong 10 phút.
- Classroom UI đã có create/edit/activate/archive/restore, conflict recovery, status
  filter và pagination. LiveKit token/telemetry chỉ chấp nhận class active.
- P2-05: migration `000010` thêm enrollment lifecycle
  `invited -> active -> suspended/left/removed`, class invite code có TTL/usage limit
  và index/constraint tenant-scoped; owner tiếp tục là implicit ở `classes.owner_user_id`.
- Invite code dùng CSPRNG 256-bit và purpose-bound HMAC; raw token chỉ trả một lần
  trong create response, nằm trong URL fragment rồi được web xóa ngay. Join truyền
  token trong JSON body, khóa/cập nhật usage atomically và không tiêu thêm lượt khi
  active member, owner hoặc organization manager gọi lại.
- Direct enrollment, suspend, remove, revoke, join/rejoin và self-leave đều đi qua
  shared policy cùng repository tenant-scoped; outbox payload dùng allowlist và không
  chứa token, hash hoặc email.
- Class detail/list và LiveKit token/event dùng `viewer_access` do server resolve từ
  owner, organization manager hoặc enrollment active. Web có management panel,
  copy-once invite, public join và self-leave với đầy đủ loading/empty/error/
  forbidden/retry states bằng tiếng Việt/Anh.
- P2-06: manager-only roster API có owner implicit ghim riêng, normalized search theo
  display name/email, status filter và opaque keyset pagination bind đúng tenant/class/
  filter. Shared policy áp dụng hierarchy `org_admin > owner > teacher/co_teacher >
teaching_assistant > student/guest`, chặn self/peer/owner mutation và chỉ ownership
  transfer mới đổi owner.
- Single role update và bulk `update_role/suspend/remove` tối đa 50 user ID có ordered
  updated/unchanged/failed outcomes. Bulk commit từng item độc lập; web refetch cả khi
  mutation thành công hoặc lỗi hạ tầng. Role-changed outbox payload dùng allowlist và
  không chứa email/display name/token.
- Roster UI có search/status, infinite pagination, row/bulk confirmation, selection
  bằng keyboard, partial-failure summary và loading/empty/error/forbidden/archived
  states. Class management dùng class-scoped lifecycle capabilities từ server.
- LiveKit grant mới lấy class role authoritative và ghi effective/organization/class
  role attributes; token cấp sau mutation phản ánh role mới. JWT/participant đã tồn tại
  không được sửa hoặc thu hồi retroactively.
- P2-07 chấp nhận ADR-0014 và migration `000011`: `audit_events` tenant-owned,
  append-only bằng trigger `ALWAYS`, actor user/system, action/resource/outcome,
  request correlation, privacy-reduced source hints và flat metadata allowlist.
- Success audit của mutation P2-02 đến P2-06 được ghi cùng business transaction và
  outbox; authenticated no-op/denied/failed attempt dùng fallback transaction có
  server-generated request-instance ID để không ghi sai sau post-commit failure. Panic
  ở sensitive handler vẫn được audit rồi chuyển tiếp tới recovery middleware. Accept
  invitation bind tenant đích do server resolve; bulk roster dedupe từng target và ghi
  cả item lỗi hạ tầng/chưa được thực hiện.
- Permission `audit.view` chỉ cấp cho active `org_admin`. API
  `GET /api/v1/tenants/{tenant_id}/audit-events` reload membership authoritative,
  khóa path theo active tenant, hỗ trợ time/action/resource/outcome filter cùng opaque
  filter-bound keyset cursor và trả `no-store` projection không có IP/UA hash.
- Web có route `/app/workspace/audit`, permission gate, tenant-scoped infinite query,
  cache isolation khi switch/archive, filter validation, pagination và đầy đủ loading/
  empty/filtered-empty/error/forbidden/stale-refresh states bằng tiếng Việt/Anh.

## Kết quả acceptance staging ngày 2026-07-16

- Web và API HTTPS hoạt động; `/health` và `/ready` đạt trực tiếp và qua proxy.
- Readiness báo `database=ready` và `object_storage=ready`.
- OIDC login/callback, `/me`, reload giữ session, logout và đăng nhập lại đạt.
- Neon migrate -> rollback -> migrate đạt, version `5`, `dirty=false`; nhánh smoke
  tạm đã được kiểm tra không còn dữ liệu webhook thử nghiệm.
- B2 PUT/GET/checksum/DELETE đạt với key staging đã rotate.
- LiveKit 2-5 người đạt camera, micro, screen share và reconnect.
- LiveKit webhook HTTPS, xác minh chữ ký và idempotency đạt.
- Secret chỉ nằm trong file local bị Git-ignore hoặc secret store của provider;
  không xuất hiện trong repository, frontend bundle hoặc log kiểm thử.

## Kết luận Phase 1

Phase 1 hoàn thành ngày 2026-07-16. Biên bản và ma trận bằng chứng nằm tại
`docs/PHASE_1_COMPLETION.md`. Repository chưa có ruleset công khai; đây là ngoại lệ
được ghi nhận trong ADR-0012, không phải kiểm soát đã bật. Ngoại lệ phải hết hiệu lực
trước pilot/public beta hoặc khi có người duy trì thứ hai.

## Phase 2 đã hoàn thành ngày 2026-07-22

Backlog có thẩm quyền: `docs/PHASE_2_BACKLOG.md`.

1. P2-00 đến P2-12 đã hoàn thành; biên bản staging và đóng phase đã đạt exit gate,
   product/engineering owner sign-off ngày 2026-07-22.
2. P2-08 nối các contract workspace/invitation/class/roster/audit thành luồng UI
   org admin, teacher và student; capability guard, cache tenant/class, trạng thái
   forbidden/retry và navigation đã được chuẩn hóa.
3. [Verify #59](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888239)
   (`836ae7e`) xanh ngày 2026-07-20: Quality/integration, Browser E2E
   PostgreSQL 17 + Chromium và Local environment smoke đều đạt. Web 130/130, API
   client 15/15, UI 6/6 và E2E infrastructure 8/8 tiếp tục xanh.
4. Scenario Playwright ba role với fake OIDC loopback/PKCE đã chạy xuyên suốt
   workspace/invitation/class/roster/archive/audit trên CI. Visual QA thủ công đạt tại
   1440x900, 1024x768 và 390x844.
   [Security #54](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888233)
   cùng commit cũng xanh.
5. Acceptance UI staging P2-08 được chạy lại ngày 2026-07-20 trên fixture dùng một
   lần với ba identity ZITADEL đã xác minh riêng biệt cho org admin, teacher và
   student. Luồng tạo/chỉnh/chuyển workspace; tạo/thu hồi/chấp nhận invitation;
   teacher tạo/chỉnh/kích hoạt lớp và tạo join link; student join; teacher đổi role,
   suspend, remove, thu hồi link và archive lớp đều đạt.
6. Org admin xem được audit đúng actor, action, resource, outcome và request ID cho
   toàn bộ chuỗi thao tác. Lượt nghiệm thu không dùng SQL/manual API và không lưu
   storage state, token hay secret vào repository hoặc artifact. Deployment/contract
   drift ghi nhận ở lượt kiểm tra trước đã được đồng bộ; P2-08 chuyển `DONE`.
7. P2-09 chấp nhận ADR-0015, migration `000012`, typed catalog với global safety
   ceiling và tenant override có optimistic version. Quota member/active class/
   invitation được enforce transactionally ở server; capability API/UI fail-closed,
   thay đổi override có audit và quota rejection có metric.
8. Anonymous invitation preview/accept và class join dùng signed edge context cùng
   shared PostgreSQL limiter. Web 139/139, API client 16/16, root format/lint/
   typecheck/build/test/security bundle cùng full Go non-integration suite và `go vet`
   đều xanh cục bộ.
9. Acceptance P2-09 ngày 2026-07-21 đạt trên commit `096620a`: Render và Cloudflare
   cùng chạy head này; health/readiness/status trực tiếp và qua Pages đều trả 200.
   Neon staging ở migration `12 false`, runtime grants đúng ma trận tối thiểu và role
   không sở hữu bảng/không có quyền nguy hiểm. Signed edge/public limiter trả 404 cho
   token giả và ghi window active với `used_count=1`.
10. Hai integration test feature-control chạy bằng runtime role trên Neon staging đã
    đạt: feature disabled, tenant isolation, audit/outbox và concurrent member/class/
    invitation quota đều giữ invariant. HTTP regression xác nhận
    `403 feature_disabled`, `404 tenant_not_found`, `429 quota_exceeded`; metric quota
    rejection dùng label bounded. Bounded cleanup xóa `0` rate-limit window và `0`
    tenant-quota window; P2-09 chuyển `DONE`.
11. P2-10 ngày 2026-07-22 đã bổ sung actor/resource matrix, PostgreSQL
    security fixture có rollback, exact foreign class/user/invite ID invariants, stale
    membership và workspace-switch token rotation. HTTP mutation dùng strict JSON object,
    từ chối unknown/duplicate/trailing/oversized payload; resource UUID ở path/query chỉ
    nhận dạng canonical. Class cursor v2 bind tenant/filter và class/roster cursor dùng
    strict decoder. Chín fuzz function cho JSON, UUID, invitation token, cursor, roster
    search và media identifier đều đạt. Commit `c4205b9` đã xanh trên
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539891), gồm
    PostgreSQL 17 matrix, và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539912),
    gồm CodeQL, Trivy repository/container cùng secret scan. Không có finding
    High/Critical chưa xử lý; P2-10 chuyển `DONE`.
12. P2-11 đã chấp nhận ADR-0016 và bổ sung migration `000013`, fixture JSON ẩn danh,
    CLI `v1-fixture-import`, external-ID mapping, per-record checkpoint/resume và
    reconciliation report. Importer chặn production, từ chối payload không strict hoặc
    email ngoài `.invalid`, không đọc dữ liệu/configuration V1 thật. PostgreSQL 17 local
    tạm xác nhận full integration; commit `f07d05d` đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333712) và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333728).
    Migration `13 -> 12 -> 13`, dry-run, apply/rerun, checkpoint/resume, reconciliation
    và cleanup/reset database tạm đều đạt; P2-11 chuyển `DONE`.
13. P2-12 đã mở rộng scenario Playwright ba role để kiểm tra class invite link đi từ
    `0/2` sang `1/2` lượt dùng, roster vẫn được giữ sau archive, invite link còn active
    không thể dùng để join lớp đã archive, và audit create-class có actor, resource ID
    cùng request ID. Implementation nằm ở commit `bf30605`; các commit `7563ed1` và
    `6fb4f84` thu hẹp locator audit để tránh match nhầm action có tiền tố giống nhau.
    Candidate `6fb4f84` đã đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433), gồm
    Browser E2E PostgreSQL 17 + Chromium, và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424).
    Checkpoint `3c48964e3900b2a262c4026abf0174b3c39c5d93` tiếp tục đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093175)
    và [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093166);
    Cloudflare Pages check suite cùng full SHA cũng success.
14. Neon P2-12 ngày 2026-07-22 đã chạy trên branch dùng một lần từ staging:
    `12 false -> 13 false -> 12 false -> 13 false`. Importer dry-run lập kế hoạch 12
    record; apply nhập 10, skip 2, fail 0; rerun giữ 10 unchanged, không duplicate;
    reconciliation có 2 run, 10 mapping và 24 item, sau đó branch được xóa. Neon staging
    thật được forward `12 false -> 13 false`.
15. Lượt provider audit phát hiện default ACL cũ của Neon owner tự cấp CRUD cho runtime
    trên bảng mới. Provisioning đã thu hồi default table ACL global/schema-scoped và
    grant materialized trên ba ledger. Kết quả cuối: default ACL leak `0`,
    effective/direct ledger privilege `0`, runtime không owner/superuser/bypass RLS,
    schema `USAGE=true`, `CREATE=false`, audit chỉ `SELECT/INSERT`, future-table probe
    không có quyền. Không sửa migration lịch sử `000013` hoặc re-grant khi rollback.
16. Sau staging migration, Render đã live release candidate full SHA
    `3c48964e3900b2a262c4026abf0174b3c39c5d93` qua deploy
    `dep-d9gaiturnols73c75qp0`. Public health/readiness/status trực tiếp Render và qua
    Pages proxy đều HTTP 200 (6/6), readiness báo database/object storage ready.
17. Lượt UI staging S01-S07 chạy khoảng 19:06-19:36 ngày 2026-07-22 trên workspace
    `P2-12 Acceptance 202607221900`, class
    `f61e3344-251f-42eb-b3bc-90fd9f9cff5d`, đã đạt. Admin tạo workspace/mời hai role;
    teacher/student accept, login và switch; teacher tạo class active cùng link 1 ngày,
    2 lượt (`0/2 -> 1/2`); student join; roster đổi Trợ giảng, suspend, remove và refresh
    vẫn giữ `Đã xóa`. Cross-tenant UI conceal exact class và exact audit filter ở `KMA`
    trả 0; exact foreign roster/media-token tiếp tục dùng P2-10 automated baseline,
    không có direct staging POST room-token trong lượt này. Archive chặn identity chưa
    enroll join qua link còn `1/2` và vẫn giữ roster history. Audit workspace có 22 event,
    exact class có 5 event create/update/roster/archive, actor/resource/request ID đầy đủ
    và denied join cũng được audit.
18. S09 provider closure đạt khoảng 19:39-19:43 ngày 2026-07-22. Render live latest
    `0be98bb`, application rollback bằng `Deploy a specific commit` về `3c48964`, rồi
    forward lại `0be98bb`; mỗi bước đều đạt 6/6 probe direct Render và qua Pages. Native
    Rollback được cancel an toàn vì cảnh báo không tải được cấu hình live; không có thay
    đổi cấu hình được tuyên bố. Phiên Admin/audit vẫn hoạt động sau forward deploy.
19. Owner đã sign-off P2-12. Closure-record docs-only phải được hậu kiểm Verify/Security
    sau push; nếu một workflow thất bại thì mở lại P2-12 và khắc phục regression.

## Phase 3 bắt đầu ngày 2026-07-22

Backlog có thẩm quyền: `docs/PHASE_3_BACKLOG.md`.

1. P3-00 đã tạo task ID/dependency/acceptance/exit gate cho daily learning workspace.
2. ADR-0017 chốt P3-01 session một lần: instant UTC + IANA timezone, DST gap/overlap,
   optimistic version, tenant/class policy và boundary với recurrence/Phase 4 media.
3. ADR-0018 chốt worker process riêng trong cùng Go modular monolith: PostgreSQL lease
   có fencing token, at-least-once, retry/backoff, idempotency và dead-letter; chưa thêm
   Redis/NATS/Kafka hoặc provider mới.
4. P3-01 là vertical slice implementation đầu tiên và đang `READY`. Scope không gồm
   recurrence, reminder, calendar tổng hợp, worker runtime hoặc media lifecycle.
5. P3-CAL-00 đã audit Google Calendar, Microsoft Teams, Zoom, ClassIn, các lựa chọn
   mã nguồn mở và TutorHub V1; kết quả nằm tại
   `docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md`.
6. Đề xuất calendar-first dùng FullCalendar Standard chỉ làm renderer sau spike; domain,
   quyền, recurrence, conflict, reminder và LiveKit vẫn do TutorHub sở hữu. Chưa thêm
   dependency hoặc runtime code trong P3-CAL-00.
7. V1 chỉ được giữ làm nguồn nghiệp vụ: event/task/availability poll, quick create và
   panel agenda. Không port CalendarFX/DAO/model vì V1 hard-code user, JDBC trực tiếp,
   thiếu tenant/timezone/DST/version/audit và nhiều control chỉ là vỏ UI.
8. P3-CAL-01 phải chốt ADR-0019 về series/exception/occurrence, recurrence DST và
   conflict policy; dependency renderer/recurrence chỉ được pin sau performance,
   accessibility, license và security spike.
9. P3-CAL-00B đã nghiên cứu lại Teams/Google và CSS live Vauliys; chốt Teams-inspired
   IA/editor, Warm Academic cream palette và professional everyday parity. Không sao
   chép asset/font/trade dress và chưa đổi runtime token.
10. Invitation/update/cancellation/reminder email, ICS và RSVP đã được đưa vào Phase 3
    exit gate. Owner đã chọn AWS SES làm provider target; P3-CAL-02/ADR-0020 vẫn phải
    xác minh account/region/sandbox/quota, adapter, provider-event ingress, iTIP/iMIP và
    deliverability.
    Trước khi có domain chỉ thử bằng owner-controlled verified identities trong SES
    sandbox; production vẫn cần domain/DNS cùng SPF/DKIM/DMARC. Mọi effect runtime chỉ
    chạy sau commit qua P3-03 worker. Đây là re-baseline tài liệu, chưa phải chức năng
    đã chạy.
11. ADR-0021 đã `Accepted` và chốt P3-02D Native Availability Poll: mọi active
    authenticated tenant member, gồm student, được tạo/quản lý poll và Study Meeting của
    mình theo feature/quota. Poll có class-only, invited-only và explicit anyone-link;
    public capability chỉ lưu hash, có expiry/revoke/scope/rate limit và không lộ roster/
    email/individual availability. Chỉ actor có `session.schedule` mới finalize thành
    ClassSession; actor khác chỉ tạo Study Meeting. When2meet chỉ là comparator, không
    phải runtime/API/iframe/fork/code dependency.
12. P3-CAL-00C đã rà soát readiness lần cuối bằng nguồn chính thức và upstream: tách
    P3-02A/B/C cùng P3-05A/B, kéo P3-03 lên trước consumer side effect, bổ sung
    WorkingSchedule/suggested-time contract, audience diff, reminder lifecycle,
    split-exception preview, direct StudyMeeting API, poll close/reopen và hardening
    capability link. SES không có caller idempotency token; timeout mơ hồ dùng app effect
    ledger + trạng thái `outcome_unknown`; canonical delivery state không gọi
    mail-server acceptance là inbox. Kế hoạch đã khóa required VCALENDAR/MIME một calendar
    part, full durable provider-event path tới inbox/consumer, iterator recurrence có
    cancellation/cap, DST suggested-time total order và giới hạn đúng mức của public-poll
    cohort/dedupe. Vòng hậu kiểm đã tách `CalendarDisplayPreference` về P3-02A,
    `WorkingSchedule` về P3-02C; buộc P3-02D phụ thuộc P3-03 cho deadline auto-close và
    khóa P3-05B cho poll reopen cùng direct StudyMeeting lifecycle. Đây vẫn là tài liệu,
    chưa có Calendar runtime.

## Rủi ro đã biết

- P3-01 mới có backlog/ADR, chưa có migration/API/UI; không được mô tả scheduling như
  chức năng đã chạy cho tới khi implementation, test và staging acceptance đạt.
- Báo cáo Calendar là `PROPOSED`; FullCalendar và recurrence library chưa được chấp nhận
  thành dependency. Không code P3-02B recurrence trước ADR-0019 và technical spike.
- AWS SES đã được chọn làm provider target nhưng chưa được cấu hình/xác minh và sending
  domain chưa có. Pre-domain chỉ cho phép owner-controlled verified identities trong
  SES sandbox; không được coi là production readiness. SPF/DKIM/DMARC, SES event ingress
  theo topology ADR-0020, bounce/complaint/suppression và cross-client ICS chưa được
  kiểm thử. Không gửi
  business email tới end user trước P3-CAL-02/ADR-0020 và P3-03 worker gate.
- Warm Academic mới là visual direction; `tokens.css` và Calendar UI chưa được đổi.
- P3-02D hiện mới có ADR/backlog/design, chưa có schema/API/UI/capability exchange hoặc
  authorization test; không được mô tả Availability Poll/Study Meeting như chức năng đã
  chạy.
- External poll link có rủi ro token/PII leak và abuse. Implementation phải đạt token
  entropy cao, hash-at-rest, fragment exchange, expiry/revoke/rate limit, log redaction
  và privacy-safe aggregate theo ADR-0021. Minimum cohort/coarse bucket chỉ giảm rủi ro
  differencing/Sybil; anonymous link không thể hứa one-human-one-response.
- Quyền tạo instant study room đã được chốt làm authorization target, nhưng LiveKit
  token, lobby, moderation và media lifecycle vẫn thuộc Phase 4.
- Outbox hiện mới là writer-side queue, chưa có lease/fencing/dead-letter hoặc
  `cmd/worker`. P3-03 phải hoàn thành trước notification, email/ICS, reminder hoặc
  message/file processing side effect; Render Free web service không được xem là
  durable worker. P3-CAL-02 trước đó chỉ được chạy renderer/provider sandbox cô lập.

- Render Free spin down khi không hoạt động và có thể cold start trên 50 giây;
  chỉ chấp nhận cho staging/private alpha.
- Direct-main chưa có pre-merge protection; `pnpm verify` và CI hậu kiểm là kiểm soát
  bù tạm thời theo ADR-0012.
- Chưa chọn managed Redis và observability provider cho quy mô lớn hơn.
- P2-09 thay limiter theo process bằng fixed-window PostgreSQL và chỉ tin client prefix
  do Cloudflare Pages ký. Staging đã đồng bộ `EDGE_CONTEXT_SECRET` ở edge/Core API và
  public limiter smoke đã đạt; assertion không hợp lệ vẫn fallback về direct peer
  prefix. Redis tiếp tục hoãn cho tới khi có số liệu tải.
- Migration `000012` không hardcode runtime role theo môi trường. Staging đã cấp grants
  tối thiểu và chạy bounded cleanup cho `tenant_quota_windows` cùng
  `rate_limit_windows`; maintenance định kỳ vẫn phải theo `docs/DATABASE.md`.
- Class projection chưa lộ `archived_from_status`, nên web chỉ dùng feature gate khi
  hiển thị restore; quota active-class vẫn được server enforce transactionally. Khi
  quota đã hết, restore lớp từng active có thể nhận 409 sau submit; bổ sung projection
  class-specific nếu UX này trở thành vấn đề thực tế.
- Verify #59 đã xác nhận PostgreSQL runtime, migration/integration và Browser E2E
  trên CI. Acceptance UI staging P2-08 ngày 2026-07-20 đã xanh sau khi web/Core API
  được đồng bộ; các lần nghiệm thu sau vẫn phải đối chiếu commit/image, migration và
  configuration trước khi kết luận lỗi contract.
- Host hiện tại thiếu Docker/PostgreSQL nên không thể lặp lại full browser scenario
  ngoài CI; nếu CI không sẵn có thì đây vẫn là hạn chế chẩn đoán cục bộ.
- P2-12 đã đóng sau khi CI/Cloudflare/Render/Neon/importer/public probe, UI staging
  S01-S07 và S09 application rollback/redeploy đều đạt. Native Rollback không được dùng
  vì Render cảnh báo không tải được cấu hình live; application rollback giữ config hiện
  tại là bằng chứng phục hồi đã chấp nhận. Closure-record CI sau push là hậu kiểm bắt buộc.
- Class/roster/audit cursor vẫn là payload client đọc được; scope hash ngăn replay sai
  tenant/filter nhưng không phải chữ ký bí mật. SQL luôn giữ tenant/class predicate nên
  finding hiện được xếp Low; quyết định HMAC toàn bộ cursor được hoãn sang backlog/ADR
  riêng sau P2-10 nếu threat model yêu cầu cursor chống giả mạo.
- Production retention/export, privacy erasure, partitioning và dedicated maintenance
  role cho audit được hoãn tới Phase 8. Audit của tenant archived được giữ nhưng chưa có
  recovery/export UI ngoài active-tenant API.
- Roster role update hiện dùng last-write-wins, chưa có enrollment version/CAS. Client
  refetch sau mutation; nếu concurrent editing trở thành rủi ro thực tế thì cần ADR và
  migration riêng.
- Ownership transfer tái dùng `auth_time` của session theo semantics P2-01; chưa ép
  OIDC `max_age`/`prompt`, nên recent-auth 10 phút chưa phải step-up tuyệt đối.
- Archive hoặc roster role mutation ngăn/đổi credential LiveKit cấp mới nhưng không
  thu hồi JWT đã phát hoặc kick/cập nhật participant đang ở trong room.
- Dữ liệu V1 chưa được migrate.
- LiveKit chunk phía web còn lớn và cần performance budget ở phase sau.

## Quy tắc Git hiện tại

- Làm việc và commit trực tiếp trên `main` để ưu tiên tốc độ phát triển.
- Không bắt buộc Issue, branch hoặc Pull Request cho mỗi task.
- Trước khi push: xem `git diff`, chạy kiểm tra phù hợp và quét secret.
- Không force-push `main`; branch tạm chỉ dùng cho thử nghiệm rủi ro cao.

## Tài liệu liên quan

- `docs/MASTER_PLAN.md`
- `docs/PHASE_1_BACKLOG.md`
- `docs/PHASE_1_COMPLETION.md`
- `docs/PHASE_2_BACKLOG.md`
- `docs/P2_12_STAGING_ACCEPTANCE.md`
- `docs/PHASE_2_COMPLETION.md`
- `docs/PHASE_3_BACKLOG.md`
- `docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/DATABASE.md`
- `docs/AUTHENTICATION.md`
- `docs/E2E_TESTING.md`
- `docs/LIVEKIT_SPIKE_RUNBOOK.md`
- `docs/CI_SECURITY.md`
- `docs/adr/0011-render-core-api-staging.md`
- `docs/adr/0012-single-maintainer-direct-main-governance.md`
- `docs/adr/0013-shared-organization-class-authorization-policy.md`
- `docs/adr/0014-append-only-tenant-audit-log.md`
- `docs/adr/0015-server-evaluated-feature-controls-and-quotas.md`
- `docs/adr/0016-idempotent-v1-fixture-import.md`
- `docs/adr/0017-class-session-scheduling-and-civil-time.md`
- `docs/adr/0018-postgresql-leased-outbox-worker.md`
- `docs/adr/0021-native-availability-polls-and-member-owned-study-meetings.md`
