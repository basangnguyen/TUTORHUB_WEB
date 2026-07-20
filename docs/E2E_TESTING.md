# Browser E2E runbook

## Phạm vi

P2-08 có một scenario Playwright đi xuyên suốt giao diện thật với ba browser
context độc lập:

1. Org admin tạo, chỉnh và chuyển workspace; mời teacher/student và revoke một
   invitation.
2. Teacher/student preview, accept invitation rồi chuyển vào workspace mới.
3. Teacher tạo, chỉnh và activate class; tạo class join link.
4. Student nhập link trong UI, join và thấy class xuất hiện trong danh sách.
5. Teacher đổi class role, suspend rồi remove student, revoke link và archive
   class.
6. Org admin kiểm tra audit log; scenario smoke AppShell ở desktop, laptop nhỏ
   và mobile. Visual QA thủ công bao phủ thêm workspace, class, join dialog và
   roster tại cả ba viewport.

Scenario không seed fixture nghiệp vụ, không gọi API trực tiếp và không yêu cầu
chạy SQL thủ công.

## Chạy local

Yêu cầu Node.js, pnpm, Go, Docker Compose và Chromium của Playwright. Cài browser
một lần:

```powershell
corepack pnpm e2e:install
```

Sau đó chạy:

```powershell
corepack pnpm e2e
```

Playwright tự gọi `scripts/e2e-local.mjs serve`. Orchestrator:

- chỉ chạy fake OIDC khi `APP_ENV=test`;
- chỉ bind web/API/issuer vào loopback `5173/8080/9091`;
- khởi tạo database tách biệt có tên chính xác `tutorhub_e2e`;
- không seed dữ liệu; scenario tạo fixture qua UI;
- sinh OIDC client secret, session secret và khóa ký tạm thời trong memory cho
  từng lượt chạy;
- xóa cấu hình B2/LiveKit kế thừa khỏi process con;
- build và chạy fake OIDC, Core API cùng Vite, rồi dừng và chờ toàn bộ process
  tree kết thúc khi Playwright thoát.

Ở mode mặc định `managed`, script khởi động PostgreSQL từ Compose, sau đó
drop/create riêng database `tutorhub_e2e` trước khi migrate. Guard từ chối URL
không phải loopback, protocol không phải PostgreSQL, database có tên khác hoặc
query parameter ngoài đúng `sslmode=disable`.
Không dùng lệnh này nếu cần giữ dữ liệu trong database local đó.

Các kiểm tra hạ tầng không cần Docker:

```powershell
corepack pnpm e2e:infra:test
corepack pnpm e2e:typecheck
```

## PostgreSQL do caller quản lý và CI

CI dùng PostgreSQL 17 service và không cho orchestrator drop database:

```powershell
$env:E2E_DATABASE_MODE = "external"
$env:E2E_DATABASE_URL = "<loopback PostgreSQL URL tới database tutorhub_e2e>"
corepack pnpm e2e
```

Mode `external` vẫn bắt buộc loopback + database `tutorhub_e2e` và vẫn chạy
migration, nhưng không drop/create database. Workflow `Verify` cài Chromium rồi
chạy scenario trong job `Browser E2E`.

## Chạy trên staging

Staging không khởi động local web/API/IdP. Chỉ chạy trên fixture staging dùng một
lần hoặc có thể bỏ:

```powershell
$env:E2E_MODE = "staging"
$env:E2E_BASE_URL = "https://<staging-web-origin>"
$env:E2E_ALLOW_STAGING_MUTATIONS = "true"
$env:E2E_ADMIN_STORAGE_STATE = "<path-to-admin-state>"
$env:E2E_TEACHER_STORAGE_STATE = "<path-to-teacher-state>"
$env:E2E_STUDENT_STORAGE_STATE = "<path-to-student-state>"
$env:E2E_TEACHER_EMAIL = "<verified-teacher-email>"
$env:E2E_STUDENT_EMAIL = "<verified-student-email>"
corepack pnpm e2e
```

Ba storage state phải thuộc ba tài khoản khác nhau. Admin cần quyền tạo
workspace; email teacher/student phải trùng identity đã xác minh trong storage
state tương ứng. Cờ `E2E_ALLOW_STAGING_MUTATIONS=true` là xác nhận rõ rằng
scenario sẽ tạo workspace/class/invitation và thay đổi roster trên fixture đó.

Không chạy staging mutation trên production hoặc tenant chứa dữ liệu thật.
Scenario dùng tên duy nhất theo thời gian nhưng không tự xóa fixture sau khi
chạy.

### Kết quả staging P2-08 (2026-07-20)

Acceptance được chạy qua UI staging thật trên fixture dùng một lần với ba tài khoản
ZITADEL đã xác minh riêng biệt cho org admin, teacher và student. Cả sáu bước runbook
đều đạt: workspace/invitation lifecycle; teacher class lifecycle và join link;
student join; role/suspend/remove; revoke link và archive; audit đúng actor, request
ID và resource.

Lượt nghiệm thu này không dùng SQL/manual API, không tạo hoặc lưu storage state và
không ghi invitation token, cookie hay secret vào repository, log hoặc artifact. Đây
là acceptance UI staging thực tế; lệnh `corepack pnpm e2e` ở mode staging không được
chạy trong lượt này. Scenario Playwright tương ứng đã xanh riêng trên CI Verify #59.

## Bảo vệ credential và artifact

- Không đặt storage state, token hoặc secret trong source, command history hay
  output CI.
- Đặt storage state ngắn hạn ngoài repository hoặc trong `e2e/.auth/` đã
  Git-ignore; giới hạn quyền đọc cho tài khoản chạy test và xóa sau lượt chạy.
- Không upload storage state, trace, video, screenshot hay `test-results/`.
- Reporter mặc định chỉ in dòng kết quả; trace/video/screenshot bị tắt để giảm
  nguy cơ giữ invitation token hoặc cookie.
- Invitation token chỉ được truyền trong fragment tạm thời hoặc input được che,
  bị scrub khỏi URL và không nằm trong React Query key/cache hay browser storage.
- Không chạy staging E2E với artifact lấy từ pull request không tin cậy.
