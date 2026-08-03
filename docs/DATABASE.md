# Database foundation

Tài liệu này là runbook cho nền PostgreSQL của TutorHub V2 từ P1-05. Mọi agent
thay đổi schema, migration hoặc repository phải đọc tài liệu này trước khi sửa.

## Trạng thái hiện tại

- System of record: Neon PostgreSQL.
- Schema ứng dụng: `tutorhub`.
- Migration mới nhất trong source: `000024_conversation_core`; disposable Neon đã forward
  `23 -> 24` và rerun idempotent giữ `24 false` ngày 2026-08-03. Shared Neon staging vẫn
  ở `23 false`; migration `000024` chưa được áp dụng lên shared staging.
- Migration 1-5 đã được chạy và kiểm tra trên Neon; smoke
  `5 false -> rollback 4 false -> migrate 5 false` đạt ngày 2026-07-16.
- Migration `000006` đến `000013` đều có up/down path. Source và PostgreSQL 17 CI
  của P2-11 đã xác nhận `000013` qua migrate `13 -> 12 -> 13`, dry-run, apply/rerun,
  checkpoint/resume và reset. P2-12 ngày 2026-07-22 đã xác nhận branch Neon dùng một
  lần qua `12 false -> 13 false -> 12 false -> 13 false`; staging thật đã forward tới
  `13 false`. Role split, default/effective/direct ledger ACL và future-table probe
  đều đạt least-privilege sau remediation provisioning.
- Neon staging đã được forward và xác nhận `21 false` ngày 2026-07-30. Migration
  `000015` mở rộng in-place `outbox_events` cho lease/fencing/retry/
  dead-letter; migration `000016` tạo notification projection/preference và mở rộng
  feature-control key; migration `000017` thêm index đọc Calendar tổng hợp cùng
  `calendar_display_preferences`; migration `000018` tạo bounded recurring series/
  exception overlay và migration `000019` bổ sung iCal identity cùng idempotency
  receipts. Migration `000020` thêm working schedule/free-busy, attendee/audience,
  invitation và RSVP; migration `000021` tách immutable business snapshot khỏi
  per-recipient delivery method. Exact Core API runtime grants và P3-02C staging
  acceptance đã đạt; quyền worker, durable-host và các acceptance gate riêng của
  P3-03/P3-04 vẫn chưa đạt.
- Migration `000020` thêm working schedule/exception, audience/attendee, external
  recipient, immutable invitation snapshot, RSVP capability và participation receipt.
  Migration `000021` tách immutable business snapshot khỏi per-recipient delivery
  method do P3-05A sở hữu. Cả hai migration đã được chạy trên Neon staging; probe
  exact runtime ACL, smoke PostgreSQL và acceptance của P3-02C đã đạt. Delivery
  boundary vẫn thuộc P3-05A và chưa bật business email/notification side effect.
- Migration `000022` thêm Availability Poll/response/capability/Study Meeting, quota và
  maintenance hard-retention function. Disposable đã PASS chuỗi lịch sử `21 -> 22 -> 21 -> 22`
  và exact runtime ACL; function `SECURITY INVOKER` từng bị blocker `42501` như ADR-0024 ghi.
  Migration forward-only `000023` đã sửa boundary sang hardened `SECURITY DEFINER`; exact
  runtime/maintenance ACL và toàn bộ database gate P3-02D-A đã PASS ở `23 false`. Disposable
  này sau đó được forward lên `24 false` cho P3-06; shared Neon staging vẫn ở `23 false`.
- Phần lớn integration test rollback bằng transaction. Chỉ focused P2-09 suite có
  fixture tự dọn hoàn toàn được chạy trên staging ngày 2026-07-21; các suite concurrency
  có thể để lại audit append-only vẫn chỉ chạy trên database CI tạm thời.
- Core API đã được smoke test với Neon: `/ready` trả `ready` và `/health` trả `ok`.

Neon có branch `production` và branch staging tách biệt. Core API staging dùng pooled
runtime role tối thiểu quyền; migration job dùng direct migration role riêng. Kết nối,
readiness và smoke sau migration `000014` đã đạt trong P3-01. Các giá trị credential
không được ghi vào runbook hoặc artifact.

## Bốn connection URL

| Biến                     | Đối tượng sử dụng | Loại URL                                 | Quy tắc                                                |
| ------------------------ | ----------------- | ---------------------------------------- | ------------------------------------------------------ |
| `DATABASE_POOL_URL`      | Core API          | Neon pooled, hostname có `-pooler`       | Chỉ API runtime role; pool nhỏ theo connection budget  |
| `DATABASE_WORKER_URL`    | Outbox worker     | Neon pooled, hostname có `-pooler`       | Chỉ worker role; không fallback sang API/migration URL |
| `DATABASE_MIGRATION_URL` | CLI/release job   | Neon direct, hostname không có `-pooler` | Chỉ migration owner; không đưa vào runtime container   |
| `DATABASE_POLL_MAINTENANCE_URL` | Poll retention operator | Neon direct, hostname không có `-pooler` | Chỉ maintenance login; schema `USAGE` + function `EXECUTE` |

Không dùng URL direct cho traffic ứng dụng thường xuyên. Không chia sẻ credential giữa
API và worker, không cho worker fallback sang `DATABASE_POOL_URL`, và không cấp URL
migration cho frontend, browser, Cloudflare Pages, Core API hoặc worker. Hai runtime
process không tự chạy migration khi khởi động.

## Cấu hình pool mặc định

| Biến                                | Mặc định | Ý nghĩa                                         |
| ----------------------------------- | -------: | ----------------------------------------------- |
| `DATABASE_MAX_CONNECTIONS`          |      `4` | Giới hạn kết nối của một Core API instance      |
| `DATABASE_MIN_CONNECTIONS`          |      `0` | Cho phép scale-to-zero khi rảnh                 |
| `DATABASE_CONNECT_TIMEOUT`          |    `10s` | Giới hạn thời gian mở/ping kết nối              |
| `DATABASE_QUERY_TIMEOUT`            |     `5s` | Timeout dùng cho readiness/repository operation |
| `DATABASE_MAX_CONNECTION_LIFETIME`  |    `30m` | Làm mới kết nối dài hạn                         |
| `DATABASE_MAX_CONNECTION_IDLE_TIME` |     `5m` | Thu hồi kết nối rảnh                            |
| `DATABASE_HEALTH_CHECK_PERIOD`      |     `1m` | Chu kỳ kiểm tra pool                            |

`application_name=tutorhub-core-api` được gắn cho API và worker phải dùng tên riêng
`tutorhub-outbox-worker` để quan sát trên Neon. Mọi truy vấn mạng/database phải chạy
ngoài UI thread ở các client native về sau.

## Schema source nền đến phiên bản 24

