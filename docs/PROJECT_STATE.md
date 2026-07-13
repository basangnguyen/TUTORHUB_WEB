# TutorHub V2 - Current Project State

> Cập nhật file này sau mỗi task/phase. Đây là điểm vào nhanh cho agent mới.

## Snapshot

| Thuộc tính | Trạng thái |
|---|---|
| Ngày cập nhật | 2026-07-13 |
| Repository chính thức | `https://github.com/basangnguyen/TUTORHUB_WEB` |
| Remote / default branch | `origin` / `main` |
| Branch | `codex/p1-05-contract-database` |
| Phase hoàn thành | Phase 0 |
| Phase hiện tại | Phase 1 - Engineering foundation |
| Task hiện tại | P1-05 hoàn thành cục bộ, đang chờ review/commit cùng P1-04 |
| Task kế tiếp | P1-06 Authentication spike |
| Application code V2 | React web shell + generated API client + Go Core API/database foundation |
| Git commit đầu tiên | `33af851` - `chore(bootstrap): initialize TutorHub V2 foundation` |

## Đã hoàn thành

- Khởi tạo repository `D:\TutorHub_V2`.
- Viết product scope, Web MVP, system context, domain/permission và migration map.
- Viết security baseline, deployment baseline và roadmap.
- Chấp nhận ADR-0001 đến ADR-0007.
- Chọn React/TypeScript/Vite, Go modular monolith, LiveKit Cloud.
- Chọn Neon, Hugging Face Docker Spaces và Backblaze B2 cho MVP/private beta.
- Khóa private alpha 0 đồng: Cloudflare Pages cho web, HF cho Go API/AI, chưa dùng Redis.
- Scaffold pnpm/Turborepo, React/Vite, shared packages và Go workspace.
- Tạo OpenAPI health contract, Go health/live/ready endpoints và React health screen.
- Thêm formatter, ESLint, TypeScript strict, Vitest, Go test/vet, pre-commit hook và GitHub Actions workflow.
- `pnpm verify` đạt trên Windows; health API trực tiếp và qua Vite proxy đều trả `ok`.
- Xác định V1 tại `D:\Ban_sao_du_an` là read-only reference.
- Xác định `basangnguyen/TUTORHUB_WEB` là repository GitHub chính thức và tạo quy trình điều phối đa-agent tại `docs/AGENT_COORDINATION.md`.
- Thêm GitHub Issue template và Pull Request template để khóa ownership, ghi phạm vi, test và bàn giao thống nhất giữa nhiều agent.
- Push initial commit `33af851` lên `origin/main`; GitHub Actions workflow `Verify` đã chạy thành công.
- Hoàn thành P1-02 Web shell: React Router, TanStack Query, session guard demo, i18n vi/en, các route trạng thái và layout responsive.
- P1-02 đã merge vào `main` qua PR #2 tại commit `6e2f98e`; 6 Vitest tests, lint, TypeScript, production build và kiểm tra UI desktop/mobile đều đạt.
- Viết lại `docs/MASTER_PLAN.md` phiên bản 2.0 theo phạm vi web-first: audit Zoom/Google Meet/Teams, tách các plane kiến trúc, roadmap 90 ngày, Phase 0-9 và exit gate.
- Hoàn thành cục bộ P1-04: config fail-fast, HTTP server timeout/graceful shutdown, JSON logging, request ID, status recorder, panic recovery và RFC 9457 Problem Details.
- Thêm `/api/v1/status`, readiness dependency model, Prometheus metrics tối thiểu và tracing adapter no-op.
- Thêm test cho config, middleware, Problem Details, panic, metrics và server lifecycle; cập nhật OpenAPI và tài liệu cấu hình.
- Thêm `.prettierrc.json` với `endOfLine: auto` để `pnpm verify` ổn định trên Windows/Linux.
- Hoàn thành cục bộ P1-05: OpenAPI source of truth, generated TypeScript client và CI stale-contract check.
- Thêm migration runner và schema PostgreSQL cho users, identities, tenants, memberships, sessions, classes và transactional outbox.
- Thêm pool Neon, readiness database, tenant context bắt buộc và classroom repository luôn lọc tenant.
- `CreateClass` ghi `class.created` vào outbox cùng transaction; deny test xác nhận không đọc chéo tenant.
- Neon đã migrate đến version `3`, `dirty=false`; integration test rollback toàn bộ fixture và runtime smoke trả `/ready=ready`.
- Đồng bộ TypeScript `5.9.3` với peer contract của `openapi-typescript 7.13.0`.

## Chưa thực hiện

- Chưa chọn OIDC provider, managed Redis hoặc observability provider.
- Chưa tạo Neon staging tách biệt và runtime/migration role tối thiểu quyền; Neon owner hiện chỉ dùng tạm cho tích hợp P1-05.
- Chưa tạo B2/HF staging resource cho V2.
- Chưa deployment V2 lên Cloudflare/HF.
- Chưa migrate dữ liệu V1.

## Việc tiếp theo

1. Review/commit P1-04 và P1-05 trên branch `codex/p1-05-contract-database`.
2. Bắt đầu P1-06: chọn IdP, OIDC Authorization Code + PKCE qua BFF, session cookie/CSRF và `/api/v1/me`.
3. Thực hiện P1-03 design system song song nếu không sửa cùng vùng auth/app shell.
4. Chưa xây classroom đầy đủ, QuizHub hoặc Lavie trước khi P1-06 đến P1-07 đạt gate.

## Verify gần nhất

- `pnpm verify`: đạt trên Windows sau khi thêm `.tools\go\bin` vào `PATH`; format, generated contract, lint, typecheck, test, build, Go test/vet đều xanh.
- Vitest: web shell 6 tests và API client 3 tests đạt.
- `pnpm peers check`: đạt, không còn peer dependency mismatch.
- `go test ./services/core-api/...` và `go vet ./services/core-api/...`: đạt.
- Neon migration: version `3`, `dirty=false`; classroom integration test đạt và rollback fixture.
- Runtime smoke với Neon: `/ready` trả `ready`, `/health` trả `ok`.
- `pnpm exec prettier --check openapi/tutorhub.yaml`: đạt.
- GitHub Actions `Verify` cho commit `33af851`: thành công.
- Docker: chưa cài trên máy; chưa build image HF local.

## Cảnh báo bảo mật

- Không dùng lại token/key từng xuất hiện ở V1 hoặc hội thoại cũ.
- Không copy `AppConfig.java`, `.env`, HF token, Gemini key, B2 key, Neon URL hoặc LiveKit secret từ V1.
- Credential V2 phải được tạo mới, tách local/staging/production và lưu bằng secret store.

## Tài liệu bắt buộc

- `docs/MASTER_PLAN.md`
- `docs/AGENT_COORDINATION.md`
- `docs/PHASE_1_BACKLOG.md`
- `docs/DATABASE.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/SECURITY_BASELINE.md`
- `docs/adr/*`

## Quy tắc cập nhật

Sau mỗi task, thay ngày, phase/task, mục đã hoàn thành, mục còn thiếu, rủi ro và lệnh verify gần nhất. Không ghi “hoàn thành” nếu code/test/deployment bắt buộc chưa đạt exit gate.
