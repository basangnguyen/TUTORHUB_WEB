# P3-07A staging acceptance — persistent message, unread/read core

Tài liệu này là runbook nghiệm thu P3-07A trên PostgreSQL disposable trước khi cân nhắc
forward shared Neon hoặc deploy. PostgreSQL qua Core API REST là source of truth; realtime,
notification, worker delivery, attachment, search và offline draft không thuộc gate này.

## Trạng thái hiện tại

Ngày ghi nhận: 2026-08-04.

| Hạng mục | Trạng thái | Ghi chú |
| --- | --- | --- |
| ADR-0025 và migration `000025` trong source | `LOCAL` | Chưa có bằng chứng Neon P3-07A |
| Core API unit/HTTP test | `PASS (cached)` | `go test ./...` trên `services/core-api`; chưa phải fresh candidate run |
| Integration-tag compile | `PASS` | `go test -tags=integration -run '^$' ./...`; chỉ compile, không chạy test |
| Full `corepack pnpm verify` trên candidate | `PENDING` | Chưa khóa exact candidate |
| Disposable owner preflight | `PENDING` | Chưa kết nối database |
| Disposable forward `24 false -> 25 false` | `PENDING` | Không suy diễn từ local compile |
| Disposable exact ACL/PostgreSQL gates | `PENDING` | Chưa chạy bằng runtime credential thật |
| Shared Neon forward/ACL | `PENDING` | Bị chặn cho tới khi disposable được báo cáo PASS |
| Render/Cloudflare deployment và live acceptance | `PENDING` | Không deploy trước báo cáo disposable |

P3-06 đã được nghiệm thu ở `24 false`. Mọi kết quả `25 false`, shared staging, CI của exact
candidate và deployment P3-07A vẫn là `PENDING`; tài liệu này không ghi nhận thao tác
database nào đã chạy.

## Phạm vi và invariant bắt buộc

P3-07A nghiệm thu:

- message plain text bền vững cho direct/class conversation hiện có;
- server sequence theo conversation, keyset pagination và database timestamp;
- `client_message_id` idempotent theo tenant/author, kể cả race giữa hai conversation;
- author-only edit/delete với `expected_version`; delete thành tombstone, không hard-delete;
- self read marker đơn điệu và unread bounded `100`, loại message của chính viewer và tombstone;
- direct chỉ ghi khi cả hai participant còn active; class dùng owner/enrollment authoritative;
- archived class giữ history/read marker nhưng chặn send/edit/delete;
- feature-off giữ history/read/replay đã commit nhưng chặn content write mới;
- tenant storage quota dùng `tenant_message_usage` O(1), hourly tenant send quota và actor
  limit `60/phút` atomically; reservation counter chạy dưới tenant advisory lock;
- exact Core API runtime ACL và không có message body trong audit/outbox/log/error/cursor.

P3-07A không bật SSE/WebSocket/LiveKit DataChannel, typing/presence, notification/outbox
delivery, attachment, search, moderation, persistent offline draft hoặc retention purge.
Không tạo worker grant hay maintenance credential cho migration này.

## Quy tắc an toàn

1. Tạo một Neon branch disposable mới từ shared staging đang ở `24 false`; xác nhận branch
   không chứa dữ liệu người dùng cần giữ và không phải production/shared staging.
2. Dùng direct migration-owner URL cho migration và pooled runtime URL cho Core API test.
   Hai login phải khác nhau. Không dùng migration credential làm runtime credential.
3. Tái sử dụng `.env.p3-02d-a-disposable.local`; file phải được Git ignore và chỉ có
   `DATABASE_MIGRATION_URL` cùng `DATABASE_POOL_URL` cần cho gate này.
4. Không in URL, password, query parameter, cookie, message text fixture hoặc connection
   error chứa credential vào chat, tài liệu, screenshot hay artifact.
5. Chỉ forward `24 -> 25`. **Không chạy `db:rollback`, down migration hoặc sửa migration
   `000025` sau khi đã forward.** Nếu cần sửa schema/security, tạo forward migration mới.
