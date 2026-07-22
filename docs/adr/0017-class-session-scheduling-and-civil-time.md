# ADR-0017: Lịch buổi học lưu instant UTC và IANA timezone

- Trạng thái: Accepted
- Ngày: 2026-07-22
- Phạm vi: P3-01, P3-02 và các lịch/reminder về sau

## Bối cảnh

Phase 3 mở đầu bằng việc teacher lên lịch buổi học. Một timestamp UTC đơn lẻ đủ để
chạy đúng một occurrence, nhưng không đủ để giữ ý định civil time khi hiển thị, sửa
lịch hoặc tạo recurrence qua daylight-saving time. Ngược lại, chỉ lưu local datetime
sẽ làm thời điểm trở nên mơ hồ ở DST overlap và không tồn tại ở DST gap.

P3-01 cần một vertical slice nhỏ, kiểm thử được và không kéo recurrence, reminder,
calendar tổng hợp hoặc media lifecycle của Phase 4 vào cùng task.

## Quyết định

1. Session scheduling ban đầu thuộc module `classroom` trong Go modular monolith.
   Module này đã sở hữu class lifecycle và permission projection; chưa tạo module hay
   service riêng chỉ cho một aggregate mới.
2. `class_sessions` luôn có `tenant_id` và `class_id`. Repository lấy tenant từ session
   authenticated, kiểm tra class authoritative và conceal foreign ID bằng `404`.
3. Mỗi occurrence lưu `starts_at`/`ends_at` dạng PostgreSQL `timestamptz` cùng
   `timezone` là tên IANA canonical. Database instant là nguồn dùng để sắp xếp, query
   range và dispatch; timezone là nguồn giữ civil-time intent và hiển thị.
4. API mutation nhận RFC 3339 timestamp có offset rõ ràng cùng IANA timezone. Server
   kiểm tra zone tồn tại, offset khớp zone tại instant và local wall time round-trip
   không đổi. DST gap bị từ chối; DST overlap chỉ hợp lệ khi offset đã disambiguate.
5. P3-01 chỉ hỗ trợ session một lần. Recurring series/occurrence, edit-one,
   edit-future và exception cần thiết kế chi tiết trong P3-02; không clone trước một
   chuỗi occurrence vô hạn.
6. Lifecycle public của P3-01 là `scheduled -> cancelled`. Giá trị `live`/`ended` có
   thể được dự phòng trong schema, nhưng transition do room/media lifecycle điều khiển
   chỉ được nối ở Phase 4. Cancel lặp lại là idempotent.
7. Teacher có quyền `session.schedule` mới được tạo, sửa hoặc hủy. Viewer của lớp chỉ
   được đọc. Draft/archived class không nhận mutation lịch; lịch lịch sử của class đã
   archive vẫn đọc được theo policy.
8. Update dùng optimistic `version`; stale mutation trả `409`. Mutation thành công ghi
   audit metadata allowlist và transactional outbox trong cùng database transaction.
9. List bắt buộc bounded time range và pagination. Server không nhận `tenant_id`, owner,
   role, lifecycle hoặc permission projection do client tự khai.
10. UI hiển thị theo viewer locale/timezone, đồng thời nêu class/session timezone khi
    khác biệt. Các test bắt buộc gồm DST gap, DST overlap, offset mismatch, date/month
    boundary và tenant/class authorization.

## Hệ quả

- Session một lần có instant không mơ hồ, query/index đơn giản và vẫn giữ ý định múi
  giờ để P3-02 xây recurrence đúng semantics.
- Client phải gửi timestamp có offset và timezone, thay vì local datetime thiếu ngữ
  cảnh. Typed problem phải giải thích lỗi timezone/DST mà không lộ dữ liệu khác tenant.
- Thay đổi timezone database của hệ điều hành không làm đổi instant đã lưu.
- P3-02 vẫn cần quyết định recurrence/exceptions và conflict policy; ADR này không tự
  định nghĩa những semantics đó.

## Phương án đã cân nhắc

### Chỉ lưu UTC

Đủ cho occurrence đơn lẻ nhưng làm mất civil-time intent, khiến recurrence qua DST dễ
lệch giờ địa phương.

### Chỉ lưu local datetime và timezone

Không có một instant duy nhất ở overlap và có thể chấp nhận thời điểm không tồn tại ở
gap; query range và dispatch cũng phức tạp hơn không cần thiết.

### Đưa scheduling sang microservice riêng

Không chấp nhận ở Phase 3 vì class policy và transaction audit/outbox đang nằm trong
modular monolith; chưa có tải hay ownership boundary chứng minh lợi ích tách service.
