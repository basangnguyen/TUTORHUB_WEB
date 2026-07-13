# TutorHub V2 - Current Project State

> Cập nhật file này sau mỗi task/phase. Đây là điểm vào nhanh cho agent mới.

## Snapshot

| Thuộc tính | Trạng thái |
|---|---|
| Ngày cập nhật | 2026-07-13 |
| Repository chính thức | `https://github.com/basangnguyen/TUTORHUB_WEB` |
| Remote / default branch | `origin` / `main` |
| Branch | `main` |
| Phase hoàn thành | Phase 0 |
| Phase hiện tại | Phase 1 - Engineering foundation |
| Task hiện tại | Bootstrap GitHub: initial commit và push đầu tiên |
| Task kế tiếp | Xác nhận CI, sau đó P1-02 Web shell và P1-04 Core API foundation |
| Application code V2 | React health app + Go health API |
| Git commit đầu tiên | Chưa tạo |

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

## Chưa thực hiện

- Chưa tạo database migration V2 hoặc generated OpenAPI client.
- Chưa chọn OIDC provider, managed Redis hoặc observability provider.
- Chưa tạo Neon/B2/HF staging resource cho V2.
- Chưa xác nhận GitHub Actions CI do repository chưa có remote/push đầu tiên.
- Chưa deployment V2 lên Cloudflare/HF.
- Chưa migrate dữ liệu V1.

## Việc tiếp theo

1. Tạo initial commit và đẩy lên Git hosting để xác nhận Linux CI.
2. Bật branch protection/status checks sau khi workflow đầu tiên chạy thành công.
3. Bắt đầu P1-02: app shell, routing, error boundary và i18n.
4. Song song P1-04: Problem Details, status recorder, config validation và observability skeleton.
5. Chưa kết nối Neon/B2/LiveKit trước khi các nền trên ổn định.

## Verify gần nhất

- `pnpm verify`: đạt, 20 Turbo tasks.
- Vitest: 4 tests đạt.
- `go test ./services/core-api/...`: đạt.
- `go vet ./services/core-api/...`: đạt.
- Runtime: `http://127.0.0.1:8080/health` và `http://127.0.0.1:5173/api/health` trả `ok`.
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
