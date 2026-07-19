# Backlog Phase 2 - Identity, tenant và class core

> Nguồn thực thi chi tiết cho Phase 2. Master Plan giữ mục tiêu và exit gate; tài
> liệu này giữ dependency, API, migration, kiểm thử và Definition of Done.

## 1. Mục tiêu phase

Tạo nền multi-tenant và quản lý lớp đủ dùng cho pilot nội bộ:

1. organization admin quản lý tenant và thành viên;
2. teacher tạo/lưu trữ lớp, mời student và quản lý roster;
3. người dùng chuyển workspace mà không lẫn dữ liệu hoặc quyền;
4. mọi API tenant/class chống IDOR và cross-tenant access;
5. hành động nhạy cảm có audit truy vấn được;
6. fixture V1 đầu tiên có thể import lặp lại an toàn.

**Thời lượng kế hoạch:** 4-6 tuần tập trung, chia thành 6 sprint kỹ thuật.

**Task đã hoàn thành:** P2-00 Policy and contract baseline; P2-01 User profile và
identity linking; P2-02 Tenant lifecycle và workspace switching; P2-03 Membership
invitation, accept và revoke; P2-04 Class lifecycle, ownership và archive; P2-05
Enrollment và class invite code; P2-06 Roster và class-level roles; P2-07 Audit log
cho hành động nhạy cảm.

**Task đang xác minh:** P2-08 Admin và teacher UI end-to-end. Implementation đã
có; còn Browser E2E local/staging acceptance.

**Task sau khi P2-08 đạt DoD:** P2-09 Feature flag và quota framework.

## 2. Non-goal

- Lịch học, persistent chat, notification và Drive đầy đủ thuộc Phase 3.
- Moderation/media classroom hoàn chỉnh thuộc Phase 4.
- Whiteboard, breakout, recording và classroom tools thuộc Phase 5.
- Assignment, exam, QuizHub và Secure Exam không nằm trong Phase 2.
- Không nhập toàn bộ dữ liệu V1; chỉ xây import contract và fixture đại diện.
- Không tối ưu cho public beta hoặc multi-region trong phase này.
- Không tách microservice; tiếp tục Go modular monolith.

## 3. Nguyên tắc bắt buộc

- OpenAPI đổi trước hoặc cùng lúc với implementation; generated client không sửa tay.
- Tenant ID lấy từ session/context đã xác minh, không tin header/body do client tự khai.
- Authorization đi qua policy layer; không rải `if role == ...` trong handler/repository.
- Repository tenant-scoped và luôn nhận tenant/class scope tường minh.
- Mutation nhạy cảm dùng transaction, idempotency khi có retry và ghi audit.
- Invitation/code lưu dạng hash; không log token thô.
- Migration có forward/down path hoặc ghi rõ vì sao irreversible.
- Mỗi vertical slice có loading/empty/error/forbidden và keyboard accessibility.
- Mỗi task chỉ `DONE` sau khi `pnpm verify` và test rủi ro liên quan đạt.

## 4. Trạng thái tổng hợp

| Task  | Nội dung                                | Dependency                 | Trạng thái |
| ----- | --------------------------------------- | -------------------------- | ---------- |
| P2-00 | Policy and contract baseline            | Phase 1                    | DONE       |
| P2-01 | User profile và identity linking        | P2-00                      | DONE       |
| P2-02 | Tenant lifecycle và workspace switching | P2-00                      | DONE       |
| P2-03 | Membership invitation/accept/revoke     | P2-02                      | DONE       |
| P2-04 | Class lifecycle, ownership và archive   | P2-00, P2-02               | DONE       |
| P2-05 | Enrollment và class invite code         | P2-03, P2-04               | DONE       |
| P2-06 | Roster và class-level roles             | P2-05                      | DONE       |
| P2-07 | Audit log cho hành động nhạy cảm        | P2-02 đến P2-06            | DONE       |
| P2-08 | Admin/teacher UI end-to-end             | P2-02 đến P2-07            | VERIFY     |
| P2-09 | Feature flag và quota framework         | P2-00, P2-02               | TODO       |
| P2-10 | Tenant isolation/IDOR security suite    | Xuyên suốt; chốt sau P2-09 | TODO       |
| P2-11 | V1 fixture import idempotent            | Schema ổn định sau P2-06   | TODO       |
| P2-12 | Staging acceptance và đóng phase        | P2-01 đến P2-11            | TODO       |