6. Không chạy focused mutation/concurrency suite trên shared staging. Không forward shared
   staging và không deploy trước khi toàn bộ disposable database gate PASS được báo cáo.

## 1. Nạp URL trong cùng PowerShell process

Không dựa vào biến đã nhập ở một PowerShell khác vì process của Codex có thể không kế thừa.
Chạy loader và mọi command database tiếp theo trong cùng một PowerShell process. Đoạn này
chỉ in trạng thái, không in giá trị:

```powershell
$dotenvPath = Join-Path (Get-Location) '.env.p3-02d-a-disposable.local'
if (-not (Test-Path -LiteralPath $dotenvPath)) {
  throw 'Missing disposable environment file.'
}

Get-Content -LiteralPath $dotenvPath | ForEach-Object {
  $line = $_.Trim()
  if ($line -and -not $line.StartsWith('#')) {
    $parts = $line -split '=', 2
    if ($parts.Count -eq 2) {
      Set-Item -Path "Env:$($parts[0].Trim())" -Value $parts[1].Trim()
    }
  }
}

foreach ($name in @('DATABASE_MIGRATION_URL', 'DATABASE_POOL_URL')) {
  $loadedValue = [Environment]::GetEnvironmentVariable($name, 'Process')
  if ([string]::IsNullOrWhiteSpace($loadedValue)) {
    throw "Missing required variable: $name"
  }
}
Remove-Variable line, parts, loadedValue -ErrorAction SilentlyContinue
'ENV_LOAD=PASS'
```

Không dùng `Get-ChildItem Env:`, `Write-Host $env:DATABASE_*`, shell tracing hoặc command
nào serialize toàn bộ process environment. Khi kết thúc phiên kiểm thử:

```powershell
Remove-Item Env:DATABASE_MIGRATION_URL -ErrorAction SilentlyContinue
Remove-Item Env:DATABASE_POOL_URL -ErrorAction SilentlyContinue
```

## 2. Disposable owner preflight

Trước forward, `corepack pnpm db:version` phải trả đúng:

```text
24 false
```

Nếu version khác hoặc dirty là `true`, dừng. Không tự sửa migration ledger.

Trong Neon SQL Editor đã chọn đúng disposable branch, hoặc một SQL session được mở an toàn
bằng migration credential, chạy probe sau. Không đặt URL trong SQL hay command history:

```sql
WITH required_relations(relname) AS (
    VALUES
        ('tenant_quota_overrides'),
        ('tenant_quota_windows'),
        ('conversations')
),
actor AS (
    SELECT oid, rolsuper, rolcreaterole, rolcreatedb, rolbypassrls
    FROM pg_roles
    WHERE rolname = current_user
),
owned_scope AS (
    SELECT
        count(*) AS relation_count,
        bool_and(pg_has_role(actor.oid, relation.relowner, 'USAGE')) AS owner_authority
    FROM required_relations
    JOIN pg_class AS relation
      ON relation.relname = required_relations.relname
    JOIN pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
     AND namespace.nspname = 'tutorhub'
    CROSS JOIN actor
)
SELECT
    NOT actor.rolsuper AS not_superuser,
    actor.rolcreaterole AS owner_can_create_role,
    actor.rolcreatedb AS owner_can_create_database,
    actor.rolbypassrls AS owner_can_bypass_rls,
    has_schema_privilege(current_user, 'tutorhub', 'USAGE') AS schema_usage,
    has_schema_privilege(current_user, 'tutorhub', 'CREATE') AS schema_create,
    owned_scope.relation_count = 3 AS required_relations_present,
    owned_scope.owner_authority
FROM actor
CROSS JOIN owned_scope;
```

PASS yêu cầu đúng một row với `not_superuser`, `schema_usage`, `schema_create`,
`required_relations_present` và `owner_authority` là `true`. Ba cờ
`owner_can_*` là residual cần ghi tổng hợp `OWNER_ADMIN_RESIDUAL=true/false`, không phải
lý do truyền quyền admin sang runtime. Operator phải xác nhận thêm:

- URL migration là direct hostname, URL runtime là pooled hostname;
- runtime login khác migration login;
- branch là disposable được tạo từ shared `24 false`;
- không có process Core API/worker nào đang dùng disposable branch.

Chỉ ghi `ENV_LOAD=PASS`, `OWNER_PREFLIGHT=PASS`, `OWNER_ADMIN_RESIDUAL=true/false` và
version; không ghi role/hostname/URL.

## 3. Forward-only `24 -> 25` và rerun idempotent

Trong đúng process đã nạp URL:

```powershell
corepack pnpm db:version
corepack pnpm db:migrate
corepack pnpm db:version
corepack pnpm db:migrate
corepack pnpm db:version
```

Chuỗi bắt buộc:

```text
24 false -> 25 false -> 25 false
```

Migration đầu phải tạo `messages`, `tenant_message_usage`, `message_receipts`, quota
keys/index/constraints và `PUBLIC` revoke. Migration thứ hai không tạo thêm object, không
đổi ledger và vẫn giữ `25 false`. Không chạy rollback để chứng minh idempotency.

Nếu forward lỗi, dừng tại disposable, giữ branch để điều tra và không nới ACL/wildcard.
Không chạy cùng command trên shared staging.

## 4. Provision exact Core API runtime ACL

Thay `tutorhub_runtime` bằng runtime role thật của disposable. Role name không phải secret,
nhưng không copy URL hoặc password vào SQL. Chạy bằng migration owner sau khi đã ở `25 false`:

```sql
BEGIN;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;
REVOKE CREATE ON SCHEMA tutorhub FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members,
    tutorhub.messages,
    tutorhub.tenant_message_usage,
    tutorhub.message_receipts
FROM tutorhub_runtime;

-- Xóa cả stale column grants trước khi cấp lại allowlist.
REVOKE UPDATE (
    id, tenant_id, kind, class_id, direct_user_low_id, direct_user_high_id,
    created_by_user_id, created_at, updated_at
), REFERENCES (
    id, tenant_id, kind, class_id, direct_user_low_id, direct_user_high_id,
    created_by_user_id, created_at, updated_at
) ON TABLE tutorhub.conversations FROM tutorhub_runtime;

REVOKE UPDATE (
    tenant_id, conversation_id, user_id, joined_at
), REFERENCES (
    tenant_id, conversation_id, user_id, joined_at
) ON TABLE tutorhub.conversation_members FROM tutorhub_runtime;

REVOKE UPDATE (
    id, tenant_id, conversation_id, author_user_id, client_message_id, sequence,
    request_fingerprint, content, state, version, edited_at, deleted_at,
    created_at, updated_at
), REFERENCES (
    id, tenant_id, conversation_id, author_user_id, client_message_id, sequence,
    request_fingerprint, content, state, version, edited_at, deleted_at,
    created_at, updated_at
) ON TABLE tutorhub.messages FROM tutorhub_runtime;

REVOKE UPDATE (
    tenant_id, message_count, updated_at
), REFERENCES (
    tenant_id, message_count, updated_at
) ON TABLE tutorhub.tenant_message_usage FROM tutorhub_runtime;

REVOKE UPDATE (
    tenant_id, conversation_id, user_id, last_read_sequence,
    last_read_message_id, updated_at
), REFERENCES (
    tenant_id, conversation_id, user_id, last_read_sequence,
    last_read_message_id, updated_at
) ON TABLE tutorhub.message_receipts FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members,
    tutorhub.messages,
    tutorhub.tenant_message_usage,
    tutorhub.message_receipts
FROM PUBLIC;

GRANT SELECT, INSERT ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members,
    tutorhub.messages,
    tutorhub.tenant_message_usage,
    tutorhub.message_receipts
TO tutorhub_runtime;

GRANT UPDATE (updated_at)
ON TABLE tutorhub.conversations
TO tutorhub_runtime;

GRANT UPDATE (content, state, version, edited_at, deleted_at, updated_at)
ON TABLE tutorhub.messages
TO tutorhub_runtime;

GRANT UPDATE (message_count, updated_at)
ON TABLE tutorhub.tenant_message_usage
TO tutorhub_runtime;

GRANT UPDATE (last_read_sequence, last_read_message_id, updated_at)
ON TABLE tutorhub.message_receipts
TO tutorhub_runtime;

COMMIT;
```

