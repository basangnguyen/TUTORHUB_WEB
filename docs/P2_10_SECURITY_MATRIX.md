# P2-10 Tenant isolation và IDOR security matrix

## 1. Mục đích và trạng thái tài liệu

Tài liệu này là đặc tả thực thi cho P2-10. Nó định nghĩa bề mặt API cần kiểm tra,
ma trận actor/resource, mã phản hồi kỳ vọng và danh sách test phải bổ sung để chốt
tenant isolation/IDOR của Phase 2.

**Trạng thái 2026-07-22: VERIFY.** Implementation và local verification đã hoàn tất;
PostgreSQL runtime matrix cùng Security workflow trên head mới vẫn là gate bắt buộc
trước khi P2-10 được chuyển `DONE`. Không suy ra trạng thái CI green từ kết quả local.

Phạm vi:

- Authenticated session, active workspace và membership hiện hành.
- Profile/linked identity chỉ thuộc current user.
- Workspace, feature control, membership invitation và audit.
- Class lifecycle, ownership, enrollment, roster và class invite code.
- Media credential, media telemetry và LiveKit webhook boundary.
- Exact-ID access, cross-tenant/cross-class access, stale session, mass assignment,
  cursor tamper, invitation abuse và fuzz input.

Ngoài phạm vi:

- Penetration test hạ tầng provider ngoài các contract HTTP của TutorHub.
- Kiểm tra secret thực tế hoặc đọc `.env*.local`.
- Secure Exam/native companion và module Phase 3 trở đi.

Tài liệu phải được đọc cùng:

- `docs/adr/0013-shared-organization-class-authorization-policy.md`
- `docs/adr/0014-append-only-tenant-audit-log.md`
- `docs/adr/0015-server-evaluated-feature-controls-and-quotas.md`
- `docs/SECURITY_BASELINE.md`
- `docs/DOMAIN_MODEL.md`
- `docs/PHASE_2_BACKLOG.md`

## 2. Actor và thuật ngữ

| Actor | Điều kiện |
|---|---|
| Anonymous | Không có TutorHub session hợp lệ. |
| No-active-tenant | Đã đăng nhập nhưng không có active tenant trong principal. Có thể là tài khoản mới hoặc membership active tenant cũ vừa bị revoke. |
| Guest | Active organization membership role `guest`, không mặc nhiên có class access. |
| Student | Active organization membership role `student`, không mặc nhiên có class access. |
| TA | Có active class enrollment role `teaching_assistant`. |
| Teacher | Active organization membership role `teacher`; là global class manager trong active tenant. |
| Co-teacher | Có active class enrollment role `co_teacher`. |
| Owner | `classes.owner_user_id` là current actor; owner là class role suy ra từ resource, không phải enrollment role được client gán. |
| Org admin | Active organization membership role `org_admin`. |

Actor có thể mang đồng thời organization role và class role. Quyền hiệu lực là hợp
các permission hợp lệ nhưng vẫn bị giới hạn bởi active tenant, resource state và
roster hierarchy. Không dùng role hoặc tenant do request body cung cấp để authorize.

Các ký hiệu trong ma trận:

- `SELF`: chỉ current user.
- `ACTIVE-T`: chỉ active tenant.
- `OWN/ENR`: class actor sở hữu hoặc có active enrollment.
- `ALL-T`: mọi class trong active tenant mà organization role quản lý.
- `TOKEN`: bearer capability hợp lệ, canonical và chưa terminal.
- `DENY`: không được phép.

## 3. Security invariants bắt buộc