| Bảng                     | Vai trò                                                                                          |
| ------------------------ | ------------------------------------------------------------------------------------------------ |
| `users`                  | Hồ sơ định danh nội bộ, email chuẩn hóa và trạng thái tài khoản                                  |
| `identities`             | Ánh xạ `(provider, subject)` từ OIDC, verified email và lần xác thực gần nhất                    |
| `tenants`                | Workspace với slug/name, locale/timezone, status, optimistic `version` và `archived_at`          |
| `memberships`            | Quan hệ user-tenant và role `org_admin/teacher/student/guest`                                    |
| `sessions`               | Hash session/CSRF, active tenant, `context_version`, idle/absolute expiry và revoke state        |
| `auth_flows`             | HMAC state/binding/nonce, PKCE verifier mã hóa và one-time consume                               |
| `classes`                | Lớp học theo tenant; owner cùng tenant, timezone, lifecycle, optimistic version và archive state |
| `class_enrollments`      | Quan hệ user-class theo tenant, class role, trạng thái tham gia và các mốc lifecycle             |
| `class_invite_codes`     | Mã mời lớp chỉ lưu HMAC, TTL, giới hạn lượt dùng, trạng thái và actor lifecycle                  |
| `membership_invitations` | Lời mời tenant một lần: normalized email, role, HMAC token, TTL và terminal state                |
| `outbox_events`          | Transactional outbox có exact versioned event type, lease/fencing, retry và retained dead-letter |
| `audit_events`           | Lịch sử tenant append-only cho actor/action/resource/outcome và request correlation              |
| `class_sessions`         | Buổi học một lần theo class, UTC instant/IANA timezone, lifecycle và optimistic version          |
| `class_session_series`   | Recurrence bounded theo class, civil-time/IANA timezone, stable iCal UID, sequence và optimistic version |
| `class_session_exceptions` | Tombstone/override occurrence theo stable key, retention policy và optimistic version             |
| `class_session_mutation_receipts` | Idempotency fingerprint cho update/cancel, tenant-scoped và append-only                         |
| `notifications`          | Projection tenant/user-scoped, idempotent theo source/recipient/effect và trạng thái read          |
| `notification_preferences` | Preference in-app/email, reminder, quiet-hours IANA và optimistic version                         |
| `calendar_display_preferences` | Preference Calendar tenant/user-scoped cho timezone, locale, view, density và optimistic version |
| `tenant_feature_control_revisions` | Phiên bản optimistic của override feature/quota theo tenant                         |
| `tenant_feature_overrides` | Override feature typed theo tenant; global disable vẫn có quyền ưu tiên                          |
| `tenant_quota_overrides` | Override hard limit typed cho member, active class và invitation rate                              |
| `tenant_quota_windows`   | Bộ đếm fixed-window tenant-scoped cho quota invitation                                              |
| `rate_limit_windows`     | Bộ đếm anonymous shared; lưu purpose và SHA-256 đã domain-separate theo version/purpose/prefix       |
| `legacy_import_runs`     | Ledger migration-role-only cho checksum, trạng thái và checkpoint fixture V1                         |
| `legacy_import_run_items` | Outcome/reason code bounded theo record để reconciliation và resume                                  |
| `legacy_import_mappings` | Mapping bền `(source_system, entity_type, external_id) -> target_id`; không chứa source payload        |
| `conversations`          | Container direct/class tenant-scoped; canonical direct pair và tối đa một conversation mỗi class     |
| `conversation_members`   | Hai membership row server-owned cho direct conversation; class membership luôn đọc từ enrollment      |

Ràng buộc quan trọng:

- Mã lớp chỉ duy nhất trong từng tenant.
- Foreign key tổng hợp chặn owner/member thuộc tenant khác.
- Repository luôn nhận `tenancy.Context` gồm `tenant_id` và `actor_user_id`.
- Class tiếp tục dùng `owner_user_id` làm owner implicit, không tạo enrollment riêng
  cho owner. `class_enrollments` giữ các role `co_teacher`, `teaching_assistant`,
  `student` với state `invited/active/suspended/left/removed`; mỗi user chỉ có một row
  trong một class và mọi foreign key actor/user đều bị khóa theo tenant.
- Chỉ enrollment `active` được nạp thành class role authoritative cho class/media.
  Enrollment inactive không cấp quyền; owner implicit và quyền organization vẫn được
  shared policy đánh giá độc lập. HTTP trả projection `viewer_access` do server tính,
  không tin class role do session hoặc browser tự khai.
- P2-06 dùng schema/index tenant-class-role-status của `000010`. P2-07 thêm migration
  `000011` riêng cho audit. Owner roster vẫn được đọc từ `classes.owner_user_id`, ghim
  trong projection và bị loại khỏi page enrollment. Search display name/email tenant/
  class-scoped dùng Unicode NFC, collapsed whitespace, lowercase và literal matching
  cho `%`/`_`; keyset ổn định theo normalized display name rồi `user_id`.
- `audit_events` luôn có `tenant_id`, actor user hoặc system nhất quán, action/resource
  đã validate, outcome `succeeded/denied/failed`, request ID và UUID request-instance.
  User actor tham chiếu user authoritative nhưng không bắt buộc đã là member của tenant:
  accept invitation có thể xác định target tenant hợp lệ trước khi actor gia nhập; target
  này phải do server resolve từ token, không nhận tenant scope do client tự khai.
  Bulk roster gắn `target_user_id` server-owned để fallback từng item dedupe đúng với
  atomic audit nếu phản hồi commit bị lỗi hoặc không chắc chắn.
  Trigger `ENABLE ALWAYS` từ chối update/delete/truncate; public API chỉ có list.
  Source IP chỉ giữ IPv4 `/24` hoặc IPv6 `/56`, user agent chỉ giữ SHA-256 và hai trường
  này không xuất hiện trong API projection. Metadata là object phẳng, bounded, string
  allowlist; constraint chặn key liên quan token/secret/session/email/name/payload/error.
- Opaque roster cursor chỉ mang user ID cùng hash scope/filter, không mang display name
  hoặc email; hash ràng buộc tenant, class, normalized search và status. Role update
  P2-06 là last-write-wins có refetch, chưa thêm version/CAS cho enrollment.
- Direct enrollment chỉ tìm active tenant member theo normalized email, tạo/reactivate
  role `student` và không tạo owner/manager trùng. Suspend/remove/leave dùng transition
  có điều kiện; suspended/removed không thể tự join lại bằng invite code, còn manager
  có thể direct-reactivate theo policy.
- Class invite token có prefix `thciv1_`, entropy 256-bit và database chỉ giữ unique
  purpose-bound HMAC 32 byte. TTL bị chặn trong khoảng 15 phút đến 30 ngày; usage limit
  từ 1 đến 1000 và `usage_count` không được vượt giới hạn.
- Join invite khóa class, tenant membership, enrollment và invite code trong transaction.
  Join mới/rejoin từ `invited` hoặc `left` tăng usage đúng một lần; replay của active
  enrollment và principal đã có quyền quản lý không tiêu thụ lượt. Lượt cuối atomically
  chuyển code sang `exhausted`; expired/revoked/exhausted/cross-scope đều unavailable.
- Archive chặn direct enrollment, create code, join mới và media request mới, nhưng giữ
  enrollment/code lịch sử; manager vẫn list/revoke code và active enrollee vẫn có thể
  leave. Restore không tự phát lại credential hay thay đổi enrollment/code state.
- Class có `timezone`, `version > 0`, `archived_at` và `archived_from_status`; archive
  draft/active rồi restore chính xác trạng thái trước, còn update chỉ cho draft -> active.
- `CreateClass` ghi lớp draft và sự kiện `class.created` trong cùng transaction.
- Get/List luôn lọc `tenant_id`; truy cập chéo tenant trả về not found.
- HTTP list/create/detail/mutation lấy `tenant_id`, actor và permission từ active
  session; request không có trường tenant hoặc owner.
- List class hỗ trợ status và opaque keyset cursor, dùng index theo
  `(tenant_id, status, created_at DESC, id DESC)` hoặc
  `(tenant_id, created_at DESC, id DESC)`.
- Tạo lớp yêu cầu `class.create` và CSRF; đọc lớp yêu cầu `class.view`.
- Update/archive/restore/transfer ownership dùng `expected_version` CAS. Các mutation
  khóa tenant, class và membership liên quan theo thứ tự ổn định, đọc lại membership
  authoritative rồi reauthorize shared policy trong transaction.
- Chỉ `org_admin` hoặc owner implicit có `class.archive` và
  `class.transfer_ownership`. Transfer target phải là active member cùng tenant đủ điều
  kiện `class.create` và actor có recent authentication trong 10 phút.
- Success event class create/update/archive/restore/ownership transfer, enrollment
  create/reactivate/suspend/remove/join/rejoin/leave/role-changed và invite-code
  create/revoke/expire/exhaust được ghi vào
  transactional outbox cùng business mutation; payload không chứa description, token
  thô, token hash, email, display name hoặc session secret. Event
  `class.enrollment.role_changed` chỉ giữ ID, role trước/sau, status và source allowlist.
- Tenant list được giới hạn bởi user membership active; detail/update/archive bắt buộc
  tenant path trùng active tenant context.
- Đọc tenant yêu cầu `tenant.view`; update/archive yêu cầu `tenant.manage` và CSRF.
- Update/archive dùng `expected_version` và SQL compare-and-swap rồi tăng version;
  stale request không ghi đè dữ liệu mới hơn.
- Create từ workspace hiện hữu, switch, update và archive khóa membership row; create,
  update và archive reauthorize qua shared policy trong transaction để concurrent
  revoke/demotion không giữ quyền từ snapshot cũ. Bootstrap khóa user rồi kiểm tra lại
  không có membership active trước khi insert để tuần tự hóa nhiều session onboarding.
- Create/switch/archive dùng `sessions.context_version` để CAS privilege context trước
  khi xoay session/CSRF. Archive xóa active context của các session còn trỏ tenant đó.