## 5. P2-00 Policy and contract baseline

**Mục tiêu:** thống nhất mô hình quyền trước khi mở rộng API và UI.

### Công việc

- [x] Chốt permission matrix organization/class trong `docs/DOMAIN_MODEL.md`.
- [x] Phân biệt organization role và class role:
  - organization: `org_admin`, `teacher`, `student`, `guest`;
  - class: `owner`, `co_teacher`, `teaching_assistant`, `student`.
- [x] Chốt quy tắc effective permission khi một người có nhiều membership/role.
- [x] Tạo policy interface dùng chung cho identity, classroom và media modules.
- [x] Di chuyển permission constants/mapping rải rác về policy package.
- [x] Định nghĩa authorization input gồm actor, active tenant, resource tenant,
      resource class, action và resource state.
- [x] Định nghĩa deny-by-default, error mapping `403`/`404` để tránh resource enumeration.
- [x] Cập nhật OpenAPI security/error conventions và test helpers.
- [x] Viết ADR nếu class role model khác mô hình miền hiện tại.

### Kiểm thử

- [x] Table-driven unit tests cho toàn bộ permission matrix.
- [x] Deny tests khi thiếu actor, active tenant, membership hoặc resource scope.
- [x] Regression tests cho class list/create/detail và LiveKit token endpoint hiện có.
- [x] Static repository search bảo đảm role check không còn nằm ngoài policy layer.

### Definition of Done

- [x] Permission matrix được tài liệu hóa và có test tương ứng từng hàng.
- [x] Classroom/identity/media dùng cùng policy interface.
- [x] Không thay đổi hành vi hợp lệ của Phase 1.
- [x] `pnpm verify` xanh ngày 2026-07-16.

## 6. P2-01 User profile và identity linking

**Mục tiêu:** người dùng quản lý hồ sơ cá nhân và xem/quản lý danh tính đã liên kết.

### Contract đề xuất

- `GET /api/v1/me/profile`
- `PATCH /api/v1/me/profile`
- `GET /api/v1/me/identities`
- `POST /api/v1/me/identities/link`
- `DELETE /api/v1/me/identities/{identityId}`

### Công việc

- [x] Bổ sung profile fields tối thiểu: display name, locale, timezone, avatar key.
- [x] Chuẩn hóa Unicode, độ dài và locale/timezone allow-list.
- [x] Identity unique theo `(issuer, subject)`; không cho một identity gắn hai user.
- [x] Link/unlink yêu cầu recent authentication và state/nonce chống CSRF.
- [x] Không cho unlink identity cuối cùng nếu không có phương thức đăng nhập thay thế.
- [x] Avatar chỉ lưu object key/metadata, không lưu public URL vĩnh viễn.
- [x] UI hồ sơ có optimistic state thận trọng, validation và error recovery.
- [x] Audit link/unlink identity và thay đổi trường nhạy cảm.

### Kiểm thử và DoD

- [x] Unit validation, repository integration và OIDC state/nonce negative tests.
- [x] Cross-user identity collision trả lỗi ổn định, không lộ user khác.
- [x] Profile cập nhật được qua web; query cache được đồng bộ sau mutation và dữ liệu
      được đọc lại từ API khi tải lại phiên.
- [x] i18n vi/en, keyboard/focus và loading/error states đạt.
- [x] Migration `000006_user_profiles_and_identity_linking` có cả đường `up` và `down`.
- [x] OpenAPI, generated TypeScript client, Go API và React settings UI dùng cùng contract.
- [x] `pnpm verify` xanh ngày 2026-07-17: 23 web tests, 9 API-client tests,
      Go test/vet, lint, typecheck, production build, Storybook và security checks đều đạt.

