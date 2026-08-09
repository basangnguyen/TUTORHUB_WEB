# P4-02 staging acceptance: RoomInstance LiveKit credential và signed webhook binding

## 1. Trạng thái và ranh giới

- Trạng thái hiện tại: `VERIFY`; disposable/provider và exact candidate CI/security đã PASS,
  shared staging/deploy/live acceptance chưa chạy.
- Kiến trúc có thẩm quyền: ADR-0030; migration source:
  `000030_room_instance_livekit_binding`.
- Ledger đầu vào bắt buộc: `29 false`; ledger đích: `30 false`.
- Hai feature `classroom_media_rooms` và `instant_study_rooms` tiếp tục force-off trong toàn bộ
  disposable/shared/live acceptance. P4-02 không phải quyền bật rollout end-user.
- P4-02 chỉ cấp credential cho exact active `RoomInstance`, dùng opaque provider room/participant
  identity, JWT TTL mặc định 5 phút và signed webhook map bằng database binding.
- Route class-wide P1 `POST /api/v1/classes/{class_id}/media-token`, deterministic room parser và
  bảng receipt `livekit_webhook_events` là legacy authority. Candidate P4-02 phải disable route
  legacy và runtime phải có zero grant trên bảng legacy; không tenant nào được dùng đồng thời P1
  và P4 media authority.

Không chạy rollback `000030`. Không forward shared staging, provision shared ACL, deploy hoặc gọi
provider shared trước khi toàn bộ disposable database/provider gate PASS đã được báo cáo và có
quyền tiếp tục. Giữ disposable branch để điều tra cho tới khi P4-02 `DONE`.

## 2. Forward design `000030`

Migration thêm:

1. unique key `(tenant_id, space_id, room_instance_id, id)` trên
   `media_participant_sessions`, để webhook không thể bind participant của instance khác;
2. `media_provider_webhook_receipts` với primary key `(provider_kind, event_id)`, exact composite
   FK tới `media_room_instances` và nullable exact composite FK tới
   `media_participant_sessions`;
3. bounded disposition `applied`, `ignored_stale`, `ignored_mismatch`, `ignored_terminal`,
   `ignored_unknown_participant`, `ignored_unsupported_event`;
4. retention bắt buộc lớn hơn `received_at` và không quá 30 ngày, cùng index retention và
   instance/time;
5. `PUBLIC` revoke, không hardcode environment role trong migration.

Receipt không lưu provider room name/SID, participant identity, JWT, secret, raw payload, SDP,
ICE, IP, email hoặc media content. `participant_session_id` phải nullable vì room-level event hợp
lệ không có participant. Down migration chỉ là recovery artifact đã review; tuyệt đối không chạy
trong acceptance.

Provider call không được giữ PostgreSQL transaction mở. Flow phải commit room/session intent,
gọi narrow LiveKit adapter ngoài transaction, rồi reconcile bằng compare-and-set hoặc signed
webhook. Provider failure trả typed `503 media_provider_unavailable`, không để partial active
instance hoặc duplicate provider room.

## 3. Secret-safe environment và owner preflight

### 3.1 Environment boundary

Dùng file disposable local đã Git-ignore, ví dụ `.env.p4-02-disposable.local`, và nạp trong cùng
PowerShell process chạy gate. Tối thiểu cần:

- `DATABASE_MIGRATION_URL`: direct URL của migration owner;
- `DATABASE_POOL_URL`: pooled URL của exact Core API runtime role;
- `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`: credential của project/provider test đã
  được phép dùng; không dùng production credential.
- `P4_02_DISPOSABLE_CONFIRM=I_UNDERSTAND_P4_02_DISPOSABLE_ONLY`: cờ an toàn không bí mật, bắt
  buộc riêng cho PostgreSQL fixture test. Test phải `SKIP` trước khi đọc hai URL hoặc chạy migration
  nếu giá trị không khớp chính xác.