- Success event `tenant.created/updated/archived/switched` được ghi vào outbox trong
  cùng transaction; payload không chứa token, cookie hoặc session secret.
- Success mutation tenant/membership/class/enrollment/roster/invite-code P2-02 đến
  P2-06 đồng thời append audit trong cùng business transaction và outbox boundary;
  audit insert lỗi sẽ rollback mutation. Authenticated no-op/denied/failed attempt ghi
  bằng transaction riêng và dedupe theo server-generated request-instance/action.
- Invitation token chỉ lưu purpose-bound HMAC 32 byte unique; một tenant/email chỉ có
  một row `pending`, TTL tối đa 30 ngày và state/timestamp bị khóa bằng CHECK constraint.
- Composite FK buộc invited/accepted/revoked actor có membership cùng tenant. Create,
  revoke và accept ghi lifecycle event trong business transaction; payload allowlist
  không chứa raw token, token hash, email hoặc session identifier.
- Accept khóa tenant/session/identity-user/membership/invitation theo thứ tự ổn định,
  yêu cầu verified linked identity khớp email và tạo tối đa một membership/event.
- Feature/quota override khóa tenant bằng advisory transaction lock, dùng revision
  compare-and-swap và không thể vượt global safety ceiling. Capacity member/class được
  kiểm tra trong cùng transaction với mutation; invitation rate dùng fixed window.
- `rate_limit_windows` không lưu địa chỉ client thô. Bucket là SHA-256 có domain
  separation theo limiter version, purpose và prefix đã được edge xác thực; purpose
  vẫn là một phần khóa window nhưng không thay thế việc bind purpose vào digest.
  Storage lỗi làm anonymous flow fail closed.

## Runtime grants và retention cho migration 000012

Migration không hardcode tên role vì mỗi môi trường có thể dùng runtime role khác.
Ngay sau `pnpm db:migrate`, migration owner phải thay `tutorhub_runtime` trong ví dụ
dưới đây bằng runtime role thực tế rồi cấp đúng quyền tối thiểu:

```sql
GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;
GRANT SELECT, INSERT, UPDATE
  ON tutorhub.tenant_feature_control_revisions,
     tutorhub.tenant_quota_windows,
     tutorhub.rate_limit_windows
  TO tutorhub_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE
  ON tutorhub.tenant_feature_overrides,
     tutorhub.tenant_quota_overrides
  TO tutorhub_runtime;
```

Kết nối bằng runtime URL và xác nhận `has_table_privilege` cho đúng ma trận trên;
runtime role không được là superuser, owner bảng hoặc có `TRUNCATE`. Core API chỉ được
deploy sau khi smoke capability read, feature-control update và limiter read/write đạt.

Hai bảng window là dữ liệu vận hành có thời hạn, không phải lịch sử nghiệp vụ. Chạy
cleanup bằng migration/maintenance role, theo lô tối đa 10.000 row, ít nhất mỗi ngày;
không cấp `DELETE` hai bảng này cho Core API chỉ để dọn dữ liệu:

```sql
WITH expired AS (
  SELECT purpose, bucket_hash, window_started_at
  FROM tutorhub.rate_limit_windows
  WHERE window_ends_at < now() - interval '24 hours'
  ORDER BY window_ends_at
  LIMIT 10000
  FOR UPDATE SKIP LOCKED
)
DELETE FROM tutorhub.rate_limit_windows target
USING expired
WHERE target.purpose = expired.purpose
  AND target.bucket_hash = expired.bucket_hash
  AND target.window_started_at = expired.window_started_at;

WITH expired AS (
  SELECT tenant_id, quota_key, window_started_at
  FROM tutorhub.tenant_quota_windows
  WHERE window_ends_at < now() - interval '7 days'
  ORDER BY window_ends_at
  LIMIT 10000
  FOR UPDATE SKIP LOCKED
)
DELETE FROM tutorhub.tenant_quota_windows target
USING expired
WHERE target.tenant_id = expired.tenant_id
  AND target.quota_key = expired.quota_key
  AND target.window_started_at = expired.window_started_at;
```

Lặp mỗi statement cho tới khi `DELETE 0`, ghi metric row count/duration nhưng không log
bucket hash. Trước pilot phải chuyển hai statement thành maintenance job có owner và
alert; index expiry của migration `000012` hỗ trợ đường quét này.

## Ledger fixture V1 của migration 000013

Ba bảng `legacy_import_*` chỉ dành cho CLI chạy bằng migration role. Không cấp quyền
cho `tutorhub_runtime`, frontend, Pages Function hoặc API container. Ledger chỉ lưu
external key bounded, UUID V2, checksum SHA-256, outcome/reason code và checkpoint;
không lưu source JSON, email, token, password hoặc connection URL.

Dry-run dùng cùng transform/upsert path trong transaction rollback. Apply commit từng
record cùng mapping/checkpoint để lỗi không làm mất vị trí resume. Natural key đã tồn tại
nhưng chưa có mapping bị từ chối fail-closed; tool không tự gộp identity/tenant/class.
Chi tiết contract và reset nằm tại `docs/P2_11_V1_FIXTURE_IMPORT.md`.

### Remediation default ACL Neon cho P2-12

Nếu migration owner đã có default table ACL cấp quyền cho runtime role, `CREATE TABLE`
sẽ materialize grant đó trên cả bảng nội bộ mới. `REVOKE ... FROM PUBLIC` trong migration
không thu hồi grant trực tiếp này. Không sửa migration `000013` đã chạy và không hardcode
tên role môi trường vào migration lịch sử. Thay `neondb_owner` và `tutorhub_runtime` dưới
đây bằng hai role thực tế và chạy bằng migration owner trên branch dùng một lần trước:

```sql
ALTER DEFAULT PRIVILEGES FOR ROLE neondb_owner
  REVOKE ALL PRIVILEGES ON TABLES FROM tutorhub_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE neondb_owner IN SCHEMA tutorhub
  REVOKE ALL PRIVILEGES ON TABLES FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
  tutorhub.legacy_import_runs,
  tutorhub.legacy_import_mappings,
  tutorhub.legacy_import_run_items
FROM tutorhub_runtime;

REVOKE CREATE ON SCHEMA tutorhub FROM tutorhub_runtime;
GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;
```

Hai câu `ALTER DEFAULT PRIVILEGES` xử lý riêng default ACL global và ACL scoped trong
schema; câu `REVOKE` tiếp theo dọn các grant đã materialize. Ba bảng này không dùng
sequence/identity nên không cần cấp hoặc thu hồi sequence privilege. Sau khi remediation
và toàn bộ assertion đạt trên branch dùng một lần, áp dụng cùng remediation lên staging
đích bằng migration owner rồi chạy lại toàn bộ assertion trước khi forward migration.
Không suy diễn kết quả disposable thành kết quả staging. Chạy assertion bằng migration
connection mà không ghi URL/password vào output:

```sql
WITH ledger(table_name) AS (
  VALUES
    ('legacy_import_runs'),
    ('legacy_import_mappings'),
    ('legacy_import_run_items')
), privileges(privilege_name) AS (
  VALUES
    ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'),
    ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')
)
SELECT count(*) AS effective_privilege_count
FROM ledger
CROSS JOIN privileges
WHERE has_table_privilege(
  'tutorhub_runtime',
  format('tutorhub.%I', ledger.table_name),
  privileges.privilege_name
);

SELECT count(*) AS direct_acl_count
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, ARRAY[]::aclitem[])) acl
WHERE n.nspname = 'tutorhub'
  AND c.relname IN (
    'legacy_import_runs',
    'legacy_import_mappings',
    'legacy_import_run_items'
  )
  AND acl.grantee = 'tutorhub_runtime'::regrole;

SELECT count(*) AS default_table_acl_count
FROM pg_default_acl d
CROSS JOIN LATERAL aclexplode(d.defaclacl) acl
WHERE d.defaclrole = 'neondb_owner'::regrole
  AND d.defaclobjtype = 'r'
  AND (
    d.defaclnamespace = 0
    OR d.defaclnamespace = 'tutorhub'::regnamespace
  )
  AND acl.grantee = 'tutorhub_runtime'::regrole;

SELECT
  has_schema_privilege('tutorhub_runtime', 'tutorhub', 'USAGE') AS schema_usage,
  has_schema_privilege('tutorhub_runtime', 'tutorhub', 'CREATE') AS schema_create;
```