## 7. P2-02 Tenant lifecycle và workspace switching

**Mục tiêu:** hoàn thiện create/list/update/archive tenant và chuyển workspace an toàn.

### Contract đề xuất

- `GET /api/v1/tenants`
- `POST /api/v1/tenants`
- `GET /api/v1/tenants/{tenant_id}`
- `PATCH /api/v1/tenants/{tenant_id}`
- `POST /api/v1/tenants/{tenant_id}/archive`
- Giữ endpoint select/switch hiện có và chuẩn hóa response/session rotation.

### Công việc

- [x] Bổ sung slug/name, locale, timezone, status, `version` và `archived_at` cho tenant.
- [x] Tenant create tạo membership `org_admin` trong cùng transaction.
- [x] Switch chỉ chấp nhận tenant có membership active và xoay session/CSRF.
- [x] Archive từ chối khi không đủ quyền; không hard-delete dữ liệu nghiệp vụ.
- [x] Chặn archive active tenant cuối cùng nếu làm user mất đường quản trị.
- [x] Repository/query bắt buộc active tenant context; `context_version` CAS chặn
      concurrent session-context mutation ghi đè nhau.
- [x] UI workspace switcher áp dụng response mới, hủy/xóa cache tenant-scoped và có
      loading/error/retry states.

### Kiểm thử và DoD

- [x] Concurrent create/switch và retry không tạo duplicate membership hoặc để response
      cũ ghi đè active workspace mới hơn.
- [x] Tenant A không đọc/sửa/archive tenant B; `tenant.view` và `tenant.manage` đi qua
      shared policy với 403/404 concealment.
- [x] Reload sau switch giữ đúng workspace và không hiện cache của tenant trước.
- [x] Success event create/update/archive/switch có actor, tenant và version chính xác,
      ghi durable qua outbox; audit query/failure event đầy đủ đã hoàn tất ở P2-07.

**Verification:** `pnpm verify` xanh ngày 2026-07-18, gồm web 38/38, API client
10/10, UI 6/6, lint/typecheck/build/Storybook, Go test/vet và security checks.
Integration-tag của migration/classroom/identity compile xanh; PostgreSQL execution và
clean migration được workflow CI có PostgreSQL 17 xác nhận sau khi push checkpoint.

## 8. P2-03 Membership invitation, accept và revoke

**Mục tiêu:** organization admin mời thành viên vào tenant bằng luồng một lần, có hạn.

### Schema triển khai

`membership_invitations`: tenant, normalized email, intended role, token hash,
status, expires_at, accepted_at, revoked_at, invited_by, accepted_by, revoked_by
và timestamps; migration `000008` khóa state/timestamp, role và actor membership.

### Contract triển khai

- `GET /api/v1/tenants/{tenantId}/invitations`
- `POST /api/v1/tenants/{tenantId}/invitations`
- `POST /api/v1/tenants/{tenantId}/invitations/{invitationId}/revoke`
- `POST /api/v1/membership-invitations/preview`
- `POST /api/v1/membership-invitations/accept`

Hai token endpoint nhận `{ "token": "..." }` trong JSON body; share URL dùng
`/invite#token=...` và web xóa fragment ngay. Không đưa bearer token vào path/query,
request log, browser history hoặc referrer.

### Công việc

- [x] Sinh token CSPRNG 256-bit, chỉ lưu purpose-bound HMAC và redaction trong log/audit.
- [x] TTL `MEMBERSHIP_INVITATION_TTL` cấu hình 15 phút đến 30 ngày; state machine
      `pending/accepted/revoked/expired` có invariant DB.
- [x] Accept yêu cầu session + CSRF và active verified linked identity khớp exact
      normalized provider email; transaction/idempotency không tự đổi active tenant.
- [x] Không tạo membership trùng; một pending invitation trên tenant/email, existing
      membership luôn conflict, revoked/expired address được re-invite.
