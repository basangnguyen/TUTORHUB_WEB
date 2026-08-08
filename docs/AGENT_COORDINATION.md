# Quy trình phát triển TutorHub V2

## 1. Mô hình làm việc hiện tại

Từ ngày 2026-07-16, dự án được duy trì bởi một coding agent. GitHub là nơi lưu
mã nguồn và lịch sử thay đổi; không còn quy trình phân chia ownership giữa nhiều
agent.

- Repository: `https://github.com/basangnguyen/TUTORHUB_WEB`.
- Thư mục chuẩn: `D:\TutorHub_V2`.
- Nhánh phát triển mặc định: `main`.
- Commit và push trực tiếp vào `main` sau khi kiểm tra đạt.
- Không bắt buộc tạo Issue, feature branch hoặc Pull Request cho từng task.
- Không force-push `main` và không commit secret.

Branch tạm vẫn được phép khi thử migration, dependency upgrade hoặc thay đổi có
nguy cơ cao. Sau khi xác minh, thay đổi phải được đưa về `main` và branch tạm có
thể xóa.

## 2. Trình tự cho mỗi task

1. Đọc `AGENTS.md`, `README.md`, `docs/PROJECT_STATE.md`, backlog và ADR liên quan.
2. Chạy `git status` và đọc diff hiện có trước khi sửa.
3. Xác định phạm vi file, contract, migration và rủi ro.
4. Thực hiện thay đổi nhỏ, bám kiến trúc đã chấp nhận.
5. Chạy formatter, lint, typecheck, test/build hoặc smoke test phù hợp với rủi ro.
6. Kiểm tra diff và secret trước khi commit.
7. Cập nhật `docs/PROJECT_STATE.md` cùng checklist phase liên quan.
8. Commit và push trực tiếp `main`.

## 3. Trạng thái Phase 1

| Task                    | Trạng thái | Ghi chú                                                              |
| ----------------------- | ---------- | -------------------------------------------------------------------- |
| P1-01 Toolchain         | DONE       | Monorepo, formatter, lint, test và CI foundation                     |
| P1-02 Web shell         | DONE       | React shell, routing, query, i18n, responsive states                 |
| P1-03 Design system     | DONE       | Tokens, UI primitives, Storybook, accessibility                      |
| P1-04 Core API          | DONE       | Go API, config, middleware, health/readiness                         |
| P1-05 Contract/database | DONE       | OpenAPI, generated client, migrations, Neon role split               |
| P1-06 Authentication    | DONE       | ZITADEL local + staging OIDC, BFF session/CSRF/logout                |
| P1-06B Class slice      | DONE       | List/create/detail, authorization, tenant isolation                  |
| P1-07 LiveKit           | DONE       | Media flow 2-5 người và webhook staging đều đạt                      |
| P1-08A CI/security      | DONE       | Verify/Security pipeline và secret controls                          |
| P1-08B Staging deploy   | DONE       | Cloudflare Pages + Render + smoke/rollback                           |
| P1-09 Local DX          | DONE       | Compose PostgreSQL/Redis, one-command setup, seed và troubleshooting |
| P1-10 Cloud foundation  | DONE       | Neon, B2, Cloudflare, Render, ZITADEL, LiveKit                       |

Phase 1 đã hoàn thành ngày 2026-07-16. Ma trận bằng chứng nằm trong
`docs/PHASE_1_COMPLETION.md`. Repository chưa có ruleset công khai; direct-main là
ngoại lệ có thời hạn theo ADR-0012 và không được mô tả như branch protection đã bật.

## 4. Trạng thái Phase 2