| ID | Invariant |
|---|---|
| P2-10-I01 | Mọi tenant/class query nghiệp vụ phải có predicate `tenant_id` do server lấy từ authenticated active tenant. |
| P2-10-I02 | Path/body/query ID không được thay đổi active tenant hoặc tạo quyền mới. |
| P2-10-I03 | Exact foreign ID và random inaccessible ID phải không phân biệt được; không lộ tên tenant, class title, email hoặc foreign ID trong body. |
| P2-10-I04 | Same-tenant permission denial trả forbidden; cross-tenant/cross-class resource scope được conceal thành not found. |
| P2-10-I05 | Mutation phải reauthorize membership và resource state hiện tại trong transaction khi repository có lock/CAS boundary. |
| P2-10-I06 | Membership revoke/demotion có hiệu lực ở request kế tiếp; không giữ quyền từ principal/claim cũ. |
| P2-10-I07 | Workspace switch rotate session token, CSRF token và context version; token cũ không tiếp tục truy cập được. |
| P2-10-I08 | Profile và linked identity chỉ được đọc/sửa/xóa bởi chính current user. |
| P2-10-I09 | Client không thể gán `tenant_id`, owner, organization role, audit actor, state hoặc quota source ngoài request schema. |
| P2-10-I10 | Enrollment/roster mutation tuân thủ hierarchy, không self-mutate, không mutate owner/peer/higher actor; bulk xử lý từng item độc lập và không để target foreign tạo mutation. |
| P2-10-I11 | Membership/class invite token chỉ lưu purpose-bound digest; malformed, wrong scope và terminal token không tạo state change hoặc enumeration oracle. |
| P2-10-I12 | Cursor luôn được xem là untrusted input; cursor chỉ điều khiển pagination anchor, không được nới tenant/class/actor predicate. |
| P2-10-I13 | Audit query luôn tenant-scoped, chỉ org admin được đọc, và cursor bị bind với tenant cùng toàn bộ filter. |
| P2-10-I14 | Media credential chỉ phát hành sau class authorization hiện hành; frontend không tự quyết định room, tenant, participant identity hoặc publish permission. |
| P2-10-I15 | LiveKit webhook chỉ được nhận sau signature verification; `(tenant_id, class_id)` của room vẫn phải thỏa composite FK. |

## 4. Route inventory Phase 2

### 4.1 Public/auth/self routes

| Method | Route | Boundary cần test |
|---|---|---|
| GET | `/api/v1/status` | Public, không trả tenant/private data. |
| GET | `/api/v1/auth/login` | Public login initiation; state/nonce/browser binding do server tạo. |
| GET | `/api/v1/auth/callback` | Public callback nhưng state/nonce/binding bắt buộc hợp lệ. |
| GET | `/api/v1/auth/csrf` | Cần session; không cache. |
| POST | `/api/v1/auth/logout` | Cần session + CSRF; revoke server-side. |
| GET | `/api/v1/me` | Current principal được reload từ DB. |
| GET, PATCH | `/api/v1/me/profile` | `SELF`; PATCH cần CSRF và strict JSON. |
| GET | `/api/v1/me/identities` | `SELF`. |
| POST | `/api/v1/me/identities/link` | `SELF`, CSRF và recent authentication. |
| DELETE | `/api/v1/me/identities/{identity_id}` | `SELF`, CSRF, recent authentication; foreign exact ID phải concealed. |

### 4.2 Workspace, feature control, invitation và audit

