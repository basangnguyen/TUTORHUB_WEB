# P2-11 V1 fixture import specification

## 1. Phạm vi

P2-11 chứng minh import lặp lại an toàn cho bốn aggregate đại diện: `user`, `tenant`,
`membership`, `class`. Nguồn duy nhất là fixture ẩn danh được review trong repository.
Không kết nối, đọc file cấu hình, secret hoặc production data tại `D:\Ban_sao_du_an`.

CLI này không phải production cutover tool. Production migration vẫn cần inventory,
privacy/legal sign-off, snapshot checksum, delta/cutover và rollback window riêng.

## 2. Mapping V1 -> V2

| Entity | V1 fixture | V2 | Quy tắc |
|---|---|---|---|
| User ID | `external_id` | `users.id` + ledger | Mapping bền theo source/entity/external ID. |
| User status | `active`, `disabled`, `deleted` | `active`, `suspended`, skip | `deleted` nằm ngoài fixture pilot và được report `skipped`. |
| Tenant ID | `external_id` | `tenants.id` + ledger | Slug collision chưa có mapping là lỗi. |
| Tenant status | `active`, `disabled`, `archived` | `active`, `suspended`, `archived` | Archived yêu cầu timestamp. |
| Membership role | `administrator`, `instructor`, `learner`, `observer` | `org_admin`, `teacher`, `student`, `guest` | Role ngoài allowlist bị từ chối khi validate. |
| Membership status | `active`, `blocked`, `removed` | `active`, `suspended`, `removed` | Active yêu cầu `joined_at`. |
| Class owner | `owner_user_external_id` | `classes.owner_user_id` | Owner phải có membership active cùng tenant. |
| Class status | `draft`, `open`, `closed` | `draft`, `active`, `archived` | Closed dùng `archived_from_status=active`. |
| Time | RFC3339 | `timestamptz` UTC | Offset được giữ đúng thời điểm; DB chuẩn hóa UTC. |
| Timezone | IANA name | `timezone` | Từ chối rỗng, `local` hoặc IANA không tồn tại. |

Không import identity provider, password, token, session, invitation, file hoặc nội dung
lớp học. User bị skip kéo theo membership/class phụ thuộc được skip bằng reason code,
không tự tạo placeholder.

## 3. Fixture contract và guardrail

- `fixture_version=1`, `anonymized=true`.
- `source_system` và mọi external ID là key bounded, không chứa khoảng trắng.
- Email phải normalized và kết thúc bằng `.invalid`.
- JSON từ chối unknown/duplicate field, trailing document và file trên 2 MiB.
- Tối đa 5.000 record mỗi entity trong P2-11.
- Fixture chuẩn: `services/core-api/testdata/v1import/p2-11-anonymized.json`.

## 4. Lệnh chạy

```powershell
# Chỉ development/test/staging; dùng direct migration URL, không dùng pool runtime.
corepack pnpm v1:import:dry-run -- --fixture services/core-api/testdata/v1import/p2-11-anonymized.json
corepack pnpm v1:import:apply -- --fixture services/core-api/testdata/v1import/p2-11-anonymized.json
```

CLI in reconciliation JSON ra stdout. Không in database URL hoặc nội dung source row.

## 5. Idempotency, checkpoint và reconciliation

- Dry-run dùng transaction rollback nên không để lại business row, mapping hoặc run.
- Apply ghi mỗi record, mapping và checkpoint trong cùng transaction.
- Run `failed/running` cùng source/fixture/checksum được resume; checkpoint chỉ tăng sau
  commit thành công.
- Apply lại fixture đã hoàn tất tạo run mới: record đã khớp là `unchanged`.
- Report theo entity và tổng gồm `source`, `mapped`, `imported`, `updated`, `unchanged`,
  `skipped`, `failed`; skip chỉ lưu reason code allowlist.

## 6. Acceptance P2-11

1. Migrate 12 -> 13, rollback 13 -> 12 và migrate lại đạt trên PostgreSQL 17.
2. Dry-run không đổi số row/ledger.
3. Apply lần đầu tạo đúng user/tenant/membership/class và owner.
4. Apply lần hai không tăng số business row/mapping; outcome là `unchanged`.
5. Inject failure sau checkpoint, resume và cho kết quả giống clean import.
6. Reconciliation không có failed record; skip đúng dữ liệu ngoài mapping.
7. Reset/rollback được kiểm tra trên database/Neon branch tạm trước khi P2-11 `DONE`.