| Task                           | Trạng thái | Ghi chú                                                    |
| ------------------------------ | ---------- | ---------------------------------------------------------- |
| P2-00 Policy/contract baseline | DONE       | Policy deny-by-default và role matrix dùng chung           |
| P2-01 Profile/identity         | DONE       | Profile, identity linking và migration `000006`            |
| P2-02 Tenant lifecycle         | DONE       | Lifecycle/switch, migration `000007`; `pnpm verify` xanh   |
| P2-03 Membership invitation    | DONE       | Invitation/accept/revoke, migration `000008`; verify xanh  |
| P2-04 Class lifecycle          | DONE       | Lifecycle/ownership, migration `000009`; verify xanh       |
| P2-05 Enrollment/invite code   | DONE       | Enrollment/invite, migration `000010`; verify xanh         |
| P2-06 Roster/class roles       | DONE       | Roster/hierarchy/single-bulk UI; verify xanh               |
| P2-07 Audit log                | DONE       | Append-only audit, query/UI org admin, migration `000011`  |
| P2-08 Admin/teacher E2E UI     | DONE       | CI và acceptance staging ba role đều xanh                  |
| P2-09 Feature flag/quota       | DONE       | Staging migration/config/acceptance đều đạt                 |
| P2-10 Tenant isolation/IDOR    | DONE       | Commit `c4205b9`; Verify/Security CI đều xanh               |
| P2-11 V1 fixture import        | DONE       | Commit `f07d05d`; PostgreSQL 17 Verify/Security đều xanh    |
| P2-12 Staging closure          | DONE       | UI, rollback/redeploy, sign-off và exit gate đều đạt         |

Nguồn thực thi: `docs/PHASE_2_BACKLOG.md`.

Phase 2 đã hoàn thành ngày 2026-07-22. CI/Cloudflare/Neon/importer/Render, UI staging
ba role, tenant/IDOR probes và S09 application rollback/redeploy đều đạt; owner đã
sign-off. Native Render Rollback không được dùng do cảnh báo không tải được cấu hình
live; rollback bằng specific commit giữ cấu hình hiện tại là bằng chứng phục hồi đã
được chấp nhận. Hồ sơ nằm tại `docs/P2_12_STAGING_ACCEPTANCE.md` và
`docs/PHASE_2_COMPLETION.md`.

## 5. Trạng thái Phase 3

| Task                                 | Trạng thái  | Ghi chú                                       |
| ------------------------------------ | ----------- | --------------------------------------------- |
| P3-00 Backlog/architecture baseline  | DONE        | Backlog, ADR scheduling và ADR worker         |
| P3-CAL-00 Calendar research/design   | DONE        | Product/UX/technical/V1/OSS report            |
| P3-CAL-00B Calendar re-baseline      | DONE        | Tài liệu only; chưa có theme/email runtime    |
| P3-CAL-00C Calendar readiness review | DONE        | Gate/dependency/contract đã được harden       |
| P3-CAL-01 Spike + ADR-0019           | DONE        | V7 được chấp nhận; manual NVDA gate PASS      |
| P3-01 Session scheduling và timezone | DONE        | One-time session staging acceptance đạt       |
| P3-CAL-02 Email/ICS + ADR-0020       | VERIFY      | Local renderer/sink/SES adapter; live SES/domain gates mở |
| P3-02D-A Native Availability Poll    | DONE        | Exact staging, browser/API và manual NVDA gates PASS |
| P3-02D-B Poll lifecycle delivery     | DEFERRED/TODO | Chưa code; auto-close/fan-out chờ worker      |
| P3-03A Worker repository foundation  | VERIFY      | Implementation local/CI đã có                 |
| P3-03B Durable worker acceptance     | DEFERRED/VERIFY | Host/role/grants/crash gate mở             |
| P3-04 In-app notification            | VERIFY      | API ACL staging xanh; worker/canary gate mở   |
| P3-02A Calendar shell/read projection | DONE      | Route E2E/perf/visual/staging gates đạt        |
| P3-02B Recurrence + class conflict     | DONE      | PostgreSQL/staging acceptance đạt              |
| P3-02C Working hours/free-busy/RSVP    | DONE      | Automated, staging/disposable DB và manual gates PASS |
| P3-06/07A/08/09/11A/12/13            | DONE        | Runnable product slices đã qua exact acceptance |
| P3-14-CORE                            | DONE        | Candidate `f5f1eb3` exact CI/deploy/live PASS   |
| P3-05A/B, P3-07B, P3-10/11B          | DEFERRED/TODO | Chưa code; phụ thuộc worker/provider        |
| P3-14 full closure                    | TODO        | Chỉ đóng sau toàn bộ carry-over               |