- [x] Chỉ `org_admin` có `tenant.manage_members`; flow này chỉ cấp
      `teacher/student/guest`, không cấp `org_admin`.
- [x] Notification adapter chỉ là interface/outbox; gửi email thật thuộc phase sau.
- [x] UI admin list/create/copy-once/revoke và trang preview/accept có i18n vi/en,
      loading/empty/error/forbidden/offline/retry phù hợp.

### Kiểm thử và DoD

- [x] Token hết hạn, revoke, reuse, malformed/brute-force shape và concurrent accept
      đều bị chặn; preview/accept có bounded in-process rate limiter theo action/IP prefix.
- [x] Token thô không xuất hiện trong DB, structured log hoặc audit payload.
- [x] Accept lặp lại trả kết quả idempotent, không tạo hai membership/event.
- [x] Cross-tenant invitation enumeration bị chặn bằng active-tenant policy,
      repository re-authorization và uniform unavailable response.

**Verification:** `pnpm verify` xanh ngày 2026-07-18: web 44/44, API client 11/11,
generated contract, lint/typecheck/build/Storybook, Go test/vet và security checks.
Identity/migration integration-tag compile xanh; runtime chưa chạy local vì không nạp
DB test env. Workflow CI PostgreSQL 17 sẽ xác nhận clean migration và PostgreSQL
lifecycle/concurrent-accept sau push.

**Giới hạn private alpha:** limiter hiện dùng `RemoteAddr`; Cloudflare/Render có thể
gộp client vào proxy bucket. Không tin trực tiếp forwarded header khi Render origin
còn public; trusted-proxy/origin authentication và distributed limiter thuộc P2-09.

## 9. P2-04 Class lifecycle, ownership và archive

**Mục tiêu:** hoàn thiện class CRUD theo tenant thay cho slice list/create/detail tối thiểu.

### Contract triển khai

- Giữ `GET/POST /api/v1/classes` và `GET /api/v1/classes/{class_id}`.
- `GET /api/v1/classes` nhận `status`, `limit`, opaque `cursor` và trả `next_cursor`.
- Bổ sung `PATCH /api/v1/classes/{class_id}`.
- Bổ sung `POST /api/v1/classes/{class_id}/archive`, `/restore` và
  `/transfer-ownership`.

### Công việc

- [x] Bổ sung class status, code, timezone, description, `version` và `archived_at`
      qua migration `000009`.
- [x] Dùng `owner_user_id` làm owner implicit; không tạo enrollment trước P2-05/P2-06.
- [x] Ownership transfer là mutation riêng, yêu cầu `expected_version`, recent
      authentication 10 phút và target là active member cùng tenant đủ điều kiện
      `class.create`; mutation vẫn được phép khi class archived.
- [x] State machine cho phép draft -> active; archive draft/active và restore chính xác
      trạng thái trước archive. Invite code chưa tồn tại đến P2-05, nhưng active/archive
      guard cho join mới đã sẵn sàng.
- [x] Optimistic concurrency dùng `expected_version` compare-and-swap cho
      update/archive/restore/transfer.
- [x] Query list hỗ trợ status, opaque keyset cursor và deterministic ordering theo
      `(created_at DESC, id DESC)`.
- [x] Mutation reauthorize membership authoritative trong transaction, khóa tenant/class
      theo thứ tự ổn định, giữ tenant isolation và ghi lifecycle event qua outbox.
- [x] Bổ sung `class.archive`/`class.transfer_ownership`; chỉ `org_admin` hoặc owner
      được lifecycle/transfer, teacher/co-teacher không được suy rộng quyền này.
- [x] UI create/edit/activate/archive/restore có confirm, stale-version recovery,
      status filter, pagination và trạng thái lỗi phù hợp.

### Kiểm thử và DoD

