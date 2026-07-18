# TutorHub V2 - Trạng thái dự án

> Điểm vào nhanh để tiếp tục phát triển. Cập nhật sau mỗi task hoặc thay đổi hạ tầng.

## Snapshot

| Thuộc tính          | Trạng thái                                                                            |
| ------------------- | ------------------------------------------------------------------------------------- |
| Ngày cập nhật       | 2026-07-18                                                                            |
| Repository          | `https://github.com/basangnguyen/TUTORHUB_WEB`                                        |
| Nhánh làm việc      | `main`                                                                                |
| Quy trình           | Một coding agent, commit trực tiếp vào `main`; GitHub dùng để lưu và sao lưu mã nguồn |
| Phase hoàn thành    | Phase 0, Phase 1                                                                      |
| Phase hiện tại      | Phase 2 - Identity, tenant và class core                                              |
| Task vừa hoàn thành | P2-02 Tenant lifecycle và workspace switching                                        |
| Task kế tiếp        | P2-03 Membership invitation, accept và revoke                                        |

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
- P1-09 PostgreSQL/Redis local bằng Compose, migration + seed idempotent,
  `local:setup` và `dev:local` một lệnh cho Windows/Linux.
- P1-10 staging resources: Neon, B2, Cloudflare Pages, Render, ZITADEL và LiveKit.
- Chấp nhận ADR-0011 để thay Hugging Face bằng Render cho Core API staging/private alpha.
- Rà exit gate Phase 1 trên commit `ee597af`: Verify/Security CI, HTTPS staging,
  OIDC, Neon, B2, LiveKit, telemetry và rollback đều đạt.
- Chấp nhận ADR-0012 cho direct-main có kiểm soát trong giai đoạn một người duy trì.
- P2-00: policy engine deny-by-default dùng chung cho identity/classroom/media;
  organization/class role matrix, effective permission, 403/404 concealment,
  OpenAPI enums/error conventions, policy test helpers và static boundary test.
- Chấp nhận ADR-0013 cho mô hình role tổ chức/lớp và authorization policy dùng chung.
- P2-01: profile GET/PATCH, identity list/link/unlink, recent-auth + state/nonce,
  collision protection, last-identity guard, audit/outbox, migration `000006`,
  OpenAPI/generated client và React settings UI có i18n vi/en.
- P2-02: tenant list/detail/create/update/archive, permission `tenant.view`/`tenant.manage`,
  optimistic version, session-context CAS, session/CSRF rotation và migration `000007`;
  success event `tenant.created/updated/archived/switched` được ghi durable qua outbox.
- Workspace UI áp dụng principal mới ngay sau create/switch/archive, hủy và xóa cache
  tenant-scoped để không flash dữ liệu workspace cũ; list/detail/update/archive có query,
  mutation và trạng thái lỗi phù hợp với contract typed.

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

## Kết luận Phase 1

Phase 1 hoàn thành ngày 2026-07-16. Biên bản và ma trận bằng chứng nằm tại
`docs/PHASE_1_COMPLETION.md`. Repository chưa có ruleset công khai; đây là ngoại lệ
được ghi nhận trong ADR-0012, không phải kiểm soát đã bật. Ngoại lệ phải hết hiệu lực
trước pilot/public beta hoặc khi có người duy trì thứ hai.

## Phase 2 đang thực hiện

Backlog có thẩm quyền: `docs/PHASE_2_BACKLOG.md`.

1. P2-00 và P2-01 đã hoàn thành; `pnpm verify` xanh ngày 2026-07-17.
2. P2-02 đã hoàn tất phạm vi implementation và tài liệu. `pnpm verify` xanh ngày
   2026-07-18: web 38/38, API client 10/10, UI 6/6, lint/typecheck/build/Storybook,
   Go test/vet và security checks đều đạt.
3. Integration-tag của migration/classroom/identity compile xanh local; clean migration
   và PostgreSQL integration được workflow CI có PostgreSQL 17 xác nhận sau push.
4. Task kế tiếp là P2-03 membership invitation, accept và revoke sau khi CI xanh.

## Rủi ro đã biết

- Render Free spin down khi không hoạt động và có thể cold start trên 50 giây;
  chỉ chấp nhận cho staging/private alpha.
- Direct-main chưa có pre-merge protection; `pnpm verify` và CI hậu kiểm là kiểm soát
  bù tạm thời theo ADR-0012.
- Chưa chọn managed Redis và observability provider cho quy mô lớn hơn.
- Enrollment, invite code, roster và quyền theo lớp chưa triển khai; thuộc P2-05/P2-06.
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
- `docs/PHASE_1_COMPLETION.md`
- `docs/PHASE_2_BACKLOG.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/DATABASE.md`
- `docs/AUTHENTICATION.md`
- `docs/LIVEKIT_SPIKE_RUNBOOK.md`
- `docs/CI_SECURITY.md`
- `docs/adr/0011-render-core-api-staging.md`
- `docs/adr/0012-single-maintainer-direct-main-governance.md`
- `docs/adr/0013-shared-organization-class-authorization-policy.md`
