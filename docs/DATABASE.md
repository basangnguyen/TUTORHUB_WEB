# Database foundation

Tài liệu này là runbook cho nền PostgreSQL của TutorHub V2 từ P1-05. Mọi agent
thay đổi schema, migration hoặc repository phải đọc tài liệu này trước khi sửa.

## Trạng thái hiện tại

- System of record: Neon PostgreSQL.
- Schema ứng dụng: `tutorhub`.
- Migration mới nhất trong source: `000010_class_enrollments`.
- Migration 1-5 đã được chạy và kiểm tra trên Neon; smoke
  `5 false -> rollback 4 false -> migrate 5 false` đạt ngày 2026-07-16.
- Migration `000006` đến `000010` đều có up/down path. Migration/classroom/identity
  integration-tag compile xanh local; runtime `000010` chưa chạy local vì không nạp
  DB test env và sẽ do workflow CI xác nhận sau push. Smoke
  `10 false -> rollback 9 false -> migrate 10 false` chưa chạy trên staging; tài liệu
  này không khẳng định staging đã nâng lên 10.
- Classroom và identity integration test chạy trong transaction và rollback toàn bộ fixture.
- Core API đã được smoke test với Neon: `/ready` trả `ready` và `/health` trả `ok`.

Neon có branch `production` và branch staging tách biệt. Core API staging dùng pooled
runtime role tối thiểu quyền; migration job dùng direct migration role riêng. Kết nối,
readiness và migration/rollback smoke đều đã đạt.

## Hai connection URL

| Biến                     | Đối tượng sử dụng  | Loại URL                                 | Quy tắc                                                   |
| ------------------------ | ------------------ | ---------------------------------------- | --------------------------------------------------------- |
| `DATABASE_POOL_URL`      | Core API đang chạy | Neon pooled, hostname có `-pooler`       | Chỉ quyền runtime; cấu hình pool nhỏ để phù hợp free tier |
| `DATABASE_MIGRATION_URL` | CLI/release job    | Neon direct, hostname không có `-pooler` | Chỉ cấp cho migration job; không đưa vào API container    |

Không dùng URL direct cho traffic ứng dụng thường xuyên. Không cấp URL migration
cho frontend, browser, Cloudflare Pages hoặc tiến trình Core API trên Render.
Core API không tự chạy migration khi khởi động.

## Cấu hình pool mặc định

| Biến                                | Mặc định | Ý nghĩa                                         |
| ----------------------------------- | -------: | ----------------------------------------------- |
| `DATABASE_MAX_CONNECTIONS`          |      `4` | Giới hạn kết nối của một Core API instance      |
| `DATABASE_MIN_CONNECTIONS`          |      `0` | Cho phép scale-to-zero khi rảnh                 |
| `DATABASE_CONNECT_TIMEOUT`          |    `10s` | Giới hạn thời gian mở/ping kết nối              |
| `DATABASE_QUERY_TIMEOUT`            |     `5s` | Timeout dùng cho readiness/repository operation |
| `DATABASE_MAX_CONNECTION_LIFETIME`  |    `30m` | Làm mới kết nối dài hạn                         |
| `DATABASE_MAX_CONNECTION_IDLE_TIME` |     `5m` | Thu hồi kết nối rảnh                            |
| `DATABASE_HEALTH_CHECK_PERIOD`      |     `1m` | Chu kỳ kiểm tra pool                            |

`application_name=tutorhub-core-api` được gắn vào kết nối để quan sát trên Neon.
Mọi truy vấn mạng/database phải chạy ngoài UI thread ở các client native về sau.

## Schema phiên bản 10

| Bảng                     | Vai trò                                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------------------ |
| `users`                  | Hồ sơ định danh nội bộ, email chuẩn hóa và trạng thái tài khoản                                  |
| `identities`             | Ánh xạ `(provider, subject)` từ OIDC, verified email và lần xác thực gần nhất                    |
| `tenants`                | Workspace với slug/name, locale/timezone, status, optimistic `version` và `archived_at`          |
| `memberships`            | Quan hệ user-tenant và role `org_admin/teacher/student/guest`                                    |
| `sessions`               | Hash session/CSRF, active tenant, `context_version`, idle/absolute expiry và revoke state        |
| `auth_flows`             | HMAC state/binding/nonce, PKCE verifier mã hóa và one-time consume                               |
| `classes`                | Lớp học theo tenant; owner cùng tenant, timezone, lifecycle, optimistic version và archive state |
| `class_enrollments`      | Quan hệ user-class theo tenant, class role, trạng thái tham gia và các mốc lifecycle             |
| `class_invite_codes`     | Mã mời lớp chỉ lưu HMAC, TTL, giới hạn lượt dùng, trạng thái và actor lifecycle                  |
| `membership_invitations` | Lời mời tenant một lần: normalized email, role, HMAC token, TTL và terminal state                |
| `outbox_events`          | Transactional outbox cho sự kiện bền vững và worker tương lai                                    |

