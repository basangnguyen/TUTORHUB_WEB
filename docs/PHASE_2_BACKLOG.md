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

**Task đã hoàn thành:** P2-00 Policy and contract baseline.

**Task kế tiếp:** P2-01 User profile và identity linking.

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
| P2-01 | User profile và identity linking        | P2-00                      | TODO       |
| P2-02 | Tenant lifecycle và workspace switching | P2-00                      | TODO       |
| P2-03 | Membership invitation/accept/revoke     | P2-02                      | TODO       |
| P2-04 | Class lifecycle, ownership và archive   | P2-00, P2-02               | TODO       |
| P2-05 | Enrollment và class invite code         | P2-03, P2-04               | TODO       |
| P2-06 | Roster và class-level roles             | P2-05                      | TODO       |
| P2-07 | Audit log cho hành động nhạy cảm        | P2-02 đến P2-06            | TODO       |
| P2-08 | Admin/teacher UI end-to-end             | P2-02 đến P2-07            | TODO       |
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

- [ ] Bổ sung profile fields tối thiểu: display name, locale, timezone, avatar key.
- [ ] Chuẩn hóa Unicode, độ dài và locale/timezone allow-list.
- [ ] Identity unique theo `(issuer, subject)`; không cho một identity gắn hai user.
- [ ] Link/unlink yêu cầu recent authentication và state/nonce chống CSRF.
- [ ] Không cho unlink identity cuối cùng nếu không có phương thức đăng nhập thay thế.
- [ ] Avatar chỉ lưu object key/metadata, không lưu public URL vĩnh viễn.
- [ ] UI hồ sơ có optimistic state thận trọng, validation và error recovery.
- [ ] Audit link/unlink identity và thay đổi trường nhạy cảm.

### Kiểm thử và DoD

- Unit validation, repository integration và OIDC state/nonce negative tests.
- Cross-user identity collision trả lỗi ổn định, không lộ user khác.
- Profile cập nhật được qua web và giữ nguyên sau đăng nhập lại.
- i18n vi/en, keyboard/focus và loading/error states đạt.

## 7. P2-02 Tenant lifecycle và workspace switching

**Mục tiêu:** hoàn thiện create/list/update/archive tenant và chuyển workspace an toàn.

### Contract đề xuất

- `GET /api/v1/tenants`
- `POST /api/v1/tenants`
- `GET /api/v1/tenants/{tenantId}`
- `PATCH /api/v1/tenants/{tenantId}`
- `POST /api/v1/tenants/{tenantId}/archive`
- Giữ endpoint select/switch hiện có và chuẩn hóa response/session rotation.

### Công việc

- [ ] Bổ sung tenant slug, display name, status và metadata có version.
- [ ] Tenant create tạo membership `org_admin` trong cùng transaction.
- [ ] Switch chỉ chấp nhận tenant có membership active và xoay session/CSRF.
- [ ] Archive từ chối khi không đủ quyền; không hard-delete dữ liệu nghiệp vụ.
- [ ] Chặn archive active tenant cuối cùng nếu làm user mất đường quản trị.
- [ ] Repository/query bắt buộc active tenant context sau switch.
- [ ] UI workspace switcher xử lý stale cache và invalidate query đúng scope.

### Kiểm thử và DoD

- Concurrent create/switch và retry không tạo duplicate membership.
- Tenant A không đọc/sửa/archive tenant B.
- Reload sau switch giữ đúng workspace và không hiện cache của tenant trước.
- Audit create/update/archive/switch có actor và tenant chính xác.

## 8. P2-03 Membership invitation, accept và revoke

**Mục tiêu:** organization admin mời thành viên vào tenant bằng luồng một lần, có hạn.

### Schema dự kiến

`membership_invitations`: tenant, normalized email, intended role, token hash,
status, expires_at, accepted_at, revoked_at, invited_by, accepted_by, created_at.

### Contract đề xuất

