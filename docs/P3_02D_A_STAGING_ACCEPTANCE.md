# P3-02D-A Staging Acceptance

## Trạng thái

| Hạng mục | Giá trị |
| --- | --- |
| Phạm vi | Native Availability Poll và member-owned Study Meeting core |
| Trạng thái task | `VERIFY` |
| Migration source | `000022_availability_polls_study_meetings` |
| Database mong đợi sau migrate | `22 false` |
| Neon staging gần nhất | `21 false`; migration `000022` chưa chạy |
| Commit nghiệm thu | Chưa ghi nhận |
| Render/Cloudflare deployment | Chưa chạy cho P3-02D-A |
| PostgreSQL/ACL/cascade acceptance | Chưa chạy |
| Browser/manual accessibility acceptance | Chưa chạy |

Tài liệu này là runbook và sổ bằng chứng không nhạy cảm. P3-02D-A chỉ chuyển `DONE` khi
toàn bộ gate trong tài liệu đạt trên exact commit đã deploy. Local compile/unit/typecheck
không thay thế migration, exact-login ACL, PostgreSQL concurrency, staging privacy hoặc
manual accessibility.

### Evidence local hiện có

Final local gate ngày 2026-08-01 trên shared tree:

- `pnpm verify`: PASS; bao phủ format, generated-client drift, local/E2E-infra/security,
  monorepo lint/typecheck/test/build, Storybook, bundle scan, toàn bộ Go test và `go vet`;
- web Vitest: 45 file/225 test PASS; API client: 3 file/30 test PASS;
- sau hardening index retention cuối: `go test -count=1 ./internal/modules/calendar` và
  scoped `git diff --check` PASS;
- sau owner-time cross-writer hardening: focused Calendar/Classroom/HTTP/ownertime test,
  full `go test -count=1 ./...`, `go vet ./...`, `corepack pnpm verify` và
  `git diff --check` PASS. Integration-tag compile cho package Classroom cũng PASS;
  barrier runtime chưa chạy vì cả hai database integration URL đều không được cấu hình
  trên host này.

Các lệnh tối thiểu tương ứng là:

```powershell
corepack pnpm api:check
corepack pnpm format:check
corepack pnpm lint
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build
corepack pnpm security:test
corepack pnpm security:bundle
go test ./services/core-api/...
go vet ./services/core-api/...
git diff --check
```

PostgreSQL integration và browser staging evidence chưa chạy, phải được ghi riêng và
không được suy ra từ unit test/mock hoặc local production build.

## Phạm vi và ranh giới

P3-02D-A nghiệm thu:

- poll normalized, manual lifecycle, response, aggregate/ranking và finalize;
- class/invited/public sharing bằng capability hash-at-rest;
- organizer UI, public responder UI và individual-response projection phân trang;
- direct/finalized Study Meeting scheduling intent;
- tenant authorization, conflict, CAS/idempotency, quota/hard cap, audit và outbox;
- hard-retention maintenance purge có role tách khỏi Core API.

P3-02D-A không nghiệm thu và không được bật:

- deadline worker auto-close;
- roster fan-out, email/ICS, notification hoặc reminder delivery;
- P3-05B lifecycle consumer;
- LiveKit token, room, lobby, moderation, reconnect hoặc media lifecycle.

P3-02D-B giữ `DEFERRED/TODO`; các worker/provider/delivery consumer gate tương ứng phải
vẫn `false`. Purge một poll đã tới hard-retention boundary không phải auto-close: function
không chuyển lifecycle, không ghi outbox và không phát delivery.

## Điều kiện trước khi chạy

- Dùng Neon staging hoặc disposable branch tạo từ staging; mutation/destructive purge
  acceptance không chạy trên production hay tenant có dữ liệu thật.
- Migration job dùng direct migration-owner URL; Core API dùng pooled runtime role riêng;
  dedicated maintenance role không dùng credential của hai role trên.
