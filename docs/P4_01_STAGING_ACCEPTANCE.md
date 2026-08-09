# P4-01 staging acceptance: MediaSpace lifecycle, schema and API core

## 1. Trạng thái và ranh giới

- Trạng thái hiện tại: `DONE` ngày 2026-08-09.
- Kiến trúc có thẩm quyền: ADR-0030; migration source:
  `000029_classroom_media_spaces`.
- Source candidate đã có schema/domain/repository, feature/quota, REST
  create/get/start/end/cancel, OpenAPI/generated client và feature-control UI.
- Typed policy/security review, fresh local verify, disposable PostgreSQL forward/exact
  ACL/integration, exact candidate GitHub CI/security, shared staging forward/ACL, deploy và
  live feature-off acceptance đều PASS.
- Migration `000029` đã forward-only trên disposable branch
  `p4-01-disposable-20260809` và shared Neon; cả hai giữ final ledger `29 false`. Không rollback.

P4-01 luôn feature-off. `classroom_media_rooms` và `instant_study_rooms` có compiled default
`false`, deployment enable mặc định `false`; child instant còn phụ thuộc parent. Thiếu LiveKit
runtime prerequisite phải fail closed. P4-01 không mint token, gọi provider, xử lý webhook,
admit participant hoặc gửi media. Start chỉ tạo database room-instance intent; provider binding
thật thuộc P4-02 và không được kích hoạt sớm.

## 2. Forward design `000029`

Migration thêm:

1. hai feature key `classroom_media_rooms`, `instant_study_rooms`;
2. bốn quota `active_media_spaces`, `media_participants_per_space`,
   `active_media_participants`, `media_space_starts_per_hour`;
3. `media_spaces` với source union chính xác cho one-time ClassSession, recurring occurrence
   hoặc StudyMeeting; instant command phải tạo/bind StudyMeeting rồi lưu source kind
   `study_meeting`, không tạo authority thứ ba;
4. `media_room_instances` với attempt, lifecycle intent và unique một instance
   `provisioning|active|closing` trên mỗi space;
5. `media_space_members`, `media_admission_requests`, `media_participant_sessions` để khóa
   schema/tenant FK cho các slice sau; P4-01 không có runtime write path vào ba bảng này;
6. `media_space_mutation_receipts` cho replay start/end/cancel theo fingerprint, với key được
   scope theo tenant và actor để không tạo receipt-existence oracle giữa hai thành viên;
7. composite tenant FK, one-space-per-source unique index, lifecycle/check constraints,
   bounded opaque identifiers và `PUBLIC` revoke trên toàn bộ bảng mới.

Lifecycle P4-01 là `scheduled -> open -> ended` hoặc `scheduled -> cancelled`, dùng expected
version, idempotency receipt và lock order thống nhất với source. Source cancel/archive và
StudyMeeting/ClassSession mutation phải khóa MediaSpace trước source row rồi fail closed nếu
còn binding `open` hoặc occurrence tiếp theo không hợp lệ. Down migration chỉ là recovery
artifact được review; không chạy rollback trong disposable/shared acceptance này.

## 3. Disposable owner preflight và forward-only gate

Nạp `DATABASE_MIGRATION_URL` và `DATABASE_POOL_URL` từ file local đã Git-ignore trong cùng
process chạy gate; không in/log URL, password, hostname hoặc query string. Không cần maintenance
credential cho P4-01. Migration URL phải direct; runtime URL phải pooled; hai login phải khác
nhau và disposable không được có Core API/worker đang dùng.

Chạy bằng migration login trước forward:

```sql
WITH required_relations(relname) AS (
    VALUES
        ('tenant_feature_overrides'),
        ('tenant_quota_overrides'),
        ('tenant_quota_windows')
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

SELECT version, dirty FROM public.tutorhub_schema_migrations;
```

PASS yêu cầu ledger đầu vào đúng `28 false`; `not_superuser`, `schema_usage`,
`schema_create`, `required_relations_present`, `owner_authority` đều `true`. Các cờ admin
residual được ghi dạng boolean, không truyền sang runtime. Nếu ledger khác hoặc dirty, dừng;
không tự sửa ledger.

Chuỗi forward bắt buộc trên disposable:

```text
28 false -> 29 false -> 29 false
```