Nguồn thực thi: `docs/PHASE_3_BACKLOG.md`. Trước khi code calendar phải đọc
`docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md` và ADR-0017; P3-02B recurrence phải chờ
P3-CAL-01/ADR-0019; invitation/RSVP/iCalendar/AWS SES adapter phải chờ
P3-CAL-02/ADR-0020. Local adapter/renderer contract đã đạt trong spike cô lập; AWS SES
mới là provider target do owner chọn, chưa được live-configure hay chấp nhận làm runtime:
trước domain chỉ dùng owner-controlled verified identities trong SES sandbox; production
vẫn cần domain/DNS, SPF/DKIM/DMARC và deliverability gate.
P3-02D tuân ADR-0021 và chỉ bắt đầu sau calendar conflict/participant foundation
P3-02B/C. Poll là module native của TutorHub; không iframe, scrape, fork hoặc phụ thuộc
runtime When2meet.
Mọi asynchronous delivery/notification/email/reminder và worker-driven file-processing
side effect tới end user phải chờ P3-03B theo ADR-0018. Message persistence, poll core và
B2 transfer core vẫn được triển khai theo lane runnable với feature gate tương ứng.
P3-04 implementation đã đạt local `VERIFY` theo ADR-0022, nhưng không được coi đó là
runtime activation. Worker chỉ đăng ký canary khi
`OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY=true`; product visibility chỉ mở khi
`FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS=true`. Cả hai mặc định false và phải
giữ false cho tới khi worker role/worker grants, durable host cùng crash/reclaim
acceptance đạt.
`X-TutorHub-Expected-Tenant-ID` chỉ là assertion chống workspace/cache race; authorization
vẫn lấy từ active tenant của session. P3-01 không gồm recurrence, calendar tổng hợp, email, reminder
hoặc media lifecycle.
P3-01 đã `DONE` ngày 2026-07-24: Neon `14 false`, runtime grants tối thiểu,
Render/Cloudflare deployment/public probes và browser acceptance Teacher
create/update/cancel, Student read-only, foreign-ID conceal `404` đều đạt. Lượt browser
thủ công không được mô tả là Playwright staging.
P3-CAL-01 đã `DONE` ngày 2026-07-24 ở cấp decision spike. ADR-0019 chấp nhận
FullCalendar Standard v7.0.1, adapter/domain boundary, Warm Academic theme và
recurrence cap query-window/series-horizon/per-series/per-request/deadline lần lượt
`366 ngày/730 ngày/512/2.000/250 ms`. V7 đạt browser performance, accessibility
automated, dependency/license và Go fuzz/benchmark; full rerun hậu fix đạt
`9 passed (23.6s)`. Comparator parity-config v6 đạt `4 passed` nhưng bị loại vì render
500 item `1.492 > 500 ms` và long-task 2.000 item `404 > 400 ms`, dù heap nhỏ hơn.
COUNT phải chứng minh occurrence cuối nằm trong horizon 730 ngày; YEARLY golden đã có.
Agenda mở progressive `24 -> 48 -> toàn bộ`, Axe waiver khóa exact node/count/scope.
Manual NVDA gate đã PASS. P3-02A đã nối exact FullCalendar Standard v7.0.1 vào route
production cùng drag/resize expected-version, optimistic revert, undo và keyboard Agenda.
Production-route Playwright 8/8, numeric benchmark 500/1.000/2.000 item, visual
desktop/tablet/mobile và staging/browser acceptance đều đạt ngày 2026-07-26; P3-02A đã
`DONE`. Recurrence, participant/RSVP, runtime email/ICS và Availability Poll vẫn thuộc
task sau.
Mọi active authenticated member có thể tạo/quản lý poll và Study Meeting của mình; chỉ
actor có `session.schedule` mới tạo ClassSession. Full LiveKit token/lobby/moderation/
room lifecycle vẫn thuộc Phase 4.
Sau khi P3-CAL-01 và P3-01 cùng đạt gate, P3-CAL-02 có thể chạy trước P3-03 vì chỉ là
ADR và test renderer/provider sandbox cô lập; không nối Core API/outbox hoặc gửi business
email tới end user. Local gate đã đạt `VERIFY` ngày 2026-07-26 với deterministic
audience/iCalendar/MIME, 7 golden lineage, sink và SES v2 Raw adapter/no-retry; ADR-0020
giữ `Proposed`. SES sandbox chỉ được dùng với identity thử nghiệm do owner kiểm soát và
đã verify; account/quota/topology/domain/cross-client live evidence còn mở. Đường gửi
runtime chỉ nằm ở P3-05A/P3-05B sau khi P3-03B và P3-02C đạt gate.
P3-03A repository/runtime foundation đã đạt `VERIFY`: migration `000015`, worker process,
lease/fencing/retry/dead-letter, startup ACL probe, CI integration và runbook đã có.
Không chuyển umbrella P3-03 sang `DONE` hoặc bật end-user side effect trước khi P3-03B
worker role/worker grants, durable host không spin-down và crash/reclaim acceptance đạt.
P3-04 chưa `DONE`: Neon staging hiện ở `23 false`; migration `000016` và exact API
runtime grants đã probe xanh, nhưng worker role/worker grants, durable worker và canary
duplicate/crash-reclaim chưa được nghiệm thu. Khi đổi worker gate phải dừng process và
đổi exact notification grant cùng lúc; quyền dư/thiếu đều phải làm startup probe fail
closed. P3-02D-A đã `DONE` sau exact staging/browser/API/NVDA acceptance; dependency
runnable tiếp theo là `P3-06`. P3-03B durable-host,
worker role/grants/crash-reclaim, P3-04 activation, live SES/domain và delivery-dependent
processing được giữ `DEFERRED/VERIFY` hoặc `DEFERRED/TODO` đúng theo mức implementation
cho tới khi có môi trường phù hợp. Khi các slice
runnable đạt `P3-14-CORE`, Phase 4 được phép bắt đầu; full Phase 3 vẫn chờ carry-over
register và không bật asynchronous delivery/notification/email/reminder hoặc Class Files
sharing/processing tới end user. Render Web Service tiếp tục là Core API
staging/private alpha, không được dùng làm durable worker.
P3-CAL-02 tiếp tục giữ live SES/domain/interoperability gate song song, không bật runtime.
P3-02C đã `DONE` ngày 2026-07-30. Neon staging `21 false`, exact ACL/runtime-role
isolation, automated local gate và Calendar E2E đều PASS. Operator xác nhận toàn bộ ma
trận staging/disposable PostgreSQL và manual accessibility/privacy còn lại PASS, gồm
concurrency/IDOR, capability lifecycle/rate, organizer transfer/cancel/archive và log
privacy. Exact runtime acceptance commit `7859c233` đã Live trên Render và Cloudflare;
Render health trả HTTP `200`. Biên bản không nhạy cảm nằm tại
`docs/P3_02C_STAGING_ACCEPTANCE.md`.