| Method | Route | Boundary cần test |
|---|---|---|
| GET, POST | `/api/v1/tenants` | GET chỉ membership active của current user; POST chỉ bootstrap account chưa có membership hoặc active org admin. |
| GET, PATCH | `/api/v1/tenants/{tenant_id}` | Path tenant phải bằng active tenant; PATCH chỉ org admin và cần CSRF/CAS. |
| POST | `/api/v1/tenants/{tenant_id}/archive` | Chỉ active org admin, CSRF/CAS; session được rotate. |
| GET | `/api/v1/tenants/{tenant_id}/capabilities` | Mọi active member cùng tenant có thể đọc effective capabilities. |
| PUT | `/api/v1/tenants/{tenant_id}/feature-controls` | Chỉ active org admin; strict complete aggregate, CSRF/CAS. |
| PUT | `/api/v1/session/active-tenant` | Chỉ target active membership; rotate token/CSRF/context. |
| GET, POST | `/api/v1/tenants/{tenant_id}/invitations` | Chỉ active org admin; POST cần CSRF, quota/rate policy. |
| POST | `/api/v1/tenants/{tenant_id}/invitations/{invitation_id}/revoke` | Chỉ active org admin; exact tenant + invitation scope. |
| POST | `/api/v1/membership-invitations/preview` | Public `TOKEN`; rate limited, no-store/no-referrer. |
| POST | `/api/v1/membership-invitations/accept` | Session + CSRF + exact verified linked email; tenant do server resolve từ token. |
| GET | `/api/v1/tenants/{tenant_id}/audit-events` | Chỉ active org admin; tenant/filter-bound cursor. |

### 4.3 Class, enrollment và roster

| Method | Route | Boundary cần test |
|---|---|---|
| GET, POST | `/api/v1/classes` | List theo active tenant/visibility; create chỉ org admin hoặc teacher. |
| GET, PATCH | `/api/v1/classes/{class_id}` | Exact class lookup tenant-scoped; PATCH cần class update permission + CSRF/CAS. |
| POST | `/api/v1/classes/{class_id}/archive` | Org admin hoặc owner; CSRF/CAS. Teacher chỉ có quyền này khi đồng thời là owner. |
| POST | `/api/v1/classes/{class_id}/restore` | Org admin hoặc owner; CSRF/CAS. |
| POST | `/api/v1/classes/{class_id}/transfer-ownership` | Org admin hoặc owner, recent authentication, target active same-tenant member. |
| POST | `/api/v1/classes/{class_id}/enrollments` | Enrollment manager + hierarchy; target phải là active same-tenant member. |
| POST | `/api/v1/classes/{class_id}/enrollments/{user_id}/suspend` | Manager + hierarchy; exact tenant/class/user scope. |
| POST | `/api/v1/classes/{class_id}/enrollments/{user_id}/remove` | Manager + hierarchy; exact tenant/class/user scope. |
| GET | `/api/v1/classes/{class_id}/roster` | Enrollment manager; tenant/class/filter-bound pagination. |
| PATCH | `/api/v1/classes/{class_id}/roster/{user_id}` | Manager + hierarchy; strict role allowlist. |
| POST | `/api/v1/classes/{class_id}/roster/bulk` | 1-50 unique target; mỗi item được xử lý độc lập, trả partial-failure summary và áp hierarchy cho từng target. |
| GET, POST | `/api/v1/classes/{class_id}/invite-codes` | Enrollment manager; POST cần CSRF, feature/quota/rate guards. |
| POST | `/api/v1/classes/{class_id}/invite-codes/{code_id}/revoke` | Enrollment manager; exact tenant/class/code scope. |
| POST | `/api/v1/class-invitations/join` | Active tenant member + canonical `TOKEN`; token lookup vẫn bind active tenant. |
| POST | `/api/v1/classes/{class_id}/leave` | Active enrollee; owner không được bỏ class đang sở hữu. |

### 4.4 Media/service boundary

| Method | Route | Boundary cần test |
|---|---|---|
| POST | `/api/v1/classes/{class_id}/media-token` | Session + CSRF, active class access và `session.join`; room/identity/grant do server tạo. |
| POST | `/api/v1/classes/{class_id}/media-events` | Session + CSRF, active class access; strict telemetry allowlist. |
| POST | `/api/v1/webhooks/livekit` | Không dùng user session; chỉ signed LiveKit webhook, content type đúng và idempotent event ID. |

## 5. Actor/resource permission matrix

