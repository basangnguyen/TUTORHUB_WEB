# Local development

## Toolchain

- Node.js `>=24.14.0 <25`
- pnpm `11.7.x`
- Go `1.26.5`
- Docker Desktop: chưa bắt buộc cho health spike; cần từ Phase 1 database integration

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

## Verify

```powershell
pnpm verify
```

Lệnh này kiểm tra format, lint, TypeScript, test, web build, Go test và Go vet.

## Security

- Copy `.env.example` thành `.env` cục bộ khi thật sự cần.
- Không đặt credential thật vào `.env.example` hoặc commit `.env`.
- Không dùng lại credential từ TutorHub V1.
