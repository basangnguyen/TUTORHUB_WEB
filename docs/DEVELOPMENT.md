# Local development

## Toolchain

- Node.js `>=24.14.0 <25`
- pnpm `11.7.x`
- Go `1.26.5`
- PostgreSQL 17 local hoặc Neon development branch cho database integration
- Docker Desktop: không bắt buộc nếu dùng Neon; CI dùng PostgreSQL container tạm thời

## Setup

```powershell
corepack enable
pnpm install
```

Nếu Go chưa được cài hệ thống, có thể giải nén bản Go chính thức vào `.tools\go` và thêm `.tools\go\bin` vào `PATH` của terminal hiện tại. `.tools` đã được Git ignore.

## Run

Terminal API:

```powershell
go run ./services/core-api/cmd/api
```

Terminal web:

```powershell
pnpm --filter @tutorhub/web dev
```

Mở `http://localhost:5173`. Vite proxy `/api` tới API ở cổng `8080`.

Các endpoint nền của Core API:

| Endpoint | Mục đích |
|---|---|
| `GET /health` | Thông tin health tổng quát, giữ tương thích web shell |
| `GET /live` | Liveness của process |
| `GET /ready` | Readiness và trạng thái dependency |
| `GET /api/v1/status` | Trạng thái API có version |
| `GET /metrics` | Metrics Prometheus tối thiểu; phải giới hạn ở ingress trước production |

Core API đọc cấu hình từ environment và dừng ngay nếu giá trị không hợp lệ. Các biến
không nhạy cảm và giá trị local mẫu nằm trong `.env.example`. Ở `staging` và
`production`, `PUBLIC_WEB_ORIGIN` bắt buộc dùng HTTPS.

## API contract

`openapi/tutorhub.yaml` là nguồn contract. Sau khi sửa contract:

```powershell
pnpm api:generate
pnpm api:check
```

`api:check` thất bại nếu generated TypeScript client khác source OpenAPI và cũng được
chạy trong CI.

## Database

Tạo `.env.local` từ `.env.example`. Dùng Neon pooled URL cho
`DATABASE_POOL_URL` và direct URL cho `DATABASE_MIGRATION_URL`; không commit file này.

```powershell
pnpm db:version
pnpm db:migrate
pnpm test:integration
```

Migration không chạy tự động cùng Core API. Hướng dẫn nạp environment, rollback,
schema và tenant boundary nằm trong [DATABASE.md](DATABASE.md).

## Verify

```powershell
pnpm verify
```

Lệnh này kiểm tra format, lint, TypeScript, test, web build, Go test và Go vet.

Kiểm tra riêng Core API:

```powershell
go test -cover ./services/core-api/...
go vet ./services/core-api/...
```

## Security

- Copy `.env.example` thành `.env.local` cục bộ khi thật sự cần.
- Không đặt credential thật vào `.env.example` hoặc commit `.env.local`.
- Không dùng lại credential từ TutorHub V1.
