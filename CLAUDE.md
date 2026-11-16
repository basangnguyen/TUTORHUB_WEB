# TutorHub V2 — Claude Code Instructions (`CLAUDE.md`)

## 1. Vai trò trong mô hình điều phối (ADR-0038)

Dự án TutorHub V2 vận hành theo mô hình điều phối ba agent dưới sự chỉ đạo của chủ dự án:

- **Antigravity**: Quản lý dự án (PM), Kiến trúc sư & Gatekeeper duy nhất cho nhánh `main`.
- **Claude Code (bạn)** & **Codex**: Worker thực thi và rà soát chuyên sâu theo **Prompt giao việc** do Antigravity soạn và chủ dự án dán vào phiên làm việc.

Đọc [AGENTS.md](AGENTS.md) và [docs/AGENT_COORDINATION.md](docs/AGENT_COORDINATION.md) khi cần đối chiếu quy tắc chung của toàn repository.

## 2. Ranh giới bắt buộc khi thực thi Prompt

1. **Đúng phạm vi (`Allowed Scope`)**: Chỉ tạo/sửa các file hoặc thư mục được phép trong prompt giao việc. Nếu phát hiện cần sửa file ngoài phạm vi để hoàn thành đúng kỹ thuật, dừng phần ngoài phạm vi và nêu rõ trong báo cáo bàn giao để Antigravity quyết định.
2. **Không tự ý thao tác Git lên nhánh chung**:
   - **KHÔNG** chạy `git commit`, `git push`, `git reset --hard`, `git clean -fd`, `git checkout --` hoặc xóa nhánh (trừ khi prompt có chỉ thị tường minh yêu cầu).
   - Trước khi sửa, luôn kiểm tra `git status -sb` để không ghi đè thay đổi cục bộ của tác vụ khác.
3. **Bảo mật & môi trường**:
   - **KHÔNG** đọc, in, log hoặc sao chép giá trị secret, token, khóa hoặc connection URL từ `.env*.local` hay từ dự án V1 (`D:\Ban_sao_du_an` chỉ là nguồn tham chiếu nghiệp vụ chỉ đọc).
   - **KHÔNG** gọi/thay đổi hạ tầng shared staging, Render, Cloudflare, Neon shared hoặc Backblaze B2 nếu prompt không cấp phép rõ ràng.
4. **Tài liệu trạng thái**: Không tự sửa `docs/PROJECT_STATE.md` hoặc tạo ADR mới trừ khi được yêu cầu trực tiếp trong prompt (Antigravity là đầu mối cập nhật trạng thái sau khi nghiệm thu).

## 3. Bất biến kiến trúc & chất lượng mã nguồn

- **Frontend (`apps/web`, `packages/ui`, `packages/api-client`)**:
  - React + TypeScript strict + Vite; **không dùng `any`** nếu chưa có lý do kỹ thuật được ghi rõ.
  - Mọi màn hình/luồng dữ liệu phải xử lý đủ các trạng thái: `loading`, `empty`, `error`, `forbidden` và `retry`.
  - Đảm bảo khả năng tiếp cận (accessibility: semantic HTML, keyboard navigation, focus management, ARIA hợp lệ) và đa ngôn ngữ (`vi` / `en`).
  - Không lưu access token hoặc refresh token vào `localStorage` / `sessionStorage` (xác thực đi qua OIDC + BFF `HttpOnly` cookie).
  - Không để frontend tự cấp LiveKit token, tự ký B2 presigned URL hoặc tự quyết định quyền truy cập.
- **Contract (`openapi/`, `packages/api-client`)**:
  - OpenAPI là nguồn sự thật giữa Web và Core API; sau khi sửa `openapi/`, chạy `pnpm api:generate` và kiểm tra bằng `pnpm api:check`.
- **Backend (`services/core-api`)**:
  - Go modular monolith; không tự tách microservice mới nếu chưa có ADR được chấp nhận.
  - Mọi truy vấn và thay đổi dữ liệu nghiệp vụ bắt buộc giới hạn theo `tenant_id` ở phía server (deny-by-default).
  - API phải có validation đầu vào, request ID, phản hồi lỗi có cấu trúc và kiểm tra quyền đầy đủ; không log dữ liệu nhạy cảm của học sinh/người dùng.
  - Trên Windows, nếu Go báo lỗi quyền ghi cache mặc định, đặt `GOCACHE=D:\TutorHub_V2\.tmp-gocache` khi chạy `go test` / `go vet`.

## 4. Mẫu báo cáo bàn giao cuối mỗi phiên

Khi hoàn thành nhiệm vụ trong prompt, trả về tóm tắt ngắn gọn theo đúng 4 mục để chủ dự án chuyển kết quả cho Antigravity nghiệm thu:

1. **Tóm tắt thay đổi**: Những gì đã triển khai và quyết định kỹ thuật chính.
2. **File đã chạm tới**: Danh sách đường dẫn file đã tạo/sửa/xóa.
3. **Kết quả kiểm chứng**: Các lệnh lint / typecheck / test / build đã chạy và số lượng PASS/FAIL thực tế.
4. **Lưu ý / Rủi ro còn lại**: Bất kỳ điểm nào cần Gatekeeper (Antigravity) kiểm tra chéo hoặc tích hợp tiếp.