| Resource/action | Anonymous | No-active-tenant | Guest | Student | TA | Teacher | Co-teacher | Owner | Org admin |
|---|---|---|---|---|---|---|---|---|---|
| Self profile/identities | DENY | SELF | SELF | SELF | SELF | SELF | SELF | SELF | SELF |
| List own workspaces | DENY | own memberships | own memberships | own memberships | own memberships | own memberships | own memberships | own memberships | own memberships |
| Create first workspace | DENY | ALLOW nếu chưa có active membership nào | DENY | DENY | DENY | DENY | DENY | DENY | N/A |
| Create additional workspace | DENY | DENY | DENY | DENY | DENY | DENY | DENY | DENY | ALLOW từ active admin context |
| Switch workspace | DENY | active membership đích | active membership đích | active membership đích | active membership đích | active membership đích | active membership đích | active membership đích | active membership đích |
| Read workspace/capabilities | DENY | DENY | ACTIVE-T | ACTIVE-T | ACTIVE-T | ACTIVE-T | ACTIVE-T | ACTIVE-T | ACTIVE-T |
| Update/archive workspace | DENY | DENY | DENY | DENY | DENY | DENY | DENY | DENY | ACTIVE-T |
| Feature override | DENY | DENY | DENY | DENY | DENY | DENY | DENY | DENY | ACTIVE-T |
| Audit list | DENY | DENY | DENY | DENY | DENY | DENY | DENY | DENY | ACTIVE-T |
| Membership invite admin | DENY | DENY | DENY | DENY | DENY | DENY | DENY | DENY | ACTIVE-T |
| Membership invite preview | TOKEN | TOKEN | TOKEN | TOKEN | TOKEN | TOKEN | TOKEN | TOKEN | TOKEN |
| Membership invite accept | DENY | TOKEN + exact verified identity | như trái | như trái | như trái | như trái | như trái | như trái | như trái |
| Class list/detail | DENY | DENY | OWN/ENR | OWN/ENR | OWN/ENR | ALL-T | OWN/ENR | OWN | ALL-T |
| Class create | DENY | DENY | DENY | DENY | DENY | ALL-T | DENY | theo organization role | ALL-T |
| Class update | DENY | DENY | DENY | DENY | DENY | ALL-T | OWN/ENR | OWN | ALL-T |
| Archive/restore/transfer | DENY | DENY | DENY | DENY | DENY | chỉ class actor sở hữu | DENY | OWN | ALL-T |
| Enrollment/roster/invite management | DENY | DENY | DENY | DENY | DENY | ALL-T + hierarchy | OWN/ENR + hierarchy | OWN + hierarchy | ALL-T + hierarchy |
| Join class bằng code | DENY | DENY | TOKEN trong ACTIVE-T | TOKEN trong ACTIVE-T | idempotent nếu đã active | TOKEN trong ACTIVE-T | idempotent nếu đã active | idempotent | TOKEN trong ACTIVE-T |
| Leave class | DENY | DENY | active enrollment | active enrollment | active enrollment | active enrollment nếu có | active enrollment | DENY khi vẫn là owner | active enrollment nếu có |
| Media token/event | DENY | DENY | active class enrollment | active class enrollment | active class enrollment | ALL-T | active class enrollment | OWN | ALL-T |
| LiveKit webhook | signed service only | N/A | N/A | N/A | N/A | N/A | N/A | N/A | N/A |

Roster mutation hierarchy:

| Actor authority | Level | Target/role có thể tác động |
|---|---:|---|
| Org admin | 4 | Co-teacher, TA, student nếu không phải self/owner. |
| Owner | 3 | Co-teacher, TA, student nếu không phải self. |
| Teacher hoặc co-teacher | 2 | TA, student; không peer teacher/co-teacher/owner. |
| TA | 1 | Không có `enrollment.manage`; nếu permission thay đổi về sau vẫn chỉ thấp hơn level 1. |
| Student/guest | 0 | Không có target hợp lệ thấp hơn. |

## 6. HTTP status và anti-enumeration contract