- `P4_02_OWNER_PREFLIGHT=I_UNDERSTAND_P4_02_OWNER_PREFLIGHT_ONLY`: opt-in riêng cho owner
  preflight; chỉ đặt trong command chạy preflight.
- `P4_02_ACL_PROVISION_CONFIRM=I_UNDERSTAND_P4_02_ACL_PROVISION_DISPOSABLE_ONLY`: opt-in riêng
  cho exact ACL provision trên disposable.
- `P4_02_PROVIDER_SMOKE_CONFIRM=I_UNDERSTAND_P4_02_TEST_PROVIDER_RESOURCE`: opt-in riêng cho
  LiveKit test-provider smoke và exact cleanup.
- `P4_02_SHARED_CONFIRM=I_UNDERSTAND_P4_02_SHARED_STAGING_ONLY`: opt-in riêng cho shared owner
  preflight; không đặt đồng thời với cờ disposable.
- `P4_02_SHARED_ACL_PROVISION_CONFIRM=I_UNDERSTAND_P4_02_ACL_PROVISION_SHARED_STAGING_ONLY`:
  opt-in riêng cho exact ACL provision trên shared sau khi ledger đã đạt `30 false`.

Chỉ báo `present/valid=true|false`; không in URL, hostname, username, password, key, secret, token,
query string hoặc toàn bộ process environment. Không dùng shell tracing, `Get-ChildItem Env:` hay
serialize environment vào artifact. Loader và database/provider command phải nằm trong cùng
process; dọn toàn bộ biến credential/URL và các cờ confirmation khỏi process khi kết thúc.

Preflight chỉ được xác nhận các thuộc tính không bí mật:

- file tồn tại và được Git ignore;
- năm biến credential/URL bắt buộc đều non-empty và cờ của gate đang chạy khớp chính xác;
- migration URL direct, runtime URL pooled và hai login khác nhau;
- LiveKit URL có scheme `wss`, token TTL thuộc `1..15` phút và mặc định là 5 phút;
- config fail closed khi thiếu bất kỳ LiveKit prerequisite nào;
- disposable branch không phải shared/production và không có Core API/worker đang dùng.

Không ghi giá trị biến vào tài liệu hoặc command output. Nếu credential LiveKit riêng chưa có,
dừng tại đây và yêu cầu người quản trị thêm vào local ignored file/provider secret store.

### 3.2 Owner preflight

Chạy bằng migration login trên disposable trước forward:

```sql
WITH required_relations(relname) AS (
    VALUES
        ('media_spaces'),
        ('media_room_instances'),
        ('media_participant_sessions')
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
    JOIN pg_class AS relation ON relation.relname = required_relations.relname
    JOIN pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
     AND namespace.nspname = 'tutorhub'
    CROSS JOIN actor
)
SELECT
    NOT actor.rolsuper AS not_superuser,
    has_schema_privilege(current_user, 'tutorhub', 'USAGE') AS schema_usage,
    has_schema_privilege(current_user, 'tutorhub', 'CREATE') AS schema_create,
    owned_scope.relation_count = 3 AS required_relations_present,
    owned_scope.owner_authority
FROM actor
CROSS JOIN owned_scope;

SELECT version, dirty FROM public.tutorhub_schema_migrations;
```

PASS yêu cầu ledger `29 false`; năm boolean đều `true`. Nếu version khác, dirty, URL/role không
đúng hoặc owner authority thiếu, dừng và giữ disposable; không tự sửa ledger hay nới ACL.

Chuỗi forward bắt buộc:

```text
29 false -> 30 false -> 30 false
```

Trong cùng process đã nạp secret, chạy `db:version`, `db:migrate`, `db:version`, chạy lại
`db:migrate`, rồi `db:version`. Không chạy down/rollback. Xác nhận table, constraint, indexes và
`PUBLIC` revoke của `000030` trước khi provision runtime.

