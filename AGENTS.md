# TutorHub V2 Agent Instructions

## Bối cảnh

TutorHub V2 là hệ sinh thái học trực tuyến web-first. TutorHub V1 tại `D:\Ban_sao_du_an` là nguồn tham chiếu nghiệp vụ, không phải nơi triển khai tính năng V2.

## Trước khi sửa mã

1. Đọc `README.md`, `docs/AGENT_COORDINATION.md`, `docs/PROJECT_STATE.md`, `docs/MASTER_PLAN.md`, backlog phase, tài liệu liên quan và ADR tương ứng.
2. Kiểm tra trạng thái Git, bảng ownership và branch hiện tại; không nhận phạm vi đang có agent khác giữ hoặc ghi đè thay đổi không thuộc task.
3. Sau initial push, nhận một GitHub Issue và ghi branch/phạm vi file trước khi sửa mã; trước initial push dùng bảng fallback trong `docs/AGENT_COORDINATION.md`.
4. Với thay đổi kiến trúc, tạo hoặc cập nhật ADR trước khi triển khai.
5. Không sao chép secret, token, URL có credential hoặc file cấu hình production từ V1.

## Repository và phối hợp

- Repository chính thức: `https://github.com/basangnguyen/TUTORHUB_WEB`.
- Remote chuẩn: `origin`; nhánh mặc định: `main`.
- Sau initial bootstrap, mỗi task phải dùng branch riêng theo mẫu `<agent>/<task-id>-<mo-ta-ngan>`.
- Không force-push `main`, không dùng chung branch cho hai agent đang hoạt động và không giải quyết conflict bằng cách ghi đè toàn bộ một phía.
- Quy trình nhận việc, bàn giao, checklist và vùng dễ xung đột nằm trong `docs/AGENT_COORDINATION.md` và là bắt buộc.

## Kiến trúc đã chấp nhận

- Web: React + TypeScript strict + Vite.
- Backend khởi đầu: Go modular monolith.
- Contract: OpenAPI và generated TypeScript client.
- Realtime media: LiveKit Cloud trong MVP.
- Dữ liệu: Neon PostgreSQL; cache/session/rate limit: managed Redis (provider chưa chọn).
- Server MVP/private beta: các Hugging Face Docker Spaces tách biệt, stateless.
- File: Backblaze B2 qua S3-compatible presigned URL do backend cấp.
- Auth: OIDC Authorization Code + PKCE qua BFF/session cookie.

## Quy tắc triển khai

- Không thêm microservice hoặc Kubernetes nếu chưa có ADR và bằng chứng tải.
- Mọi truy vấn nghiệp vụ phải được giới hạn bởi `tenant_id` ở server.
- Không lưu access token hoặc refresh token trong `localStorage`.
- Không cho frontend tự cấp LiveKit token, presigned URL hoặc quyết định quyền.
- Mỗi tính năng phải có trạng thái loading, empty, error, forbidden và retry phù hợp.
- Không đưa Secure Exam vào web như một tính năng khóa máy; phần đó thuộc native companion.

## Chất lượng

- TypeScript không dùng `any` nếu chưa có lý do ghi rõ.
- API có validation, request ID, lỗi có cấu trúc và kiểm tra quyền.
- Test theo rủi ro: unit, integration, Playwright E2E, accessibility và authorization tests.
- Không log secret, token, nội dung riêng tư hoặc dữ liệu học sinh không cần thiết.

## Cập nhật trạng thái

Sau mỗi task phải cập nhật `docs/PROJECT_STATE.md`, checklist phase và trạng thái ownership trong `docs/AGENT_COORDINATION.md`. Sau mỗi phase phải cập nhật trạng thái/exit gate trong master plan hoặc tài liệu phase tương ứng. Không để lịch sử hội thoại trở thành nguồn thông tin duy nhất.
