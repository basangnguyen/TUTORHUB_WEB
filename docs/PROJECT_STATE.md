# TutorHub V2 - Current Project State

> Cập nhật file này sau mỗi task/phase. Đây là điểm vào nhanh cho agent mới.

## Snapshot

| Thuộc tính              | Trạng thái                                                                                                                           |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Ngày cập nhật           | 2026-07-14                                                                                                                           |
| Repository chính thức   | `https://github.com/basangnguyen/TUTORHUB_WEB`                                                                                       |
| Remote / default branch | `origin` / `main`                                                                                                                    |
| Branch                  | `codex/p1-07-livekit-spike`                                                                                                          |
| Phase hoàn thành        | Phase 0                                                                                                                              |
| Phase hiện tại          | Phase 1 - Engineering foundation                                                                                                     |
| Task hiện tại           | P1-07 LiveKit spike: implementation cục bộ hoàn tất, chờ project staging và smoke test thật 2-5 người                                |
| Task kế tiếp            | Provision LiveKit staging credential/webhook rồi chạy ma trận camera, micro, screen share và reconnect                               |
| Application code V2     | React multi-tenant web shell + classroom/prejoin/LiveKit room UI + generated API client + Go Core API/OIDC/database/media foundation |
| Git commit đầu tiên     | `33af851` - `chore(bootstrap): initialize TutorHub V2 foundation`                                                                    |

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
- Neon đã migrate đến version `4`, `dirty=false`; classroom và identity integration test rollback toàn bộ fixture.
- Đồng bộ TypeScript `5.9.3` với peer contract của `openapi-typescript 7.13.0`.
- Tạo checkpoint cục bộ P1-04/P1-05 tại commit `e9ab598` và tách branch P1-06.
- Chấp nhận ADR-0008: ZITADEL Cloud cho local/staging, fake OIDC issuer cho test.
- Hoàn thiện cục bộ P1-06: OIDC Authorization Code + PKCE `S256`, state/nonce/browser binding one-time, verified ID token, UserInfo và provider-neutral adapter.
- Thêm opaque session cookie, keyed session/CSRF hash, idle + absolute timeout, revoke/logout, `/api/v1/me` và secure `__Host-` cookie ở HTTPS.
- Migration 004 bổ sung auth flow, identity verification và session lifecycle; Neon identity integration test kiểm tra replay, hash token, permission, CSRF và revoke.
- Đồng bộ OpenAPI/generated TypeScript client; React bỏ demo session mặc định, hydrate `/me`, có sign-in/signed-out/error guard và logout CSRF.
- Provision ZITADEL application `tutorhub-local`; secret chỉ nằm trong `.env.local` đã Git-ignore.
- Browser smoke thật đạt login/callback, `/api/v1/me`, reload giữ phiên, CSRF, logout/revoke, post-logout redirect và route guard.
- Sửa adapter OIDC theo chuẩn ZITADEL: profile/email lấy từ UserInfo sau khi xác minh ID token; `sub` sai khác bị từ chối và có test hồi quy.
- Hoàn thiện P1-06A: người dùng chưa có membership được điều hướng đến onboarding để tạo workspace đầu tiên.
- Transaction tạo đồng thời tenant, membership `org_admin`, active tenant trong session và sự kiện outbox `tenant.created`; khóa user/session ngăn tạo trùng do race.
- Thêm API tạo workspace và đổi active workspace với session + double-submit CSRF; cả hai thao tác đều xoay opaque session token và CSRF token.
- Web có workspace gate, form onboarding, trạng thái chọn workspace, workspace selector ở topbar và cập nhật session/query cache sau mutation.
- OpenAPI, generated TypeScript client, unit/HTTP/web test và Neon integration test bao phủ tạo workspace, đổi workspace và từ chối truy cập chéo tenant.
- Hoàn thành P1-06B list/create/detail class; tenant, actor và permission chỉ lấy từ authenticated active session.
- API tạo lớp dùng strict JSON, double-submit CSRF, permission `class.create`, chuẩn hóa dữ liệu và trả Problem Details cho 400/403/404/409.
- Web lớp học song ngữ có loading, empty, error, forbidden, not-found, retry và cache key tách theo tenant; create thành công mở trang chi tiết.
- OpenAPI/generated client và test contract được đồng bộ; Neon tenant-isolation test xác nhận truy vấn lớp không đọc chéo tenant và rollback fixture.
- P1-07 đã thêm LiveKit config fail-fast, backend token TTL ngắn với grant tối thiểu, room/participant identity theo tenant-class-session và không đưa secret xuống frontend.
- Thêm prejoin lazy route, device preview, speaker test, listen-only mode, room video grid, camera/micro/screen share, reconnect state và credential chỉ giữ trong React memory.
- Thêm join/reconnect telemetry có schema giới hạn; webhook xác minh chữ ký bằng thư viện LiveKit chính thức và receipt idempotent trong PostgreSQL.
- OpenAPI/generated client, HTTP/media/web tests và production build đã được cập nhật; Neon đã migrate migration 005 đến version `5`, `dirty=false`.

