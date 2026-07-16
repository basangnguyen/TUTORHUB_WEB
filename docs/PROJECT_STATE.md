# TutorHub V2 - Trạng thái dự án

> Điểm vào nhanh để tiếp tục phát triển. Cập nhật sau mỗi task hoặc thay đổi hạ tầng.

## Snapshot

| Thuộc tính | Trạng thái |
| --- | --- |
| Ngày cập nhật | 2026-07-16 |
| Repository | `https://github.com/basangnguyen/TUTORHUB_WEB` |
| Nhánh làm việc | `main` |
| Quy trình | Một coding agent, commit trực tiếp vào `main`; GitHub dùng để lưu và sao lưu mã nguồn |
| Phase hoàn thành | Phase 0 |
| Phase hiện tại | Phase 1 - Engineering Foundation |
| Task vừa hoàn thành | P1-10 Cloud foundation và P1-08B staging deployment |
| Task kế tiếp | P1-09 Local developer experience, sau đó rà exit gate Phase 1 |

## Kiến trúc đang chạy

- Web: React + TypeScript + Vite trên `https://tutorhub-web.pages.dev`.
- Edge/BFF origin: Cloudflare Pages Function proxy `/api/*` tới Core API.
- Core API: Go modular monolith, OCI container trên `https://tutorhub-core-api.onrender.com`.
- Identity: ZITADEL OIDC Authorization Code + PKCE, session cookie phía BFF.
- Database: Neon PostgreSQL; staging tách branch và role runtime/migration.
- Object storage: Backblaze B2, application key tối thiểu quyền.
- Media: LiveKit Cloud; token do backend cấp, webhook được xác minh và lưu idempotent.
- Hugging Face không còn chạy Core API; chỉ là lựa chọn cho dịch vụ AI độc lập sau này.

## Đã hoàn thành

- P1-01 toolchain, monorepo, CI foundation.
- P1-02 React web shell, routing, TanStack Query, i18n và responsive states.
- P1-03 design tokens, UI primitives, Storybook và accessibility baseline.
- P1-04 Go API foundation: config fail-fast, graceful shutdown, structured log,
  request ID, Problem Details, metrics, health/live/ready.
- P1-05 OpenAPI source of truth, generated TypeScript client, PostgreSQL
  migrations, tenant context, outbox và integration tests.
- P1-06 OIDC/BFF authentication, session/CSRF/logout và workspace onboarding.
- P1-06B class vertical slice list/create/detail với authorization và tenant isolation.
- P1-07 LiveKit token service, prejoin, room UI, media controls, reconnect,
  telemetry và webhook receipt idempotent.
- P1-08A Verify/Security pipeline, Gitleaks, Dependency Review, CodeQL, Trivy,
  Dependabot, CODEOWNERS và bundle secret guard.
- P1-08B Cloudflare Pages production deployment, same-origin API proxy, Render
  Core API deployment, provider auto-deploy, health/readiness và rollback smoke.
- P1-10 staging resources: Neon, B2, Cloudflare Pages, Render, ZITADEL và LiveKit.
- Chấp nhận ADR-0011 để thay Hugging Face bằng Render cho Core API staging/private alpha.

## Kết quả acceptance staging ngày 2026-07-16

- Web và API HTTPS hoạt động; `/health` và `/ready` đạt trực tiếp và qua proxy.
- Readiness báo `database=ready` và `object_storage=ready`.
- OIDC login/callback, `/me`, reload giữ session, logout và đăng nhập lại đạt.
- Neon migrate -> rollback -> migrate đạt, version `5`, `dirty=false`; nhánh smoke
  tạm đã được kiểm tra không còn dữ liệu webhook thử nghiệm.
- B2 PUT/GET/checksum/DELETE đạt với key staging đã rotate.
- LiveKit 2-5 người đạt camera, micro, screen share và reconnect.
- LiveKit webhook HTTPS, xác minh chữ ký và idempotency đạt.
- Secret chỉ nằm trong file local bị Git-ignore hoặc secret store của provider;
  không xuất hiện trong repository, frontend bundle hoặc log kiểm thử.

## Còn lại trong Phase 1

1. P1-09: chuẩn hóa local developer experience, dữ liệu seed và troubleshooting.
2. Xác nhận bằng chứng cấu hình ruleset/required checks/security switches cho `main`.
3. Rà lại exit gate Phase 1 và lập backlog Phase 2.

## Rủi ro đã biết

- Render Free spin down khi không hoạt động và có thể cold start trên 50 giây;
  chỉ chấp nhận cho staging/private alpha.
- Chưa chọn managed Redis và observability provider cho quy mô lớn hơn.
- Enrollment, invite code, roster và quyền theo lớp thuộc Phase 2.
- Dữ liệu V1 chưa được migrate.
- LiveKit chunk phía web còn lớn và cần performance budget ở phase sau.

## Quy tắc Git hiện tại

- Làm việc và commit trực tiếp trên `main` để ưu tiên tốc độ phát triển.
- Không bắt buộc Issue, branch hoặc Pull Request cho mỗi task.
- Trước khi push: xem `git diff`, chạy kiểm tra phù hợp và quét secret.
- Không force-push `main`; branch tạm chỉ dùng cho thử nghiệm rủi ro cao.

## Tài liệu liên quan

- `docs/MASTER_PLAN.md`
- `docs/PHASE_1_BACKLOG.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/DATABASE.md`
- `docs/AUTHENTICATION.md`
- `docs/LIVEKIT_SPIKE_RUNBOOK.md`
- `docs/CI_SECURITY.md`
- `docs/adr/0011-render-core-api-staging.md`
