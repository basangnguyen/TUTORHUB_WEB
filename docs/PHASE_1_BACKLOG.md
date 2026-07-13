# Backlog Phase 1 - Engineering Foundation

## Mục tiêu

Tạo nền kỹ thuật có thể triển khai staging và chứng minh vertical spike: đăng nhập -> `/me` -> danh sách lớp giả/lưu DB -> cấp LiveKit token test -> vào phòng test.

## P1-01 Repository và toolchain

- [x] Tạo pnpm workspace và Turborepo.
- [x] Tạo `apps/web`, `packages/ui`, `packages/design-tokens`, `packages/api-client`, `packages/domain`.
- [x] Tạo Go workspace với `services/core-api`.
- [x] Thiết lập Node LTS, pnpm và Go version pin.
- [x] Thêm `.editorconfig`, formatter, lint, typecheck và commit hooks tối thiểu.

**Done:** clean clone chạy được một lệnh setup và một lệnh verify trên Windows/Linux CI.

**Trạng thái 2026-07-12:** hoàn thành và verify cục bộ trên Windows. Workflow Linux đã tạo nhưng cần lần push đầu tiên để xác nhận trên GitHub Actions.

## P1-02 Web shell

- [x] React + TypeScript strict + Vite.
- [x] React Router, TanStack Query; state local trước, chỉ dùng store toàn cục khi cần.
- [x] Error boundary, route guards, 404/403/error/offline states.
- [x] i18n `vi` và `en` từ đầu.
- [x] Layout desktop/tablet/mobile cơ bản.

**Trạng thái 2026-07-13:** hoàn thành trên nhánh `codex/p1-02-web-shell`, đang chờ review qua Issue #1. Đã kiểm tra lint, TypeScript, 6 Vitest tests, production build và giao diện desktop/mobile với Core API cục bộ.

## P1-03 Design system

- [ ] Token màu, typography, spacing, radius, shadow, motion, z-index và breakpoint.
- [ ] Button, icon button, input, select, dialog, drawer, menu, tabs, tooltip, toast, skeleton và empty state.
- [ ] Storybook, keyboard navigation và contrast check.

## P1-04 Go core API

- [ ] Cấu trúc `cmd`, `internal/platform`, `internal/modules` và migration.
- [ ] Config từ environment, validation lúc khởi động.
- [ ] Structured JSON log, request ID, panic recovery, metrics, tracing.
- [ ] Health/live/readiness endpoint.
- [ ] Error response theo Problem Details và version `/api/v1`.

## P1-05 Contract và database

- [ ] OpenAPI source of truth.
- [ ] Generate TypeScript API client trong CI.
- [ ] PostgreSQL migration cho users, tenants, memberships, sessions, classes.
- [ ] Repository nhận tenant context và integration test bằng database thật trong container.

## P1-06 Authentication spike

- [ ] Chọn IdP cho local/staging và tạo OIDC clients tách biệt.
- [ ] Authorization Code + PKCE qua BFF.
- [ ] Session cookie, CSRF, logout/revoke và `/api/v1/me`.
- [ ] Không lưu token trong localStorage.

## P1-07 LiveKit spike

- [ ] Tạo LiveKit project riêng cho staging.
- [ ] API cấp token từ backend với grant tối thiểu.
- [ ] Trang prejoin và room test camera/mic/reconnect.
- [ ] Ghi telemetry join failure; không đưa secret LiveKit vào frontend.

## P1-08 CI/CD và security

- [ ] PR pipeline: format/lint -> typecheck -> unit -> integration -> build -> scan.
- [ ] Secret, dependency, SAST và container scan.
- [ ] Preview deployment cho web và staging deployment cho API.
- [ ] Branch protection, CODEOWNERS, dependency update automation.

## P1-09 Local developer experience

- [ ] Docker Compose cho PostgreSQL, Redis và service phụ trợ local.
- [ ] `.env.example` chỉ chứa tên biến và giá trị giả an toàn.
- [ ] Seed dữ liệu giả có tenant/teacher/student.
- [ ] `docs/DEVELOPMENT.md` và troubleshooting Windows.

## P1-10 Cloud foundation

- [ ] Tạo Neon project/branch tách biệt cho staging; runtime role và migration role riêng.
- [ ] Tạo B2 bucket staging, application key tối thiểu quyền và lifecycle policy.
- [ ] Tạo Cloudflare Pages project cho `tutorhub-web` và Hugging Face Docker Space cho `tutorhub-core-api`.
- [ ] Lưu credential bằng HF Secrets; xác nhận không xuất hiện trong image/log/frontend bundle.
- [ ] Thêm health/readiness, graceful shutdown và deploy rollback.
- [ ] Spike cold start, restart, HTTP concurrency và WebSocket/SSE trên Space thực.
- [ ] Ghi lại connection budget của Neon và upload/download flow B2.

## Thứ tự sprint đề xuất

1. Repo/toolchain + API skeleton + design tokens.
2. OpenAPI/PostgreSQL + app shell.
3. OIDC/BFF + `/me`.
4. Class vertical slice.
5. LiveKit spike + staging demo.

## Exit gate Phase 1

- CI xanh từ clean clone.
- Staging có HTTPS, OIDC và observability tối thiểu.
- Staging/alpha chạy web trên Cloudflare Pages, API trên HF Space, dùng Neon và B2 tách biệt với production tương lai.
- Một teacher và một student test có thể đăng nhập và vào cùng phòng LiveKit test.
- Không có secret trong Git history hoặc frontend bundle.
- ADR, OpenAPI và runbook được cập nhật.