Kết quả bắt buộc lần lượt là `0`, `0`, `0`, rồi `schema_usage=true` và
`schema_create=false`. Cuối cùng tạo bảng probe trong transaction; kết quả phải là `0`
và `ROLLBACK` phải xóa bảng:

```sql
BEGIN;
CREATE TABLE tutorhub.p2_12_acl_probe (id uuid PRIMARY KEY DEFAULT gen_random_uuid());
SELECT count(*) AS probe_effective_privilege_count
FROM unnest(ARRAY[
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER'
]) AS permissions(privilege_name)
WHERE has_table_privilege(
  'tutorhub_runtime', 'tutorhub.p2_12_acl_probe', permissions.privilege_name
);
ROLLBACK;
SELECT to_regclass('tutorhub.p2_12_acl_probe') IS NULL AS probe_removed;
```

Đây là security remediation không được đảo ngược: rollback schema hoặc ứng dụng không
được re-grant runtime vào ledger hay khôi phục default ACL rộng.

## Runtime grant cho migration 000014

Default ACL đã được thu hồi có chủ ý nên migration owner tạo bảng
`tutorhub.class_sessions` nhưng runtime role không tự nhận quyền. Sau khi migration
`000014` chạy bằng direct migration URL, migration owner phải cấp đúng quyền mà
repository P3-01 dùng:

```sql
REVOKE ALL PRIVILEGES
ON TABLE tutorhub.class_sessions
FROM tutorhub_runtime;

GRANT SELECT, INSERT, UPDATE
ON TABLE tutorhub.class_sessions
TO tutorhub_runtime;
```

Không cấp `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER`, ownership hoặc schema
`CREATE`. Xác minh effective privilege bằng migration connection:

```sql
SELECT
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.class_sessions', 'SELECT'
  ) AS runtime_select,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.class_sessions', 'INSERT'
  ) AS runtime_insert,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.class_sessions', 'UPDATE'
  ) AS runtime_update,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.class_sessions', 'DELETE'
  ) AS runtime_delete,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.class_sessions', 'TRUNCATE'
  ) AS runtime_truncate;
```

Kết quả bắt buộc là `true, true, true, false, false`. Sau đó dùng runtime connection
chạy focused integration/smoke; không in connection string ra terminal hoặc artifact.

## Lease schema và role grants cho migration 000015

Migration `000015` mở rộng bảng `outbox_events` hiện hữu, không tạo queue hoặc bảng
dead-letter thứ hai. `lease_token` là fencing generation `bigint` theo từng event, bắt
đầu từ `0` và phải tăng atomically mỗi lần claim. Một lease active giữ đồng thời
`lease_owner`, `lease_token > 0`, `leased_at` và `leased_until`; ack/retry/dead-letter
phải compare-and-set theo `(id, lease_owner, lease_token)`. `attempts` tiếp tục chỉ đếm
lần xử lý thất bại, còn `available_at` giữ lịch retry/backoff. Dead-letter là terminal
state bằng `dead_lettered_at`, vẫn giữ row gốc để inspect/replay có kiểm soát và không
bị claim hoặc cleanup như published event.

`last_error` chỉ được lưu error **code** đã redact, lowercase và tối đa 100 ký tự; không
lưu error message, stack trace, provider response, token, signed URL hoặc nội dung riêng
tư. Khi migrate, giá trị legacy không đạt format này được thay bằng một code cố định
`legacy_error_redacted` trước khi constraint được bật; không làm migration fail hoặc giữ
lại arbitrary text có thể chứa dữ liệu nhạy cảm. Version contract tiếp tục nằm trong exact `event_type`, ví dụ
`class_session.scheduled.v1`; không có cột `event_version` thứ hai. `tenant_id` tiếp tục
nullable cho event global/system và handler phải tự validate context của type đã đăng ký.
Worker chỉ claim exact event type allowlist; event Phase 1/2 unversioned hoặc historical
không được tự mark published/dead-letter.

Foreign key `tenant_id` vẫn giữ hành vi `ON DELETE CASCADE` từ migration `000003` để
không đổi lifecycle ngoài phạm vi P3-03; API hiện chỉ archive tenant. Trước khi bổ sung
physical tenant delete phải có retention/erasure decision và drain/reconcile outbox,
vì hard delete hiện có thể xóa cả pending/dead-letter thuộc tenant đó.

Migration dùng `ALTER TABLE` và build B-tree index trong transaction theo runner hiện
tại (`statement_timeout=2m`), nên sẽ chặn writer trong cửa sổ DDL. Trước staging phải
đo row count/index-build time trên branch disposable cùng cỡ dữ liệu và chọn release
window ngắn. Nếu table vượt budget, tạo migration online/concurrent mới được review;
không sửa file `000015` sau khi đã chạy và không tăng timeout mù.

Tên role phụ thuộc môi trường nên không hardcode vào migration. Sau khi migrate bằng
migration owner, thay hai placeholder dưới đây bằng API runtime role và worker role thật,
rồi thu hồi grant cũ trước khi cấp lại:

```sql
REVOKE ALL PRIVILEGES
ON TABLE tutorhub.outbox_events
FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES
ON TABLE tutorhub.outbox_events
FROM tutorhub_worker;

REVOKE ALL PRIVILEGES
ON ALL TABLES IN SCHEMA tutorhub
FROM tutorhub_worker;

REVOKE ALL PRIVILEGES
ON ALL SEQUENCES IN SCHEMA tutorhub
FROM tutorhub_worker;

REVOKE CREATE ON SCHEMA tutorhub, public FROM tutorhub_worker;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime, tutorhub_worker;

GRANT INSERT (
  tenant_id,
  aggregate_type,
  aggregate_id,
  event_type,
  payload,
  occurred_at,
  available_at
)
ON TABLE tutorhub.outbox_events
TO tutorhub_runtime;

GRANT SELECT
ON TABLE tutorhub.outbox_events
TO tutorhub_worker;

GRANT UPDATE (
  attempts,
  available_at,
  published_at,
  last_error,
  lease_owner,
  lease_token,
  leased_at,
  leased_until,
  dead_lettered_at
)
ON TABLE tutorhub.outbox_events
TO tutorhub_worker;
```

API role không có `SELECT/UPDATE/DELETE/TRUNCATE`; worker role không có
`INSERT/DELETE/TRUNCATE/REFERENCES/TRIGGER`, không được update event identity, tenant,
aggregate, type, payload hoặc occurred time và không có table/column privilege trên bảng
nghiệp vụ khác. Worker phải là direct LOGIN (`session_user = current_user`), không
superuser/create-role/create-db/replication/bypass-RLS, không có membership và không sở
hữu database/schema/table. Cả hai role không có schema `CREATE` và không dùng chung URL.
Vì `outbox_events` là bảng
có sẵn, `REVOKE ... FROM PUBLIC` trong migration không thu hồi direct/inherited grant
của role môi trường; bước provisioning trên là bắt buộc. Nếu role từng có column-level
ACL riêng, phải liệt kê `information_schema.column_privileges` và revoke ACL legacy đó
trước khi grant allowlist mới; negative effective-privilege probe bên dưới là authority.

Xác minh grant sơ bộ bằng migration connection, không in credential:

```sql
SELECT
  has_schema_privilege('tutorhub_runtime', 'tutorhub', 'USAGE') AS api_schema_usage,
  has_schema_privilege('tutorhub_runtime', 'tutorhub', 'CREATE') AS api_schema_create,
  has_any_column_privilege(
    'tutorhub_runtime', 'tutorhub.outbox_events', 'INSERT'
  ) AS api_insert,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.outbox_events', 'SELECT'
  ) AS api_select,
  has_any_column_privilege(
    'tutorhub_runtime', 'tutorhub.outbox_events', 'UPDATE'
  ) AS api_update,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.outbox_events', 'DELETE'
  ) AS api_delete,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.outbox_events', 'TRUNCATE'
  ) AS api_truncate,
  has_schema_privilege('tutorhub_worker', 'tutorhub', 'USAGE') AS worker_schema_usage,
  has_schema_privilege('tutorhub_worker', 'tutorhub', 'CREATE') AS worker_schema_create,
  has_table_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'SELECT'
  ) AS worker_select,
  has_any_column_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'UPDATE'
  ) AS worker_update,
  has_table_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'INSERT'
  ) AS worker_insert,
  has_table_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'DELETE'
  ) AS worker_delete,
  has_table_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'TRUNCATE'
  ) AS worker_truncate,
  has_any_column_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'REFERENCES'
  ) AS worker_references,
  has_table_privilege(
    'tutorhub_worker', 'tutorhub.outbox_events', 'TRIGGER'
  ) AS worker_trigger;
```