| Tình huống | Expected status | Ghi chú |
|---|---:|---|
| Không có, hết hạn hoặc server-revoked session | `401` | Không biến thành `404` để che lỗi auth. |
| Mutation thiếu/sai CSRF | `403` | Không gọi repository mutation. |
| Active same-tenant resource tồn tại nhưng actor thiếu permission | `403` | Ví dụ student update class hoặc teacher update feature controls. |
| Active same-tenant roster target vi phạm hierarchy/state | `409` | Domain conflict; không thay đổi row/outbox/audit. |
| Exact foreign tenant/class/invitation/code/user/identity ID | `404` | Body giống inaccessible/random resource và không lộ foreign metadata. |
| Random inaccessible resource ID | `404` | Dùng làm control cho exact foreign-ID assertions. |
| Malformed UUID, malformed cursor hoặc malformed token | `400` hoặc contract-specific generic unavailable | Không được phân biệt token tồn tại với token terminal. |
| Stale expected version/context version | `409` | Không ghi đè mutation mới hơn. |
| Feature/quota policy từ chối | `403` hoặc `409` theo contract hiện hành | Không được client override bằng body. |

Ngoại lệ có chủ ý: list class của guest/student không enrolled trả danh sách rỗng thay
vì tiết lộ class rồi trả từng item forbidden. Membership/class invitation preview dùng
generic unavailable response cho malformed, random, expired và revoked token theo
contract; test phải so sánh code/body shape và state invariants, không dựa vào timing
tuyệt đối.

## 7. Stale session semantics

### 7.1 Membership revoke hoặc demotion

- TutorHub session có thể vẫn hợp lệ cho self endpoints sau khi một workspace
  membership bị revoke. Revoke membership không đồng nghĩa revoke toàn bộ identity
  session.
- Request kế tiếp phải reload membership từ database. Tenant bị revoke không còn là
  `ActiveTenant`; tenant/class/media/audit endpoint của tenant đó phải thất bại.
- Nếu actor còn membership active tenant khác, actor có thể switch hợp lệ sang tenant
  đó. Không được giữ permission/role cũ từ OIDC claim hoặc principal cache.
- Mutation đang chạy phải reauthorize current membership trong transaction để revoke
  hoặc demotion đồng thời không lọt qua stale preflight.

### 7.2 Workspace switch

- Switch hợp lệ yêu cầu active membership tại target tenant.
- Success phải rotate session token, CSRF token và tăng context version.
- Cookie/CSRF cũ phải thất bại; cookie mới chỉ có target active tenant.
- Dùng cookie mới với exact resource ID của tenant trước phải trả `404`, kể cả actor
  vẫn còn membership ở tenant trước.
- Hai switch/create/archive mutation dùng cùng stale context version: chỉ một mutation
  được commit, mutation còn lại trả conflict.

## 8. Test checklist

### 8.1 Exact-ID và tenant/class isolation

- [x] Với từng resource ID, test bốn control: authorized same tenant, forbidden same
  tenant, exact foreign ID và random ID.
- [x] Foreign `identity_id` không unlink được linked identity của user khác.
- [x] Foreign `tenant_id` không đọc/update/archive/capabilities/features/audit/invites.
- [x] Foreign `invitation_id` không list/revoke qua active tenant khác.
- [x] Foreign `class_id` không get/update/archive/restore/transfer/enroll/list roster,
  mutate roster, manage code, leave hoặc lấy media token.
- [x] Foreign target `user_id` không direct-enroll/suspend/remove/update role/bulk.
- [x] Same-tenant nhưng foreign-class `code_id` không revoke được.
- [x] Class invite token tenant A không join được khi active tenant là B.
- [x] Cross-scope response không chứa tenant name, class title, email, display name,
  invitation/code ID hoặc target user ID.
- [x] Mọi denied mutation giữ nguyên business row, usage counter, outbox và audit; item hợp lệ khác trong bulk vẫn có thể commit theo contract P2-06.

### 8.2 Actor/resource matrix

