# TutorHub V2 - Trạng thái dự án

> Điểm vào nhanh để tiếp tục phát triển. Cập nhật sau mỗi task hoặc thay đổi hạ tầng.

## Snapshot

| Thuộc tính          | Trạng thái                                                                            |
| ------------------- | ------------------------------------------------------------------------------------- |
| Ngày cập nhật       | 2026-07-19                                                                            |
| Repository          | `https://github.com/basangnguyen/TUTORHUB_WEB`                                        |
| Nhánh làm việc      | `main`                                                                                |
| Quy trình           | Một coding agent, commit trực tiếp vào `main`; GitHub dùng để lưu và sao lưu mã nguồn |
| Phase hoàn thành    | Phase 0, Phase 1                                                                      |
| Phase hiện tại      | Phase 2 - Identity, tenant và class core                                              |
| Task vừa hoàn thành | P2-07 Audit log cho hành động nhạy cảm                                                |
| Task kế tiếp        | P2-08 Admin và teacher UI end-to-end                                                  |

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
- P2-03: permission `tenant.manage_members`, migration `000008`, invitation CSPRNG chỉ
  lưu purpose-bound HMAC, TTL/state machine, list/create/revoke và preview/accept bằng
  verified linked identity trong transaction idempotent; accept không tự đổi active tenant.
- Invitation URL giữ token trong fragment, web xóa fragment ngay và chỉ gửi token trong
  JSON POST body; admin UI có list/create/copy-once/revoke, public UI có đủ loading,
  offline, unavailable, mismatch, retry và success states bằng tiếng Việt/Anh.
- P2-04: migration `000009` bổ sung class `timezone`, optimistic `version` và
  `archived_at`; list có status filter cùng opaque keyset cursor, update/archive/restore/
  transfer ownership dùng `expected_version` CAS và transactional outbox.
- Class đi từ draft sang active; archive draft/active và restore đúng trạng thái trước.
  `owner_user_id` là owner implicit cho đến P2-05/P2-06, không tạo enrollment sớm.
  Chỉ `org_admin` hoặc owner có `class.archive`/`class.transfer_ownership`; target
  transfer phải là active member cùng tenant đủ điều kiện tạo lớp và actor phải có
  recent authentication trong 10 phút.
- Classroom UI đã có create/edit/activate/archive/restore, conflict recovery, status
  filter và pagination. LiveKit token/telemetry chỉ chấp nhận class active.
- P2-05: migration `000010` thêm enrollment lifecycle
  `invited -> active -> suspended/left/removed`, class invite code có TTL/usage limit
  và index/constraint tenant-scoped; owner tiếp tục là implicit ở `classes.owner_user_id`.
- Invite code dùng CSPRNG 256-bit và purpose-bound HMAC; raw token chỉ trả một lần
  trong create response, nằm trong URL fragment rồi được web xóa ngay. Join truyền
  token trong JSON body, khóa/cập nhật usage atomically và không tiêu thêm lượt khi
  active member, owner hoặc organization manager gọi lại.
- Direct enrollment, suspend, remove, revoke, join/rejoin và self-leave đều đi qua
  shared policy cùng repository tenant-scoped; outbox payload dùng allowlist và không
  chứa token, hash hoặc email.
- Class detail/list và LiveKit token/event dùng `viewer_access` do server resolve từ
  owner, organization manager hoặc enrollment active. Web có management panel,
  copy-once invite, public join và self-leave với đầy đủ loading/empty/error/
  forbidden/retry states bằng tiếng Việt/Anh.
- P2-06: manager-only roster API có owner implicit ghim riêng, normalized search theo
  display name/email, status filter và opaque keyset pagination bind đúng tenant/class/
  filter. Shared policy áp dụng hierarchy `org_admin > owner > teacher/co_teacher >
teaching_assistant > student/guest`, chặn self/peer/owner mutation và chỉ ownership
  transfer mới đổi owner.
- Single role update và bulk `update_role/suspend/remove` tối đa 50 user ID có ordered
  updated/unchanged/failed outcomes. Bulk commit từng item độc lập; web refetch cả khi
  mutation thành công hoặc lỗi hạ tầng. Role-changed outbox payload dùng allowlist và
  không chứa email/display name/token.
