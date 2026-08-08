# P3-08 staging acceptance: file metadata, upload intent and finalize

## 1. Trạng thái và ranh giới

- Trạng thái hiện tại: `VERIFY`.
- Migration source: `000026_content_files`.
- P3-08 chỉ gồm metadata class file, quota reservation, upload intent idempotent,
  metadata read và server-side finalize proof.
- P3-08 không cấp presigned URL, không proxy binary, không download/share file và không
  chuyển file sang `ready`. Các phần đó lần lượt thuộc P3-09, P3-10 và P3-11A/B.
- Không migrate shared staging hoặc deploy trước khi disposable PostgreSQL gates bên dưới
  đạt và candidate đã qua CI/security.

## 2. Preconditions

Disposable branch phải có hai database credential tách biệt, nạp từ file local đã ignore và không
in/log giá trị:

- `DATABASE_MIGRATION_URL`: owner/direct URL cho migration và fixture.
- `DATABASE_POOL_URL`: Core API runtime pooled URL.

Credential `B2_*` của private test bucket chưa phải precondition cho database gate P3-08.
Chúng chỉ được yêu cầu ở provider acceptance P3-09, nơi phải chứng minh `readFiles`/`writeFiles`
và `HeadObject` trên bucket không phải production.

Chạy owner preflight trước forward:

```sql
SELECT current_user,
       has_schema_privilege(current_user, 'tutorhub', 'CREATE') AS can_migrate;
SELECT version, dirty FROM schema_migrations;
```

Yêu cầu đầu vào: `25 false` và migration owner có quyền thay constraint/tạo table. Không
rollback shared/disposable để chứng minh idempotency.

## 3. Forward migration gate

Chuỗi bắt buộc:

```text
25 false -> 26 false -> 26 false
```

Lần đầu phải tạo `content_files`, `tenant_file_usage`, feature/quota keys, lifecycle/index/
tenant-composite constraints và `PUBLIC` zero-grant. Lần hai không đổi ledger hoặc tạo
thêm object. Nếu forward lỗi, dừng tại disposable và giữ branch để điều tra.

## 4. Exact Core API runtime ACL

Thay `tutorhub_runtime` bằng role thật, chạy bằng migration owner. Không đưa password hay
URL vào SQL:

Trước khi cấp ACL cho hai bảng mới, runtime role phải vẫn có baseline đã nghiệm thu
từ các phase trước: `SELECT` trên `tenants`, `memberships`, `users`, `classes`,
`class_enrollments`, `tenant_feature_overrides`, `tenant_quota_overrides` và
`tenant_quota_windows`; riêng `tenant_quota_windows` cần thêm `INSERT, UPDATE` cho fixed-window
rate quota. P3-08 không tự mở rộng các grant cũ; exact-login integration gate sẽ fail
closed nếu thiếu bất kỳ dependency nào.

```sql
BEGIN;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;
REVOKE CREATE ON SCHEMA tutorhub FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.content_files,
    tutorhub.tenant_file_usage
FROM tutorhub_runtime;

REVOKE INSERT (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, status, version,
    upload_expires_at, stored_size_bytes, stored_media_type,
    stored_checksum_sha256, storage_etag, storage_version_id, uploaded_at,
    processing_at, ready_at, rejected_at, deleted_at, deletion_reason,
    created_at, updated_at
), UPDATE (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, status, version,
    upload_expires_at, stored_size_bytes, stored_media_type,
    stored_checksum_sha256, storage_etag, storage_version_id, uploaded_at,
    processing_at, ready_at, rejected_at, deleted_at, deletion_reason,
    created_at, updated_at
), REFERENCES (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, status, version,
    upload_expires_at, stored_size_bytes, stored_media_type,
    stored_checksum_sha256, storage_etag, storage_version_id, uploaded_at,
    processing_at, ready_at, rejected_at, deleted_at, deletion_reason,
    created_at, updated_at
) ON TABLE tutorhub.content_files FROM tutorhub_runtime;

REVOKE INSERT (
    tenant_id, file_count, reserved_bytes, committed_bytes, updated_at
), UPDATE (
    tenant_id, file_count, reserved_bytes, committed_bytes, updated_at
), REFERENCES (
    tenant_id, file_count, reserved_bytes, committed_bytes, updated_at
) ON TABLE tutorhub.tenant_file_usage FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.content_files,
    tutorhub.tenant_file_usage
FROM PUBLIC;

GRANT SELECT ON TABLE
    tutorhub.content_files,
    tutorhub.tenant_file_usage
TO tutorhub_runtime;

GRANT INSERT (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, upload_expires_at,
    created_at, updated_at
) ON TABLE tutorhub.content_files TO tutorhub_runtime;

GRANT INSERT (tenant_id, updated_at)
ON TABLE tutorhub.tenant_file_usage TO tutorhub_runtime;

GRANT UPDATE (
    status, version, stored_size_bytes, stored_media_type,
    stored_checksum_sha256, storage_etag, storage_version_id,
    uploaded_at, deleted_at, deletion_reason, updated_at
) ON TABLE tutorhub.content_files TO tutorhub_runtime;

GRANT UPDATE (file_count, reserved_bytes, committed_bytes, updated_at)
ON TABLE tutorhub.tenant_file_usage TO tutorhub_runtime;

COMMIT;
```