- Render và Cloudflare phải chạy đúng commit cần nghiệm thu.
- Có identity riêng cho Organization Admin, Teacher có/không có `session.schedule`,
  Student/ordinary member và class membership chéo tenant để chạy denial matrix.
- Calendar protected-data key hợp lệ đã được provision cho Core API. Chỉ xác nhận biến có
  mặt; không đọc/in key, database URL, cookie, token, email, roster hoặc response payload.
- Feature `availability_polls` chỉ bật cho tenant canary sau khi nhánh kill switch/
  protected-data-prerequisite fail-closed đã được thử.
- Capability public/invited phát hành qua business API. Không chèn raw token vào SQL,
  không lấy token từ log và không lưu full public URL trong tài liệu/artifact.

## Database gate

### 1. Migration up/down/up

Trên disposable database ở version `21 false`:

```powershell
corepack pnpm db:version
corepack pnpm db:migrate
corepack pnpm db:version
go run ./services/core-api/cmd/migrate down -steps 1
corepack pnpm db:version
corepack pnpm db:migrate
corepack pnpm db:version
```

Kỳ vọng: `21 false -> 22 false -> 21 false -> 22 false`. Down `22 -> 21` xóa toàn bộ
poll/Study Meeting data, nên chỉ chạy trên disposable database sau preflight. Trên shared
staging chỉ forward migrate và ưu tiên application rollback.

Xác nhận constraint/index/composite FK, response version `1..100`, retention boundary,
feature/quota key và down cleanup đều đúng. Dùng `EXPLAIN` với fixture đủ lớn để xác nhận
batch purge có thể dùng index toàn lifecycle `(retention_until, tenant_id, id)`, không chỉ
terminal poll. Chạy migration từ database sạch trong CI.

### 2. Exact Core API runtime matrix

