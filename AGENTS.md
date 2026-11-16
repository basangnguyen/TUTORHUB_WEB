# TutorHub V2 Agent Instructions

## Bối cảnh

TutorHub V2 là hệ sinh thái học trực tuyến web-first. TutorHub V1 tại `D:\Ban_sao_du_an` là nguồn tham chiếu nghiệp vụ, không phải nơi triển khai tính năng V2.

## Trước khi sửa mã

1. Đọc `README.md`, `docs/AGENT_COORDINATION.md`, `docs/PROJECT_STATE.md`, `docs/MASTER_PLAN.md`, backlog phase, tài liệu liên quan và ADR tương ứng (với Claude Code, đọc thêm `CLAUDE.md`).
2. Kiểm tra trạng thái Git và các thay đổi cục bộ trước khi sửa; không ghi đè thay đổi chưa được hiểu rõ.
3. Với thay đổi kiến trúc, tạo hoặc cập nhật ADR trước khi triển khai.
4. Không sao chép secret, token, URL có credential hoặc file cấu hình production từ V1; không đọc hoặc in giá trị secret từ `.env*.local`.
5. Ưu tiên làm việc nhanh chóng, không overthinking: chọn phương án đơn giản, đủ an toàn và
   có thể kiểm chứng; tránh phân tích hoặc trao đổi lặp lại khi không làm thay đổi quyết định.

## Repository và phối hợp (theo ADR-0012 & ADR-0038)

- Repository chính thức: `https://github.com/basangnguyen/TUTORHUB_WEB`.
- Remote chuẩn: `origin`; nhánh mặc định: `main`.
- Mô hình điều phối 3-agent dưới sự chỉ đạo của chủ dự án:
  - **Antigravity (PM / Architect / Gatekeeper)**: quản lý lộ trình, kiến trúc, ADR, phân rã task và xuất **Prompt giao việc chuẩn** để chủ dự án dán vào **Codex** hoặc **Claude Code**; trực tiếp review diff/bảo mật, chạy cổng kiểm chứng (`pnpm verify` và các test liên quan), cập nhật `docs/PROJECT_STATE.md` và là **agent duy nhất** thực hiện `git commit` / `git push origin main`.
  - **Codex & Claude Code (Implementation / Review Workers)**: thực thi nghiêm ngặt trong phạm vi file (`Allowed Scope`) của prompt được dán vào; chạy kiểm thử cục bộ theo yêu cầu của prompt và báo cáo kết quả bàn giao; **tuyệt đối không** tự `git commit`, `git push`, `git reset --hard`, sửa `docs/PROJECT_STATE.md` hoặc chạm hạ tầng staging/production khi prompt không chỉ định rõ.
- GitHub chủ yếu là nơi lưu mã, sao lưu lịch sử và chạy hậu kiểm CI (`Verify`, `Security`). Không bắt buộc tạo Issue, branch hoặc Pull Request cho từng task.
- Trước mỗi lần push, Gatekeeper phải chạy kiểm tra phù hợp với phạm vi thay đổi (`pnpm verify`), xem lại diff và xác nhận không có secret.
- Không force-push `main`; khi cần thử nghiệm rủi ro cao hoặc giao việc song song dễ đụng file, dùng branch hoặc `git worktree` tạm.

## Kiến trúc đã chấp nhận

- Web: React + TypeScript strict + Vite.
- Backend khởi đầu: Go modular monolith.
- Contract: OpenAPI và generated TypeScript client.
- Realtime media: LiveKit Cloud trong MVP.
- Dữ liệu: Neon PostgreSQL; cache/session/rate limit: managed Redis (provider chưa chọn).
- Server staging/private alpha: Go Core API stateless chạy OCI container trên Render; Hugging Face chỉ còn là lựa chọn cho dịch vụ AI độc lập.
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

Sau mỗi task phải cập nhật `docs/PROJECT_STATE.md` và checklist phase liên quan. Sau mỗi phase phải cập nhật trạng thái/exit gate trong master plan hoặc tài liệu phase tương ứng. Không để lịch sử hội thoại trở thành nguồn thông tin duy nhất.