Chạy `db:version`, `db:migrate`, `db:version`, lặp `db:migrate`, rồi `db:version` trong đúng
process đã nạp hai URL. Không rollback. Nếu lỗi, giữ disposable để điều tra, không nới
constraint/ACL và tuyệt đối không chạy command tương tự trên shared staging.

## 4. Exact Core API runtime ACL

P4-01 chỉ cấp write allowlist cho ba relation lifecycle. Hai bảng future-slice chỉ được đọc
vì repository/quota hiện cần chúng; admission giữ zero grant.

| Relation                        | Table SELECT | Column SELECT                         | Column INSERT                                             | Column UPDATE                               | DELETE |
| ------------------------------- | :----------: | ------------------------------------- | --------------------------------------------------------- | ------------------------------------------- | :----: |
| `media_spaces`                  |      no      | exact repository projection/predicate | source/idempotency/actor/create timestamps theo SQL       | lifecycle/version/actor timestamps theo SQL |   no   |
| `media_room_instances`          |      no      | exact active-instance columns         | intent identity/attempt/opaque room name/actor timestamps | terminal intent lifecycle theo SQL          |   no   |
| `media_space_mutation_receipts` |      no      | exact replay predicate/proof columns  | exact receipt row theo SQL                                | none                                        |   no   |
| `media_space_members`           |      no      | `tenant_id,space_id,user_id,status`   | none                                                      | none                                        |   no   |
| `media_participant_sessions`    |      no      | `tenant_id,status`                    | none                                                      | none                                        |   no   |
| `media_admission_requests`      |      no      | none                                  | none                                                      | none                                        |   no   |

Không relation nào được table-level `SELECT`/`INSERT`/`UPDATE`, `DELETE`, `TRUNCATE`,
`REFERENCES`, `TRIGGER`, ownership hoặc schema `CREATE`. `media_space_members` chỉ có bốn
column `SELECT` cho explicit-member visibility. `media_participant_sessions` chỉ có hai column
`SELECT` để feature-control đếm active participants. P4-01 không được cấp DML vào hai bảng này;
`media_admission_requests` không có quyền nào.

Thay `tutorhub_runtime` bằng runtime role thật, chạy bằng migration owner sau khi ở
`29 false`. Block đầu xóa cả stale table/column grant trước khi cấp lại allowlist:

```sql
BEGIN;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;
REVOKE CREATE ON SCHEMA tutorhub FROM tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.media_spaces,
    tutorhub.media_room_instances,
    tutorhub.media_space_members,
    tutorhub.media_admission_requests,
    tutorhub.media_participant_sessions,
    tutorhub.media_space_mutation_receipts
FROM tutorhub_runtime;

DO $p4_acl$
DECLARE
    runtime_role name := 'tutorhub_runtime';
    relation_name text;
    column_list text;
BEGIN
    FOREACH relation_name IN ARRAY ARRAY[
        'media_spaces',
        'media_room_instances',
        'media_space_members',
        'media_admission_requests',
        'media_participant_sessions',
        'media_space_mutation_receipts'
    ]
    LOOP
        SELECT string_agg(format('%I', column_name), ', ' ORDER BY ordinal_position)
        INTO column_list
        FROM information_schema.columns
        WHERE table_schema = 'tutorhub'
          AND table_name = relation_name;

        EXECUTE format(
            'REVOKE SELECT (%1$s), INSERT (%1$s), UPDATE (%1$s), REFERENCES (%1$s) '
            'ON TABLE tutorhub.%2$I FROM %3$I',
            column_list,
            relation_name,
            runtime_role
        );
    END LOOP;
END
$p4_acl$;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.media_spaces,
    tutorhub.media_room_instances,
    tutorhub.media_space_members,
    tutorhub.media_admission_requests,
    tutorhub.media_participant_sessions,
    tutorhub.media_space_mutation_receipts
FROM PUBLIC;

GRANT SELECT (
    id, tenant_id, source_kind, class_id, source_class_session_id,
    source_series_id, source_occurrence_key, source_study_meeting_id,
    status, version, create_idempotency_key, create_request_fingerprint,
    created_by, created_at, updated_at
) ON TABLE tutorhub.media_spaces TO tutorhub_runtime;

GRANT SELECT (
    id, tenant_id, space_id, status, version, created_at, updated_at
) ON TABLE tutorhub.media_room_instances TO tutorhub_runtime;

GRANT SELECT (tenant_id, space_id, user_id, status)
ON TABLE tutorhub.media_space_members TO tutorhub_runtime;

GRANT SELECT (tenant_id, status)
ON TABLE tutorhub.media_participant_sessions TO tutorhub_runtime;

GRANT SELECT (
    tenant_id, idempotency_key, request_fingerprint,
    operation, space_id, actor_user_id
) ON TABLE tutorhub.media_space_mutation_receipts TO tutorhub_runtime;

GRANT INSERT (
    id, tenant_id, source_kind, class_id, source_class_session_id,
    source_series_id, source_occurrence_key, source_study_meeting_id,
    create_idempotency_key, create_request_fingerprint,
    created_by, updated_by, created_at, updated_at
) ON TABLE tutorhub.media_spaces TO tutorhub_runtime;

GRANT UPDATE (
    status, version, locked, updated_by,
    opened_at, opened_by, ended_at, ended_by,
    cancelled_at, cancelled_by, updated_at
) ON TABLE tutorhub.media_spaces TO tutorhub_runtime;

GRANT INSERT (
    id, tenant_id, space_id, attempt_number, provider_room_name,
    created_by, updated_by, created_at, updated_at
) ON TABLE tutorhub.media_room_instances TO tutorhub_runtime;

GRANT UPDATE (
    status, version, updated_by, closing_at, ended_at,
    failed_at, failure_code, updated_at
) ON TABLE tutorhub.media_room_instances TO tutorhub_runtime;

GRANT INSERT (
    tenant_id, idempotency_key, request_fingerprint, operation, space_id,
    result_space_version, result_room_instance_id, actor_user_id, created_at
) ON TABLE tutorhub.media_space_mutation_receipts TO tutorhub_runtime;

COMMIT;
```

P4-02 phải provision delta riêng nếu cần đọc `closing_at`, ghi activation/provider SID,
member/admission hoặc participant lifecycle; không được cấp trước trong P4-01. P4-01 không thể
tạo instance `active|closing`, nên nhánh dùng `COALESCE(closing_at, ...)` chưa reachable; phải cấp
`SELECT(closing_at)` trước khi P4-02 cho phép trạng thái đó.

## 5. Exact ACL probe

Thay role placeholder rồi chạy bằng migration owner. Query đầu phải trả **zero rows**:

```sql
WITH target_tables(table_name) AS (
    VALUES
        ('media_spaces'),
        ('media_room_instances'),
        ('media_space_members'),
        ('media_admission_requests'),
        ('media_participant_sessions'),
        ('media_space_mutation_receipts')
),
expected_select(table_name, column_name) AS (
    VALUES
        ('media_spaces', 'id'),
        ('media_spaces', 'tenant_id'),
        ('media_spaces', 'source_kind'),
        ('media_spaces', 'class_id'),
        ('media_spaces', 'source_class_session_id'),
        ('media_spaces', 'source_series_id'),
        ('media_spaces', 'source_occurrence_key'),
        ('media_spaces', 'source_study_meeting_id'),
        ('media_spaces', 'status'),
        ('media_spaces', 'version'),
        ('media_spaces', 'create_idempotency_key'),
        ('media_spaces', 'create_request_fingerprint'),
        ('media_spaces', 'created_by'),
        ('media_spaces', 'created_at'),
        ('media_spaces', 'updated_at'),
        ('media_room_instances', 'id'),
        ('media_room_instances', 'tenant_id'),
        ('media_room_instances', 'space_id'),
        ('media_room_instances', 'status'),
        ('media_room_instances', 'version'),
        ('media_room_instances', 'created_at'),
        ('media_room_instances', 'updated_at'),
        ('media_space_members', 'tenant_id'),
        ('media_space_members', 'space_id'),
        ('media_space_members', 'user_id'),
        ('media_space_members', 'status'),
        ('media_participant_sessions', 'tenant_id'),
        ('media_participant_sessions', 'status'),
        ('media_space_mutation_receipts', 'tenant_id'),
        ('media_space_mutation_receipts', 'idempotency_key'),
        ('media_space_mutation_receipts', 'request_fingerprint'),
        ('media_space_mutation_receipts', 'operation'),
        ('media_space_mutation_receipts', 'space_id'),
        ('media_space_mutation_receipts', 'actor_user_id')
),
expected_insert(table_name, column_name) AS (
    VALUES
        ('media_spaces', 'id'),
        ('media_spaces', 'tenant_id'),
        ('media_spaces', 'source_kind'),
        ('media_spaces', 'class_id'),
        ('media_spaces', 'source_class_session_id'),
        ('media_spaces', 'source_series_id'),
        ('media_spaces', 'source_occurrence_key'),
        ('media_spaces', 'source_study_meeting_id'),
        ('media_spaces', 'create_idempotency_key'),
        ('media_spaces', 'create_request_fingerprint'),
        ('media_spaces', 'created_by'),
        ('media_spaces', 'updated_by'),
        ('media_spaces', 'created_at'),
        ('media_spaces', 'updated_at'),
        ('media_room_instances', 'id'),
        ('media_room_instances', 'tenant_id'),
        ('media_room_instances', 'space_id'),
        ('media_room_instances', 'attempt_number'),
        ('media_room_instances', 'provider_room_name'),
        ('media_room_instances', 'created_by'),
        ('media_room_instances', 'updated_by'),
        ('media_room_instances', 'created_at'),
        ('media_room_instances', 'updated_at'),
        ('media_space_mutation_receipts', 'tenant_id'),
        ('media_space_mutation_receipts', 'idempotency_key'),
        ('media_space_mutation_receipts', 'request_fingerprint'),
        ('media_space_mutation_receipts', 'operation'),
        ('media_space_mutation_receipts', 'space_id'),
        ('media_space_mutation_receipts', 'result_space_version'),
        ('media_space_mutation_receipts', 'result_room_instance_id'),
        ('media_space_mutation_receipts', 'actor_user_id'),
        ('media_space_mutation_receipts', 'created_at')
),
expected_update(table_name, column_name) AS (
    VALUES
        ('media_spaces', 'status'),
        ('media_spaces', 'version'),
        ('media_spaces', 'locked'),
        ('media_spaces', 'updated_by'),
        ('media_spaces', 'opened_at'),
        ('media_spaces', 'opened_by'),
        ('media_spaces', 'ended_at'),
        ('media_spaces', 'ended_by'),
        ('media_spaces', 'cancelled_at'),
        ('media_spaces', 'cancelled_by'),
        ('media_spaces', 'updated_at'),
        ('media_room_instances', 'status'),
        ('media_room_instances', 'version'),
        ('media_room_instances', 'updated_by'),
        ('media_room_instances', 'closing_at'),
        ('media_room_instances', 'ended_at'),
        ('media_room_instances', 'failed_at'),
        ('media_room_instances', 'failure_code'),
        ('media_room_instances', 'updated_at')
),
table_matrix AS (
    SELECT
        'table'::text AS scope,
        target_tables.table_name,
        NULL::text AS column_name,
        verbs.privilege_type,
        false AS expected,
        has_table_privilege(
            'tutorhub_runtime',
            format('tutorhub.%I', target_tables.table_name),
            verbs.privilege_type
        ) AS actual
    FROM target_tables
    CROSS JOIN (
        VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'),
               ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')
    ) AS verbs(privilege_type)
),
column_matrix AS (
    SELECT
        'column'::text AS scope,
        columns.table_name,
        columns.column_name,
        verbs.privilege_type,
        CASE
            WHEN verbs.privilege_type = 'SELECT' THEN expected_select.column_name IS NOT NULL
            WHEN verbs.privilege_type = 'INSERT' THEN expected_insert.column_name IS NOT NULL
            WHEN verbs.privilege_type = 'UPDATE' THEN expected_update.column_name IS NOT NULL
            ELSE false
        END AS expected,
        has_column_privilege(
            'tutorhub_runtime',
            format('tutorhub.%I', columns.table_name),
            columns.column_name,
            verbs.privilege_type
        ) AS actual
    FROM information_schema.columns AS columns
    JOIN target_tables ON target_tables.table_name = columns.table_name
    CROSS JOIN (
        VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('REFERENCES')
    ) AS verbs(privilege_type)
    LEFT JOIN expected_select
      ON expected_select.table_name = columns.table_name
     AND expected_select.column_name = columns.column_name
    LEFT JOIN expected_insert
      ON expected_insert.table_name = columns.table_name
     AND expected_insert.column_name = columns.column_name
    LEFT JOIN expected_update
      ON expected_update.table_name = columns.table_name
     AND expected_update.column_name = columns.column_name
    WHERE columns.table_schema = 'tutorhub'
)
SELECT scope, table_name, column_name, privilege_type, expected, actual
FROM (
    SELECT * FROM table_matrix
    UNION ALL
    SELECT * FROM column_matrix
) AS matrix
WHERE actual IS DISTINCT FROM expected
ORDER BY scope, table_name, column_name, privilege_type;
```

