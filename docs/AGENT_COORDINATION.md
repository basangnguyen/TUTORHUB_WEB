# Quy trình phát triển TutorHub V2

## 1. Mô hình làm việc hiện tại

Từ ngày 2026-07-16, dự án được duy trì bởi một coding agent. GitHub là nơi lưu
mã nguồn và lịch sử thay đổi; không còn quy trình phân chia ownership giữa nhiều
agent.

- Repository: `https://github.com/basangnguyen/TUTORHUB_WEB`.
- Thư mục chuẩn: `D:\TutorHub_V2`.
- Nhánh phát triển mặc định: `main`.
- Commit và push trực tiếp vào `main` sau khi kiểm tra đạt.
- Không bắt buộc tạo Issue, feature branch hoặc Pull Request cho từng task.
- Không force-push `main` và không commit secret.

Branch tạm vẫn được phép khi thử migration, dependency upgrade hoặc thay đổi có
nguy cơ cao. Sau khi xác minh, thay đổi phải được đưa về `main` và branch tạm có
thể xóa.

## 2. Trình tự cho mỗi task

1. Đọc `AGENTS.md`, `README.md`, `docs/PROJECT_STATE.md`, backlog và ADR liên quan.
2. Chạy `git status` và đọc diff hiện có trước khi sửa.
3. Xác định phạm vi file, contract, migration và rủi ro.
4. Thực hiện thay đổi nhỏ, bám kiến trúc đã chấp nhận.
5. Chạy formatter, lint, typecheck, test/build hoặc smoke test phù hợp với rủi ro.
6. Kiểm tra diff và secret trước khi commit.
7. Cập nhật `docs/PROJECT_STATE.md` cùng checklist phase liên quan.
8. Commit và push trực tiếp `main`.

## 3. Trạng thái Phase 1

| Task                    | Trạng thái | Ghi chú                                                              |
| ----------------------- | ---------- | -------------------------------------------------------------------- |
| P1-01 Toolchain         | DONE       | Monorepo, formatter, lint, test và CI foundation                     |
| P1-02 Web shell         | DONE       | React shell, routing, query, i18n, responsive states                 |
| P1-03 Design system     | DONE       | Tokens, UI primitives, Storybook, accessibility                      |
| P1-04 Core API          | DONE       | Go API, config, middleware, health/readiness                         |
| P1-05 Contract/database | DONE       | OpenAPI, generated client, migrations, Neon role split               |
| P1-06 Authentication    | DONE       | ZITADEL local + staging OIDC, BFF session/CSRF/logout                |
| P1-06B Class slice      | DONE       | List/create/detail, authorization, tenant isolation                  |
| P1-07 LiveKit           | DONE       | Media flow 2-5 người và webhook staging đều đạt                      |
| P1-08A CI/security      | DONE       | Verify/Security pipeline và secret controls                          |
| P1-08B Staging deploy   | DONE       | Cloudflare Pages + Render + smoke/rollback                           |
| P1-09 Local DX          | DONE       | Compose PostgreSQL/Redis, one-command setup, seed và troubleshooting |
| P1-10 Cloud foundation  | DONE       | Neon, B2, Cloudflare, Render, ZITADEL, LiveKit                       |

Phase 1 đã hoàn thành ngày 2026-07-16. Ma trận bằng chứng nằm trong
`docs/PHASE_1_COMPLETION.md`. Repository chưa có ruleset công khai; direct-main là
ngoại lệ có thời hạn theo ADR-0012 và không được mô tả như branch protection đã bật.

## 4. Trạng thái Phase 2

