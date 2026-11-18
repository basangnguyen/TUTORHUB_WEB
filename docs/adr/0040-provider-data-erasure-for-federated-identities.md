# ADR-0040: Xóa dữ liệu do provider cấp cho identity liên kết (Meta Data Deletion)

- Status: Accepted
- Date: 2026-11-18
- Related: [ADR-0039](0039-native-multi-method-authentication-and-federation.md), [AUTH_MULTI_METHOD_RESEARCH_AND_PLAN.md](../AUTH_MULTI_METHOD_RESEARCH_AND_PLAN.md) §7.2.3

## Context

Phase 0 của `AUTH-06A` xác nhận Meta yêu cầu Data Deletion Request Callback phải xóa dữ liệu nhận từ
Facebook, **bao gồm app-scoped user ID (ASID)**. TutorHub lưu ASID ở `tutorhub.identities.subject`
(NOT NULL, unique theo `(provider, subject)`). Chỉ đặt `status='unlinked'` và xóa `email_at_provider`
vẫn để lại ASID, nên TutorHub vẫn truy ngược được tài khoản. Runtime ACL hiện hành (`AUTH-05A`) cố ý
cấm runtime `DELETE` hàng `identities` và cấm `UPDATE (subject)`, để một lỗi ứng dụng hoặc SQL
injection không thể đổi chủ/định danh của identity.

## Decision

1. **Hàm xóa hẹp `SECURITY DEFINER`** (migration `000046`), ví dụ
   `tutorhub.erase_provider_identity(p_provider_slug text, p_issuer text, p_subject text, p_erased_at timestamptz)`:
   - Thuộc sở hữu role migration/owner; `SET search_path = pg_catalog, tutorhub`; `REVOKE ALL ... FROM PUBLIC`;
     chỉ `GRANT EXECUTE` cho runtime role.
   - Chỉ chấp nhận `p_provider_slug` thuộc danh sách cố định trong thân hàm (hiện chỉ `facebook`; provider
     khác cần migration mới).
   - Trong một transaction: khóa identity theo `(provider, subject)`; thu hồi mọi `sessions` có
     `identity_id` tương ứng (`revoked_reason='provider_data_deleted'`); thay `subject` bằng giá trị ngẫu
     nhiên không suy ra được (`'erased:' || gen_random_uuid()`); đặt `email_at_provider`,
     `provider_org_id`, `provider_stable_id` = `NULL`; đặt `status='erased'`; hủy và xóa
     `payload_ciphertext` của mọi `auth_transactions` đang `pending` gắn với identity đó (qua cột digest
     ở mục 3). Trả số hàng bị ảnh hưởng, không trả dữ liệu.
   - Runtime **không** được cấp `DELETE` hay `UPDATE (subject)` trực tiếp; ACL exact-check phải xác nhận
     điều này cùng quyền `EXECUTE` duy nhất.
2. **Trạng thái `erased`**: thêm vào `identities_status_check`. Identity `erased` không bao giờ được đăng
   nhập, relink hay kích hoạt lại, và không được tính là phương thức đăng nhập. Nếu người dùng đăng nhập
   Facebook lại sau này, đó là identity mới → pending registration → email OTP → link intent nếu email đã
   có chủ (không tự gộp).
3. **Tra cứu giao dịch chờ**: thêm cột `auth_transactions.provider_subject_digest bytea NULL` (HMAC-SHA256
   với khóa server của `issuer|subject`), chỉ đặt cho kind `federated_registration` và `link_intent` của
   provider federation; hàm xóa đặt cột này và payload về `NULL`. Giao dịch có TTL ≤ 15 phút.
4. **Biên nhận** `external_data_deletion_requests`: không lưu ASID hay hash cố định của ASID.
   Khóa idempotency là HMAC-SHA256 (khóa server) của chuỗi `signed_request` payload nguyên dạng, nên bản gửi lặp
   của cùng một sự kiện trả lại cùng `confirmation_code`, còn yêu cầu mới sau khi cấp quyền lại (khác
   `issued_at`) tạo biên nhận mới. Lưu `confirmation_code` ngẫu nhiên, `status`, thời điểm; giữ 1 năm.
5. **Đồng bộ**: xóa và chuyển biên nhận sang `completed` trong cùng transaction với callback. Không tìm thấy
   identity vẫn tạo biên nhận `completed` (không tiết lộ tài khoản có tồn tại hay không).
6. **Deauthorize Callback** không xóa dữ liệu: đặt identity `unlinked` và thu hồi các phiên có
   `identity_id` tương ứng bằng quyền runtime hiện có; idempotent.
7. **Phạm vi dữ liệu**: chỉ xóa dữ liệu provider lưu ở `identities` và giao dịch chờ. Hồ sơ TutorHub
   (`users.email` đã được người dùng tự xác minh bằng OTP, `users.display_name` hiển thị và sửa được) là dữ
   liệu người dùng cung cấp cho TutorHub, không tự xóa; Chính sách quyền riêng tư phải nêu rõ điều này và
   cách yêu cầu xóa tài khoản. Tài khoản vẫn khôi phục được bằng email đã xác minh.

## Consequences

- Đáp ứng yêu cầu xóa ASID của Meta mà không nới runtime ACL; bề mặt đặc quyền mới chỉ là một hàm với
  danh sách provider cố định, kiểm thử được bằng exact-ACL test.
- `000046.down` phải chặn rollback khi còn identity `erased`, biên nhận hoặc giao dịch `federated_registration`.
- Thêm provider có yêu cầu xóa tương tự (Apple, Microsoft) cần migration cập nhật danh sách trong hàm.