- [x] Table-driven test đủ chín actor của mục 5 trên từng route group.
- [x] Guest/student không enrolled không thấy class trong list và không get exact ID.
- [x] TA join/publish/admit theo policy nhưng không manage enrollment/roster.
- [x] Teacher quản lý mọi class cùng tenant nhưng chỉ archive/restore/transfer class do
  mình sở hữu, trừ khi đồng thời là org admin.
- [x] Co-teacher update/manage class mình enrolled nhưng không archive/transfer.
- [x] Owner không self-leave và không bị roster mutation.
- [x] Org admin không bypass active-tenant scope dù có admin ở tenant khác.
- [x] Roster hierarchy chặn self, owner, peer/higher target và desired role ngang/cao.

### 8.3 Stale session và race

- [x] Revoke membership sau khi lấy cookie; request tenant/class kế tiếp mất quyền.
- [x] Demote org admin/teacher; mutation kế tiếp dùng permission mới từ DB.
- [x] Suspend/remove class enrollment; exact class/media request kế tiếp mất class role.
- [x] Switch tenant; token và CSRF cũ thất bại, token mới không truy cập tenant cũ.
- [x] Concurrent switch/create/archive CAS chỉ commit một transaction.
- [x] Concurrent role/suspend/remove reauthorize target state và hierarchy trong lock.

### 8.4 Mass assignment

Mỗi mutation JSON phải có ít nhất một test field lạ và một test field nhạy cảm. Toàn bộ
request phải bị từ chối, không silently ignore:

- [x] `tenant_id`, `active_tenant_id`, `actor_id`, `user_id` ngoài schema.
- [x] `owner_user_id`, `organization_role`, `class_role` ngoài allowlist.
- [x] `status`, `version`, `context_version`, timestamps và lifecycle actor.
- [x] `feature_source`, deployment ceiling, quota usage counter.
- [x] `audit_actor_id`, `request_id`, `outcome`, outbox/audit metadata.
- [x] Duplicate JSON keys, trailing JSON object, wrong content type và oversized body.
- [x] Bulk roster duplicate user, hơn 50 target, mixed tenant target và unknown action; foreign item chỉ nhận failure item-level, không làm thay đổi target foreign.

### 8.5 Cursor tamper

- [x] Invalid base64, missing/empty anchor, nil UUID, invalid timestamp, wrong version,
  unknown JSON field và oversized cursor.
- [x] Reuse cursor với status/search/limit khác.
- [x] Reuse roster/audit cursor giữa tenant hoặc class khác.
- [x] Recompute public scope hash và forge valid-looking anchor; kết quả chỉ được thay
  pagination trong đúng authorized SQL scope, không trả foreign item.
- [x] Forge class cursor bằng arbitrary `(created_at,id,status)` và replay tenant A -> B;
  sau finding P2-10-F01, replay phải bị từ chối vì tenant binding.
- [x] Cursor trỏ user không tồn tại trong đúng class/filter phải bị coi là invalid.

### 8.6 Membership/class invitation abuse

- [x] Empty, wrong prefix, short/long, noncanonical base64url, padding, whitespace,
  Unicode lookalike và one-byte mutation.
- [x] Random token, expired, revoked, exhausted/accepted và reuse token.
- [x] Membership invitation accept bằng user không có exact verified linked email.
- [x] Invitation intended role ngoài `teacher/student/guest` bị từ chối.
- [x] Concurrent accept/join chỉ tạo một membership/enrollment và counter chính xác.
- [x] Token tenant A không mutate tenant B; response không tạo existence oracle.
- [x] Public preview/join rate limit áp dụng cả malformed/random token flood.
- [x] Raw token không xuất hiện trong DB, log, audit, outbox hoặc list/revoke response.

### 8.7 Fuzz checklist