- [x] Hai update đồng thời không ghi đè âm thầm; stale version trả conflict.
- [x] Non-owner/non-admin không transfer/archive class; target không hợp lệ bị từ chối.
- [x] Class tenant A không xuất hiện hoặc bị mutate từ tenant B dù đoán đúng ID.
- [x] Existing classroom/LiveKit route tương thích class active; draft/archived không
      được cấp media token hoặc nhận media event mới.
- [x] Full `pnpm verify` xanh ngày 2026-07-18: web 55/55, API client 11/11,
      UI 6/6, generated contract, lint/typecheck/build/Storybook, Go test/vet và
      security checks.
- [x] Migration/classroom/identity integration-tag compile xanh local. Runtime
      PostgreSQL chưa chạy local vì không nạp DB test env; CI PostgreSQL 17 sẽ xác
      nhận clean migration và integration runtime sau push.

**Giới hạn đã biết:** recent-auth tái dùng `auth_time` session theo semantics P2-01,
chưa force OIDC `max_age`/`prompt`. Archive ngăn token/event LiveKit mới nhưng không
thu hồi JWT đã cấp hoặc kick participant đang trong room.

## 10. P2-05 Enrollment và class invite code

**Mục tiêu:** cho phép student tham gia lớp bằng enrollment trực tiếp hoặc invite code.

### Schema dự kiến

- `class_enrollments`: class, user, class role, status, enrolled_by, timestamps.
- `class_invite_codes`: class, code hash, status, expires_at, usage_limit,
  usage_count, created_by, revoked_at.

### State machine

- Enrollment: `invited -> active -> suspended/left/removed`.
- Invite code: `active -> exhausted/expired/revoked`.

### Contract triển khai

- `POST /api/v1/classes/{class_id}/enrollments`
- `POST /api/v1/classes/{class_id}/enrollments/{user_id}/suspend`
- `POST /api/v1/classes/{class_id}/enrollments/{user_id}/remove`
- `POST /api/v1/classes/{class_id}/invite-codes`
- `GET /api/v1/classes/{class_id}/invite-codes`
- `POST /api/v1/classes/{class_id}/invite-codes/{code_id}/revoke`
- `POST /api/v1/class-invitations/join`; opaque token chỉ nằm trong JSON body.
- `POST /api/v1/classes/{class_id}/leave`

### Công việc

- [x] Code 256-bit CSPRNG có prefix `thciv1_`, chỉ lưu purpose-bound HMAC, TTL
      15 phút-30 ngày và usage limit 1-1000.
- [x] Join transaction khóa/cập nhật usage count an toàn khi đồng thời; lượt cuối
      chuyển code sang `exhausted` atomically.
- [x] Idempotent join cho user đã active và manager/owner; không tiêu usage lần hai.
- [x] Policy self-leave, manager remove/suspend, direct-reactivate và rejoin đã được
      ghi rõ trong domain model/ADR-0013 và kiểm tra ở service/repository.
- [x] Class detail/list và room token/event chỉ dùng owner, organization manager hoặc
      enrollment active được resolve authoritative; browser/session không tự khai role.

### Kiểm thử và DoD

- [x] Có PostgreSQL integration test cho usage-limit race, same-user replay và
      expired/revoked/exhausted code.
- [x] Malformed/cross-scope/unavailable token trả cùng 404, không lộ class hoặc tenant;
      token không nằm trong path/query/log/cache/browser storage.
- [x] Enrollment active là điều kiện thống nhất cho class detail/list/room access;
      `viewer_access` tách rõ join và publish.
- [x] Transactional outbox ghi create/reactivate/revoke/join/rejoin/leave/remove/
      suspend/expire/exhaust bằng payload allowlist không chứa token/hash/email.

**Hoàn thành 2026-07-19:** migration `000010` thêm schema/constraint/index tenant-scoped;
OpenAPI/generated client và web có direct enroll, copy-once invite, revoke, join, leave
cùng loading/empty/error/forbidden/retry states. Web 69/69, API client 13/13, UI 6/6,
Go unit/HTTP tests, integration-tag compile, lint/typecheck/build/Storybook và security
checks đều xanh qua `pnpm verify`. PostgreSQL runtime cho migration/test `000010` chưa
chạy local vì không nạp DB test env; CI PostgreSQL 17 sẽ xác nhận sau khi push.