Exact matrix:

| Relation            | table SELECT | table INSERT/UPDATE | Column INSERT                                     | Column UPDATE                                               | DELETE |
| ------------------- | :----------: | :-----------------: | ------------------------------------------------- | ----------------------------------------------------------- | :----: |
| `content_files`     |     yes      |         no          | intent identity/declared metadata/timestamps only | finalize proof, tombstone lifecycle, version/timestamp only |   no   |
| `tenant_file_usage` |     yes      |         no          | `tenant_id,updated_at`                            | `file_count,reserved_bytes,committed_bytes,updated_at`      |   no   |

Role cũng phải không có `TRUNCATE`, `REFERENCES`, `TRIGGER`, ownership, superuser,
create-role/database, replication, bypass-RLS hoặc membership trong migration role.

## 5. Database and API gates

Chạy PostgreSQL integration package bằng runtime role sau provisioning:

```text
go test -count=1 -tags=integration ./services/core-api/internal/modules/content
```

Phải PASS:

1. `PUBLIC` zero grant và exact column/table ACL.
2. Hai concurrent intent cùng actor/idempotency/payload trả cùng file; đúng một request
   `created=true`; quota/rate chỉ tiêu thụ một lần.
3. Reuse idempotency key với payload khác trả `409`; expired key không tạo object mới.
4. Student không có `file.upload`; foreign tenant và inaccessible ID trả `404`.
5. Reservation tăng `file_count/reserved_bytes`; finalize atomically chuyển đúng byte sang
   `committed_bytes`, không double-count dưới concurrent finalize.
6. Archived class chặn intent/finalize; metadata chưa `ready` chỉ creator/upload manager xem.
7. Size, normalized MIME, SHA-256, ETag và version thiếu/sai đều fail-closed, không đổi state.
8. Response/log/audit/outbox không chứa object key, provider proof, filename/checksum ngoài
   projection có chủ đích; P3-08 không phát outbox event.
9. Feature `file_uploads`, file-count/byte/single-file/hourly quotas có tenant override,
   deployment ceiling và object-storage runtime prerequisite fail-closed.

## 6. B2 evidence boundary

P3-08 unit/contract tests chứng minh Core API yêu cầu checksum SHA-256 và immutable version
trước commit. P3-09 phải chứng minh trên disposable B2 rằng exact presigned PUT làm provider
trả hai metadata này qua `HeadObject`. Nếu provider không trả đủ, không nới finalize; sửa
transfer contract hoặc giữ feature off.

## 7. Exit decision

Chỉ chuyển `P3-08 IN PROGRESS -> VERIFY` khi local Go/OpenAPI/client/full security gates và
disposable `25 -> 26` + exact ACL/PostgreSQL tests PASS. Chỉ chuyển `VERIFY -> DONE` sau
review diff/secret, candidate CI xanh và báo cáo rõ provider evidence còn thuộc P3-09.

## 8. Evidence ngày 2026-08-07

- Local `pnpm verify`: PASS, gồm format/generated contract, lint, typecheck, unit, build,
  Storybook, client-bundle security, Go test/vet và workflow-security policy.
- Disposable owner preflight: PASS; `OWNER_ADMIN_RESIDUAL=true` được ghi nhận nhưng runtime
  role không nhận đặc quyền owner/admin.
- Forward-only migration: `25 false -> 26 false -> 26 false`, PASS; không rollback.
- Exact runtime column ACL, schema no-create, role safety và `PUBLIC` zero-grant: PASS.
- `go test -count=1 -tags=integration ./services/core-api/internal/modules/content`: PASS,
  zero `SKIP`.
- Shared staging vẫn `25 false`; chưa deploy. P3-08 chuyển `IN PROGRESS -> VERIFY` và chờ
  exact candidate diff/secret review cùng CI/security trước quyết định `DONE`.