- [x] Fuzz UUID path/query/body: empty, nil UUID, truncated, overlong và Unicode.
- [x] Fuzz membership invitation token normalizer/digest boundary.
- [x] Fuzz class invite token normalizer/digest boundary.
- [x] Fuzz class, roster và audit cursor decoders; không panic, allocation có giới hạn.
- [x] Fuzz JSON decoder với duplicate/trailing/unknown/oversized structures.
- [x] Fuzz roster search normalization (`%`, `_`, combining characters, NFC/NFD,
  whitespace) và xác nhận ký tự wildcard luôn literal.
- [x] Fuzz LiveKit room/event ID parser và webhook event ID validation sau
  signature-verifier boundary; participant identity chỉ được lưu như opaque provider data.

## 9. Traceability tới test hiện có

Bảng dưới chỉ ghi nơi đã có coverage liên quan; không khẳng định các test đang green
trong lần thực thi P2-10 này.

| Concern | Test file hiện có |
|---|---|
| Shared organization/class policy, conceal scope | `services/core-api/internal/policy/policy_test.go` |
| Roster hierarchy | `services/core-api/internal/policy/roster_policy_test.go` |
| Session, CSRF, tenant create/switch | `services/core-api/internal/httpapi/auth_test.go`; `services/core-api/internal/modules/identity/service_test.go`; `services/core-api/internal/modules/identity/postgres_repository_integration_test.go` |
| Self profile/linked identity, strict fields | `services/core-api/internal/httpapi/profile_test.go`; identity repository integration tests |
| Membership invitation handler/service/token | `services/core-api/internal/httpapi/membership_invitation_test.go`; `services/core-api/internal/modules/identity/invitation_test.go`; `postgres_invitation_repository_integration_test.go`; `invitation_fuzz_test.go` |
| Feature controls and cross-tenant handler | `services/core-api/internal/httpapi/feature_control_test.go`; `services/core-api/internal/modules/featurecontrol/postgres_repository_integration_test.go` |
| Class HTTP/service/repository/cursor | `services/core-api/internal/httpapi/classroom_test.go`; `services/core-api/internal/modules/classroom/service_test.go`; `postgres_repository_integration_test.go`; `cursor_fuzz_test.go` |
| Enrollment and class invitation | `services/core-api/internal/httpapi/class_enrollment_test.go`; `services/core-api/internal/modules/classroom/enrollment_service_test.go`; `postgres_enrollment_repository_integration_test.go`; `invite_code_token_fuzz_test.go` |
| Roster HTTP/service/repository | `services/core-api/internal/httpapi/class_roster_test.go`; `services/core-api/internal/modules/classroom/roster_service_test.go`; `postgres_roster_repository_integration_test.go` |
| Tenant audit và cursor | `services/core-api/internal/httpapi/audit_test.go`; `services/core-api/internal/modules/audit/postgres_integration_test.go`; `cursor_filter_test.go`; `cursor_fuzz_test.go` |
| Media authorization và signed webhook | `services/core-api/internal/httpapi/media_test.go`; `services/core-api/internal/modules/media/service_test.go`; `livekit_test.go`; `postgres_repository_test.go`; `identifier_fuzz_test.go` |

## 10. Test P2-10 đã bổ sung

Các file dưới đây là coverage mới của P2-10:

| File | Trách nhiệm |
|---|---|
| `services/core-api/internal/securitysuite/security_integration_test.go` | PostgreSQL thật: actor matrix, exact foreign class/user/code IDs, state invariants, stale membership và workspace-switch rotation. |
| `services/core-api/internal/httpapi/request_security_test.go` | Strict JSON, sensitive fields, duplicate/trailing/oversized payload trên mutation DTOs và JSON fuzz seed boundary. |
| `services/core-api/internal/httpapi/resource_id_test.go` | Canonical resource UUID regression và fuzz boundary cho path/query IDs. |
| `services/core-api/internal/modules/classroom/cursor_fuzz_test.go` | Class/roster cursor tamper, tenant/filter replay, unknown/trailing JSON và untrusted-anchor containment. |
| `services/core-api/internal/modules/audit/cursor_fuzz_test.go` | Audit cursor malformed/unknown/trailing/filter binding fuzz seeds. |
| `services/core-api/internal/modules/identity/invitation_fuzz_test.go` | Membership invitation token normalizer fuzz seeds. |
| `services/core-api/internal/modules/classroom/invite_code_token_fuzz_test.go` | Class invite token normalizer fuzz seeds. |
| `services/core-api/internal/modules/media/identifier_fuzz_test.go` | LiveKit room/event identifier parser fuzz boundary. |