Ràng buộc quan trọng:

- Mã lớp chỉ duy nhất trong từng tenant.
- Foreign key tổng hợp chặn owner/member thuộc tenant khác.
- Repository luôn nhận `tenancy.Context` gồm `tenant_id` và `actor_user_id`.
- Class tiếp tục dùng `owner_user_id` làm owner implicit, không tạo enrollment riêng
  cho owner. `class_enrollments` giữ các role `co_teacher`, `teaching_assistant`,
  `student` với state `invited/active/suspended/left/removed`; mỗi user chỉ có một row
  trong một class và mọi foreign key actor/user đều bị khóa theo tenant.
- Chỉ enrollment `active` được nạp thành class role authoritative cho class/media.
  Enrollment inactive không cấp quyền; owner implicit và quyền organization vẫn được
  shared policy đánh giá độc lập. HTTP trả projection `viewer_access` do server tính,
  không tin class role do session hoặc browser tự khai.
- Direct enrollment chỉ tìm active tenant member theo normalized email, tạo/reactivate
  role `student` và không tạo owner/manager trùng. Suspend/remove/leave dùng transition
  có điều kiện; suspended/removed không thể tự join lại bằng invite code, còn manager
  có thể direct-reactivate theo policy.
- Class invite token có prefix `thciv1_`, entropy 256-bit và database chỉ giữ unique
  purpose-bound HMAC 32 byte. TTL bị chặn trong khoảng 15 phút đến 30 ngày; usage limit
  từ 1 đến 1000 và `usage_count` không được vượt giới hạn.
- Join invite khóa class, tenant membership, enrollment và invite code trong transaction.
  Join mới/rejoin từ `invited` hoặc `left` tăng usage đúng một lần; replay của active
  enrollment và principal đã có quyền quản lý không tiêu thụ lượt. Lượt cuối atomically
  chuyển code sang `exhausted`; expired/revoked/exhausted/cross-scope đều unavailable.
- Archive chặn direct enrollment, create code, join mới và media request mới, nhưng giữ
  enrollment/code lịch sử; manager vẫn list/revoke code và active enrollee vẫn có thể
  leave. Restore không tự phát lại credential hay thay đổi enrollment/code state.
- Class có `timezone`, `version > 0`, `archived_at` và `archived_from_status`; archive
  draft/active rồi restore chính xác trạng thái trước, còn update chỉ cho draft -> active.
- `CreateClass` ghi lớp draft và sự kiện `class.created` trong cùng transaction.
- Get/List luôn lọc `tenant_id`; truy cập chéo tenant trả về not found.
- HTTP list/create/detail/mutation lấy `tenant_id`, actor và permission từ active
  session; request không có trường tenant hoặc owner.
- List class hỗ trợ status và opaque keyset cursor, dùng index theo
  `(tenant_id, status, created_at DESC, id DESC)` hoặc
  `(tenant_id, created_at DESC, id DESC)`.
- Tạo lớp yêu cầu `class.create` và CSRF; đọc lớp yêu cầu `class.view`.
- Update/archive/restore/transfer ownership dùng `expected_version` CAS. Các mutation
  khóa tenant, class và membership liên quan theo thứ tự ổn định, đọc lại membership
  authoritative rồi reauthorize shared policy trong transaction.
- Chỉ `org_admin` hoặc owner implicit có `class.archive` và
  `class.transfer_ownership`. Transfer target phải là active member cùng tenant đủ điều
  kiện `class.create` và actor có recent authentication trong 10 phút.
- Success event class create/update/archive/restore/ownership transfer, enrollment
  create/reactivate/suspend/remove/join/rejoin/leave và invite-code
  create/revoke/expire/exhaust được ghi vào
  transactional outbox cùng business mutation; payload không chứa description, token
  thô, token hash hoặc session secret.
- Tenant list được giới hạn bởi user membership active; detail/update/archive bắt buộc
  tenant path trùng active tenant context.
- Đọc tenant yêu cầu `tenant.view`; update/archive yêu cầu `tenant.manage` và CSRF.
- Update/archive dùng `expected_version` và SQL compare-and-swap rồi tăng version;
  stale request không ghi đè dữ liệu mới hơn.
- Create từ workspace hiện hữu, switch, update và archive khóa membership row; create,
  update và archive reauthorize qua shared policy trong transaction để concurrent
  revoke/demotion không giữ quyền từ snapshot cũ. Bootstrap khóa user rồi kiểm tra lại
  không có membership active trước khi insert để tuần tự hóa nhiều session onboarding.