Quyết định vận hành ngày 2026-07-31 là giữ Render Web Service cho Core API staging/
private alpha và không chuyển provider trong lượt hiện tại. Render Free không phải
durable worker. Test local/CI/disposable vẫn bắt buộc; gate cần host không spin-down,
worker live grants, crash/reclaim/duplicate canary hoặc SES/domain production được ghi
`DEFERRED/VERIFY`, không được coi là PASS do không chạy. Hai feature gate notification
và Class Files activation tiếp tục `false`; không bật asynchronous delivery/email/reminder
hoặc worker-driven file processing/sharing tới end user.

## 6. Hạ tầng staging đã chốt

- Web: `https://tutorhub-web.pages.dev`.
- Core API: `https://tutorhub-core-api.onrender.com`.
- Database: Neon PostgreSQL staging branch.
- Storage: Backblaze B2 staging bucket.
- Identity: ZITADEL `tutorhub-staging`.
- Media: LiveKit Cloud staging project.
- Tất cả smoke test acceptance đã đạt ngày 2026-07-16.

## 7. Quy tắc tránh mất mã

- Push sau mỗi checkpoint hoàn chỉnh, không gom quá nhiều thay đổi không liên quan.
- File `.env*.local`, token, key và URL chứa credential phải được Git-ignore.
- Không dùng `git reset --hard`, force-push hoặc xóa lịch sử để xử lý lỗi.
- Nếu test chưa đạt, không ghi task là `DONE`; ghi rõ trạng thái và lỗi trong
  `docs/PROJECT_STATE.md`.