## 4. Exact Core API runtime ACL

ACL cuối là hợp của P4-01 lifecycle queries và P4-02 credential/webhook queries. Không cấp
table-level `SELECT`/`INSERT`/`UPDATE`; không `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER`, schema
`CREATE`, ownership, migration membership hoặc maintenance membership.

| Relation | Column SELECT | Column INSERT | Column UPDATE | Table/Delete |
| --- | --- | --- | --- | --- |
| `media_spaces` | P4-01 projection + `lobby_enabled,locked` | P4-01 exact | P4-01 exact | none |
| `media_room_instances` | exact lifecycle/provider binding | P4-01 intent | lifecycle + provider activation/SID | none |
| `media_space_members` | `tenant_id,space_id,user_id,status` | none | none | none |
| `media_admission_requests` | none trong P4-02 | none | none | none |
| `media_participant_sessions` | exact authority/lifecycle row | exact join session | exact webhook/CAS lifecycle | none |
| `media_space_mutation_receipts` | P4-01 replay proof | P4-01 exact | none | none |
| `media_provider_webhook_receipts` | exact bounded receipt | exact bounded receipt | none | none |
| `livekit_webhook_events` legacy | none | none | none | none |

`rate_limit_windows` giữ exact `SELECT/INSERT/UPDATE` đã provision cho shared rate-limit service;
không cấp `DELETE` hoặc quyền mới trong P4-02. Join credential dùng key
tenant/actor/session/action, baseline tối đa 30 lần/10 phút và hard bound 60. Storage failure phải
fail closed `503`, không mint JWT.

Thay `tutorhub_runtime` bằng exact runtime role đã xác minh. Chạy block sau bằng migration owner
sau khi ledger ở `30 false`:

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
    tutorhub.media_space_mutation_receipts,
    tutorhub.media_provider_webhook_receipts,
    tutorhub.livekit_webhook_events
FROM tutorhub_runtime;

DO $p4_02_acl$
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
        'media_space_mutation_receipts',
        'media_provider_webhook_receipts',
        'livekit_webhook_events'
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
        EXECUTE format(
            'REVOKE SELECT (%1$s), INSERT (%1$s), UPDATE (%1$s), REFERENCES (%1$s) '
            'ON TABLE tutorhub.%2$I FROM PUBLIC',
            column_list,
            relation_name
        );
    END LOOP;
END
$p4_02_acl$;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.media_spaces,
    tutorhub.media_room_instances,
    tutorhub.media_space_members,
    tutorhub.media_admission_requests,
    tutorhub.media_participant_sessions,
    tutorhub.media_space_mutation_receipts,
    tutorhub.media_provider_webhook_receipts,
    tutorhub.livekit_webhook_events
FROM PUBLIC;

GRANT SELECT (
    id, tenant_id, source_kind, class_id, source_class_session_id,
    source_series_id, source_occurrence_key, source_study_meeting_id,
    status, version, lobby_enabled, locked,
    create_idempotency_key, create_request_fingerprint,
    created_by, created_at, updated_at
) ON TABLE tutorhub.media_spaces TO tutorhub_runtime;

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

GRANT SELECT (
    id, tenant_id, space_id, attempt_number, status, version, provider_kind,
    provider_room_name, provider_room_sid, activated_at, closing_at,
    ended_at, failed_at, created_at, updated_at
) ON TABLE tutorhub.media_room_instances TO tutorhub_runtime;

GRANT INSERT (
    id, tenant_id, space_id, attempt_number, provider_room_name,
    created_by, updated_by, created_at, updated_at
) ON TABLE tutorhub.media_room_instances TO tutorhub_runtime;

GRANT UPDATE (
    status, version, provider_room_sid, activated_at, closing_at,
    ended_at, failed_at, failure_code, updated_by, updated_at
) ON TABLE tutorhub.media_room_instances TO tutorhub_runtime;

