# P3-01 Staging Acceptance

| Thuộc tính | Giá trị |
| --- | --- |
| Ngày nghiệm thu | 2026-07-24 |
| Phạm vi | One-time ClassSession scheduling và timezone |
| Trạng thái | **DONE** |
| Feature commit | `b58666c` |
| Security/deploy patch | `a5741a1` |
| Web acceptance fix | `e7dc161` |
| Môi trường | Cloudflare Pages, Render Core API, Neon PostgreSQL, ZITADEL staging |

## Hạ tầng và migration

- Neon staging đã migrate `13 -> 14`; migration state là `14 false`.
- Runtime role có `SELECT`, `INSERT`, `UPDATE`; không có `DELETE`, `TRUNCATE` trên các
  bảng P3-01 theo ma trận quyền tối thiểu.
- Render direct `/health`, `/ready` và Cloudflare same-origin `/api/health`, `/api/ready`,
  `/api/v1/status` đều trả HTTP 200.
- Web regression fix `e7dc161` đã được Cloudflare phục vụ trước lượt browser acceptance.

## Automated gate

- Web typecheck, lint, production build đạt.
- Web test đạt `25` files / `144` tests; có regression test cho dialog edit hydrate
  title, description, start, end và timezone khi parent mở controlled dialog.
- API-client và các Go package P3-01 liên quan đã đạt trong lượt implementation trước.
- `git diff --check` và pre-commit monorepo lint đạt; không có secret trong diff.

## Browser staging acceptance

1. **Teacher create/update/cancel:** session được tạo; dialog edit nạp đúng dữ liệu cũ;
   update title/description thành công; cancel giữ lịch sử và bỏ action edit/cancel.
2. **Student read-only:** Student đã enroll xem được session nhưng không thấy action
   schedule, edit hoặc cancel.
3. **Foreign-ID concealment:** khi Student đang ở workspace khác, truy cập exact foreign
   class ID hiển thị `Không tìm thấy lớp học`; không lộ tên class hoặc session.

Lượt này dùng browser staging có phiên ZITADEL thật; không đọc/lưu password, cookie,
token hoặc browser storage. Đây là browser acceptance, không được mô tả là một lượt
Playwright staging tự động.

## Giới hạn phạm vi

P3-01 chỉ chốt one-time ClassSession. Recurrence, calendar tổng hợp, reminder,
email/ICS/RSVP, durable worker và media lifecycle tiếp tục thuộc các task Phase 3/4 sau.
