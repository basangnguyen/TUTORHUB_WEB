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
   - có `SELECT/INSERT/UPDATE` trên `class_session_series` và
     `class_session_exceptions`;
   - chỉ có `SELECT/INSERT` trên append-only `class_session_mutation_receipts`;
   - không có `DELETE/TRUNCATE` trên ba bảng và không có `UPDATE` trên
     `class_session_mutation_receipts`;
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

## Kết quả chạy 2026-07-28

- Commit Core API: `c622244` (`fix(calendar): preserve append-only receipt permissions`).
- Commit web timezone/drag: `f67ace26`.
- Neon branch: `staging` (`br-royal-hill-aoqn7igi`).
- Render deployment:
  `https://dashboard.render.com/web/srv-d9c1tmmrnols73dkl5g0/deploys/dep-d9k2ik1t0dsc738qoh7g`
  chạy commit `c622244`; `/health` trả `200`.
- Cloudflare deployment: `https://tutorhub-web.pages.dev`.
- Database gate: **PASS** cho migration `19 false`, runtime role không phải owner/superuser,
  exact ACL series/exception `SELECT/INSERT/UPDATE`, receipt append-only `SELECT/INSERT`,
  và không có `DELETE/TRUNCATE`.
- UI smoke Teacher: **PASS** cho quick create chuỗi ba buổi, projection Week/Agenda,
  preview `this_occurrence`/`this_and_following`/`entire_series`, cancel/revert, hard
  conflict, thông báo/focus `409` và drag theo scope.
- Mutation persistence: **PASS**. Kéo buổi 2026-07-30 từ `13:00–14:00` sang
  `11:30–12:30`, chọn `this_occurrence`, lưu thành công và vẫn đúng sau reload; hai
  occurrence 2026-08-06 và 2026-08-13 vẫn `13:00–14:00`.
- Observability: **PASS**. `/metrics` trả `200` với
  `expansions_total=15`, `occurrences_total=17`, `rejections_total=0` và
  `duration_seconds_sum=0.001345`.
- Lỗi staging đã đóng: receipt lookup từng dùng `SELECT ... FOR UPDATE`, làm PostgreSQL
  đòi quyền `UPDATE` trái với ACL append-only. Core API hiện khóa series row, đọc receipt
  không khóa và vẫn dựa vào receipt primary key để chặn idempotency collision.
- Neon disposable acceptance branch:
  `p3-calendar-pre-migration-20260726` (`br-silent-math-aozfo2ci`), parent `staging`.
  Không tạo hoặc xóa thêm branch; branch này đã có expiration do Neon quản lý.
- Database version sau migration trên disposable branch: literal **`19 false`**.
- Automated PostgreSQL acceptance tại commit `734d2b6`: **PASS** trong `25,75s` cho
  `TestPostgresClassSessionSeriesLifecycleConflictAndTenantScope`.
  - Hai request đồng thời cùng key/payload tạo đúng một mutation và một replay; hai
    request khác key cùng `expected_version` tạo đúng một success và một stale-version
    conflict, không nhân đôi exception/receipt.
  - Student enrollment active đọc được projection read-only nhưng bị từ chối
    create/preview/update/cancel. Teacher không được dùng conflict override;
    Organization Admin chỉ override thành công khi có reason hợp lệ.
  - Read và mutation chéo tenant trả concealment, không mutate tài nguyên tenant khác.
  - Split `this_and_following` giữ exception trước boundary trên parent và carry đủ ba
    exception từ boundary về sau sang child series.
  - `EXPLAIN` của query class-scoped có `LIMIT 129` xác nhận index viability qua
    `class_session_series_class_start_idx`; recurrence expansion tiếp tục bị chặn bởi
    window/occurrence/deadline cap của ADR-0019.
- Full Core API regression `go test ./services/core-api/...` và
  `go vet ./services/core-api/...`: **PASS**.
- Trạng thái tổng: **DONE**. Tất cả database/API/UI/authorization/idempotency/conflict/
  metrics gate của phạm vi P3-02B đã có bằng chứng. Working hours, attendee/free-busy và
  RSVP vẫn thuộc P3-02C; email/ICS provider delivery vẫn thuộc P3-05A/P3-CAL-02.
