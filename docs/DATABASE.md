# Database foundation

Tài liệu này là runbook cho nền PostgreSQL của TutorHub V2 từ P1-05. Mọi agent
thay đổi schema, migration hoặc repository phải đọc tài liệu này trước khi sửa.

## Trạng thái hiện tại

- System of record: Neon PostgreSQL.
- Schema ứng dụng: `tutorhub`.
- Migration hiện tại: `4`, trạng thái `dirty=false`.
- Migration 1-4 đã được chạy và kiểm tra trên Neon ngày 2026-07-13.
- Classroom và identity integration test chạy trong transaction và rollback toàn bộ fixture.
- Core API đã được smoke test với Neon: `/ready` trả `ready` và `/health` trả `ok`.

Neon branch đang được chủ dự án cấp có tên `production`, nhưng trong Phase 1 chỉ
được xem là cơ sở dữ liệu tích hợp phát triển. P1-10 vẫn phải tạo resource staging
riêng và tách role tối thiểu quyền trước khi triển khai API công khai.

## Hai connection URL

| Biến | Đối tượng sử dụng | Loại URL | Quy tắc |
|---|---|---|---|
| `DATABASE_POOL_URL` | Core API đang chạy | Neon pooled, hostname có `-pooler` | Chỉ quyền runtime; cấu hình pool nhỏ để phù hợp free tier |
| `DATABASE_MIGRATION_URL` | CLI/release job | Neon direct, hostname không có `-pooler` | Chỉ cấp cho migration job; không đưa vào API container |

Không dùng URL direct cho traffic ứng dụng thường xuyên. Không cấp URL migration
cho frontend, browser, Cloudflare Pages hoặc tiến trình Core API trên Hugging Face.
Core API không tự chạy migration khi khởi động.

## Cấu hình pool mặc định

| Biến | Mặc định | Ý nghĩa |
|---|---:|---|
| `DATABASE_MAX_CONNECTIONS` | `4` | Giới hạn kết nối của một Core API instance |
| `DATABASE_MIN_CONNECTIONS` | `0` | Cho phép scale-to-zero khi rảnh |
| `DATABASE_CONNECT_TIMEOUT` | `10s` | Giới hạn thời gian mở/ping kết nối |
| `DATABASE_QUERY_TIMEOUT` | `5s` | Timeout dùng cho readiness/repository operation |
| `DATABASE_MAX_CONNECTION_LIFETIME` | `30m` | Làm mới kết nối dài hạn |
| `DATABASE_MAX_CONNECTION_IDLE_TIME` | `5m` | Thu hồi kết nối rảnh |
| `DATABASE_HEALTH_CHECK_PERIOD` | `1m` | Chu kỳ kiểm tra pool |

`application_name=tutorhub-core-api` được gắn vào kết nối để quan sát trên Neon.
Mọi truy vấn mạng/database phải chạy ngoài UI thread ở các client native về sau.

## Schema phiên bản 4

| Bảng | Vai trò |
|---|---|
| `users` | Hồ sơ định danh nội bộ, email chuẩn hóa và trạng thái tài khoản |
| `identities` | Ánh xạ `(provider, subject)` từ OIDC, verified email và lần xác thực gần nhất |
| `tenants` | Tổ chức/trường/lớp độc lập ở biên multi-tenant |
| `memberships` | Quan hệ user-tenant và role `org_admin/teacher/student/guest` |
| `sessions` | Hash session/CSRF, identity, idle/absolute expiry, auth time và revoke reason |
| `auth_flows` | HMAC state/binding/nonce, PKCE verifier mã hóa và one-time consume |
| `classes` | Lớp học theo tenant; owner bắt buộc là membership cùng tenant |
| `outbox_events` | Transactional outbox cho sự kiện bền vững và worker tương lai |

Ràng buộc quan trọng:

- Mã lớp chỉ duy nhất trong từng tenant.
- Foreign key tổng hợp chặn owner/member thuộc tenant khác.
- Repository luôn nhận `tenancy.Context` gồm `tenant_id` và `actor_user_id`.
- `CreateClass` ghi lớp và sự kiện `class.created` trong cùng một câu lệnh CTE.
- Get/List luôn lọc `tenant_id`; truy cập chéo tenant trả về not found.
- HTTP list/create/detail lấy `tenant_id`, actor và permission từ active session; request không có trường tenant hoặc owner.
- Tạo lớp yêu cầu `class.create` và CSRF; đọc lớp yêu cầu `class.view`.

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

Kết quả phiên bản hợp lệ hiện tại là `4 false`. Rollback chỉ dùng khi đã đánh giá
mất dữ liệu và có backup/restore plan:

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

CI tạo PostgreSQL 17 tạm thời, chạy migration từ database sạch rồi chạy integration
test. Bài test Neon cục bộ dùng transaction bao ngoài và rollback nên không để lại
user, tenant, class hoặc outbox fixture.

## Quy tắc thay đổi schema

1. Không sửa migration đã chạy; tạo migration số tiếp theo với cặp `up/down`.
2. Migration phải chạy được từ database sạch và từ version liền trước.
3. Mọi bảng nghiệp vụ tenant-scoped phải có `tenant_id`, index phù hợp và deny test.
4. Mọi repository phải nhận tenant context; không dùng tenant do request body tự khai.
5. Không ghi password, access token, session token hoặc secret thô vào database/log.
6. Event cần độ bền phải ghi bằng outbox trong cùng transaction với dữ liệu nghiệp vụ.
7. Cập nhật OpenAPI/generated client khi thay đổi contract công khai.

## Việc còn lại

- P1-06 đã triển khai OIDC/BFF, session rotation, CSRF và `/api/v1/me`; ZITADEL local đã được provision, client staging thuộc P1-10.
- P1-06B đã hoàn thành list/create/detail class; enrollment, invite code và roster thuộc Phase 2.
- P1-10 tạo database/branch staging riêng, runtime role và migration role riêng.
- Chưa import dữ liệu TutorHub V1; migration V1 sẽ làm theo module/cohort ở phase sau.
- Chưa có backup/restore drill, PITR gate hoặc connection load test cho pilot.