Các cột positive phải `true`; mọi cột mang nghĩa CREATE/quyền dư phải `false`. Sau đó
khởi động worker bằng chính `DATABASE_WORKER_URL`: startup probe mới là authority cho
direct-login, membership, ownership, exact column allowlist và quyền trên bảng khác.
Negative test phải cấp tạm SELECT một bảng nghiệp vụ hoặc CREATE schema rồi chứng minh
probe từ chối, sau đó revoke. API không được insert `published_at/lease_*/dead_lettered_at`;
worker không được update `tenant_id/event_type/payload`.

Local compose không tự tạo credential worker. Sau migration, migration owner có thể tạo
role development riêng (password dưới đây chỉ là placeholder local đã có trong
`.env.example`, tuyệt đối không dùng ở hosted environment), rồi chạy grant block trên:

```sql
CREATE ROLE tutorhub_worker
  LOGIN PASSWORD 'tutorhub_worker_local'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
```

Nếu PostgreSQL local cũ còn cho `PUBLIC` tạo object trong schema `public`, harden local
bằng `REVOKE CREATE ON SCHEMA public FROM PUBLIC;`; chỉ revoke trực tiếp từ worker không
thể triệt quyền thừa kế qua `PUBLIC`.

Rollback ứng dụng phải dừng worker và giữ schema `15`; API phiên bản cũ vẫn tương thích
vì các writer hiện hữu chỉ insert cột cũ và cột lease có default/null an toàn. Database
down `15 -> 14` chỉ dành cho disposable/test hoặc tình huống đã reconcile: migration lấy
`ACCESS EXCLUSIVE`, fail nếu còn bất kỳ retained lease state hoặc dead-letter, và không tự biến
dead-letter thành published. Nếu đã có dead-letter cần inspect/replay/purge theo runbook
được phê duyệt; không bypass guard hay blanket mark published để ép rollback. Role grant
đã thu hẹp không được re-grant rộng khi rollback. Redaction
`last_error -> legacy_error_redacted` là security cleanup có chủ ý và không được khôi
phục arbitrary legacy text trong down migration.

## Notification projection và role grants cho migration 000016

Migration `000016` tạo hai bảng tenant/user-scoped:

- `tutorhub.notifications`: projection idempotent theo
  `(source_outbox_event_id, recipient_user_id, effect_key)`, chỉ cho phép thay đổi
  `read_at` sau khi row được tạo;
- `tutorhub.notification_preferences`: preference self-scoped, full replacement bằng
  optimistic `version` và quiet-hours theo IANA timezone.

Migration không hardcode role môi trường và chỉ `REVOKE ... FROM PUBLIC`. Sau khi chạy
bằng migration owner, thu hồi grant cũ rồi cấp đúng bề mặt Core API cần dùng:

```sql
REVOKE ALL PRIVILEGES
ON TABLE tutorhub.notifications, tutorhub.notification_preferences
FROM tutorhub_runtime;

GRANT SELECT
ON TABLE tutorhub.notifications
TO tutorhub_runtime;

GRANT UPDATE (read_at)
ON TABLE tutorhub.notifications
TO tutorhub_runtime;

GRANT SELECT
ON TABLE tutorhub.notification_preferences
TO tutorhub_runtime;

GRANT INSERT (
  tenant_id,
  user_id,
  in_app_enabled,
  email_enabled,
  reminder_offset_minutes,
  quiet_hours_enabled,
  quiet_hours_start,
  quiet_hours_end,
  quiet_hours_timezone,
  version,
  created_at,
  updated_at
)
ON TABLE tutorhub.notification_preferences
TO tutorhub_runtime;

GRANT UPDATE (
  in_app_enabled,
  email_enabled,
  reminder_offset_minutes,
  quiet_hours_enabled,
  quiet_hours_start,
  quiet_hours_end,
  quiet_hours_timezone,
  version,
  updated_at
)
ON TABLE tutorhub.notification_preferences
TO tutorhub_runtime;
```

API role không được `INSERT` notification, cập nhật cột notification ngoài `read_at`,
hoặc có `DELETE/TRUNCATE/REFERENCES/TRIGGER` trên hai bảng. Repository vẫn bắt buộc
membership authoritative và predicate tenant/user; grant không thay authorization.

Worker có hai exact ACL state đi cùng
`OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY`:

1. **Gate tắt (mặc định):** worker không có bất kỳ table/column privilege nào trên
   `notifications` hoặc `notification_preferences`.
2. **Gate bật cho controlled canary:** ngoài exact outbox ACL của `000015`, worker chỉ
   có column-level `INSERT` vào tám cột projection dưới đây và không có quyền trên
   `notification_preferences`.

```sql
REVOKE ALL PRIVILEGES
ON TABLE tutorhub.notifications, tutorhub.notification_preferences
FROM tutorhub_worker;

GRANT INSERT (
  tenant_id,
  recipient_user_id,
  source_outbox_event_id,
  effect_key,
  kind,
  template_key,
  context,
  occurred_at
)
ON TABLE tutorhub.notifications
TO tutorhub_worker;
```

Không cấp table-level `INSERT`: startup probe yêu cầu chính xác tám column grant trên
và từ chối mọi `SELECT/UPDATE/DELETE/TRUNCATE/REFERENCES/TRIGGER`, ownership, membership,
DDL hoặc quyền bảng nghiệp vụ khác. Gate và grant phải chuyển cùng nhau khi worker đã
dừng: grant nhưng vẫn chạy gate tắt là quyền dư; bật gate trước khi grant là thiếu quyền;
cả hai trường hợp đều phải fail startup. Rollback canary làm ngược lại: dừng worker,
revoke notification grant, đặt gate false rồi mới khởi động lại.

`FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS` là product-visibility guardrail độc lập,
không thay database ACL và phải giữ `false` trên staging/production cho tới khi P3-03B
cùng P3-04 acceptance đạt. Canary `system.worker_canary` luôn bị API feed loại bỏ.

Down `16 -> 15` xóa cả hai projection table và override key
`in_app_notifications`; chỉ dùng trên disposable/test hoặc sau khi worker đã dừng và
notification state được đánh giá. Application rollback giữ schema 16 là đường ưu tiên;
không chạy down chỉ để tắt feature.

## Calendar read projection và runtime grant cho migration 000017

Migration `000017` không tạo một domain Event thứ hai. Nó chỉ thêm index bounded-read
cho `class_sessions` và bảng preference riêng theo `(tenant_id, user_id)`. Calendar API
vẫn kiểm tra active tenant/member ở server, chỉ đọc những ClassSession mà actor có quyền
theo source policy, và dùng overlap half-open `starts_at < to AND ends_at > from`.

Default ACL đã bị thu hồi có chủ ý nên sau khi chạy migration bằng direct migration
role, migration owner phải cấp đúng quyền cần cho Core API runtime:

```sql
REVOKE ALL PRIVILEGES
ON TABLE tutorhub.calendar_display_preferences
FROM tutorhub_runtime;

GRANT SELECT, INSERT, UPDATE
ON TABLE tutorhub.calendar_display_preferences
TO tutorhub_runtime;
```

Không cấp `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER`, ownership hoặc schema `CREATE`.
Runtime đã có `SELECT` trên các bảng nguồn `tenants`, `memberships`, `users`, `classes`,
`class_enrollments` và `class_sessions` từ các migration trước; không mở rộng thêm quyền
ghi lên source chỉ để phục vụ Calendar. Xác minh exact privilege sau grant:

```sql
SELECT
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.calendar_display_preferences', 'SELECT'
  ) AS runtime_select,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.calendar_display_preferences', 'INSERT'
  ) AS runtime_insert,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.calendar_display_preferences', 'UPDATE'
  ) AS runtime_update,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.calendar_display_preferences', 'DELETE'
  ) AS runtime_delete,
  has_table_privilege(
    'tutorhub_runtime', 'tutorhub.calendar_display_preferences', 'TRUNCATE'
  ) AS runtime_truncate;
```