Áp dụng SQL least-privilege trong
[`DATABASE.md`](DATABASE.md#exact-core-api-runtime-grants). Exact expected matrix:

| Relation | SELECT | INSERT | UPDATE | DELETE |
| --- | :---: | :---: | :---: | :---: |
| `availability_polls` | yes | yes | yes | **no** |
| `availability_poll_slots` | yes | yes | yes | **no** |
| `availability_poll_participants` | yes | yes | yes | **no** |
| `availability_poll_capabilities` | yes | yes | yes | **no** |
| `availability_poll_responses` | yes | yes | yes | **no** |
| `availability_poll_answers` | yes | yes | yes | **no** |
| `availability_poll_mutation_receipts` | yes | yes | **no** | **no** |
| `study_meetings` | yes | yes | yes | **no** |
| `purge_expired_availability_polls(integer)` | — | — | `EXECUTE`: **no** | — |

Core API role cũng không được `TRUNCATE`, `REFERENCES`, `TRIGGER`, không phải owner/
superuser và không inherit migration/maintenance role. Probe effective privilege bằng
`has_*_privilege`, không chỉ dựa vào `information_schema.role_table_grants`.

### 3. Exact maintenance purge matrix

Function là `SECURITY INVOKER`; dedicated maintenance login cần đúng matrix sau:

| Object | Exact privilege |
| --- | --- |
| schema `tutorhub` | `USAGE` |
| `purge_expired_availability_polls(integer)` | `EXECUTE` |
| `availability_polls` | column `SELECT(tenant_id,id,retention_until)` + table `DELETE` |
| `study_meetings` | column `SELECT(tenant_id,source_poll_id)` + `UPDATE(source_poll_id)` |
| poll child tables | none |
| migration/other tenant tables | none |

Áp dụng exact SQL và metadata probe tại
[`DATABASE.md`](DATABASE.md#exact-maintenance-purge-grants). Runtime role tuyệt đối không
nhận `EXECUTE`/`DELETE`; maintenance role không được wildcard/broad table access.

Đăng nhập bằng **maintenance credential thực tế** trên disposable database và thử:

1. tạo fixture poll ở mỗi lifecycle với child slot/participant/capability/response/
   answer/receipt và một Study Meeting outcome;
2. batch `1` chỉ xóa một poll đã tới boundary, detach `source_poll_id` và cascade sạch
   toàn bộ poll child;
3. poll chưa tới boundary không bị xóa;
4. poll `draft/open/closed` bị bỏ quên nhưng đã tới hard boundary cũng được xóa mà không
   phát lifecycle/outbox/delivery;
5. batch `1000` hợp lệ; `NULL`, `0`, số âm và `1001` fail SQLSTATE `22023`;
6. hai maintenance transaction không tranh row đã lock nhờ `SKIP LOCKED`;
7. maintenance không thể xóa child trực tiếp hay đọc business columns ngoài allowlist;
8. Core API login không thể gọi function hoặc `DELETE` poll.

Nếu FK cascade đòi quyền ngoài matrix vì ownership/trigger khác source baseline, dừng
rollout và review migration/provisioning; không cấp wildcard để làm smoke xanh.

## API, authorization và concurrency gate

### Poll ownership và class scope

- Active Teacher, Student và ordinary member tạo/list/detail/update/cancel poll của mình.
- External/anonymous responder không thể tạo poll/meeting hoặc gọi authenticated command.
- Poll bind `class_id` chỉ khi creator là active class member có `class.view`; foreign/
  inaccessible class và cross-tenant poll ID đều conceal `404`.
- Organization Admin không được giả làm class member để response. Safety-admin recovery
  cancel/revoke bắt buộc reason bounded và audit metadata allowlist.
- Kill switch hoặc thiếu protected-data key fail closed trước protected operation/public
  exchange; public lỗi dùng response generic không tạo enumeration oracle.

### Lifecycle, response và projection

- `draft -> open -> closed -> finalized/cancelled`, manual close/reopen/cancel đúng state;
  finalized không bị cancel ngược qua poll command.
- Poll hết deadline từ chối response nhưng không tự close. Không có deadline worker event.
- Structural edit sau response bị chặn; retired slot/participant không quay lại projection
  hoặc được chọn finalize.
- Internal response CAS và public response-session handle không ghi đè responder khác;
  idempotent replay không nhân row, stale version trả `409`, version 100 chặn edit tiếp.
- Ordinary participant chỉ thấy response của mình và aggregate privacy-safe.
- Individual response endpoint dùng page 25/keyset cursor bind tenant/poll/scope; owner,
  safety admin và visible class member có authoritative `session.schedule` đọc được.
  Actor khác và public projection không nhận roster/email/individual response.
- Public cohort dưới 3 bị suppress; trên ngưỡng chỉ trả `low/medium/high`, không exact count.
  Acceptance/UI nêu rõ đây không bảo đảm chống Sybil/differencing/one-human-one-vote.

### Capability và abuse boundary

- Raw token chỉ trả một lần, entropy 32 byte, versioned/HMAC hash-at-rest, TTL tối đa 30
  ngày, scope/purpose/participant binding và constant-time verification.
- Link dùng fragment; SPA scrub đồng bộ trước network; token không vào query/referrer/
  localStorage/sessionStorage/IndexedDB/log/metric/analytics.
- Expired/revoked/wrong-scope link trả uniform unavailable; revoke replay là no-op và không
  tăng poll version/outbox lần hai.
- Broad link chỉ mint response session 30 phút; không dùng broad token để sửa response.
- Rate-limit prefix/token/poll đạt, retry có `Retry-After` bounded khi contract quy định.
- Hard cap được chứng minh: access capability active 500/history 1.000; response session
  active 500/history 1.000; slot history 10.000; participant history 5.000; response
  version 100. Expired/revoked row reuse không phá child/reference integrity.

### Ranking, finalize và Study Meeting

- Ranking deterministic theo unavailable/preferred/available/start/UUID; explanation chỉ
  có bounded counts/reason, không identity.
- Finalize recheck slot active, expected poll version, conflict và idempotency trong
  transaction. Concurrent/retry finalize tạo đúng một outcome.
- Chỉ actor có `session.schedule` trên class đích tạo ClassSession; actor khác chỉ tạo
  Study Meeting của mình. External responder không finalize.
- Direct và finalized Study Meeting dùng chung active/create-rate quota; lowering quota
  không xóa meeting và không chặn active-to-active reschedule không mở rộng usage.
- Owner conflict chặn overlap với scheduled Study Meeting và one-time/recurring
  ClassSession nơi owner là organizer/active attendee, kể cả source hiển thị free.
- Study Meeting và writer one-time/recurring ClassSession, internal audience addition,
  organizer transfer đã dùng chung advisory authority theo tenant/user, stable UUID order
  và reverse-recheck StudyMeeting trong source local. Two-writer barrier test giữ exact
  owner lock rồi chạy StudyMeeting/ClassSession thật đã có và integration-tag compile
  PASS; gate vẫn **chưa đóng** cho tới khi test này chạy trên PostgreSQL thật và chứng minh
  chỉ một writer commit, không còn cặp lịch overlap sau race.
- Owner list/detail/update/cancel đúng scope; safety admin chỉ recovery cancel với reason/
  audit. Cross-owner/cross-tenant ID bị conceal.
- Không endpoint nào mint LiveKit token hoặc tuyên bố media room đã tồn tại.

## UI, accessibility và security-header gate

- Organizer tạo poll bằng timezone/civil-time hợp lệ, mở/chia sẻ/copy one-time link,
  close/reopen/cancel, response, xem aggregate/ranking/individual page và finalize.
- Desktop heatmap drag/paint; keyboard có action tương đương; mobile dùng list/card;
  state có text/label, không chỉ màu; forced-colors vẫn phân biệt được.
- Loading/empty/error/forbidden/retry/feature-off/quota/conflict `409` có UI rõ, không mất
  draft và focus trở lại vị trí thao tác.
- Public route scrub fragment trước React/network, không có third-party pre-exchange và
  trả header `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, `X-Robots-Tag:
  noindex` cùng CSP/frame/form/base policy theo ADR-0021.
- Chạy automated axe/Playwright desktop/mobile/keyboard/forced-colors và manual NVDA trên
  organizer/public production route. Không lưu screenshot chứa token/roster/response.

## Observability và side-effect gate

- Audit có action allowlist cho manual lifecycle/share/finalize/Study Meeting; metadata
  không chứa raw token, email, title/description hoặc answer detail.
- Log/error/metric/proxy artifact không chứa fragment/token, protected payload, roster hay
  individual availability. Request ID và bounded outcome/reason vẫn quan sát được.
- Outbox manual lifecycle row atomic với business mutation; replay không nhân event.
- Không có outbox consumer/worker auto-close, roster fan-out, email/ICS, notification,
  reminder hoặc LiveKit side effect. Xác nhận các deployment/consumer flag này vẫn false.

## Deployment và exit gate

1. Chạy fresh local/CI gates và ghi exact kết quả không nhạy cảm.
2. Migrate disposable up/down/up; cấp/probe exact runtime + maintenance roles; chạy
   PostgreSQL concurrency/cascade matrix.
3. Forward Neon staging tới literal `22 false`; smoke Core API bằng exact runtime role.
4. Deploy exact commit lên Render/Cloudflare; xác nhận `/health`/`/ready` và public headers.
5. Chạy API/browser/manual matrix bằng tenant canary rồi tắt/revoke fixture capability.
6. Ghi exact commit/deployment/version và tổng hợp PASS/FAIL ở phần dưới.

Chỉ chuyển P3-02D-A `VERIFY -> DONE` khi tất cả bước trên PASS. P3-02D-B, P3-05B,
email/notification/reminder worker và LiveKit vẫn giữ trạng thái/gate riêng, không được
suy ra từ P3-02D-A.

## Kết quả chạy

Chưa chạy migration `000022`, exact-login ACL/cascade, staging deployment/browser hoặc
manual accessibility. Giữ trạng thái `VERIFY`; cập nhật phần này sau từng gate thực tế,
không điền trước `PASS`, commit, URL hoặc số liệu.
