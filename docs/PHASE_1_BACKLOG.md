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
`codex/p1-05-contract-database`. Neon đã migrate đến version `4`, `dirty=false` sau
P1-06; classroom và identity integration test đều rollback fixture;
integration test có rollback, runtime smoke `/ready` và toàn bộ `pnpm verify` đều đạt.
Role Neon hiện tại là owner tạm thời; tách runtime/migration role vẫn thuộc P1-10.

## P1-06 Authentication spike

- [x] Chọn ZITADEL Cloud cho local/staging và khóa thiết kế hai OIDC clients tách biệt.
- [x] Provision `tutorhub-local` và browser smoke đầy đủ với IdP thật.
- [ ] Provision `tutorhub-staging` khi P1-10 đã có web/API HTTPS staging.
- [x] Authorization Code + PKCE `S256` qua BFF, state/nonce/browser binding one-time.
- [x] Xác minh ID token, lấy profile/email qua UserInfo và bắt buộc `sub` khớp.
- [x] Session cookie, CSRF, logout/revoke và `/api/v1/me`.
- [x] Không lưu provider token hoặc session token trong localStorage.

**Trạng thái 2026-07-14:** implementation hoàn thành cục bộ trên branch
`codex/p1-06-authentication`; migration `4 false`, fake OIDC issuer ký RSA, unit test,
HTTP test, generated client, web remote-session test và Neon integration test đều đạt.
`tutorhub-local` đã provision; browser smoke thật đạt login, `/me`, reload giữ phiên,
CSRF, logout/revoke và route guard. `tutorhub-staging` được hoãn có chủ đích đến
P1-10 để dùng đúng URL HTTPS và secret riêng; P1-06 đang ở `REVIEW` để bàn giao.

## P1-06A Workspace onboarding prerequisite

- [x] Điều hướng tài khoản chưa có membership đến luồng tạo workspace đầu tiên.
- [x] Tạo tenant, membership `org_admin`, active tenant và `tenant.created` trong một transaction.
- [x] Chỉ cho phép đổi active tenant sang tenant có membership đang hoạt động.
- [x] Xoay session token và CSRF token sau khi tạo hoặc đổi workspace.
- [x] Cập nhật OpenAPI, generated TypeScript client và giao diện onboarding/selector song ngữ.
- [x] Bao phủ unit test, HTTP test, web test và Neon integration test có rollback.

**Trạng thái 2026-07-14:** hoàn thành cục bộ trên branch
`codex/p1-workspace-onboarding`. `pnpm verify` đạt; Neon integration test xác nhận
transaction tạo workspace, quyền `org_admin`, session rotation và tenant isolation.
Không cần migration mới vì schema migration 001/003/004 đã có đủ tenant, membership,
session và outbox. Task ở `REVIEW` để bàn giao.

## P1-06B Class vertical slice

- [x] Service lớp học chỉ nhận tenant và actor từ authenticated active session.
- [x] Permission gate server-side cho `class.view` và `class.create`; không tin `tenant_id` hoặc owner từ browser.
- [x] API list/create/detail, strict JSON, CSRF cho create và RFC 9457 Problem Details.
- [x] OpenAPI source of truth, generated TypeScript client và contract test.
- [x] Web list/create/detail song ngữ với loading, empty, error, forbidden, not-found và retry.
- [x] Cache key chứa tenant ID và được invalidation sau khi tạo lớp.
- [x] Unit test, HTTP test, web test, API client test, Neon tenant-isolation test và runtime smoke.

**Trạng thái 2026-07-14:** hoàn thành cục bộ trên branch
`codex/p1-class-vertical-slice`. Không cần migration mới và không thêm dependency;
schema `classes`/outbox từ P1-05 được tái sử dụng. Neon xác nhận migration `4`,
`dirty=false`, truy vấn không đọc chéo tenant và fixture được rollback. Enrollment,
invite code, roster và quyền theo từng lớp chưa nằm trong slice này; chúng thuộc Phase 2.
Task ở `REVIEW` để bàn giao.

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