Kết quả bắt buộc là `true, true, true, false, false`. Smoke tiếp theo phải chứng minh
read projection bị giới hạn tenant/source permission và preference update CAS trả `409`
khi version cũ. Application rollback giữ schema 17 là đường ưu tiên. Down `17 -> 16`
xóa preference Calendar và index đọc; chỉ chạy trên disposable/test hoặc sau khi đánh
giá mất dữ liệu preference.

## Recurrence series, exception và runtime grants cho migration 000018/000019

Migration `000018` tạo `class_session_series` và `class_session_exceptions`, đồng thời
gắn `series_id`, `occurrence_key` và civil-time identity vào `class_sessions`.
Migration `000019` thêm `ical_uid`/`sequence` contract và
`class_session_mutation_receipts`. Cả ba bảng mới đều `REVOKE ALL FROM PUBLIC`; source
không hardcode tên runtime role.

Sau khi chạy migration bằng direct migration role, thay `tutorhub_runtime` bằng role
runtime thực tế rồi cấp đúng ma trận dưới đây:

```sql
REVOKE ALL PRIVILEGES
ON TABLE
  tutorhub.class_session_series,
  tutorhub.class_session_exceptions,
  tutorhub.class_session_mutation_receipts
FROM tutorhub_runtime;

GRANT SELECT, INSERT, UPDATE
ON TABLE
  tutorhub.class_session_series,
  tutorhub.class_session_exceptions
TO tutorhub_runtime;

GRANT SELECT, INSERT
ON TABLE tutorhub.class_session_mutation_receipts
TO tutorhub_runtime;
```

Không cấp `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER`, ownership hoặc schema
`CREATE`. Cancel occurrence/series là tombstone hoặc `UPDATE`; receipt là append-only
để replay idempotent. Cleanup exception/receipt và hard delete series không thuộc quyền
Core API runtime.

Runtime không được đọc receipt bằng `SELECT ... FOR UPDATE` vì PostgreSQL yêu cầu quyền
`UPDATE` cho khóa hàng này. Mutation cùng series được tuần tự hóa bằng khóa trên series
row; primary key của receipt tiếp tục chặn idempotency collision.

Xác minh effective privilege bằng migration connection; kết quả bắt buộc theo thứ tự
là `true,true,true,false,false` cho series và exceptions, và
`true,true,false,false,false` cho receipts:

```sql
SELECT
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_series', 'SELECT') AS series_select,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_series', 'INSERT') AS series_insert,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_series', 'UPDATE') AS series_update,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_series', 'DELETE') AS series_delete,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_series', 'TRUNCATE') AS series_truncate,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_exceptions', 'SELECT') AS exception_select,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_exceptions', 'INSERT') AS exception_insert,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_exceptions', 'UPDATE') AS exception_update,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_exceptions', 'DELETE') AS exception_delete,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_exceptions', 'TRUNCATE') AS exception_truncate,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_mutation_receipts', 'SELECT') AS receipt_select,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_mutation_receipts', 'INSERT') AS receipt_insert,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_mutation_receipts', 'UPDATE') AS receipt_update,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_mutation_receipts', 'DELETE') AS receipt_delete,
  has_table_privilege('tutorhub_runtime', 'tutorhub.class_session_mutation_receipts', 'TRUNCATE') AS receipt_truncate;
```

Sau grant phải chạy focused recurring integration/smoke bằng runtime URL: create series,
read overlay, edit one/following/all, cancel tombstone, stale `409`, changed-payload
idempotency conflict, cross-tenant concealment và one-time/recurring half-open conflict.
Không in connection string, token hoặc dữ liệu fixture ra terminal/artifact. Application
rollback giữ schema 19 là đường ưu tiên; down `19 -> 18` chỉ chạy trên disposable/test
và sau khi đã đánh giá mất receipt/UID.

## Working schedule, participation và delivery boundary 000020/000021

Migration `000020` tạo working schedule tenant/user-scoped, weekly/special-hour
interval và holiday/out-of-office exception; đồng thời bổ sung audience authority cho
session/series, encrypted external recipient, attendee/RSVP state, immutable invitation
revision/recipient snapshot, hashed RSVP capability và append-only participation receipt.
Các trigger/constraint giữ cap interval/exception, source scope, RSVP state, token
expiry/revoke và snapshot immutability.

Migration `000021` làm rõ delivery boundary: invitation revision là encrypted immutable
business/audience snapshot, không phải delivery ledger hoặc rendered MIME. Revision-level
`method` phải `NULL` cho snapshot mới; P3-05A mới suy `REQUEST`/`CANCEL` theo recipient
từ audience diff và source lifecycle. Migration cũng cho phép protected display name
một ký tự theo kích thước envelope AES-GCM thực tế.

P3-02C đã chuyển sang `DONE` ngày 2026-07-30 sau khi staging acceptance đạt. Neon
staging ở `21 false`; migration `000020/000021`, exact Core API runtime ACL, smoke
PostgreSQL và các probe trong
[`P3_02C_STAGING_ACCEPTANCE.md`](P3_02C_STAGING_ACCEPTANCE.md) đều đã có bằng chứng.
Các lần thay đổi database sau này vẫn phải dùng direct migration role, cấp đúng
least-privilege matrix và không in URL/credential, raw capability, email hoặc
decrypted snapshot ra terminal/artifact.

Application rollback giữ schema 21 là đường ưu tiên sau khi forward thành công. Database
down `21 -> 20 -> 19` chỉ chạy trên disposable/test hoặc sau khi dừng runtime liên quan,
đánh giá dữ liệu participation/snapshot và có backup/restore plan.

## Availability Poll, Study Meeting và hard-retention purge 000022/000023

Migration `000022` tạo các bảng tenant-scoped sau:

- `availability_polls`;
- `availability_poll_slots`;
- `availability_poll_participants`;
- `availability_poll_capabilities`;
- `availability_poll_responses`;
- `availability_poll_answers`;
- `availability_poll_mutation_receipts`;
- `study_meetings`.

Migration đồng thời mở rộng feature/quota constraint cho `availability_polls`, poll
active/range/slot/participant/create/capability-create và Study Meeting active/create-rate.
Mọi tenant/class/participant/capability/response relation dùng composite integrity; raw
capability token không được lưu. `availability_poll_mutation_receipts` là append-only.

Migration `000023` không đổi schema nghiệp vụ. Đây là forward-only security correction:
purge function chuyển sang `SECURITY DEFINER`, dùng `search_path = pg_catalog, pg_temp`,
schema-qualify mọi application relation, không dynamic SQL và revoke `PUBLIC EXECUTE`.
Down `000023` chỉ gỡ purge function để fail closed, không phục hồi invoker contract đã lỗi
và không xóa dữ liệu `000022`. Lượt acceptance hiện tại không chạy rollback này.

`retention_until` là hard-retention boundary, không phải deadline worker:

- poll `draft/open/closed` neo boundary tối đa 180 ngày sau `deadline_at`;
- poll `finalized/cancelled` neo boundary tối đa 180 ngày sau terminal transition;
- maintenance purge có thể xóa poll ở mọi lifecycle khi boundary đã tới. Nó không đổi
  status, không auto-close, không ghi outbox/delivery và không thuộc P3-02D-B;
- index `(retention_until, tenant_id, id)` bao phủ mọi lifecycle để batch purge không
  suy giảm thành full-table scan khi số poll bỏ quên tăng;
- trước khi xóa poll, function đặt `study_meetings.source_poll_id = NULL`; các poll child
  còn lại phải bị xóa qua FK cascade.

### Exact Core API runtime grants

Migration revoke `PUBLIC` nhưng không tự cấp role deployment. Chạy bằng migration owner,
thay `tutorhub_runtime` nếu staging dùng tên khác:

```sql
BEGIN;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;

REVOKE ALL PRIVILEGES ON FUNCTION
    tutorhub.purge_expired_availability_polls(integer)
FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.availability_polls,
    tutorhub.availability_poll_slots,
    tutorhub.availability_poll_participants,
    tutorhub.availability_poll_capabilities,
    tutorhub.availability_poll_responses,
    tutorhub.availability_poll_answers,
    tutorhub.availability_poll_mutation_receipts,
    tutorhub.study_meetings
FROM tutorhub_runtime;

GRANT SELECT, INSERT, UPDATE ON TABLE
    tutorhub.availability_polls,
    tutorhub.availability_poll_slots,
    tutorhub.availability_poll_participants,
    tutorhub.availability_poll_capabilities,
    tutorhub.availability_poll_responses,
    tutorhub.availability_poll_answers,
    tutorhub.study_meetings
TO tutorhub_runtime;

GRANT SELECT, INSERT ON TABLE
    tutorhub.availability_poll_mutation_receipts
TO tutorhub_runtime;

COMMIT;
```