GRANT SELECT (tenant_id, space_id, user_id, status)
ON TABLE tutorhub.media_space_members TO tutorhub_runtime;

GRANT SELECT (
    id, tenant_id, space_id, room_instance_id, user_id, admission_request_id,
    join_attempt_id, provider_participant_identity, instance_role, status,
    capacity_reserved, version, admitted_at, joining_at, connected_at,
    reconnecting_at, terminal_at, removed_by, failure_code, created_at, updated_at
) ON TABLE tutorhub.media_participant_sessions TO tutorhub_runtime;

GRANT INSERT (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, instance_role, status, capacity_reserved,
    admitted_at, joining_at, created_at, updated_at
) ON TABLE tutorhub.media_participant_sessions TO tutorhub_runtime;

GRANT UPDATE (
    instance_role, status, version, capacity_reserved, connected_at, reconnecting_at,
    terminal_at, failure_code, updated_at
) ON TABLE tutorhub.media_participant_sessions TO tutorhub_runtime;

GRANT SELECT (
    tenant_id, idempotency_key, request_fingerprint,
    operation, space_id, actor_user_id
) ON TABLE tutorhub.media_space_mutation_receipts TO tutorhub_runtime;

GRANT INSERT (
    tenant_id, idempotency_key, request_fingerprint, operation, space_id,
    result_space_version, result_room_instance_id, actor_user_id, created_at
) ON TABLE tutorhub.media_space_mutation_receipts TO tutorhub_runtime;

GRANT SELECT (
    provider_kind, event_id, tenant_id, space_id, room_instance_id,
    participant_session_id, event_type, disposition, occurred_at,
    received_at, retention_until
) ON TABLE tutorhub.media_provider_webhook_receipts TO tutorhub_runtime;

GRANT INSERT (
    provider_kind, event_id, tenant_id, space_id, room_instance_id,
    participant_session_id, event_type, disposition, occurred_at,
    received_at, retention_until
) ON TABLE tutorhub.media_provider_webhook_receipts TO tutorhub_runtime;

COMMIT;
```

Trước disposable, static SQL/repository audit phải xác nhận mọi production query chỉ dùng đúng
allowlist trên. Nếu query mới cần column khác, cập nhật runbook và review security trước khi grant;
không cấp broad table privilege để làm test PASS.

## 5. Exact ACL probe

Chạy bằng migration owner. Query broad privilege, legacy và PUBLIC sau phải trả zero rows:

```sql
WITH targets(table_name) AS (
    VALUES
        ('media_spaces'), ('media_room_instances'), ('media_space_members'),
        ('media_admission_requests'), ('media_participant_sessions'),
        ('media_space_mutation_receipts'), ('media_provider_webhook_receipts'),
        ('livekit_webhook_events')
)
SELECT targets.table_name, verbs.privilege_type
FROM targets
CROSS JOIN (
    VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'),
           ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')
) AS verbs(privilege_type)
WHERE has_table_privilege(
    'tutorhub_runtime',
    format('tutorhub.%I', targets.table_name),
    verbs.privilege_type
);

SELECT table_name, column_name, privilege_type
FROM information_schema.column_privileges
WHERE table_schema = 'tutorhub'
  AND table_name = 'livekit_webhook_events'
  AND grantee = 'tutorhub_runtime';

SELECT table_name, privilege_type
FROM information_schema.table_privileges
WHERE table_schema = 'tutorhub'
  AND table_name IN (
      'media_spaces', 'media_room_instances', 'media_space_members',
      'media_admission_requests', 'media_participant_sessions',
      'media_space_mutation_receipts', 'media_provider_webhook_receipts',
      'livekit_webhook_events'
  )
  AND grantee = 'PUBLIC';

