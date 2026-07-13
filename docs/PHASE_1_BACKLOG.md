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

**Trạng thái 2026-07-13:** hoàn thành; verify cục bộ trên Windows và workflow `Verify` trên GitHub Actions Linux đều thành công.

## P1-02 Web shell

- [x] React + TypeScript strict + Vite.
- [x] React Router, TanStack Query; state local trước, chỉ dùng store toàn cục khi cần.
- [x] Error boundary, route guards, 404/403/error/offline states.
- [x] i18n `vi` và `en` từ đầu.
- [x] Layout desktop/tablet/mobile cơ bản.

**Trạng thái 2026-07-13:** hoàn thành và đã merge vào `main` qua PR #2 tại commit `6e2f98e`. Đã kiểm tra lint, TypeScript, 6 Vitest tests, production build và giao diện desktop/mobile với Core API cục bộ.

## P1-03 Design system

- [ ] Token màu, typography, spacing, radius, shadow, motion, z-index và breakpoint.
- [ ] Button, icon button, input, select, dialog, drawer, menu, tabs, tooltip, toast, skeleton và empty state.
- [ ] Storybook, keyboard navigation và contrast check.

## P1-04 Go core API

**Trạng thái 2026-07-13:** hoàn thành cục bộ trên branch `codex/p1-04-core-api-foundation`; `pnpm verify`, runtime smoke và OpenAPI format đều đạt. Tracing hiện là adapter no-op để gắn provider OpenTelemetry sau khi có ADR.

- [x] Cấu trúc `cmd`, `internal/platform` và `internal/modules`.
- [x] Config từ environment, validation lúc khởi động.
- [x] Structured JSON log, request ID, panic recovery, metrics và tracing adapter.
- [x] Health/live/readiness endpoint.
- [x] Error response theo Problem Details và version `/api/v1`.

## P1-05 Contract và database

- [x] OpenAPI source of truth.
- [x] Generate TypeScript API client và kiểm tra diff trong CI.
- [x] PostgreSQL migration cho users, identities, tenants, memberships, sessions và classes.
- [x] Repository nhận tenant context và integration test bằng PostgreSQL thật trong CI/Neon.
- [x] Transactional outbox schema và ghi `class.created` cùng transaction.

**Trạng thái 2026-07-13:** hoàn thành cục bộ trên branch
`codex/p1-05-contract-database`. Neon đã migrate đến version `3`, `dirty=false`;
integration test có rollback, runtime smoke `/ready` và toàn bộ `pnpm verify` đều đạt.
Role Neon hiện tại là owner tạm thời; tách runtime/migration role vẫn thuộc P1-10.

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