- Roster UI có search/status, infinite pagination, row/bulk confirmation, selection
  bằng keyboard, partial-failure summary và loading/empty/error/forbidden/archived
  states. Class management dùng class-scoped lifecycle capabilities từ server.
- LiveKit grant mới lấy class role authoritative và ghi effective/organization/class
  role attributes; token cấp sau mutation phản ánh role mới. JWT/participant đã tồn tại
  không được sửa hoặc thu hồi retroactively.
- P2-07 chấp nhận ADR-0014 và migration `000011`: `audit_events` tenant-owned,
  append-only bằng trigger `ALWAYS`, actor user/system, action/resource/outcome,
  request correlation, privacy-reduced source hints và flat metadata allowlist.
- Success audit của mutation P2-02 đến P2-06 được ghi cùng business transaction và
  outbox; authenticated no-op/denied/failed attempt dùng fallback transaction có
  server-generated request-instance ID để không ghi sai sau post-commit failure. Panic
  ở sensitive handler vẫn được audit rồi chuyển tiếp tới recovery middleware. Accept
  invitation bind tenant đích do server resolve; bulk roster dedupe từng target và ghi
  cả item lỗi hạ tầng/chưa được thực hiện.
- Permission `audit.view` chỉ cấp cho active `org_admin`. API
  `GET /api/v1/tenants/{tenant_id}/audit-events` reload membership authoritative,
  khóa path theo active tenant, hỗ trợ time/action/resource/outcome filter cùng opaque
  filter-bound keyset cursor và trả `no-store` projection không có IP/UA hash.
- Web có route `/app/workspace/audit`, permission gate, tenant-scoped infinite query,
  cache isolation khi switch/archive, filter validation, pagination và đầy đủ loading/
  empty/filtered-empty/error/forbidden/stale-refresh states bằng tiếng Việt/Anh.

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

1. P2-00 đến P2-07 đã hoàn thành.
2. Full `pnpm verify` của P2-07 xanh ngày 2026-07-19: web 79/79, API client 15/15,
   UI 6/6, generated-contract check, lint/typecheck/build/Storybook, Go test/vet và
   security checks.
3. Full integration-tag compile và focused audit/request metadata/policy/HTTP/classroom/
   identity tests đều xanh local. Runtime PostgreSQL migration `000011` và audit
   integration chưa chạy vì không nạp DB test env; workflow CI PostgreSQL 17 sẽ xác
   nhận sau push.
4. Task kế tiếp là P2-08 Admin và teacher UI end-to-end.

## Rủi ro đã biết

- Render Free spin down khi không hoạt động và có thể cold start trên 50 giây;
  chỉ chấp nhận cho staging/private alpha.
- Direct-main chưa có pre-merge protection; `pnpm verify` và CI hậu kiểm là kiểm soát
  bù tạm thời theo ADR-0012.
- Chưa chọn managed Redis và observability provider cho quy mô lớn hơn.
- Membership invitation preview/accept và class invite join hiện dùng bounded
  in-process limiter theo `RemoteAddr`; sau Cloudflare/Render có thể gộp client vào
  proxy bucket. Không tin forwarded header khi Render origin còn public; P2-09 phải
  chốt trusted-proxy/origin authentication và distributed limiter trước khi tăng tải.
- Runtime PostgreSQL cho migration/test `000011`, audit append-only và các repository
  đã gắn atomic audit chưa được chạy local; integration-tag compile đã xanh nhưng clean
  migration/concurrency runtime cần CI/staging xác nhận.
- Production retention/export, privacy erasure, partitioning và dedicated maintenance
  role cho audit được hoãn tới Phase 8. Audit của tenant archived được giữ nhưng chưa có
  recovery/export UI ngoài active-tenant API.
- Roster role update hiện dùng last-write-wins, chưa có enrollment version/CAS. Client
  refetch sau mutation; nếu concurrent editing trở thành rủi ro thực tế thì cần ADR và
  migration riêng.
- Ownership transfer tái dùng `auth_time` của session theo semantics P2-01; chưa ép
  OIDC `max_age`/`prompt`, nên recent-auth 10 phút chưa phải step-up tuyệt đối.
- Archive hoặc roster role mutation ngăn/đổi credential LiveKit cấp mới nhưng không
  thu hồi JWT đã phát hoặc kick/cập nhật participant đang ở trong room.
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
- `docs/adr/0014-append-only-tenant-audit-log.md`