Không cấp table-level `UPDATE`. Runtime role không được `DELETE`, `TRUNCATE`, `REFERENCES`,
`TRIGGER`, ownership, superuser, role creation, database creation, bypass-RLS hay membership
trong migration role. `conversation_members` vẫn không có update path. Message delete là
column update thành tombstone; không phải SQL `DELETE`. `tenant_message_usage` chỉ được
reserve transactionally dưới tenant advisory lock, không phải API generic mutation.

Exact matrix mong đợi:

| Relation | SELECT | INSERT | table UPDATE | Column UPDATE allowlist | DELETE |
| --- | :---: | :---: | :---: | --- | :---: |
| `conversations` | yes | yes | no | `updated_at` | no |
| `conversation_members` | yes | yes | no | none | no |
| `messages` | yes | yes | no | `content,state,version,edited_at,deleted_at,updated_at` | no |
| `tenant_message_usage` | yes | yes | no | `message_count,updated_at` | no |
| `message_receipts` | yes | yes | no | `last_read_sequence,last_read_message_id,updated_at` | no |

Chạy query dưới đây và yêu cầu **zero rows**. Thay role placeholder trước khi chạy:

```sql
WITH target_tables(table_name) AS (
    VALUES
        ('conversations'),
        ('conversation_members'),
        ('messages'),
        ('tenant_message_usage'),
        ('message_receipts')
),
table_verbs(privilege_type) AS (
    VALUES
        ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'),
        ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')
),
expected_table(table_name, privilege_type) AS (
    VALUES
        ('conversations', 'SELECT'),
        ('conversations', 'INSERT'),
        ('conversation_members', 'SELECT'),
        ('conversation_members', 'INSERT'),
        ('messages', 'SELECT'),
        ('messages', 'INSERT'),
        ('tenant_message_usage', 'SELECT'),
        ('tenant_message_usage', 'INSERT'),
        ('message_receipts', 'SELECT'),
        ('message_receipts', 'INSERT')
),
expected_update(table_name, column_name) AS (
    VALUES
        ('conversations', 'updated_at'),
        ('messages', 'content'),
        ('messages', 'state'),
        ('messages', 'version'),
        ('messages', 'edited_at'),
        ('messages', 'deleted_at'),
        ('messages', 'updated_at'),
        ('tenant_message_usage', 'message_count'),
        ('tenant_message_usage', 'updated_at'),
        ('message_receipts', 'last_read_sequence'),
        ('message_receipts', 'last_read_message_id'),
        ('message_receipts', 'updated_at')
),
table_mismatch AS (
    SELECT
        'table'::text AS scope,
        target_tables.table_name,
        NULL::text AS column_name,
        table_verbs.privilege_type,
        expected_table.table_name IS NOT NULL AS expected,
        has_table_privilege(
            'tutorhub_runtime',
            format('tutorhub.%I', target_tables.table_name),
            table_verbs.privilege_type
        ) AS actual
    FROM target_tables
    CROSS JOIN table_verbs
    LEFT JOIN expected_table
      ON expected_table.table_name = target_tables.table_name
     AND expected_table.privilege_type = table_verbs.privilege_type
),
column_mismatch AS (
    SELECT
        'column'::text AS scope,
        columns.table_name,
        columns.column_name,
        column_verbs.privilege_type,
        CASE
            WHEN column_verbs.privilege_type = 'UPDATE'
                THEN expected_update.column_name IS NOT NULL
            ELSE false
        END AS expected,
        has_column_privilege(
            'tutorhub_runtime',
            format('tutorhub.%I', columns.table_name),
            columns.column_name,
            column_verbs.privilege_type
        ) AS actual
    FROM information_schema.columns AS columns
    JOIN target_tables ON target_tables.table_name = columns.table_name
    CROSS JOIN (VALUES ('UPDATE'), ('REFERENCES')) AS column_verbs(privilege_type)
    LEFT JOIN expected_update
      ON expected_update.table_name = columns.table_name
     AND expected_update.column_name = columns.column_name
    WHERE columns.table_schema = 'tutorhub'
)
SELECT scope, table_name, column_name, privilege_type, expected, actual
FROM (
    SELECT * FROM table_mismatch
    UNION ALL
    SELECT * FROM column_mismatch
) AS matrix
WHERE actual IS DISTINCT FROM expected
ORDER BY scope, table_name, column_name, privilege_type;
```

