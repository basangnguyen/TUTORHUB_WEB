# Biên bản hoàn thành Phase 1 - Engineering Foundation

## 1. Kết luận

| Thuộc tính       | Kết quả                                                         |
| ---------------- | --------------------------------------------------------------- |
| Ngày rà soát     | 2026-07-16                                                      |
| Commit làm chuẩn | `ee597afd58e9639c0b465038cb1b9cd132e70b99`                      |
| Kết luận         | **ĐẠT - Phase 1 hoàn thành**                                    |
| Ngoại lệ         | Quản trị `main` theo ADR-0012 trong giai đoạn một người duy trì |
| Phase kế tiếp    | Phase 2 - Identity, tenant và class core                        |

Phase 1 được đóng sau khi đối chiếu mã nguồn, CI, staging acceptance và tài liệu vận
hành. Ngoại lệ GitHub không bị che giấu: repository chưa có ruleset công khai và quy
trình hiện tại cho phép push trực tiếp vào `main`. Rủi ro này được chấp nhận có thời
hạn với kiểm soát bù trong ADR-0012; phải thay thế trước pilot/public beta hoặc khi có
người duy trì thứ hai.

## 2. Phạm vi đã hoàn thành

- Monorepo pnpm/Turborepo, version pin và quality gate dùng được trên Windows/Linux.
- React + TypeScript + Vite web shell, i18n, responsive states và design system.
- Go modular monolith với cấu hình fail-fast, middleware, Problem Details, health,
  readiness, metrics và graceful shutdown.
- OpenAPI source of truth, generated TypeScript client và PostgreSQL migrations.
- ZITADEL OIDC Authorization Code + PKCE, BFF session, CSRF, `/me` và logout.
- Multi-tenant foundation, workspace onboarding và class vertical slice.
- LiveKit prejoin/room, token tối thiểu quyền, media controls, reconnect, telemetry
  và webhook idempotent.
- Cloudflare Pages, same-origin `/api/*`, Render Core API, Neon PostgreSQL,
  Backblaze B2 và LiveKit Cloud staging.
- PostgreSQL/Redis local bằng Compose, migration + seed idempotent và one-command DX.
- Verify/Security CI, dependency automation, secret controls và runbook vận hành.

## 3. Ma trận exit gate

| Exit gate                              | Bằng chứng                                                                                                                                                               | Kết quả         |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------- |
| `pnpm verify` từ môi trường sạch và CI | Workflow [Verify 29508603407](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29508603407) thành công trên commit chuẩn; local smoke PostgreSQL/Redis cũng đạt | ĐẠT             |
| Security pipeline                      | Workflow [Security 29508603437](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29508603437) thành công; Gitleaks, CodeQL và Trivy không có finding chặn       | ĐẠT             |
| HTTPS staging                          | `https://tutorhub-web.pages.dev`, `https://tutorhub-core-api.onrender.com/health` và proxy `/api/health` trả phản hồi hợp lệ ngày 2026-07-16                             | ĐẠT             |
| Readiness phụ thuộc                    | `/ready` trả `database=ready`, `object_storage=ready`                                                                                                                    | ĐẠT             |
| Không có secret trong Git/frontend     | Gitleaks, bundle guard, `.env*.local` Git-ignore và kiểm tra log staging                                                                                                 | ĐẠT             |
| OIDC/BFF và `/me`                      | Login/callback, giữ session sau reload, `/me`, logout và đăng nhập lại đã smoke test thủ công                                                                            | ĐẠT             |
| Migration trên PostgreSQL thật         | Neon migrate -> rollback -> migrate, version `5`, `dirty=false`; runtime/migration role tách biệt                                                                        | ĐẠT             |
| LiveKit token đúng quyền               | Backend cấp token theo lớp và vai trò; teacher/student vào cùng room; 2-5 người test camera, mic, screen share, reconnect                                                | ĐẠT             |
| Telemetry tối thiểu                    | Structured log, request ID, metrics, join telemetry và LiveKit webhook receipt idempotent                                                                                | ĐẠT             |
| Rollback                               | Migration down/up, Render rollback/redeploy và runbook đã được kiểm tra                                                                                                  | ĐẠT             |
| P1-01 đến P1-10                        | Toàn bộ task đạt DoD; phần ruleset được xử lý bằng ADR-0012 thay vì mô tả sai là đã bật                                                                                  | ĐẠT CÓ NGOẠI LỆ |

## 4. Bằng chứng runtime tại thời điểm rà soát

Kết quả kiểm tra ngày 2026-07-16:

```text
GET https://tutorhub-core-api.onrender.com/health
status=ok, service=tutorhub-core-api, environment=staging

GET https://tutorhub-core-api.onrender.com/ready
status=ready, database=ready, object_storage=ready

GET https://tutorhub-web.pages.dev/api/health
status=ok, service=tutorhub-core-api, environment=staging
```

## 5. Trạng thái task

| Task                             | Kết quả                                  |
| -------------------------------- | ---------------------------------------- |
| P1-01 Repository và toolchain    | DONE                                     |
| P1-02 Web shell                  | DONE                                     |
| P1-03 Design system              | DONE                                     |
| P1-04 Go Core API foundation     | DONE                                     |
| P1-05 Contract và PostgreSQL     | DONE                                     |
| P1-06 Authentication spike       | DONE                                     |
| P1-06B Class vertical slice      | DONE                                     |
| P1-07 LiveKit spike              | DONE                                     |
| P1-08A CI/security               | DONE, governance exception theo ADR-0012 |
| P1-08B Staging deploy            | DONE                                     |
| P1-09 Local developer experience | DONE                                     |
| P1-10 Cloud foundation           | DONE                                     |

## 6. Rủi ro chuyển tiếp sang Phase 2

1. Render Free có spin-down/cold start; chỉ phù hợp development/staging/private alpha.
2. Direct-main không có pre-merge protection; phải thay bằng ruleset trước pilot.
3. Chưa có managed Redis và observability provider cho tải lớn hơn.
4. Permission hiện còn biểu diễn ở nhiều module; Phase 2 phải gom về policy layer.
5. Chưa có enrollment, invitation, roster và class-level role hoàn chỉnh.
6. Chưa nhập dữ liệu V1; Phase 2 chỉ xây fixture/import contract idempotent đầu tiên.
7. LiveKit web chunk cần performance budget trước khi mở rộng classroom.

## 7. Quyết định chuyển phase

Cho phép bắt đầu Phase 2 theo backlog `docs/PHASE_2_BACKLOG.md`. Task đầu tiên là
P2-00 Policy and contract baseline; không bắt đầu từ UI vì mọi vertical slice sau đó
phụ thuộc permission matrix và class role model thống nhất.