## 11. P2-06 Roster và class-level roles

**Mục tiêu:** teacher quản lý danh sách lớp và phân vai ở cấp lớp.

### Contract đề xuất

- `GET /api/v1/classes/{class_id}/roster`
- `PATCH /api/v1/classes/{class_id}/roster/{user_id}`
- `POST /api/v1/classes/{class_id}/roster/bulk`

### Công việc

- [x] Pagination, search theo normalized display name/email và status filter.
- [x] Role transition matrix owner/co-teacher/TA/student.
- [x] Không cho xóa/demote owner cuối cùng.
- [x] TA không được tự nâng quyền hoặc cấp quyền cao hơn.
- [x] Bulk action có giới hạn kích thước, partial-failure contract rõ ràng.
- [x] UI roster hỗ trợ keyboard, confirm mutation và empty/loading/error states.

### Kiểm thử và DoD

- [x] Table-driven role transition tests.
- [x] Cross-class/cross-tenant roster mutation đều bị từ chối.
- [x] Pagination không lặp/mất item khi dữ liệu không đổi.
- [x] Quyền LiveKit/class APIs phản ánh role mới ngay sau mutation.

**Hoàn thành 2026-07-19:** owner vẫn implicit và được ghim riêng khỏi page enrollment;
hierarchy shared-policy chặn self/peer/owner mutation. Search Unicode NFC/literal,
status filter, cursor bind scope/filter, single role update và bulk một action cho 1-50
user ID đã có OpenAPI/generated client, Go API/repository/service và React roster UI.
Bulk commit từng item, trả ordered `updated/unchanged/failed`; client refetch sau mọi
outcome. Viewer lifecycle capability và LiveKit role attributes đều lấy từ projection
authoritative. Full `pnpm verify` xanh: web 71/71, API client 14/14, UI 6/6 cùng
lint/typecheck/build/Storybook, Go test/vet và security checks. Integration-tag compile
xanh; runtime PostgreSQL roster integration chưa chạy local vì không nạp DB test env.

## 12. P2-07 Audit log hành động nhạy cảm

**Mục tiêu:** truy vết ai đã thay đổi tenant, membership, class, enrollment và role.

### Công việc

- [x] Schema append-only gồm actor, tenant, action, resource type/id, outcome,
      request ID, timestamp và metadata đã redaction.
- [x] Ghi audit trong cùng transaction/outbox boundary phù hợp.
- [x] Query API tenant-scoped có pagination/time/action/resource filter.
- [x] Không lưu token, secret, session ID thô hoặc PII không cần thiết.
- [x] Retention/export interface; policy production chốt ở Phase 8.
- [x] UI audit tối thiểu cho org admin.

### Kiểm thử và DoD

- [x] Mọi mutation nhạy cảm P2-02 đến P2-06 có success/failure audit phù hợp.
- [x] Audit tenant A không thể được query từ tenant B.
- [x] Audit append-only qua runtime role; không có update/delete API.
- [x] Request ID liên kết được structured log với audit record.

**Verification:** migration `000011`, trigger append-only, allowlist metadata, atomic
success audit và failure/no-op fallback có unit/static/integration test; invitation
accept bind tenant do server resolve và bulk roster dedupe/ghi đủ từng target. API cursor
bind tenant/filter, authorization `audit.view`, cache isolation và UI states đã được
kiểm tra. Full `pnpm verify` xanh ngày 2026-07-19: web 79/79, API client 15/15, UI 6/6,
lint/typecheck/build/Storybook, Go test/vet và security checks. Integration-tag compile
xanh; runtime PostgreSQL chưa chạy local vì không nạp DB test env.

## 13. P2-08 Admin và teacher UI end-to-end

**Mục tiêu:** kết nối các contract thành luồng dùng được, không chỉ có API.

### Luồng bắt buộc