- Create/switch/archive dùng `sessions.context_version` để CAS privilege context trước
  khi xoay session/CSRF. Archive xóa active context của các session còn trỏ tenant đó.
- Success event `tenant.created/updated/archived/switched` được ghi vào outbox trong
  cùng transaction; payload không chứa token, cookie hoặc session secret.
- Invitation token chỉ lưu purpose-bound HMAC 32 byte unique; một tenant/email chỉ có
  một row `pending`, TTL tối đa 30 ngày và state/timestamp bị khóa bằng CHECK constraint.
- Composite FK buộc invited/accepted/revoked actor có membership cùng tenant. Create,
  revoke và accept ghi lifecycle event trong business transaction; payload allowlist
  không chứa raw token, token hash, email hoặc session identifier.
- Accept khóa tenant/session/identity-user/membership/invitation theo thứ tự ổn định,
  yêu cầu verified linked identity khớp email và tạo tối đa một membership/event.

## Chạy migration

Tạo `.env.local` từ `.env.example` và điền hai URL. File này đã được Git ignore;
không in URL ra terminal, issue, log hoặc tài liệu.

Nạp biến môi trường trong PowerShell mà không in giá trị:

```powershell
Get-Content .env.local | ForEach-Object {
  $line = $_.Trim()
  if ($line -and -not $line.StartsWith('#')) {
    $parts = $line -split '=', 2
    if ($parts.Count -eq 2) {
      Set-Item -Path "Env:$($parts[0].Trim())" -Value $parts[1].Trim()
    }
  }
}
```

Sau đó chạy:

```powershell
pnpm db:version
pnpm db:migrate
pnpm db:version
```

Sau khi áp dụng toàn bộ migration trong source, kết quả mong đợi là `10 false`. Chỉ ghi
đó là kết quả môi trường khi lệnh thực tế đã chạy; bằng chứng staging gần nhất hiện vẫn
là `5 false` ngày 2026-07-16. Rollback chỉ dùng khi đã đánh giá mất dữ liệu và có
backup/restore plan:

```powershell
go run ./services/core-api/cmd/migrate down -steps 1
```

## Kiểm thử

Unit test và static verification:

```powershell
pnpm verify
```

Integration test bằng PostgreSQL thật:

```powershell
pnpm test:integration
```

Với P2-05, cần kiểm tra riêng migrate 9 -> 10, rollback 10 -> 9, migrate lại 9 -> 10;
tenant-scoped FK/unique/state constraints; direct enroll và các transition; same-user
replay; concurrent join ở usage limit; atomic exhausted/expired state; archive guard;
active enrollment projection cho class/media; cross-tenant concealment và
transactional outbox. Local hiện mới compile integration-tag vì không nạp DB test env;
không coi đó là bằng chứng runtime PostgreSQL đã chạy.

CI tạo PostgreSQL 17 tạm thời, chạy migration từ database sạch rồi chạy integration
test. Bài test Neon cục bộ dùng transaction bao ngoài và rollback nên không để lại
user, tenant, class, enrollment, invite code, invitation, membership hoặc outbox fixture.

## Quy tắc thay đổi schema

1. Không sửa migration đã chạy; tạo migration số tiếp theo với cặp `up/down`.
2. Migration phải chạy được từ database sạch và từ version liền trước.
3. Mọi bảng nghiệp vụ tenant-scoped phải có `tenant_id`, index phù hợp và deny test.
4. Mọi repository phải nhận tenant context; không dùng tenant do request body tự khai.
5. Không ghi password, access token, session token hoặc secret thô vào database/log.
6. Event cần độ bền phải ghi bằng outbox trong cùng transaction với dữ liệu nghiệp vụ.
7. Cập nhật OpenAPI/generated client khi thay đổi contract công khai.

## Việc còn lại

- P1-06 đã triển khai OIDC/BFF, session rotation, CSRF và `/api/v1/me`; cả ZITADEL local và staging đã được provision và smoke test.
- P1-06B đã hoàn thành list/create/detail class; các lát cắt enrollment, invite code và roster thuộc Phase 2.
- P1-10 đã hoàn thành database/branch staging riêng, runtime role và migration role riêng.
- P2-04 đã bổ sung class lifecycle/ownership/archive và migration `000009`.
- P2-05 đã bổ sung enrollment, class invite code và migration `000010`; roster search,
  phân trang và UI đổi class role đầy đủ vẫn thuộc P2-06.
- Chưa import dữ liệu TutorHub V1; migration V1 sẽ làm theo module/cohort ở phase sau.
- Chưa có backup/restore drill, PITR gate hoặc connection load test cho pilot.