- `GET /api/v1/tenants/{tenantId}/invitations`
- `POST /api/v1/tenants/{tenantId}/invitations`
- `POST /api/v1/tenants/{tenantId}/invitations/{invitationId}/revoke`
- `GET /api/v1/membership-invitations/{token}/preview`
- `POST /api/v1/membership-invitations/{token}/accept`

### Công việc

- [ ] Sinh token CSPRNG, chỉ lưu hash và redaction trong log/audit.
- [ ] TTL cấu hình được; status state machine `pending/accepted/revoked/expired`.
- [ ] Accept kiểm tra email/identity policy, transaction và idempotency.
- [ ] Không tạo membership trùng; re-invite xử lý theo policy rõ ràng.
- [ ] Role được mời phải nằm trong tập role actor có quyền cấp.
- [ ] Notification adapter chỉ là interface/outbox; gửi email thật thuộc phase sau.
- [ ] UI admin list/create/revoke và trang preview/accept.

### Kiểm thử và DoD

- Token hết hạn, revoke, reuse, brute-force shape và concurrent accept đều bị chặn.
- Token thô không xuất hiện trong DB, structured log hoặc audit payload.
- Accept lặp lại trả kết quả idempotent, không tạo hai membership.
- Cross-tenant invitation enumeration bị chặn.

## 9. P2-04 Class lifecycle, ownership và archive

**Mục tiêu:** hoàn thiện class CRUD theo tenant thay cho slice list/create/detail tối thiểu.

### Contract đề xuất

- Giữ `GET/POST /api/v1/classes` và `GET /api/v1/classes/{classId}`.
- Bổ sung `PATCH /api/v1/classes/{classId}`.
- Bổ sung `POST /api/v1/classes/{classId}/archive` và `/restore` nếu policy cho phép.

### Công việc

- [ ] Bổ sung class status, code/slug, timezone, description và version.
- [ ] Creator nhận class role `owner` trong cùng transaction.
- [ ] Ownership transfer là mutation riêng, yêu cầu recent confirmation.
- [ ] Archive đóng invite code mới nhưng không xóa roster/audit.
- [ ] Optimistic concurrency bằng version/ETag hoặc updated-at precondition.
- [ ] Query list hỗ trợ status, pagination ổn định và deterministic ordering.
- [ ] UI create/edit/archive/restore có confirm và recovery.

### Kiểm thử và DoD

- Hai update đồng thời không ghi đè âm thầm.
- Non-owner/non-admin không transfer/archive class.
- Class tenant A không xuất hiện ở tenant B dù đoán đúng ID.
- Existing classroom/LiveKit route tương thích class active.

## 10. P2-05 Enrollment và class invite code

**Mục tiêu:** cho phép student tham gia lớp bằng enrollment trực tiếp hoặc invite code.

### Schema dự kiến

- `class_enrollments`: class, user, class role, status, enrolled_by, timestamps.
- `class_invite_codes`: class, code hash, status, expires_at, usage_limit,
  usage_count, created_by, revoked_at.

### State machine

- Enrollment: `invited -> active -> suspended/left/removed`.
- Invite code: `active -> exhausted/expired/revoked`.

### Contract đề xuất

- `POST /api/v1/classes/{classId}/enrollments`
- `POST /api/v1/classes/{classId}/invite-codes`
- `GET /api/v1/classes/{classId}/invite-codes`
- `POST /api/v1/classes/{classId}/invite-codes/{codeId}/revoke`
- `POST /api/v1/class-invitations/{code}/join`
- `POST /api/v1/classes/{classId}/leave`

### Công việc

- [ ] Code đủ entropy, lưu hash, có TTL và usage limit atomic.
- [ ] Join transaction khóa/cập nhật usage count an toàn khi đồng thời.
- [ ] Idempotent join cho user đã active; không tiêu usage lần hai.
- [ ] Policy self-leave, teacher remove, suspend và rejoin được ghi rõ.
- [ ] Room token chỉ cấp khi enrollment active hoặc actor có quyền quản trị.

### Kiểm thử và DoD

- Race test usage limit, expired/revoked/exhausted code.
- Guessing/enumeration không lộ class hoặc tenant.
- Enrollment active là điều kiện thống nhất cho class detail/room access.
- Audit đầy đủ create/revoke/join/leave/remove/suspend.

