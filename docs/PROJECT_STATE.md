# TutorHub V2 - Current Project State

> Cập nhật file này sau mỗi task/phase. Đây là điểm vào nhanh cho agent mới.

## Snapshot

| Thuộc tính | Trạng thái |
|---|---|
| Ngày cập nhật | 2026-07-13 |
| Repository chính thức | `https://github.com/basangnguyen/TUTORHUB_WEB` |
| Remote / default branch | `origin` / `main` |
| Branch | `codex/p1-02-web-shell` |
| Phase hoàn thành | Phase 0 |
| Phase hiện tại | Phase 1 - Engineering foundation |
| Task hiện tại | P1-02 Web shell hoàn thành trên nhánh riêng, chờ review qua Issue #1 |
| Task kế tiếp | P1-04 Core API foundation; chỉ bắt đầu sau khi xác nhận ownership/branch |
| Application code V2 | React health app + Go health API |
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
- Hoàn thành P1-02 Web shell trên `codex/p1-02-web-shell`: React Router, TanStack Query, session guard demo, i18n vi/en, các route trạng thái và layout responsive.
- Tạo GitHub Issue #1 cho P1-02; 6 Vitest tests, lint, TypeScript, production build và kiểm tra UI desktop/mobile đều đạt cục bộ.

## Chưa thực hiện

- Chưa tạo database migration V2 hoặc generated OpenAPI client.
- Chưa chọn OIDC provider, managed Redis hoặc observability provider.
- Chưa tạo Neon/B2/HF staging resource cho V2.
- Chưa deployment V2 lên Cloudflare/HF.
- Chưa migrate dữ liệu V1.

## Việc tiếp theo

1. Bật branch protection/status checks sau khi workflow đầu tiên chạy thành công.
2. Review và merge P1-02 từ Issue #1 trước khi các vertical slice dùng lại web shell.
3. Tạo issue và branch ownership cho P1-04: Problem Details, status recorder, config validation và observability skeleton.
4. Chưa kết nối Neon/B2/LiveKit trước khi các nền trên ổn định.

## Verify gần nhất

- `pnpm verify`: đạt sau khi thêm `.tools\go\bin` vào `PATH`; 20 Turbo tasks.
- Vitest web shell: 6 tests đạt.
- `go test ./services/core-api/...`: đạt.
- `go vet ./services/core-api/...`: đạt.
- Runtime: `http://127.0.0.1:8080/health` và `http://localhost:5173/api/health` trả `ok`; desktop/mobile shell hiển thị đúng.
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
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/SECURITY_BASELINE.md`
- `docs/adr/*`

## Quy tắc cập nhật

Sau mỗi task, thay ngày, phase/task, mục đã hoàn thành, mục còn thiếu, rủi ro và lệnh verify gần nhất. Không ghi “hoàn thành” nếu code/test/deployment bắt buộc chưa đạt exit gate.