SELECT table_name, column_name, privilege_type
FROM information_schema.column_privileges
WHERE table_schema = 'tutorhub'
  AND table_name IN (
      'media_spaces', 'media_room_instances', 'media_space_members',
      'media_admission_requests', 'media_participant_sessions',
      'media_space_mutation_receipts', 'media_provider_webhook_receipts',
      'livekit_webhook_events'
  )
  AND grantee = 'PUBLIC';
```

Role-safety query phải có `schema_usage=true`; các cờ đặc quyền, schema create, migration-role
membership và ownership target đều false. Query exact column matrix sau cũng phải trả zero rows:

```sql
WITH expected_sets(table_name, privilege_type, column_names) AS (
    VALUES
        ('media_spaces', 'SELECT', ARRAY[
            'id','tenant_id','source_kind','class_id','source_class_session_id',
            'source_series_id','source_occurrence_key','source_study_meeting_id',
            'status','version','lobby_enabled','locked','create_idempotency_key',
            'create_request_fingerprint','created_by','created_at','updated_at'
        ]::text[]),
        ('media_spaces', 'INSERT', ARRAY[
            'id','tenant_id','source_kind','class_id','source_class_session_id',
            'source_series_id','source_occurrence_key','source_study_meeting_id',
            'create_idempotency_key','create_request_fingerprint','created_by',
            'updated_by','created_at','updated_at'
        ]::text[]),
        ('media_spaces', 'UPDATE', ARRAY[
            'status','version','locked','updated_by','opened_at','opened_by',
            'ended_at','ended_by','cancelled_at','cancelled_by','updated_at'
        ]::text[]),
        ('media_room_instances', 'SELECT', ARRAY[
            'id','tenant_id','space_id','attempt_number','status','version','provider_kind',
            'provider_room_name','provider_room_sid','activated_at','closing_at',
            'ended_at','failed_at','created_at','updated_at'
        ]::text[]),
        ('media_room_instances', 'INSERT', ARRAY[
            'id','tenant_id','space_id','attempt_number','provider_room_name',
            'created_by','updated_by','created_at','updated_at'
        ]::text[]),
        ('media_room_instances', 'UPDATE', ARRAY[
            'status','version','provider_room_sid','activated_at','closing_at',
            'ended_at','failed_at','failure_code','updated_by','updated_at'
        ]::text[]),
        ('media_space_members', 'SELECT', ARRAY[
            'tenant_id','space_id','user_id','status'
        ]::text[]),
        ('media_participant_sessions', 'SELECT', ARRAY[
            'id','tenant_id','space_id','room_instance_id','user_id',
            'admission_request_id','join_attempt_id','provider_participant_identity',
            'instance_role','status','capacity_reserved','version','admitted_at',
            'joining_at','connected_at','reconnecting_at','terminal_at','removed_by',
            'failure_code','created_at','updated_at'
        ]::text[]),
        ('media_participant_sessions', 'INSERT', ARRAY[
            'id','tenant_id','space_id','room_instance_id','user_id','join_attempt_id',
            'provider_participant_identity','instance_role','status','capacity_reserved',
            'admitted_at','joining_at','created_at','updated_at'
        ]::text[]),
        ('media_participant_sessions', 'UPDATE', ARRAY[
            'instance_role','status','version','capacity_reserved','connected_at','reconnecting_at',
            'terminal_at','failure_code','updated_at'
        ]::text[]),
        ('media_space_mutation_receipts', 'SELECT', ARRAY[
            'tenant_id','idempotency_key','request_fingerprint','operation','space_id',
            'actor_user_id'
        ]::text[]),
        ('media_space_mutation_receipts', 'INSERT', ARRAY[
            'tenant_id','idempotency_key','request_fingerprint','operation','space_id',
            'result_space_version','result_room_instance_id','actor_user_id','created_at'
        ]::text[]),
        ('media_provider_webhook_receipts', 'SELECT', ARRAY[
            'provider_kind','event_id','tenant_id','space_id','room_instance_id',
            'participant_session_id','event_type','disposition','occurred_at',
            'received_at','retention_until'
        ]::text[]),
        ('media_provider_webhook_receipts', 'INSERT', ARRAY[
            'provider_kind','event_id','tenant_id','space_id','room_instance_id',
            'participant_session_id','event_type','disposition','occurred_at',
            'received_at','retention_until'
        ]::text[])
),
expected AS (
    SELECT table_name, privilege_type, unnest(column_names) AS column_name
    FROM expected_sets
),
targets(table_name) AS (
    VALUES
        ('media_spaces'), ('media_room_instances'), ('media_space_members'),
        ('media_admission_requests'), ('media_participant_sessions'),
        ('media_space_mutation_receipts'), ('media_provider_webhook_receipts'),
        ('livekit_webhook_events')
),
actual AS (
    SELECT table_name, privilege_type, column_name
    FROM information_schema.column_privileges
    WHERE table_schema = 'tutorhub'
      AND grantee = 'tutorhub_runtime'
      AND table_name IN (SELECT table_name FROM targets)
      AND privilege_type IN ('SELECT', 'INSERT', 'UPDATE', 'REFERENCES')
)
SELECT
    CASE WHEN expected.table_name IS NULL THEN 'unexpected' ELSE 'missing' END AS mismatch,
    COALESCE(expected.table_name, actual.table_name) AS table_name,
    COALESCE(expected.column_name, actual.column_name) AS column_name,
    COALESCE(expected.privilege_type, actual.privilege_type) AS privilege_type
