# Database foundation

Tài liệu này là runbook cho nền PostgreSQL của TutorHub V2 từ P1-05. Mọi agent
thay đổi schema, migration hoặc repository phải đọc tài liệu này trước khi sửa.

## Trạng thái hiện tại

- System of record: Neon PostgreSQL.
- Schema ứng dụng: `tutorhub`.
- Migration mới nhất trong source: `000013_legacy_fixture_import`.
- Migration 1-5 đã được chạy và kiểm tra trên Neon; smoke
  `5 false -> rollback 4 false -> migrate 5 false` đạt ngày 2026-07-16.
- Migration `000006` đến `000013` đều có up/down path. Source và PostgreSQL 17 CI
  của P2-11 đã xác nhận `000013` qua migrate `13 -> 12 -> 13`, dry-run, apply/rerun,
  checkpoint/resume và reset. Neon staging mới có bằng chứng `000012` với `12 false`
  ngày 2026-07-21; migrate `12 -> 13 -> 12 -> 13` và kiểm tra tách quyền migration/
  runtime cuối cho ba bảng `legacy_import_*` vẫn `PENDING` trong P2-12.
- Phần lớn integration test rollback bằng transaction. Chỉ focused P2-09 suite có
  fixture tự dọn hoàn toàn được chạy trên staging ngày 2026-07-21; các suite concurrency
  có thể để lại audit append-only vẫn chỉ chạy trên database CI tạm thời.
- Core API đã được smoke test với Neon: `/ready` trả `ready` và `/health` trả `ok`.

Neon có branch `production` và branch staging tách biệt. Core API staging dùng pooled
runtime role tối thiểu quyền; migration job dùng direct migration role riêng. Kết nối,
readiness và smoke đến migration `000012` đã đạt; không suy diễn kết quả đó thành bằng
chứng migration `000013` hoặc role split cuối trước khi các kiểm tra P2-12 thực sự chạy.

## Hai connection URL

| Biến                     | Đối tượng sử dụng  | Loại URL                                 | Quy tắc                                                   |
| ------------------------ | ------------------ | ---------------------------------------- | --------------------------------------------------------- |
| `DATABASE_POOL_URL`      | Core API đang chạy | Neon pooled, hostname có `-pooler`       | Chỉ quyền runtime; cấu hình pool nhỏ để phù hợp free tier |
| `DATABASE_MIGRATION_URL` | CLI/release job    | Neon direct, hostname không có `-pooler` | Chỉ cấp cho migration job; không đưa vào API container    |

Không dùng URL direct cho traffic ứng dụng thường xuyên. Không cấp URL migration
cho frontend, browser, Cloudflare Pages hoặc tiến trình Core API trên Render.
Core API không tự chạy migration khi khởi động.

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

`application_name=tutorhub-core-api` được gắn vào kết nối để quan sát trên Neon.
Mọi truy vấn mạng/database phải chạy ngoài UI thread ở các client native về sau.

## Schema phiên bản 13

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
| `outbox_events`          | Transactional outbox cho sự kiện bền vững và worker tương lai                                    |
| `audit_events`           | Lịch sử tenant append-only cho actor/action/resource/outcome và request correlation              |
| `tenant_feature_control_revisions` | Phiên bản optimistic của override feature/quota theo tenant                         |
| `tenant_feature_overrides` | Override feature typed theo tenant; global disable vẫn có quyền ưu tiên                          |
| `tenant_quota_overrides` | Override hard limit typed cho member, active class và invitation rate                              |
| `tenant_quota_windows`   | Bộ đếm fixed-window tenant-scoped cho quota invitation                                              |
| `rate_limit_windows`     | Bộ đếm anonymous shared; lưu purpose và SHA-256 đã domain-separate theo version/purpose/prefix       |
| `legacy_import_runs`     | Ledger migration-role-only cho checksum, trạng thái và checkpoint fixture V1                         |
| `legacy_import_run_items` | Outcome/reason code bounded theo record để reconciliation và resume                                  |
| `legacy_import_mappings` | Mapping bền `(source_system, entity_type, external_id) -> target_id`; không chứa source payload        |

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

## Chạy migration

Tạo `.env.local` từ `.env.example` và điền hai URL. File này đã được Git ignore;
không in URL ra terminal, issue, log hoặc tài liệu.

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

Sau khi áp dụng toàn bộ migration trong source, kết quả mong đợi là `13 false`. Chỉ ghi
đó là kết quả môi trường khi lệnh thực tế đã chạy; bằng chứng Neon staging gần nhất hiện
vẫn là `12 false` ngày 2026-07-21. Migration `000013` up/down/up và xác nhận runtime role
không có quyền trên `legacy_import_*` vẫn `PENDING`. Rollback chỉ dùng khi đã đánh giá
mất dữ liệu và có backup/restore plan:

```powershell
go run ./services/core-api/cmd/migrate down -steps 1
```

## Kiểm thử

Unit test và static verification:

```powershell
pnpm verify
```

Integration test bằng PostgreSQL thật:

```powershell
pnpm test:integration
```

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
  user/tenant/membership/class bằng migration `000013`; Neon staging migration 13 và
  kiểm tra role split cuối vẫn thuộc acceptance P2-12. Production data/cohort migration
  vẫn thuộc discovery/cutover phase sau.
- Chưa có backup/restore drill, PITR gate hoặc connection load test cho pilot.
