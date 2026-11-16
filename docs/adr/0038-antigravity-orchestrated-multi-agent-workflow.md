# ADR-0038: Mô hình điều phối đa coding agent dưới một PM/Gatekeeper (Antigravity)

- Trạng thái: Accepted
- Ngày: 2026-11-16
- Phạm vi: development, staging và private alpha
- Liên quan: [ADR-0012](0012-single-maintainer-direct-main-governance.md)

## Bối cảnh

Từ ngày 2026-07-16 ([ADR-0012](0012-single-maintainer-direct-main-governance.md)), TutorHub V2
vận hành theo mô hình một chủ dự án duy trì và commit trực tiếp vào `main` kèm cổng kiểm chứng
cục bộ (`pnpm verify`) và CI hậu kiểm (`Verify`, `Security`).

Khi quy mô mã nguồn đạt qua Phase 0–2, Phase 4 và Phase 5 Collaboration, chủ dự án quyết định
sử dụng đồng thời ba công cụ AI chuyên biệt trên cùng máy phát triển:

1. **Antigravity**: quản lý dự án (PM), kiến trúc sư và chốt chặn chất lượng (Gatekeeper).
2. **Codex**: worker thực thi và rà soát chuyên sâu backend Go, SQL/ACL, script hạ tầng và kiểm thử.
3. **Claude Code (Claude Pro)**: worker thực thi chuyên sâu frontend React/TypeScript, UI/UX,
   accessibility, OpenAPI integration và refactor đa file.

Nếu nhiều agent cùng tự ý sửa ngoài phạm vi, tự cập nhật tài liệu trạng thái hoặc tự `git commit` /
`git push` trực tiếp vào `main`, dự án sẽ gặp xung đột working tree, trôi ranh giới kiến trúc và
mất tính nguyên tử của từng checkpoint.

## Quyết định

Giữ hiệu lực cốt lõi của [ADR-0012](0012-single-maintainer-direct-main-governance.md) (một chủ dự án,
commit trực tiếp vào `main` trong giai đoạn development/staging/private alpha), đồng thời bổ sung
cơ chế phân tầng điều phối bắt buộc sau:

1. **Một PM / Gatekeeper duy nhất (`Antigravity`)**:
   - Nắm ngữ cảnh tổng thể (`docs/PROJECT_STATE.md`, `docs/MASTER_PLAN.md`, backlog và các ADR).
   - Phân rã yêu cầu thành các gói việc nhỏ, rõ ranh giới và xuất **Prompt giao việc chuẩn** để chủ
     dự án sao chép – dán vào **Codex** hoặc **Claude Code**.
   - Trực tiếp kiểm tra `git status` / `git diff`, rà soát bảo mật/tenant isolation, chạy cổng chất
     lượng phù hợp (và `pnpm verify` trước khi push), cập nhật `docs/PROJECT_STATE.md` cùng backlog
     liên quan.
   - Là **agent duy nhất** được phép thực hiện `git commit` và `git push origin main` (trừ khi chủ
     dự án tự chạy tay hoặc chỉ định khác một cách tường minh).

2. **Worker thực thi theo phạm vi đóng (`Codex` và `Claude Code`)**:
   - Chỉ đọc ngữ cảnh cần thiết và **chỉ sửa các file/thư mục nằm trong `Allowed Scope`** của prompt.
   - **Tuyệt đối không** tự chạy `git commit`, `git push`, `git reset --hard`, `git checkout --`,
     xóa nhánh, hoặc can thiệp lịch sử Git nếu prompt không yêu cầu rõ.
   - **Tuyệt đối không** đọc/in giá trị secret từ `.env*.local`, không chạm shared staging,
     production hoặc chạy migration ngoài môi trường local/disposable đã được phép trong prompt.
   - Không tự cập nhật `docs/PROJECT_STATE.md` để tránh xung đột trạng thái, trừ khi prompt giao
     đích danh nhiệm vụ tài liệu đó.
   - Kết thúc phiên bằng báo cáo bàn giao ngắn gọn gồm: danh sách file đã sửa, lệnh kiểm thử đã
     chạy kèm kết quả, và rủi ro/điểm cần Gatekeeper lưu ý.

3. **Quy tắc chống xung đột khi giao việc**:
   - Mặc định ưu tiên giao tuần tự theo lát cắt nhỏ trên `D:\TutorHub_V2` để kiểm chứng nhanh.
   - Khi cần giao **song song** cho cả Codex và Claude Code, Antigravity bắt buộc phải tách
     **phạm vi thư mục rời nhau hoàn toàn** (ví dụ `services/core-api/**` và `apps/web/**`) hoặc
     tạo `git worktree` tạm riêng cho từng worker trước khi xuất prompt.

4. **Tài liệu hướng dẫn bắt buộc tại gốc repository**:
   - `AGENTS.md`: quy tắc chung cho Antigravity và Codex.
   - `CLAUDE.md`: quy tắc chuyên biệt cho Claude Code.
   - `docs/AGENT_COORDINATION.md`: quy trình vận hành và mẫu Prompt giao việc chuẩn.

## Hệ quả

- Tận dụng đúng thế mạnh của từng mô hình (Antigravity điều phối & kiểm chứng tổng thể; Codex mạnh
  về Go/SQL/hệ thống; Claude Code mạnh về React/TypeScript/UI/a11y) mà không phát sinh thủ tục PR
  rườm rà cho một chủ dự án.
- Giữ nguyên một đầu mối chịu trách nhiệm về chất lượng, bảo mật và lịch sử `main`.
- Các điều kiện hết hiệu lực của [ADR-0012](0012-single-maintainer-direct-main-governance.md) (có
  người duy trì con người thứ hai, mở pilot người dùng thật, xử lý dữ liệu cá nhân production,
  public beta hoặc yêu cầu kiểm toán độc lập) vẫn giữ nguyên hiệu lực.