| Task                           | Trạng thái | Ghi chú                                                    |
| ------------------------------ | ---------- | ---------------------------------------------------------- |
| P2-00 Policy/contract baseline | DONE       | Policy deny-by-default và role matrix dùng chung           |
| P2-01 Profile/identity         | DONE       | Profile, identity linking và migration `000006`            |
| P2-02 Tenant lifecycle         | DONE       | Lifecycle/switch, migration `000007`; `pnpm verify` xanh   |
| P2-03 Membership invitation    | DONE       | Invitation/accept/revoke, migration `000008`; verify xanh  |
| P2-04 Class lifecycle          | DONE       | Lifecycle/ownership, migration `000009`; verify xanh       |
| P2-05 Enrollment/invite code   | DONE       | Enrollment/invite, migration `000010`; verify xanh         |
| P2-06 Roster/class roles       | DONE       | Roster/hierarchy/single-bulk UI; verify xanh               |
| P2-07 Audit log                | DONE       | Append-only audit, query/UI org admin, migration `000011`  |
| P2-08 Admin/teacher E2E UI     | DONE       | CI và acceptance staging ba role đều xanh                  |
| P2-09 Feature flag/quota       | DONE       | Staging migration/config/acceptance đều đạt                 |
| P2-10 Tenant isolation/IDOR    | DONE       | Commit `c4205b9`; Verify/Security CI đều xanh               |
| P2-11 V1 fixture import        | DONE       | Commit `f07d05d`; PostgreSQL 17 Verify/Security đều xanh    |
| P2-12 Staging closure          | DONE       | UI, rollback/redeploy, sign-off và exit gate đều đạt         |

Nguồn thực thi: `docs/PHASE_2_BACKLOG.md`.

Phase 2 đã hoàn thành ngày 2026-07-22. CI/Cloudflare/Neon/importer/Render, UI staging
ba role, tenant/IDOR probes và S09 application rollback/redeploy đều đạt; owner đã
sign-off. Native Render Rollback không được dùng do cảnh báo không tải được cấu hình
live; rollback bằng specific commit giữ cấu hình hiện tại là bằng chứng phục hồi đã
được chấp nhận. Hồ sơ nằm tại `docs/P2_12_STAGING_ACCEPTANCE.md` và
`docs/PHASE_2_COMPLETION.md`.

## 5. Trạng thái Phase 3

| Task                                 | Trạng thái | Ghi chú                                 |
| ------------------------------------ | ---------- | --------------------------------------- |
| P3-00 Backlog/architecture baseline  | DONE       | Backlog, ADR scheduling và ADR worker   |
| P3-CAL-00 Calendar research/design   | DONE       | Product/UX/technical/V1/OSS report      |
| P3-CAL-01 Spike + ADR-0019           | READY      | Renderer, recurrence, conflict gate     |
| P3-01 Session scheduling và timezone | READY      | Vertical slice implementation đầu tiên  |
| P3-02 đến P3-14                      | TODO       | Theo dependency trong backlog           |

Nguồn thực thi: `docs/PHASE_3_BACKLOG.md`. Trước khi code calendar phải đọc
`docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md` và ADR-0017; P3-02 recurrence phải chờ
P3-CAL-01/ADR-0019. Mọi notification/message/file side effect phải chờ worker
foundation P3-03 theo ADR-0018. P3-01 không gồm recurrence, calendar tổng hợp,
reminder hoặc media lifecycle.

## 6. Hạ tầng staging đã chốt

- Web: `https://tutorhub-web.pages.dev`.
- Core API: `https://tutorhub-core-api.onrender.com`.
- Database: Neon PostgreSQL staging branch.
- Storage: Backblaze B2 staging bucket.
- Identity: ZITADEL `tutorhub-staging`.
- Media: LiveKit Cloud staging project.
- Tất cả smoke test acceptance đã đạt ngày 2026-07-16.

## 7. Quy tắc tránh mất mã

- Push sau mỗi checkpoint hoàn chỉnh, không gom quá nhiều thay đổi không liên quan.
- File `.env*.local`, token, key và URL chứa credential phải được Git-ignore.
- Không dùng `git reset --hard`, force-push hoặc xóa lịch sử để xử lý lỗi.
- Nếu test chưa đạt, không ghi task là `DONE`; ghi rõ trạng thái và lỗi trong
  `docs/PROJECT_STATE.md`.