Role-safety query phải trả một runtime row có `schema_usage=true`; mọi cờ đặc quyền,
schema create, migration-role membership và ownership đều false. Hai `PUBLIC` query phải
zero rows:

```sql
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb,
    rolreplication,
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
              'media_spaces', 'media_room_instances', 'media_space_members',
              'media_admission_requests', 'media_participant_sessions',
              'media_space_mutation_receipts'
          )
          AND relation.relowner = pg_roles.oid
    ) AS owns_target_relation
FROM pg_roles
WHERE rolname = 'tutorhub_runtime';

SELECT table_name, privilege_type
FROM information_schema.table_privileges
WHERE table_schema = 'tutorhub'
  AND table_name IN (
      'media_spaces', 'media_room_instances', 'media_space_members',
      'media_admission_requests', 'media_participant_sessions',
      'media_space_mutation_receipts'
  )
  AND grantee = 'PUBLIC';

SELECT table_name, column_name, privilege_type
FROM information_schema.column_privileges
WHERE table_schema = 'tutorhub'
  AND table_name IN (
      'media_spaces', 'media_room_instances', 'media_space_members',
      'media_admission_requests', 'media_participant_sessions',
      'media_space_mutation_receipts'
  )
  AND grantee = 'PUBLIC';
```

Nếu migration role không phải `current_user`, thay đối số membership bằng exact migration
role đã xác minh trong preflight. Chỉ báo cáo boolean/mismatch count; không ghi credential.

## 6. PostgreSQL, authorization và concurrency gates

Chạy integration bằng exact runtime login sau provisioning; không thay
`DATABASE_POOL_URL` bằng migration URL để làm test giả PASS. Toàn bộ test phải chạy, không
được `SKIP` ACL vì hai role trùng nhau.

1. Migration từ database sạch và disposable `28 -> 29`; rerun giữ `29 false`.
2. Exact ACL/role safety/PUBLIC zero-grant theo mục 4-5.
3. Composite FK/source union, unique one-space-per-source, one-active-instance, lifecycle,
   mutation receipt và tenant cascade.
4. Official class owner/org admin/teacher/co-teacher matrix theo policy review; student không
   được start/end. StudyMeeting chỉ owner hoặc explicit authorized boundary; anonymous denied.
5. Cross-tenant/inaccessible ID conceal `404`; request không nhận tenant/owner/role/provider
   grant từ body.
6. Concurrent create/start/replay chỉ có một space/instance intent; changed payload conflict;
   end/cancel/start race deterministic và quota không double-consume.
7. ClassSession/series/StudyMeeting cancel/archive/mutation barrier giữ lock order, không deadlock
   và không để source terminal trong khi MediaSpace còn open.
8. Parent feature off, child dependency off, quota/storage failure không tạo row; storage
   unavailable trả typed `503`.
9. Audit/outbox chỉ allowlist ID/status/reason bounded; không provider identifier, token, source
   private content, participant data hoặc raw database error.
10. Không có LiveKit request/token/webhook/provider side effect trong toàn bộ P4-01 gate.

## 7. Bằng chứng local và disposable hiện có

Checkpoint 2026-08-09 hiện ghi nhận:

- feature-control/config/HTTP focused Go tests: PASS;
- API client generated-contract check, typecheck, lint và 45 tests: PASS;
- web typecheck/lint và 262 tests: PASS;
- OpenAPI/generated schema formatting và `git diff --check`: PASS;
- hourly quota regression xác nhận `media_space_starts_per_hour` dùng rate-window path: PASS;
- typed `room.create.instant` theo ADR-0021, active-member/cross-tenant policy tests và focused
  policy/media/HTTP tests: PASS;
