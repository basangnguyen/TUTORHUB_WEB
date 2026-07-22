# ADR-0016: Import fixture V1 idempotent bằng migration ledger

- Trạng thái: Accepted
- Ngày: 2026-07-22
- Phạm vi: P2-11, fixture ẩn danh development/test/staging

## Bối cảnh

Phase 2 cần chứng minh đường chuyển đầu tiên cho user, tenant, membership và class từ
TutorHub V1 sang schema V2. Đây chưa phải production migration: chất lượng, quyền sử
dụng và retention của dữ liệu V1 thật chưa được xác minh. Đọc trực tiếp database V1,
sao chép cấu hình legacy hoặc dùng email thật trong fixture sẽ tạo rủi ro secret/PII.

Import phải chạy lại an toàn, báo trước thay đổi, tiếp tục được sau lỗi giữa chừng và
giữ mapping ID cũ-mới để các aggregate sau tham chiếu ổn định.

## Quyết định

1. Thêm CLI `v1-fixture-import` vào Go modular monolith; không tạo service mới.
2. CLI chỉ nhận JSON fixture versioned, bắt buộc `anonymized=true`, email miền
   `.invalid`, kích thước/record count hữu hạn và strict schema. CLI chặn production.
3. Dùng `DATABASE_MIGRATION_URL`; Core API runtime không được cấp quyền vào ledger.
4. Migration `000013` tạo ba bảng nội bộ:
   - `legacy_import_runs`: checksum, trạng thái và checkpoint ordinal;
   - `legacy_import_run_items`: outcome/reason code theo record;
   - `legacy_import_mappings`: `(source_system, entity_type, external_id) -> target_id`.
5. Thứ tự canonical là user -> tenant -> membership -> class. Mỗi record apply cùng
   mapping và checkpoint trong một transaction. Lỗi không nâng checkpoint; lần sau
   resume đúng record đó.
6. `dry-run` chạy cùng transform/upsert path trong transaction rồi rollback. `apply`
   ghi theo record và tạo reconciliation report JSON.
7. Upsert chỉ theo mapping external-ID đã sở hữu. Natural-key collision chưa có mapping
   là lỗi fail-closed, không tự gộp hai identity/tenant/class.
8. Import fixture không tạo OIDC identity, password, session, invitation hoặc token.

## Hệ quả

- Có thêm schema metadata migration nhưng không mở public API và không ảnh hưởng runtime.
- Commit theo record cho phép resume, đổi lại toàn fixture không atomic. Ledger và report
  là nguồn quan sát phần đã hoàn thành.
- Fixture chạy lại tạo run mới và cho outcome `unchanged`; không tạo duplicate business
  row hoặc mapping.
- Production/cohort migration cần discovery, privacy sign-off, snapshot checksum,
  cutover/rollback riêng và có thể supersede ADR này.

## Phương án đã cân nhắc

### Một transaction cho toàn fixture

Đơn giản nhưng không chứng minh checkpoint/resume sau lỗi giữa chừng.

### Chỉ dùng UUID deterministic, không lưu mapping

Idempotent về ID nhưng thiếu ledger để reconcile, đổi mapping và xử lý nguồn legacy có
natural key xung đột.

### Đọc trực tiếp V1 trong CI

Không chấp nhận vì kéo secret/PII và phụ thuộc schema/runtime legacy vào test V2.