- [x] Org admin tạo/chỉnh workspace và mời/revoke thành viên.
- [x] Người dùng preview/accept invitation và chuyển workspace.
- [x] Teacher tạo/chỉnh/archive lớp và tạo/revoke invite code.
- [x] Student join class bằng code và thấy class trong danh sách.
- [x] Teacher xem roster, đổi role hợp lệ và remove/suspend thành viên.
- [x] Org admin xem audit cơ bản.

### Chất lượng UI

- Dùng component từ `@tutorhub/ui`, không tạo style rời khi primitive đã có.
- Desktop và mobile responsive; focus order, label, live region hợp lệ.
- Loading/empty/error/forbidden/offline/degraded có nội dung hành động được.
- Mutation invalidates đúng tenant/class cache; không flash dữ liệu workspace cũ.

### Definition of Done

- [ ] E2E chính chạy được bằng teacher/student fixtures local và staging.
- [x] Không cần dùng SQL/manual API để hoàn thành deliverable Phase 2.
- [x] Visual QA ở viewport laptop nhỏ, desktop và mobile đạt.

**Implementation checkpoint 2026-07-20:** navigation đã thu gọn theo capability;
org admin có luồng tạo/chỉnh workspace, invitation và audit; invitation accept
chuyển được đúng workspace; teacher/student có class join, lifecycle, invite và
roster role/suspend/remove xuyên suốt. Cache tenant/class được cancel, che hoặc
invalidate theo quyền để không flash dữ liệu cũ.

Playwright có một scenario ba browser context admin/teacher/student, fake OIDC
loopback dùng Authorization Code + PKCE và job CI PostgreSQL 17 + Chromium. Guard
database chỉ chấp nhận database `tutorhub_e2e` trên loopback với query duy nhất
`sslmode=disable`; process tree được dừng có chờ trên Windows và Unix. Full
`pnpm verify` xanh: web 130/130, API client 15/15, UI 6/6, E2E infrastructure 7/7,
lint/typecheck/build/Storybook, Go test/vet và security checks. Integration-tag
compile xanh; Playwright discovery thấy đúng một scenario. Visual QA thủ công đạt
ở 1440x900, 1024x768 và 390x844. Full browser scenario chưa chạy local vì máy hiện
không có Docker/PostgreSQL; job Browser E2E trên CI sẽ xác nhận runtime sau push,
và chưa ghi nhận đây là staging acceptance. Vì gate đầu tiên của DoD còn mở, P2-08
giữ trạng thái `VERIFY`, chưa phải `DONE`.

## 14. P2-09 Feature flag và quota framework

**Mục tiêu:** có cơ chế tắt/mở và giới hạn tính năng theo tenant mà không hardcode UI.

### Công việc

- [ ] Định nghĩa feature catalog typed, default an toàn và source precedence.
- [ ] Tenant feature override chỉ do org/platform admin có quyền.
- [ ] Quota tối thiểu: members, active classes, invite creation rate.
- [ ] Server là nguồn quyết định; UI chỉ dùng capability response để hiển thị.
- [ ] Audit thay đổi flag/quota; metric cho quota rejection.
- [ ] Không xây billing trong Phase 2.

### Kiểm thử và DoD

- Disabled feature bị chặn server-side dù gọi API trực tiếp.
- Quota concurrent mutation không vượt giới hạn.
- Capability response không lộ cấu hình tenant khác.

## 15. P2-10 Tenant isolation và IDOR security suite

**Mục tiêu:** chứng minh bằng test rằng mọi resource mới được cô lập đúng tenant/class.

### Công việc

- [ ] Xây actor/resource matrix cho anonymous, guest, student, TA, teacher,
      co-teacher, owner và org admin.
- [ ] Test ID đoán đúng nhưng tenant khác cho từng endpoint đọc/ghi.
- [ ] Test stale session sau membership revoke hoặc workspace switch.
- [ ] Test mass assignment, pagination cursor tamper và invite token abuse.
- [ ] Fuzz parser/validation cho token/code và resource IDs quan trọng.
- [ ] Thêm integration suite vào workflow `Verify`.
- [ ] Chạy dependency/SAST/container scan trên head Phase 2.

