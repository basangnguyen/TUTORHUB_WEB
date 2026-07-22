# Browser E2E runbook

## Phạm vi

Scenario Playwright khởi tạo từ P2-08 và được mở rộng cho closure P2-12, đi xuyên
suốt giao diện thật với ba browser context độc lập:

1. Org admin tạo, chỉnh và chuyển workspace; mời teacher/student và revoke một
   invitation.
2. Teacher/student preview, accept invitation rồi chuyển vào workspace mới.
3. Teacher tạo, chỉnh và activate class; tạo class join link TTL một ngày, giới
   hạn hai lượt và xác nhận bộ đếm ban đầu `0/2`.
4. Student nhập link trong UI, join, thấy class xuất hiện trong danh sách và
   teacher thấy bộ đếm chuyển thành `1/2`.
5. Teacher đổi class role, suspend rồi remove student, sau đó archive class mà
   không revoke link. Trang class archived vẫn giữ roster lịch sử và link đang
   active với bộ đếm `1/2`.
6. Một actor cùng tenant nhưng chưa enrolled thử dùng link còn active sau archive
   và phải nhận lỗi unavailable chung; org admin kiểm tra audit đúng actor,
   request ID và resource. Scenario smoke AppShell ở desktop, laptop nhỏ và
   mobile; visual QA thủ công bao phủ thêm workspace, class, join dialog và
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

### Trạng thái closure P2-12 (2026-07-22)

Source scenario đã được mở rộng để kiểm tra `0/2 -> 1/2`, retention của roster/link
sau archive và archive guard đối với lượt join mới. Sau khi sửa locator audit để chỉ
chọn action `Create class`, commit `6fb4f84` đã đạt
[Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433), gồm
Browser E2E PostgreSQL 17 + Chromium, và
[Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424).
Kết quả staging P2-08 ở trên là bằng chứng lịch sử và không thay thế lượt acceptance
cuối của P2-12. Không đánh dấu Phase 2 hoàn tất cho đến khi commit đóng phase cùng
staging acceptance cuối đều đạt.

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