## Chưa thực hiện

- Chưa provision `tutorhub-staging`; việc này thuộc P1-10 sau khi có URL HTTPS staging.
- Chưa chọn managed Redis hoặc observability provider.
- Chưa tạo Neon staging tách biệt và runtime/migration role tối thiểu quyền; Neon owner hiện chỉ dùng tạm cho tích hợp P1-05.
- Chưa tạo B2/HF staging resource cho V2.
- Chưa deployment V2 lên Cloudflare/HF.
- Chưa migrate dữ liệu V1.
- Chưa thêm audit query/UI cho các thao tác tenant nhạy cảm; audit read model thuộc phase bảo mật/vận hành tiếp theo.
- Chưa có enrollment, invite code, roster hoặc quyền theo từng lớp; P1-06B mới dùng quyền membership cấp tenant, các phần này thuộc Phase 2.
- Chưa có `LIVEKIT_URL`, API key/secret trong local secret file; chưa thể chạy smoke test phòng thật 2-5 người hoặc webhook staging.

## Việc tiếp theo

1. Tạo project LiveKit staging riêng, lưu ba credential bằng secret store/local ignored file và cấu hình webhook HTTPS.
2. Chạy smoke test teacher + student và mở rộng đến 5 người theo `docs/LIVEKIT_SPIKE_RUNBOOK.md`.
3. P1-03 có thể làm song song nếu ownership không trùng app shell, classroom hoặc media route.
4. Provision `tutorhub-staging` trong P1-10 bằng URL HTTPS và secret riêng.

## Verify gần nhất

- `pnpm verify`: đạt trên Windows sau khi thêm `.tools\go\bin` vào `PATH`; format, generated contract, lint, typecheck, test, build, Go test/vet đều xanh.
- Vitest: web 18 tests và API client 7 tests đạt, gồm media browser support, route state, API token/telemetry và recovery state.
- `pnpm peers check`: đạt, không còn peer dependency mismatch.
- `go test ./services/core-api/...` và `go vet ./services/core-api/...`: đạt.
- Neon migration: version `5`, `dirty=false`; classroom, identity và LiveKit webhook receipt integration đạt, gồm onboarding workspace, session rotation, class create/list/get, deny truy cập chéo tenant và idempotency theo event ID; toàn bộ fixture kiểm thử được rollback.
- ZITADEL local browser smoke: callback `303`, `/api/v1/me` `200`, reload giữ phiên; CSRF `200`, logout `200`, sau revoke `/api/v1/me` `401`.
- Runtime smoke với Neon/ZITADEL config: web `200`, `/ready=200`, `/health=200`; `GET /api/v1/classes` khi chưa xác thực bị từ chối `401`.
- Production build tách LiveKit thành lazy chunk riêng; chunk media hiện khoảng `595.50 kB` (`160.05 kB` gzip), cần tiếp tục theo dõi và tối ưu ở giai đoạn performance budget.
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
- `docs/AUTHENTICATION.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/SECURITY_BASELINE.md`
- `docs/adr/*`

## Quy tắc cập nhật

Sau mỗi task, thay ngày, phase/task, mục đã hoàn thành, mục còn thiếu, rủi ro và lệnh verify gần nhất. Không ghi “hoàn thành” nếu code/test/deployment bắt buộc chưa đạt exit gate.