### Definition of Done

- Toàn bộ matrix xanh trên PostgreSQL thật.
- Không endpoint nào dựa duy nhất vào ID do client gửi để xác định tenant.
- Finding High/Critical được sửa hoặc có exception có owner/expiry.

## 16. P2-11 V1 fixture import idempotent

**Mục tiêu:** chứng minh đường chuyển dữ liệu user/tenant/class đầu tiên từ V1.

### Công việc

- [ ] Chốt mapping V1 -> V2 và dữ liệu không thể ánh xạ.
- [ ] Fixture đã ẩn danh gồm Unicode tiếng Việt, timezone và edge cases.
- [ ] Dry-run report trước khi ghi.
- [ ] Import có external/source key, upsert policy và checkpoint.
- [ ] Chạy lặp lại không tạo duplicate; lỗi giữa chừng có thể resume.
- [ ] Reconciliation report đếm source/imported/skipped/failed.
- [ ] Không đọc secret/production data từ `D:\Ban_sao_du_an`.

### Kiểm thử và DoD

- Import fixture hai lần cho cùng kết quả.
- Rollback/reset test trên Neon branch tạm.
- User, membership, class và ownership sau import khớp mapping đã duyệt.

## 17. P2-12 Staging acceptance và đóng phase

### Acceptance scenarios

- [ ] Admin tạo tenant và mời teacher/student.
- [ ] Teacher/student accept invitation, login và switch đúng workspace.
- [ ] Teacher tạo class và invite code có TTL/usage limit.
- [ ] Student join class; teacher thấy roster và đổi role hợp lệ.
- [ ] Student tenant khác không đọc/ghi class, roster, audit hoặc room token.
- [ ] Archive class chặn join mới nhưng giữ audit/roster lịch sử.
- [ ] Audit query trả đúng actor/request/resource.
- [ ] V1 fixture import dry-run + apply + rerun đạt idempotency.
- [ ] Deploy, migration up/down/up và rollback smoke đạt trên staging.

### Exit gate Phase 2

- Permission matrix được phê duyệt và có automated tests.
- IDOR/cross-tenant suite xanh trong CI.
- Audit query được, tenant-scoped và không chứa secret.
- Import fixture idempotent, có reconciliation report.
- UI đầy đủ loading/empty/error/forbidden cho các luồng bắt buộc.
- Không còn role check rải rác ngoài policy layer.
- `pnpm verify`, Security workflow và staging acceptance đều xanh.
- Biên bản `PHASE_2_COMPLETION.md` được tạo trước khi sang Phase 3.

## 18. Thứ tự sprint

| Sprint | Task chính          | Kết quả demo                                  |
| ------ | ------------------- | --------------------------------------------- |
| 0      | P2-00               | Permission matrix + policy layer thống nhất   |
| 1      | P2-01, P2-02        | Profile và tenant lifecycle/switch hoàn chỉnh |
| 2      | P2-03, P2-04        | Mời thành viên và class lifecycle             |
| 3      | P2-05, P2-06        | Join class, roster và class roles             |
| 4      | P2-07, P2-08, P2-09 | Audit + UI end-to-end + feature/quota         |
| 5      | P2-10, P2-11, P2-12 | Security suite, V1 fixture và staging closure |

## 19. Việc cần làm ngay

1. Chạy Browser E2E PostgreSQL 17 trên CI và local/staging acceptance của P2-08.
2. Chỉ chuyển P2-08 sang `DONE` sau khi gate trên xanh hoặc có waiver được ghi rõ.
3. Sau đó bắt đầu P2-09 bằng typed feature catalog và quota server-authoritative.
4. Giữ audit append-only, tenant-scoped và không log token, session ID hoặc PII thừa.
5. Giữ notification invitation ở interface/outbox; chưa gửi email thật trong Phase 2.