Core API runtime tuyệt đối không nhận `DELETE` trên bất kỳ bảng migration `000022` nào,
không nhận `EXECUTE` purge function và không nhận `UPDATE/DELETE` mutation receipt.
Deployment probe phải tính cả quyền kế thừa qua role membership, không chỉ đọc row grant.

### Exact maintenance purge grants

Sau migration `000023`, function `tutorhub.purge_expired_availability_polls(integer)` là
hardened `SECURITY DEFINER`, dùng `FOR UPDATE SKIP LOCKED` và trả số poll đã xóa. Chỉ cấp
entry point cho dedicated maintenance role; ví dụ dưới dùng
`tutorhub_poll_maintenance` làm placeholder cho exact role đã được provision:

```sql
BEGIN;

REVOKE ALL PRIVILEGES ON SCHEMA tutorhub FROM tutorhub_poll_maintenance;
GRANT USAGE ON SCHEMA tutorhub TO tutorhub_poll_maintenance;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.availability_polls,
    tutorhub.availability_poll_slots,
    tutorhub.availability_poll_participants,
    tutorhub.availability_poll_capabilities,
    tutorhub.availability_poll_responses,
    tutorhub.availability_poll_answers,
    tutorhub.availability_poll_mutation_receipts,
    tutorhub.study_meetings
FROM tutorhub_poll_maintenance;

-- Revoke the column grants used by the old 000022 invoker contract.
REVOKE SELECT (tenant_id, id, retention_until)
ON TABLE tutorhub.availability_polls
FROM tutorhub_poll_maintenance;

REVOKE SELECT (tenant_id, source_poll_id), UPDATE (source_poll_id)
ON TABLE tutorhub.study_meetings
FROM tutorhub_poll_maintenance;

REVOKE ALL PRIVILEGES ON FUNCTION
    tutorhub.purge_expired_availability_polls(integer)
FROM tutorhub_poll_maintenance;

GRANT EXECUTE ON FUNCTION
    tutorhub.purge_expired_availability_polls(integer)
TO tutorhub_poll_maintenance;

COMMIT;
```

Maintenance không cần và không được có table/column privilege trực tiếp trên bất kỳ bảng
`000022` nào; function owner thực hiện lock/detach/delete theo body đã review. Exact-login
acceptance phải chứng minh function kích hoạt FK cascade, còn direct `SELECT`, `UPDATE` và
`DELETE` của maintenance login đều fail. Nếu deployment cần mở thêm quyền để làm smoke
xanh, dừng rollout và review function/ownership thay vì cấp wildcard.

Batch hợp lệ là `1..1000`, mặc định `100`. `NULL`, `0`, số âm và `>1000` phải fail với
SQLSTATE `22023`; không tự chuyển `NULL` thành default. Chạy nhiều batch bounded cho tới
khi trả `0`, có rate/transaction monitoring phù hợp, không gọi từ request path Core API:

```sql
SELECT tutorhub.purge_expired_availability_polls(100);
```

### ACL và cascade acceptance tối thiểu

Chạy các probe sau bằng migration owner sau khi cấp role. Mọi cột `*_must_be_false` phải
`false`; hai cột `*_must_be_true` phải `true`:

```sql
SELECT
    has_function_privilege(
        'tutorhub_runtime',
        'tutorhub.purge_expired_availability_polls(integer)',
        'EXECUTE'
    ) AS runtime_execute_must_be_false,
    has_table_privilege(
        'tutorhub_runtime',
        'tutorhub.availability_polls',
        'DELETE'
    ) AS runtime_poll_delete_must_be_false,
    has_schema_privilege(
        'tutorhub_poll_maintenance',
        'tutorhub',
        'USAGE'
    ) AS maintenance_schema_usage_must_be_true,
    has_function_privilege(
        'tutorhub_poll_maintenance',
        'tutorhub.purge_expired_availability_polls(integer)',
        'EXECUTE'
    ) AS maintenance_execute_must_be_true,
    has_schema_privilege(
        'tutorhub_poll_maintenance',
        'tutorhub',
        'CREATE'
    ) AS maintenance_schema_create_must_be_false,
    has_table_privilege(
        'tutorhub_poll_maintenance',
        'tutorhub.availability_polls',
        'DELETE'
    ) AS maintenance_poll_delete_must_be_false,
    has_any_column_privilege(
        'tutorhub_poll_maintenance',
        'tutorhub.availability_polls',
        'SELECT'
    ) AS maintenance_poll_select_must_be_false,
    has_any_column_privilege(
        'tutorhub_poll_maintenance',
        'tutorhub.study_meetings',
        'UPDATE'
    ) AS maintenance_source_update_must_be_false;
```

Probe thêm `pg_proc.prosecdef = true`, exact
`proconfig = ARRAY['search_path=pg_catalog, pg_temp']`, function owner khác runtime và
maintenance login, owner không phải superuser, cùng `PUBLIC EXECUTE = false`. Maintenance
login phải có `CREATE` trên schema `tutorhub` bằng `false`.

Ngoài probe metadata, đăng nhập **đúng maintenance role** trên disposable database và
chứng minh:

1. batch `1` xóa đúng một poll tới hard-retention boundary;
2. Study Meeting outcome còn tồn tại nhưng `source_poll_id` thành `NULL`;
3. slot/participant/capability/response/answer/receipt của poll bị cascade sạch;
4. poll chưa tới boundary không bị xóa;
5. `NULL`, `0`, `1001` đều bị từ chối;
6. role không thể `SELECT`, `UPDATE` hoặc `DELETE` trực tiếp trên poll/Study Meeting/child;
   Core API runtime không thể gọi function hoặc xóa poll.

Không chạy purge acceptance trên shared production hoặc dữ liệu người dùng thật.

## Conversation core 000024

Migration `000024` tạo `conversations` và `conversation_members`, đồng thời thêm feature
key `conversations`. Mỗi direct conversation giữ đúng canonical pair
`direct_user_low_id < direct_user_high_id`; partial unique index chặn duplicate khi hai
phía create đồng thời. Mỗi class có tối đa một conversation qua partial unique index.
Class không snapshot roster vào `conversation_members`: quyền đọc/ghi luôn được tính lại
từ owner và `class_enrollments` authoritative. Archive giữ conversation để đọc nhưng
không cấp `chat.send`; feature emergency-off chỉ chặn create và không xóa/ẩn history.

Migration revoke `PUBLIC` và không hardcode tên role môi trường. Sau khi forward bằng
migration owner, provision exact Core API runtime role như sau, thay
`tutorhub_runtime` bằng role thật của môi trường:

```sql
BEGIN;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members
FROM tutorhub_runtime;

GRANT SELECT, INSERT ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members
TO tutorhub_runtime;

COMMIT;
```

Runtime role không được nhận `UPDATE`, `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER` hoặc
ownership trên hai bảng này. API chỉ ghi hai `conversation_members` do server resolve;
request direct chỉ nhận exact target-member email và không nhận participant ID/array.
`conversation_members` không phải nguồn authorization: direct access vẫn kiểm tra canonical
pair trên `conversations`, còn class access luôn lấy từ owner/enrollment authoritative.
P3-07 không được tin hoặc mở generic mutation cho bảng này nếu chưa giữ đúng invariant
hai direct member/không snapshot class roster và bổ sung DB enforcement khi xuất hiện write
path mới.
Audit `conversation.created.v1` chỉ append khi insert conversation mới thực sự thắng;
replay canonical không tạo audit hoặc side effect thứ hai. P3-06 không tạo message,
outbox delivery, notification hoặc realtime transport.

## Chạy migration

Tạo `.env.local` từ `.env.example` và điền migration, API, worker cùng poll-maintenance URL
đúng role. File này đã được Git ignore; không in URL ra terminal, issue, log hoặc tài liệu.

Nạp biến môi trường trong PowerShell mà không in giá trị:

```powershell
Get-Content .env.local | ForEach-Object {
  $line = $_.Trim()
  if ($line -and -not $line.StartsWith('#')) {
    $parts = $line -split '=', 2
    if ($parts.Count -eq 2) {
      Set-Item -Path "Env:$($parts[0].Trim())" -Value $parts[1].Trim()
    }
  }
}
```