Xác nhận thêm role safety và `PUBLIC` không có grant:

```sql
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb,
    rolbypassrls,
    has_schema_privilege(rolname, 'tutorhub', 'USAGE') AS schema_usage,
    has_schema_privilege(rolname, 'tutorhub', 'CREATE') AS schema_create,
    pg_has_role(rolname, current_user, 'MEMBER') AS member_of_migration_role,
    EXISTS (
        SELECT 1
        FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname IN (
              'conversations', 'conversation_members', 'messages',
              'tenant_message_usage', 'message_receipts'
          )
          AND relation.relowner = pg_roles.oid
    ) AS owns_target_relation
FROM pg_roles
WHERE rolname = 'tutorhub_runtime';

SELECT table_name, privilege_type
FROM information_schema.table_privileges
WHERE table_schema = 'tutorhub'
  AND table_name IN (
      'conversations', 'conversation_members', 'messages',
      'tenant_message_usage', 'message_receipts'
  )
  AND grantee = 'PUBLIC';
```

Role query phải trả đúng một row: `schema_usage=true`; mọi cờ đặc quyền,
`schema_create`, migration membership và `owns_target_relation` đều `false`. `PUBLIC`
query phải zero rows. Nếu migration role thực tế khác `current_user`, thay đối số
membership bằng exact migration role đã xác minh trong owner preflight.

## 5. Focused PostgreSQL integration gates

Chạy bằng đúng hai URL disposable đã nạp, không dùng migration URL cho cả hai biến:

```powershell
go test -count=1 -tags=integration `
  ./services/core-api/internal/modules/conversation `
  -run 'TestPostgresConversationAndMessageRuntimeExactACL|TestPostgresPersistentMessage'
```

Không chấp nhận test ACL bị `SKIP` vì runtime/migration cùng role. Suite phải PASS toàn bộ:

1. **Exact ACL:** schema usage/no create, exact table/column matrix, không owner/superuser/
   migration membership.
2. **Idempotency:** hai send đồng thời cùng conversation/key/payload tạo đúng một row,
   sequence và quota; replay trả row cũ; payload khác trả conflict.
3. **Cross-conversation race:** cùng author/client ID gửi đồng thời vào direct và class có
   đúng một winner, một idempotency conflict, một row và một quota consumption.
4. **Ordering/pagination:** server sequence tăng đơn điệu; `limit=1` rồi next cursor không
   lặp/mất row và không dùng offset.
5. **Author lifecycle:** non-author edit/delete conceal; CAS stale conflict; edit tăng
   version; delete xóa content và giữ tombstone; không SQL hard-delete.
6. **Read/unread:** concurrent/out-of-order mark-read không lùi marker; unread loại own và
   deleted, cap ở `100` cùng `unread_count_capped=true`.
7. **Authorization:** foreign tenant/ID conceal; direct peer inactive thành read-only;
   class enrollment suspended mất access; archived class vẫn list/mark-read nhưng chặn
   send/edit/delete.
8. **Controls:** storage hard cap reserve `tenant_message_usage` O(1) dưới tenant advisory
   lock; counter đúng bằng số message/tombstone đã commit; tenant hourly quota và actor
   `60/phút` trả typed quota/retry metadata; failed send rollback row, counter và quota
   window; replay không tiêu thụ quota/counter.
9. **Feature lifecycle:** emergency-off chặn content write mới nhưng giữ history, self-read
   và identical replay đã commit.
