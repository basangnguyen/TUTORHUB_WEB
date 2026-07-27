# P3-02B Staging Acceptance

## Mục tiêu

Xác nhận recurrence và class schedule conflict hoạt động đúng trên Neon staging trước khi
chuyển P3-02B từ `VERIFY` sang `DONE`. Không chạy checklist này trên production.

## Điều kiện trước khi chạy

- Neon branch là staging hoặc disposable branch được tạo từ staging.
- `DATABASE_MIGRATION_URL` chỉ tồn tại trong biến môi trường cục bộ, không ghi vào log,
  tài liệu hoặc Git.
- Core API đang chạy đúng commit cần nghiệm thu.
- Deployment guardrail vẫn giữ `FEATURE_CLASS_SESSION_RECURRENCE=false`; chỉ bật canary
  bằng tenant override cho tenant kiểm thử.

## Database gate

1. Ghi nhận phiên bản hiện tại:

   ```powershell
   corepack pnpm db:version
   ```

2. Chạy migration:

   ```powershell
   corepack pnpm db:migrate
   corepack pnpm db:version
   ```

3. Kết quả mong đợi: `19 false`.
4. Áp dụng exact runtime grants của migration `000018/000019` theo
   [DATABASE.md](DATABASE.md#recurrence-series-exception-và-runtime-grants-cho-migration-000018000019).
5. Xác nhận `tutorhub_runtime`:
   - có `SELECT/INSERT/UPDATE` trên `class_session_series`,
     `class_session_exceptions`, `class_session_mutation_receipts`;
   - không có `DELETE/TRUNCATE`;
   - không phải superuser, owner hoặc member của `neondb_owner`.

## API và authorization smoke

Chạy bằng một lớp staging có Teacher, Student và Organization Admin:

1. Khi feature tắt, create/read/preview/update/cancel recurrence phải fail closed.
2. Bật tenant override cho canary:
   - Teacher tạo chuỗi hữu hạn và đọc lại đúng normalized rule/timezone.
   - Student không có quyền schedule không thể tạo, sửa hoặc hủy chuỗi.
3. Preview lần lượt ba scope:
   - `this_occurrence`;
   - `this_and_following` với `carry`, `rebase`, `discard`;
   - `entire_series`.
4. Update/hủy đúng scope và kiểm tra calendar projection không tạo occurrence trùng.
5. Gửi lại cùng idempotency key và cùng payload: trả replay an toàn, không nhân đôi dữ liệu.
6. Dùng lại key với payload khác: trả conflict.
7. Gửi `expected_version` cũ: trả `409`, dữ liệu không thay đổi.
8. Tạo lịch one-time/recurring giao nhau tại khoảng nửa mở:
   - Teacher bị chặn bởi schedule conflict;
   - `org_admin` chỉ override được khi có lý do;
   - Student/Teacher không thể dùng cờ override.
9. Dùng tenant/class/series ID chéo tenant: không lộ tài nguyên và không mutate.

## UI smoke

- Quick Create tạo được lịch lặp hợp lệ.
- Drag/resize occurrence mở dialog chọn phạm vi; Cancel hoàn nguyên ngay khi đóng dialog.
- Preview hiển thị số buổi và ngoại lệ bị ảnh hưởng.
- Hủy lịch lặp hiển thị cùng preview trước khi xác nhận.
- Conflict chặn nút lưu; Admin nhập lý do override mới có thể tiếp tục.
- `409` giữ dữ liệu an toàn, thông báo rõ và focus quay lại điểm thao tác.
- Month/Week/Agenda hiển thị occurrence lặp đúng timezone và không trùng ID.

## Observability gate

Endpoint `/metrics` phải có và tăng hợp lý sau smoke:

- `tutorhub_calendar_recurrence_expansions_total`
- `tutorhub_calendar_recurrence_occurrences_total`
- `tutorhub_calendar_recurrence_rejections_total`
- `tutorhub_calendar_recurrence_duration_seconds_sum`

Không metric hoặc log nào chứa title, mô tả lớp, email, token hay recurrence payload riêng tư.

## Exit gate

P3-02B chỉ chuyển `DONE` khi:

- migration là `19 false`;
- exact grants và tenant isolation đạt;
- API/UI/authorization/idempotency/conflict smoke đạt;
- metrics hiện diện;
- commit và URL deployment được ghi lại trong tài liệu này.

Sau nghiệm thu, bổ sung ngày, commit, URL và kết quả PASS vào phần dưới:

```text
Ngày:
Commit:
Neon branch:
Render deployment:
Cloudflare deployment:
Database/API/UI/Observability: PASS | FAIL
Ghi chú:
```