Sau đó chạy:

```powershell
pnpm db:version
pnpm db:migrate
pnpm db:version
```

Sau khi áp dụng toàn bộ migration trong source hiện tại, kết quả mong đợi là
`24 false`. Disposable P3-06 đã được xác nhận thực tế ở `24 false` và migrate lặp vẫn giữ
nguyên; shared Neon staging mới chỉ được xác nhận `23 false`, không được suy diễn kết quả
disposable thành kết quả shared staging.
Không chạy database rollback thêm cho P3-02D-A. Emergency down `000023` chỉ disable purge
function; down `000022` mới phá hủy poll/Study Meeting data. Recovery ưu tiên application
rollback hoặc migration forward mới.

## Kiểm thử

Unit test và static verification:

```powershell
pnpm verify
```

Integration test bằng PostgreSQL thật:

```powershell
pnpm test:integration
```

P3-03/P3-04/P3-02A phải kiểm tra riêng migrate `14 -> 15 -> 16 -> 17`, rollback
`17 -> 16`, rollback `16 -> 15`, rollback tiếp `15 -> 14`, rồi migrate lại tới `17`;
nullable `tenant_id`, writer insert shape cũ và feature-control constraint qua mỗi
version phải còn tương thích. PostgreSQL worker
suite phải chứng minh nhiều replica claim bằng
`FOR UPDATE SKIP LOCKED`, fencing token chặn stale owner, crash/lease-expiry reclaim,
retry/backoff, duplicate idempotency, permanent/max-attempt dead-letter và graceful
shutdown. Không dùng database down để test dead-letter bằng cách biến row terminal thành
pending; rollback guard được verify riêng và fixture worker phải chạy trên database CI
tạm thời.

P3-02B/P3-02C phải kiểm tra tiếp migrate `17 -> 18 -> 19 -> 20 -> 21`, rollback
`21 -> 20 -> 19`, migrate lại tới `21`, exact runtime ACL, snapshot immutability,
capability expiry/revoke, concurrent RSVP, cancellation lifecycle và
cross-tenant/cross-class concealment. Local compile/test không thay thế PostgreSQL staging
evidence.

P3-02D-A đã kiểm tra historical sequence `21 -> 22 -> 21 -> 22` trên disposable; không
rollback thêm. Forward `22 -> 23` và migrate idempotent đều PASS ở `23 false`; exact Core
API/maintenance ACL; poll ownership/quota/isolation/capability lifecycle; Study Meeting
conflict/quota; public capability expiry/revoke/rate/privacy; hard-retention batch validation
và parent/FK cascade đều đã có PostgreSQL evidence PASS. Shared staging, authenticated
browser/API và manual NVDA acceptance cũng PASS.

P3-06 đã forward `23 -> 24` trên disposable mà không rollback `000022/000023`; rerun giữ
`24 false`. Exact runtime ACL, canonical direct và one-class create dưới concurrency,
authoritative active membership/enrollment, archived-class read-only, feature-off history,
cross-tenant concealment và audit-once đều PASS bằng PostgreSQL thật. Shared staging vẫn ở
`23 false`; phải re-provision ACL và chạy lại acceptance sau forward, không suy diễn từ
disposable.

Với P2-05, cần kiểm tra riêng migrate 9 -> 10, rollback 10 -> 9, migrate lại 9 -> 10;
tenant-scoped FK/unique/state constraints; direct enroll và các transition; same-user
replay; concurrent join ở usage limit; atomic exhausted/expired state; archive guard;
active enrollment projection cho class/media; cross-tenant concealment và
transactional outbox. Local hiện mới compile integration-tag vì không nạp DB test env;
không coi đó là bằng chứng runtime PostgreSQL đã chạy.

P2-06 không đổi schema. PostgreSQL integration test bổ sung owner dedupe, pagination
không lặp/mất, UUID tie-break cho tên trùng, Unicode/literal-wildcard search, status
filter, role hierarchy/outbox redaction, projection API sau mutation, archived-class
guards và cross-class/cross-tenant denial. Integration-tag compile xanh local; runtime
chưa chạy vì không nạp DB test env và sẽ do CI PostgreSQL 17 xác nhận sau push.

P2-07 cần kiểm tra migrate 10 -> 11, rollback 11 -> 10, migrate lại 10 -> 11; trigger
append-only cho update/delete/truncate; metadata/IP/UA constraints; actor/tenant FK;
atomic business/outbox/audit; tenant A không query được audit tenant B; cursor bind mọi
filter và authoritative `audit.view`. Integration-tag compile xanh local; runtime
PostgreSQL chưa chạy vì không nạp DB test env.

CI tạo PostgreSQL 17 tạm thời, chạy migration từ database sạch rồi chạy integration
test. Test có transaction bao ngoài sẽ rollback toàn bộ. Concurrency test commit thật có
thể giữ fixture audit duy nhất đến khi database test tạm bị hủy; đây là hệ quả có chủ ý
của lịch sử append-only và không phải quy trình cleanup cho staging/production.

## Quy tắc thay đổi schema

1. Không sửa migration đã chạy; tạo migration số tiếp theo với cặp `up/down`.
2. Migration phải chạy được từ database sạch và từ version liền trước.
3. Mọi bảng nghiệp vụ tenant-scoped phải có `tenant_id`, index phù hợp và deny test.
4. Mọi repository phải nhận tenant context; không dùng tenant do request body tự khai.
5. Không ghi password, access token, session token hoặc secret thô vào database/log.
6. Event cần độ bền phải ghi bằng outbox trong cùng transaction với dữ liệu nghiệp vụ.
7. Cập nhật OpenAPI/generated client khi thay đổi contract công khai.

## Việc còn lại

- P1-06 đã triển khai OIDC/BFF, session rotation, CSRF và `/api/v1/me`; cả ZITADEL local và staging đã được provision và smoke test.
- P1-06B đã hoàn thành list/create/detail class; các lát cắt enrollment, invite code và roster thuộc Phase 2.
- P1-10 đã hoàn thành database/branch staging riêng, runtime role và migration role riêng.
- P2-04 đã bổ sung class lifecycle/ownership/archive và migration `000009`.
- P2-05 đã bổ sung enrollment, class invite code và migration `000010`.
- P2-06 đã bổ sung roster search/filter/keyset pagination, role hierarchy, single/bulk
  mutation, outbox và UI mà không cần migration mới.
- P2-07 đã bổ sung audit append-only, migration `000011`, atomic writer, tenant query
  API và UI org admin; retention/erasure/partitioning production chốt ở Phase 8.
- P2-09 đã áp dụng migration `000012` trên staging, cấp grants runtime tối thiểu và
  chạy bounded cleanup `rate_limit_windows=0`, `tenant_quota_windows=0` ngày
  2026-07-21; maintenance định kỳ tiếp tục theo runbook ở trên.
- P2-11 đã hoàn tất source và PostgreSQL 17 CI cho fixture V1 ẩn danh
  user/tenant/membership/class bằng migration `000013`. P2-12 đã xác nhận Neon staging
  migration 13, role split/default ACL và importer idempotency trên disposable branch;
  production data/cohort migration vẫn thuộc discovery/cutover phase sau.
- P3-03A/P3-04 đã bổ sung migration `000015`/`000016`; P3-02A/P3-02B bổ sung
  `000017`/`000018`/`000019`; P3-02C bổ sung `000020/000021`; P3-02D-A bổ sung
  `000022/000023`. Disposable và shared Neon staging đã xác nhận `23 false`, exact Core
  API/maintenance ACL và toàn bộ database gate P3-02D-A; authenticated browser/API và
  manual NVDA acceptance cũng PASS, nên task đã `DONE`.
  Worker ACL, durable-host, controlled-canary/crash-reclaim và các gate P3-03/P3-04
  vẫn là gate bên ngoài, hiện được phân loại `DEFERRED/VERIFY` chứ không tự động bỏ qua.
- P3-02A Calendar shell/read projection, P3-02B recurrence/class-conflict và
  P3-02C working schedule/free-busy, internal/external audience, organizer transfer,
  cancellation lifecycle và RSVP đều đã đạt staging gate. Delivery/email side effect
  vẫn bị khóa cho tới khi P3-03B/P3-05A và các provider gate tương ứng hoàn tất.
- Chưa có backup/restore drill, PITR gate hoặc connection load test cho pilot.
