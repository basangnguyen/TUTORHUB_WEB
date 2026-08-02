# ADR-0024: Forward-only security correction for Availability Poll maintenance purge

- Trạng thái: **Accepted for P3-02D-A remediation**
- Ngày: 2026-08-02
- Phạm vi: migration `000023`, dedicated poll-maintenance login và P3-02D-A acceptance
- Bổ sung: ADR-0021, `docs/DATABASE.md` và `docs/P3_02D_A_STAGING_ACCEPTANCE.md`

## Bối cảnh

Migration `000022` tạo `tutorhub.purge_expired_availability_polls(integer)` với
`SECURITY INVOKER` và `FOR UPDATE SKIP LOCKED`. Disposable acceptance đã chứng minh exact
maintenance metadata grants/login đúng, nhưng cả locking query và function đều trả SQLSTATE
`42501`: PostgreSQL yêu cầu `UPDATE` trên ít nhất một cột của relation bị khóa, trong khi
exact matrix cố ý không cấp quyền business mutation này cho maintenance login.

Grant thêm `UPDATE` cho `availability_polls` sẽ phá least-privilege contract và cho phép
maintenance login sửa dữ liệu nghiệp vụ trực tiếp. `000022` đã được áp dụng và là migration
lịch sử bất biến; không sửa hoặc rollback file đó.

## Quyết định

1. Tạo migration forward-only `000023_availability_poll_maintenance_security`.
2. Giữ nguyên function signature, batch validation, `SKIP LOCKED`, detach
   `study_meetings.source_poll_id`, FK cascade và bounded return value của `000022`.
3. Đổi execution context của function sang `SECURITY DEFINER`, giữ owner hiện tại của
   function (migration owner), và đặt `search_path` cố định là `pg_catalog, pg_temp`;
   `pg_temp` luôn ở cuối và mọi application relation được schema-qualify trực tiếp trong
   thân hàm. Function không dùng dynamic SQL, không nhận identifier/table name từ caller.
4. Revoke toàn bộ `PUBLIC EXECUTE` trong cùng migration. Dedicated maintenance role chỉ
   dùng grant `USAGE` trên schema và `EXECUTE` đã provision; direct table grants trên các
   bảng `000022` phải được revoke trong bước provisioning. Core API runtime tiếp tục không
   có `EXECUTE` hoặc `DELETE`. Migration không tự tạo role hoặc hardcode role name.
5. Trong staging/private alpha, giữ migration owner hiện tại là lựa chọn tối thiểu chấp
   nhận được khi owner khác runtime/maintenance login, không phải superuser, function body
   vẫn đóng trên một tham số số nguyên, không dynamic SQL và mọi object đều được
   schema-qualify. Các cờ quản trị do Neon gắn cho owner được ghi là residual risk, không
   truyền thêm quyền cho caller ngoài code path của function. Trước production scale hoặc
   trước mọi thay đổi mở rộng input/body, phải chuyển ownership sang function-owner
   `NOLOGIN` tối thiểu quyền bằng quy trình Neon được phê duyệt.
6. Down migration của `000023` không bao giờ phục hồi `SECURITY INVOKER`; nó fail closed
   bằng cách gỡ purge function, không đụng dữ liệu `000022`. Đây chỉ là emergency disable
   path để migration runner không bị dirty; operational policy vẫn là forward-only và
   lượt acceptance hiện tại không chạy rollback thêm.

## Hệ quả và kiểm chứng

- Sau khi operator re-provision ACL, maintenance login không thể đọc child/business columns
  hoặc mutate poll trực tiếp; function là entry point duy nhất cho purge.
- `SECURITY DEFINER` làm owner/search_path trở thành trust boundary mới, nên static test,
  exact-login probe, cascade test và two-transaction `SKIP LOCKED` test là bắt buộc trước
  staging.
- Acceptance phải fail nếu owner trùng runtime/maintenance login, là superuser, function
  đổi sang dynamic SQL hoặc maintenance login có `CREATE` trên schema `tutorhub`.
- Disposable phải forward migrate từ `22 false` lên `23 false`, chạy idempotent migrate lại,
  rồi mới chạy các gate downstream. Shared staging/deploy chưa được phép trước disposable
  PASS.

## Xác minh disposable 2026-08-02

Owner preflight PASS trước forward; migration đạt và giữ idempotent `23 false`. Exact
runtime/maintenance ACL, function metadata, direct-DML denial, cascade/detach, batch validation
và two-transaction `SKIP LOCKED` đều PASS bằng exact login. Migration owner không phải
superuser nhưng có Neon admin residual đã ghi nhận; không quyền nào trong residual đó được
truyền sang runtime/maintenance. Không rollback thêm, không migrate shared staging và giữ
disposable branch cho tới khi staging acceptance hoàn tất.