10. **Privacy:** message text không xuất hiện trong audit/outbox; send/read không tạo
    audit/outbox/delivery side effect; database error không serialize failing-row detail.
11. **Counter/query plan:** `tenant_message_usage` có đúng một row/tenant qua primary key,
    FK cascade và non-negative constraint; storage capacity đọc/reserve row này, không
    `count(*)` toàn bảng `messages`. `EXPLAIN` với fixture lớn phải giữ lookup bounded theo
    tenant primary key.

Chạy thêm local gate trên exact candidate:

```powershell
corepack pnpm verify
```

Lưu bằng chứng chỉ gồm commit, UTC, tên gate và PASS/FAIL. Không lưu fixture content, UUID,
role credential, SQL error detail chứa row, URL hoặc environment dump.

## 6. Thứ tự sau disposable

Chỉ sau khi owner nhận báo cáo disposable PASS:

1. khóa exact commit/candidate và xác nhận CI/security xanh;
2. owner phê duyệt forward shared staging `24 false -> 25 false`;
3. chạy owner preflight, forward-only, idempotent rerun và exact ACL trên shared;
4. không chạy mutation/concurrency fixture suite trên shared; dùng ACL probe và controlled
   authenticated API/browser smoke với fixture canary;
5. deploy đúng candidate lên Render/Cloudflare;
6. chạy Teacher/Student/Admin authorization, reload/reconnect, retry, keyboard/focus/Axe
   và log-privacy acceptance trên deployment đúng commit.

Không rollback shared `000025`. Nếu lỗi sau forward, giữ database ở `25 false`, fail closed
content writes khi cần và sửa bằng application rollback hoặc forward migration reviewed.

## Bảng kết quả

| Gate | Kết quả | Bằng chứng không nhạy cảm |
| --- | --- | --- |
| Local Core API `go test ./...` | `PASS (cached)` | Cần fresh `-count=1` trên exact candidate |
| Integration-tag compile, không có DB evidence | `PASS` | `-run '^$'`; không chạy test |
| Local working tree + `pnpm verify` | `PASS` | 2026-08-04; workspace-scoped `GOCACHE` |
| Exact committed candidate | `PASS` | Commit local hiện tại; chưa push/deploy |
| Disposable branch safety + env load | `PENDING` | Chưa kết nối |
| Owner preflight ở `24 false` | `PENDING` | Chưa chạy |
| Forward `24 false -> 25 false` | `PENDING` | Chưa chạy |
| Idempotent rerun giữ `25 false` | `PENDING` | Chưa chạy |
| Exact runtime table/column ACL | `PENDING` | Chưa provision/probe |
| Idempotency + cross-conversation race | `PENDING` | Chưa chạy PostgreSQL thật |
| CAS/tombstone + pagination/order | `PENDING` | Chưa chạy PostgreSQL thật |
| Monotonic read + unread/cap | `PENDING` | Chưa chạy PostgreSQL thật |
| Direct/class/tenant authorization | `PENDING` | Chưa chạy PostgreSQL thật |
| Storage counter/hourly/actor rate + query plan | `PENDING` | Chưa chạy PostgreSQL thật |
| Audit/outbox/error privacy | `PENDING` | Chưa chạy PostgreSQL thật |
| Shared staging forward/ACL | `PENDING` | Bị chặn bởi disposable report |
| Exact deploy + CI/security | `PENDING` | Chưa deploy |
| Live role/reload/retry/keyboard/Axe | `PENDING` | Chưa chạy |

## Exit gate

P3-07A chỉ được chuyển `IN PROGRESS -> VERIFY` sau khi exact candidate local gate và toàn
bộ disposable migration/ACL/PostgreSQL gates PASS. Chỉ chuyển `VERIFY -> DONE` sau shared
forward/ACL, exact deploy, CI/security và authenticated live/browser/accessibility matrix
PASS. Không dùng local compile, mock HTTP test hoặc P3-06 `24 false` làm bằng chứng thay cho
P3-07A disposable/shared acceptance.
