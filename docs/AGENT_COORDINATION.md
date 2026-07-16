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

| Task | Trạng thái | Ghi chú |
| --- | --- | --- |
| P1-01 Toolchain | DONE | Monorepo, formatter, lint, test và CI foundation |
| P1-02 Web shell | DONE | React shell, routing, query, i18n, responsive states |
| P1-03 Design system | DONE | Tokens, UI primitives, Storybook, accessibility |
| P1-04 Core API | DONE | Go API, config, middleware, health/readiness |
| P1-05 Contract/database | DONE | OpenAPI, generated client, migrations, Neon role split |
| P1-06 Authentication | DONE | ZITADEL local + staging OIDC, BFF session/CSRF/logout |
| P1-06B Class slice | DONE | List/create/detail, authorization, tenant isolation |
| P1-07 LiveKit | DONE | Media flow 2-5 người và webhook staging đều đạt |
| P1-08A CI/security | DONE | Verify/Security pipeline và secret controls |
| P1-08B Staging deploy | DONE | Cloudflare Pages + Render + smoke/rollback |
| P1-09 Local DX | DONE | Compose PostgreSQL/Redis, one-command setup, seed và troubleshooting |
| P1-10 Cloud foundation | DONE | Neon, B2, Cloudflare, Render, ZITADEL, LiveKit |

P1-08 tổng thể còn một việc quản trị: lưu bằng chứng ruleset/required checks và
các security switches của `main`. Việc này được xử lý khi rà exit gate Phase 1.

## 4. Hạ tầng staging đã chốt

- Web: `https://tutorhub-web.pages.dev`.
- Core API: `https://tutorhub-core-api.onrender.com`.
- Database: Neon PostgreSQL staging branch.
- Storage: Backblaze B2 staging bucket.
- Identity: ZITADEL `tutorhub-staging`.
- Media: LiveKit Cloud staging project.
- Tất cả smoke test acceptance đã đạt ngày 2026-07-16.

## 5. Quy tắc tránh mất mã

- Push sau mỗi checkpoint hoàn chỉnh, không gom quá nhiều thay đổi không liên quan.
- File `.env*.local`, token, key và URL chứa credential phải được Git-ignore.
- Không dùng `git reset --hard`, force-push hoặc xóa lịch sử để xử lý lỗi.
- Nếu test chưa đạt, không ghi task là `DONE`; ghi rõ trạng thái và lỗi trong
  `docs/PROJECT_STATE.md`.