- full local `pnpm verify` với workspace `GOCACHE`: PASS;
- PostgreSQL integration tag compile và no-env skip (hai URL chủ động unset): PASS, không kết nối DB.
- disposable owner/runtime preflight PASS với migration login direct, runtime login pooled,
  hai role tách biệt, owner không superuser, schema `USAGE/CREATE`, required relations và
  owner authority đều đúng; ledger đầu vào `28 false`;
- forward-only `28 false -> 29 false -> 29 false`: PASS; final ledger sau toàn bộ gate giữ
  `29 false`, không rollback;
- exact runtime schema/column ACL provisioning: PASS; runtime không có schema `CREATE`, broad
  table grant hoặc PUBLIC grant;
- full media PostgreSQL integration bằng exact pooled runtime login: PASS, không `SKIP`
  (`go test -count=1 -tags=integration ./services/core-api/internal/modules/media`, 187.326 giây);
- gate xác nhận authority/tenant concealment, idempotency/concurrency, ClassSession và
  StudyMeeting barrier, feature/quota, privacy audit/outbox và zero provider side effect;
- fresh full local `pnpm verify` sau sửa integration harness: PASS trong 195.8 giây.

Hai lỗi harness phát hiện trong lần chạy đầu đã được sửa tối thiểu: same-key concurrent start
thực sự chạy hai operation; timeout toàn suite tăng lên 5 phút cho Neon; fixture có audit history
được giữ lại vì `audit_events` append-only với tenant FK `RESTRICT`. Không thay đổi production
schema/logic để làm test PASS.

Disposable branch được giữ lại sau acceptance; không xóa trong task này.

## 8. Exact candidate, shared staging và live evidence

- Exact implementation candidate:
  `183ca338557fafd6e8fe502d67763bb2a73d9aa0`.
- GitHub Verify run `31291917865`: PASS; Security run `31291917871`: PASS.
- Cloudflare Pages check `93190579210`, deployment
  `98c7f7fd-e1e6-474c-8cf1-924b6971aa64`: PASS trên exact candidate.
- Shared owner/runtime preflight: PASS; migration login direct, runtime login pooled, role tách
  biệt, owner authority hợp lệ và ledger đầu vào `28 false`.
- Shared forward-only `28 false -> 29 false -> 29 false`: PASS; exact runtime ACL provisioning
  và focused PostgreSQL ACL integration PASS; final ledger giữ `29 false`, không rollback.
- Render deployment `dep-d9rv61ajobas73e9pq90` chạy exact candidate và đạt `live`.
- Direct Render và Pages proxy health/ready/status: 6/6 HTTP `200`, `no-store`.
- Authenticated Organization Admin UI xác nhận cả `classroom_media_rooms` và
  `instant_study_rooms` ở trạng thái off; browser console không có warning/error.
- Anonymous direct/Pages GET/POST media route: 4/4 HTTP `401`, `Cache-Control: no-store`,
  `Referrer-Policy: no-referrer`.
- Read-only shared database probe sau live giữ `media_spaces`, `media_room_instances`,
  `media_space_mutation_receipts` và `media_participant_sessions` đều bằng `0`.
- Không tạo MediaSpace, RoomInstance, participant session, token, webhook hoặc provider room;
  không có provider side effect trong acceptance P4-01.

Full mutation/concurrency/privacy PostgreSQL suite chỉ chạy trên disposable để tránh ghi fixture
audit append-only vào shared. Shared chỉ chạy preflight, forward/idempotency, exact ACL và focused
ACL integration; live probe chỉ đọc.

## 9. Exit decision

P4-01 đã chuyển `IN PROGRESS -> VERIFY` sau disposable forward/ACL/PostgreSQL gates và exact
candidate CI/security; sau đó chuyển `VERIFY -> DONE` khi shared owner preflight/forward/exact ACL,
exact deploy và authenticated/live feature-off acceptance đều PASS. Hai media feature tiếp tục off.
Không rollback; thay đổi schema tiếp theo phải dùng forward migration mới được review.