FROM expected
FULL JOIN actual USING (table_name, column_name, privilege_type)
WHERE expected.table_name IS NULL OR actual.table_name IS NULL
ORDER BY table_name, column_name, privilege_type;
```

Static SQL/repository gate phải dùng cùng allowlist này để phát hiện query drift trước khi chạm DB.
Chỉ báo boolean/mismatch count, không log role password hoặc URL.

## 6. Disposable PostgreSQL integration gates

Chạy bằng exact pooled runtime login sau ACL provisioning; không thay runtime URL bằng owner URL và
không chấp nhận `SKIP` vì hai role trùng nhau.

1. Clean migration và disposable `29 -> 30`; rerun giữ `30 false`.
2. Exact ACL, runtime role safety, PUBLIC zero-grant và legacy zero-grant.
3. Receipt PK làm signed duplicate idempotent; concurrent duplicate chỉ có một receipt/domain CAS.
4. Exact composite RoomInstance FK; participant receipt nullable, nhưng participant khác tenant,
   space hoặc instance phải fail.
5. Unknown provider room/SID, unknown participant, malformed/mismatched signed event không tạo
   tenant/space/session và không mutate lifecycle.
6. Out-of-order/stale/terminal webhook dùng bounded disposition, không hồi sinh terminal session
   hoặc hạ version; replay sau kết quả đầu giữ nguyên state.
7. Participant lifecycle CAS chỉ đi forward; terminal state không quay lại connected. Concurrent
   connect/leave/remove có một kết quả hợp lệ, không double-release capacity.
8. One-active-instance/provider SID uniqueness và concurrent provider reconcile không tạo duplicate
   active room.
9. Tenant isolation/concealment: foreign space/instance/session/event không đọc hoặc mutate; API
   trả `404` phù hợp, không lộ existence.
10. Room/space cascade dọn receipt và participant binding đúng FK; retention constraint/index
    giữ receipt tối đa 30 ngày. P4-02 chưa cấp runtime purge/delete.
11. PostgreSQL/provider error được map typed và redacted; audit/outbox/log không chứa token, secret,
    provider room/SID, participant identity, raw payload hoặc raw database error.
12. Rate-limit storage unavailable trả `503`; không insert session, activate room hoặc mint token.

Chạy focused migration/media PostgreSQL integration và full relevant PostgreSQL integration theo
repository scripts. Ghi command, duration, PASS/FAIL/SKIP count và final ledger, nhưng không ghi
connection error có URL/credential. Focused P4-02 fixture chỉ hợp lệ khi cờ confirmation ở mục 3.1
được nạp trong cùng process; `SKIP` vì thiếu cờ không phải acceptance PASS.

## 7. Credential, provider và signed webhook gates

Provider gates chỉ chạy sau mục 3-6 PASS trên disposable:

1. Request không nhận/echo tenant, provider room, participant identity, role hoặc grant từ client;
   active tenant/session là authority duy nhất.
2. Inactive, provisioning chưa reconcile, locked, ended, failed, foreign, archived/cancelled source,
   revoked membership/invite hoặc denied admission không mint credential.
3. Credential bind exact active RoomInstance và idempotent join attempt/session. Same key/same
   fingerprint reuse; changed payload conflict; concurrent request không tạo hai sessions.
4. JWT TTL nằm trong `1..15` phút, mặc định 5 phút; exact opaque room/participant identity không
   encode tenant/class/user/session. Token chỉ ở response/component memory.
5. `CanSubscribe` chỉ true khi join hợp lệ; camera/mic publish và screen-share grant được tính riêng;
   `CanPublishData=false`. Student/attendee không tự nhận screen share nếu policy không cấp.
6. Response credential luôn `Cache-Control: no-store`, `Referrer-Policy: no-referrer`; reload,
   workspace/principal/source change không phục hồi token cũ.
7. Narrow provider adapter create/reuse room idempotent. Timeout/5xx trả
   `503 media_provider_unavailable`; retry không tạo duplicate room và không để partial active row.
8. Official LiveKit verifier kiểm signature/body hash trước database lookup. Unsigned, malformed hoặc
   wrong-key webhook bị từ chối và không ghi receipt/mutate state.
9. Signed duplicate trả success idempotent; known room/SID map bằng database, không parse business
   IDs từ room name. Unknown/stale/out-of-order event tuân mục 6.
10. Secret, JWT, provider identifiers và participant identity không xuất hiện trong log, audit,
    metric label, trace, error response, client bundle, CI artifact hoặc screenshot.
11. Route legacy class-wide bị disabled; request không gọi issuer/provider và runtime không ghi
    `livekit_webhook_events`. Compatibility test chứng minh P1/P4 không thể cùng active.
12. Dọn provider test room/participant bằng supported adapter sau gate, không xóa disposable DB
    branch và không dùng destructive wildcard.

## 8. Verification và disposable report

Trước database/provider gate chạy focused unit, policy, HTTP, config, OpenAPI/generated-client,
security bundle và full local `pnpm verify`. Tối thiểu phải có:

- token grant matrix cho organization admin/teacher/class owner/co-teacher/TA/student/guest;
- separate `media.publish`, `media.share_screen`, subscribe và `CanPublishData=false` assertions;
- feature/prerequisite fail-closed, rate-limit/idempotency/concurrency tests;
- signed webhook verification, duplicate/out-of-order/unknown/mismatch/privacy tests;
- legacy route disabled and no dual-authority tests;
- migration 000030 exact schema/privacy/down-scope tests;
- PostgreSQL integration compile/no-env skip locally và real runtime execution trên disposable.

Disposable report phải gồm, chỉ ở dạng non-secret:

1. candidate commit/diff identity và danh sách file liên quan;
2. local focused/full verify PASS/FAIL;
3. owner/runtime preflight boolean và xác nhận direct/pooled/different-role;
4. `29 false -> 30 false -> 30 false`, final ledger `30 false`, no rollback;
5. exact ACL mismatch `0`, PUBLIC/legacy broad grant `0`;
6. PostgreSQL integration PASS, không ACL skip;
7. provider credential/webhook gate PASS, số room test đã cleanup và không secret leakage;
8. residual risk hoặc gate chưa chạy.

### 8.1 Kết quả disposable/test-provider ngày 2026-08-09

- Exact implementation candidate `f622e5f4b4c5efd6b877914e35aff16d765fba53` đã push lên
  `main`. Full local `pnpm verify` PASS trong 182.5 giây; rerun sau khi bỏ log endpoint LiveKit
  tiếp tục PASS trong 26.2 giây với cache.
- Owner preflight direct/pooled/different-role PASS, chỉ báo boolean không bí mật.
- Forward-only và rerun idempotent PASS: `29 false -> 30 false -> 30 false`; final ledger
  `30 false`, `rollback_run=false`.
- Exact runtime ACL provision/probe PASS; mismatch, `PUBLIC` broad grant và legacy grant đều bằng
  `0`. Gate phát hiện và candidate đã sửa allowlist thiếu `SELECT attempt_number` cho truy vấn latest
  RoomInstance, sau đó provision/probe lại PASS.
- Focused RoomInstance credential/webhook PostgreSQL gate PASS trong 223.7 giây; lifecycle exact
  ACL, authority, quota, concurrency và privacy suite PASS trong 200.2 giây, không ACL skip.
- LiveKit test-provider smoke PASS trong 29.8 giây: create/reuse một opaque room, mint token 5 phút
  least-privilege, real connect/disconnect và exact delete/list cleanup. Official verifier synthetic
  valid/wrong-key/body-tamper PASS; provider-emitted webhook tới public Core API vẫn thuộc live
  acceptance sau deploy.
- Exact candidate GitHub Verify run `31303424310` PASS trong 3 phút 13 giây; Security run
  `31303424335` PASS trong 2 phút 55 giây, gồm secret scan, CodeQL JavaScript/TypeScript + Go,
  repository vulnerability scan và Core API container scan. Dependency Review được skip đúng
  contract vì đây là push event, không phải pull request.
- Không log secret/URL/token, không chạm shared staging, không deploy và giữ disposable branch.
  P4-02 chuyển `IN PROGRESS -> VERIFY`; chỉ shared forward/ACL, deploy và live acceptance còn mở.

Nếu bất kỳ gate FAIL: dừng, giữ disposable, không rollback/nới ACL và không chuyển shared. Sửa bằng
candidate/forward migration mới phù hợp rồi chạy lại từ gate liên quan.

## 9. Shared staging, deploy và exit decision

Chỉ sau khi disposable report được review và có quyền tiếp tục mới được:

1. khóa exact candidate và chạy GitHub Verify/Security trên commit đó;
2. shared owner/runtime secret-safe preflight;
3. shared forward-only `29 false -> 30 false -> 30 false`;
4. provision exact shared ACL và chạy focused read/ACL integration, không chạy destructive fixture;
5. deploy exact candidate với hai media feature vẫn force-off;
6. live health/readiness, anonymous auth/no-store, legacy-disabled, authenticated capability-off và
   signed provider smoke được phép;
7. read-only shared probe xác nhận không có unexpected room/session/receipt/provider side effect.

Shared preflight chỉ chạy
`TestPostgresP402SharedOwnerPreflight` với `P4_02_SHARED_CONFIRM` và
`P4_02_OWNER_PREFLIGHT`. Exact ACL provision chỉ chạy
`TestProvisionPostgresMediaLifecycleRuntimeExactACLShared` với shared confirmation riêng; không
được giả mạo hai cờ `DISPOSABLE_ONLY`. Sau provisioning, chỉ chạy read-only
`TestPostgresMediaLifecycleRuntimeExactACL`; không chạy fixture authority/concurrency trên shared.

P4-02 chuyển `IN PROGRESS -> VERIFY` sau khi disposable forward/ACL/PostgreSQL/provider gates và
exact candidate CI/security đều PASS. Chỉ chuyển `VERIFY -> DONE` sau shared forward/exact ACL,
exact deploy và live feature-off/provider acceptance PASS. Không rollback; mọi schema/security fix
sau `000030` phải là forward migration mới đã review.