## 11. P2-06 Roster và class-level roles

**Mục tiêu:** teacher quản lý danh sách lớp và phân vai ở cấp lớp.

### Contract đề xuất

- `GET /api/v1/classes/{classId}/roster`
- `PATCH /api/v1/classes/{classId}/roster/{userId}`
- `DELETE /api/v1/classes/{classId}/roster/{userId}` hoặc mutation remove rõ nghĩa.

### Công việc

- [ ] Pagination, search theo normalized display name/email và status filter.
- [ ] Role transition matrix owner/co-teacher/TA/student.
- [ ] Không cho xóa/demote owner cuối cùng.
- [ ] TA không được tự nâng quyền hoặc cấp quyền cao hơn.
- [ ] Bulk action có giới hạn kích thước, partial-failure contract rõ ràng.
- [ ] UI roster hỗ trợ keyboard, confirm mutation và empty/loading/error states.

### Kiểm thử và DoD

- Table-driven role transition tests.
- Cross-class/cross-tenant roster mutation đều bị từ chối.
- Pagination không lặp/mất item khi dữ liệu không đổi.
- Quyền LiveKit/class APIs phản ánh role mới ngay sau mutation.

## 12. P2-07 Audit log hành động nhạy cảm

**Mục tiêu:** truy vết ai đã thay đổi tenant, membership, class, enrollment và role.

### Công việc

- [ ] Schema append-only gồm actor, tenant, action, resource type/id, outcome,
      request ID, timestamp và metadata đã redaction.
- [ ] Ghi audit trong cùng transaction/outbox boundary phù hợp.
- [ ] Query API tenant-scoped có pagination/time/action/resource filter.
- [ ] Không lưu token, secret, session ID thô hoặc PII không cần thiết.
- [ ] Retention/export interface; policy production chốt ở Phase 8.
- [ ] UI audit tối thiểu cho org admin.

### Kiểm thử và DoD

- Mọi mutation nhạy cảm P2-02 đến P2-06 có success/failure audit phù hợp.
- Audit tenant A không thể được query từ tenant B.
- Audit append-only qua runtime role; không có update/delete API.
- Request ID liên kết được structured log với audit record.

## 13. P2-08 Admin và teacher UI end-to-end

**Mục tiêu:** kết nối các contract thành luồng dùng được, không chỉ có API.

### Luồng bắt buộc

- [ ] Org admin tạo/chỉnh workspace và mời/revoke thành viên.
- [ ] Người dùng preview/accept invitation và chuyển workspace.
- [ ] Teacher tạo/chỉnh/archive lớp và tạo/revoke invite code.
- [ ] Student join class bằng code và thấy class trong danh sách.
- [ ] Teacher xem roster, đổi role hợp lệ và remove/suspend thành viên.
- [ ] Org admin xem audit cơ bản.

### Chất lượng UI

- Dùng component từ `@tutorhub/ui`, không tạo style rời khi primitive đã có.
- Desktop và mobile responsive; focus order, label, live region hợp lệ.
- Loading/empty/error/forbidden/offline/degraded có nội dung hành động được.
- Mutation invalidates đúng tenant/class cache; không flash dữ liệu workspace cũ.

### Definition of Done

- E2E chính chạy được bằng teacher/student fixtures local và staging.
- Không cần dùng SQL/manual API để hoàn thành deliverable Phase 2.
- Visual QA ở viewport laptop nhỏ, desktop và mobile đạt.

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

1. Bắt đầu P2-01 từ contract profile/identity, chưa mở rộng màn hình admin.
2. Rà schema `users`, `user_identities`, session và OIDC flow hiện có trước khi tạo migration.
3. Chốt validation cho display name, locale, timezone và avatar object key.
4. Triển khai repository/service/API cùng policy, audit và negative tests cho link/unlink identity.
5. Hoàn thiện web profile vertical slice rồi mới chuyển sang P2-02 tenant lifecycle.
