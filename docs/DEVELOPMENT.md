# Local development

## Toolchain bắt buộc

- Node.js `>=24.14.0 <25` và Corepack.
- pnpm `11.7.x`.
- Go `1.26.5`.
- Docker Desktop trên Windows hoặc Docker Engine + Compose plugin trên Linux.
- Git.

Docker chỉ chạy PostgreSQL 17 và Redis. Core API và Vite chạy trực tiếp trên máy để
giữ hot reload nhanh. Các cổng container chỉ bind vào `127.0.0.1`, không mở ra mạng LAN.

## Khởi tạo lần đầu

Tại thư mục gốc repository:

```powershell
corepack enable
corepack pnpm install
corepack pnpm local:setup
```

`local:setup` thực hiện tuần tự bốn việc:

1. Khởi động PostgreSQL và Redis, chờ health check đạt.
2. Chạy toàn bộ migration bằng `DATABASE_MIGRATION_URL` local.
3. Seed dữ liệu phát triển có UUID cố định.
4. Có thể chạy lại nhiều lần mà không nhân đôi dữ liệu.

Lệnh local tự ép `APP_ENV=development`, dùng database loopback và xóa OIDC, B2,
LiveKit cùng session secret khỏi process con. Vì vậy lệnh seed không thể chạy vào
staging hoặc production do vô tình kế thừa biến môi trường của terminal.

## Chạy ứng dụng bằng một lệnh

```powershell
corepack pnpm dev:local
```

Lệnh này chạy `local:setup`, sau đó khởi động Core API và Vite song song. Dừng cả hai
bằng `Ctrl+C`.

| Thành phần | URL local |
|---|---|
| Web | `http://localhost:5173` |
| Core API | `http://localhost:8080` |
| Health | `http://localhost:8080/health` |
| Readiness | `http://localhost:8080/ready` |
| PostgreSQL | `localhost:5432`, database/user `tutorhub` |
| Redis | `localhost:6379` |

Vite chuyển tiếp `/api` tới Core API ở cổng `8080`. OIDC và object storage được tắt
trong cấu hình mặc định local; health, status và public web shell vẫn phát triển được
độc lập.

## Dữ liệu mẫu

Seed chỉ được phép chạy khi `APP_ENV` là `development` hoặc `test`.

| Loại | Dữ liệu |
|---|---|
| Tenant | `tutorhub-demo` - Trường học mẫu TutorHub |
| Giảng viên | `giangvien.demo@tutorhub.local`, múi giờ `Asia/Ho_Chi_Minh` |
| Học viên | `hocsinh.demo@tutorhub.local`, múi giờ `UTC` |
| Lớp học | `DEMO-VI-01` - Lớp học trực tuyến mẫu |

Hai múi giờ được giữ có chủ ý để phát hiện lỗi chuyển đổi thời gian giữa Việt Nam
và UTC ngay trong quá trình phát triển.

## Lệnh quản lý môi trường

```powershell
corepack pnpm local:status
corepack pnpm local:down
corepack pnpm local:reset -- --yes
```

`local:down` giữ volume. `local:reset` xóa toàn bộ dữ liệu PostgreSQL/Redis local rồi
migrate và seed lại; script nội bộ yêu cầu cờ `--yes` để tránh xóa nhầm.

Các lệnh database riêng:

```powershell
corepack pnpm db:version
corepack pnpm db:migrate
corepack pnpm db:seed
corepack pnpm test:integration
```

Migration không chạy tự động khi Core API khởi động. Quy trình migration, rollback và
tenant boundary chi tiết nằm trong [DATABASE.md](DATABASE.md).

## API contract

`openapi/tutorhub.yaml` là nguồn contract. Sau khi sửa contract:

```powershell
corepack pnpm api:generate
corepack pnpm api:check
```

`api:check` thất bại nếu generated TypeScript client khác nguồn OpenAPI.

## Cấu hình xác thực tùy chọn

Không cấu hình OIDC thì auth endpoints trả `503`, còn health/status vẫn hoạt động.
Muốn kiểm tra ZITADEL local, chạy API/web thủ công với các biến `OIDC_*` và
`SESSION_SECRET` trong `.env.local`; không dùng `dev:local` vì lệnh này chủ động tắt
provider cloud. Health, status và public web shell vẫn dùng được trong chế độ này.
Chi tiết cấu hình xác thực nằm trong [AUTHENTICATION.md](AUTHENTICATION.md).

## Troubleshooting Windows

### Không kết nối được Docker Desktop

Mở Docker Desktop, chờ trạng thái Engine running rồi kiểm tra:

```powershell
docker version
docker compose version
```

Nếu báo lỗi named pipe, bật WSL 2 backend trong Docker Desktop và khởi động lại Docker.
Không chạy repository trong filesystem của distro WSL nếu đang dùng terminal Windows.

### Cổng 5432, 6379, 8080 hoặc 5173 đã được dùng

```powershell
Get-NetTCPConnection -State Listen |
  Where-Object LocalPort -In 5432,6379,8080,5173
```

Dừng PostgreSQL/Redis/Vite cũ hoặc container xung đột trước khi chạy lại. Không đổi
cổng trong `.env.example` mà không cập nhật đồng thời `compose.yaml` và script local.

### Corepack hoặc pnpm không được nhận diện

Đóng terminal sau khi cài Node.js, mở lại rồi chạy `corepack enable`. Có thể dùng đầy
đủ `corepack pnpm <script>` thay cho `pnpm <script>`.

### Docker volume cũ làm migration sai

Chỉ khi không cần dữ liệu local hiện tại:

```powershell
corepack pnpm local:reset -- --yes
```

## Troubleshooting Linux

Nếu Docker yêu cầu `sudo`, thêm tài khoản vào nhóm `docker`, đăng xuất rồi đăng nhập lại:

```bash
sudo usermod -aG docker "$USER"
docker version
docker compose version
```

Kiểm tra cổng bằng `ss -ltnp | grep -E ':(5432|6379|8080|5173)'`. Trên hệ thống có
PostgreSQL hoặc Redis cài sẵn, dừng service hệ thống trước khi chạy Compose.

## Kiểm tra chất lượng

```powershell
corepack pnpm verify
```

Lệnh này kiểm tra format, local orchestrator, workflow security, lint, TypeScript,
unit test, Storybook build, frontend bundle, Go test và Go vet. CI còn chạy
`local:setup` hai lần trên container sạch và kiểm tra số lượng fixture để bảo đảm
migration/seed idempotent.

## Quy tắc bảo mật

- `.env.example` chỉ dùng credential giả cục bộ; không thay bằng secret thật.
- Secret thật chỉ đặt trong `.env*.local` bị Git-ignore hoặc secret store của provider.
- Không dùng lại credential từ TutorHub V1.
- Không chuyển `APP_ENV` của lệnh seed sang `staging` hoặc `production`; code sẽ từ chối.