Integration test phải dùng database fixture riêng và build tag/quy trình integration hiện
hành; không dùng production credential. Unit HTTP matrix không thay thế repository
integration test vì fake service không chứng minh SQL tenant predicate.

## 11. Finding register

| ID | Severity | Finding | Ảnh hưởng và quyết định P2-10 |
|---|---|---|---|
| P2-10-F01 | Low - fixed | Class cursor v1 chỉ là base64 JSON của `created_at`, `id`, `status`; chưa bind `tenant_id` và dùng decoder cho phép unknown field. | Đã thay bằng prefix v2, bind tenant/filter, strict decode và regression unit + PostgreSQL cursor replay tenant B -> A. Cursor cũ bị từ chối fail-closed. |
| P2-10-F02 | Low / follow-up | Roster/audit cursor bind scope bằng SHA-256 không có secret; client có thể tự tính hash và forge anchor. | Containment tests đã đạt: cursor chỉ ảnh hưởng pagination trong đúng tenant/class/filter và không cấp quyền hay mutate state. HMAC/versioned signing được ghi follow-up trong backlog/ADR riêng nếu threat model P2-12 yêu cầu. |
| P2-10-F03 | Informational | Membership revoke loại active tenant khỏi principal nhưng không revoke toàn bộ identity session. | Đây là semantics có chủ ý nếu self endpoints và membership tenant khác vẫn dùng được. P2-10 phải test mất workspace permission ngay request kế tiếp và ghi rõ không kỳ vọng global `401`. |
| P2-10-F04 | Informational | Signed LiveKit webhook lấy tenant/class từ room name rồi dựa vào composite FK để bác mismatched pair. | Không phải user-controlled IDOR khi signature verification đúng. Bổ sung test malformed/mismatched signed room không ghi receipt; cân nhắc map FK failure sang ignored/controlled error để tránh availability noise. |

Không có finding High hoặc Critical trong implementation/local verification hiện tại.
Kết luận cuối cùng vẫn chờ Verify PostgreSQL và Security workflow trên cùng head.

## 12. Exit criteria P2-10

- [x] Actor/resource matrix được encode thành automated tests, không chỉ review thủ công.
- [x] Exact foreign-ID suite phủ route inventory qua suite mới và integration test đã trace.
- [x] Membership revoke và workspace-switch suite đạt ở mức compile/local fixture;
  runtime PostgreSQL chờ CI.
- [x] Mass-assignment, cursor tamper, invitation abuse và fuzz suite đạt cục bộ.
- [x] P2-10-F01 đã được sửa và có regression test.
- [x] P2-10-F02 có containment tests và signed-cursor follow-up được ghi backlog.
- [x] Denied mutation suite chụp snapshot/counter để chứng minh không đổi business row.
- [x] API unit tests, generated contract checks và `corepack pnpm verify` đạt cục bộ.
- [ ] PostgreSQL actor/foreign-ID/stale-session matrix xanh trong workflow `Verify`.
- [ ] Security/dependency/secret scans theo CI hiện hành không có finding chưa xử lý.
- [ ] Chỉ sau các bước trên mới cập nhật `docs/PROJECT_STATE.md`,
  `docs/PHASE_2_BACKLOG.md` và exit gate liên quan thành DONE/green.
