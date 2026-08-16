# ADR 0030: Authoritative Classroom Media spaces, lifecycle và LiveKit grants

- Status: Accepted
- Date: 2026-08-08
- Scope: P4-00 đến P4-12 Classroom Media MVP
- Supersedes only the class-wide runtime shape of the P1-07 spike; ADR-0004 remains accepted

## Context

P1-07 đã chứng minh LiveKit Cloud, server-issued token, signed webhook, prejoin, camera/mic,
screen share và reconnect với 2-5 người. Spike hiện cấp credential theo class và dùng một tên
phòng deterministic cho cả lớp. Nó chưa có scheduled room authority, lobby, one-active-instance
invariant, moderation lifecycle, member-owned instant room hoặc participant-session persistence.

Phase 3 đã bổ sung ClassSession, recurring occurrence, StudyMeeting, shared class policy,
feature/quota controls, audit/outbox và exact tenant ACL. ADR-0017 dành transition
`scheduled -> live -> ended` cho Phase 4; ADR-0021 cho active member quyền mục tiêu
`room.create.instant` nhưng không cho StudyMeeting tự mint LiveKit token. Phase 4 cần nối các
nền này mà không biến LiveKit state, JWT claim hoặc client role thành business authority.

## Decision

### 1. Giữ modular monolith và LiveKit Cloud

Module `media` trong Go modular monolith tiếp tục sở hữu room policy, provider adapter,
credential, webhook mapping và telemetry. React tiếp tục dùng LiveKit client/components đã khóa
version. PostgreSQL là source of truth cho lifecycle và authorization projection; LiveKit là
media transport/provider state, không phải nguồn quyền nghiệp vụ.

P4-00 không thêm dependency, microservice, Redis, Kafka, Kubernetes hoặc provider. ADR-0004
tiếp tục có hiệu lực: secret chỉ ở Core API/provider secret store, browser không giữ API secret,
media đi trực tiếp browser <-> LiveKit và self-host chỉ được xem lại khi có số liệu vận hành.

### 2. Ba cấp aggregate có authority

Phase 4 dùng ba cấp domain thay vì một room class-wide:

1. `MediaSpace` là logical learning space tenant-scoped. Nó bind đúng một nguồn được server
   resolve: one-time `ClassSession`, recurring class-session occurrence hoặc `StudyMeeting`.
   Command “instant” atomically tạo một StudyMeeting bắt đầu hiện tại với duration/timezone
   bounded rồi bind MediaSpace; nó không tạo loại authority thứ ba. Một source occurrence chỉ
   có tối đa một MediaSpace.
2. `RoomInstance` là một lần chạy provider của MediaSpace. Mỗi space chỉ có tối đa một instance
   active. Provider failure có thể tạo recovery instance mới dưới cùng space sau khi instance cũ
   đã terminal; không tái sử dụng JWT hoặc participant identity cũ.
3. `ParticipantSession` là một lần actor được admit/join vào một RoomInstance. Nó có opaque ID,
   join-attempt state và thời điểm bounded; nó không tự trở thành attendance/grade record.

Lifecycle logical:

```text
scheduled -> open -> ended
scheduled --------> cancelled
open -------------> ended
```

Lifecycle provider instance:

```text
provisioning -> active -> closing -> ended
provisioning ---------------------> failed
active ---------------------------> failed
```

`locked` và lobby policy là state có version của MediaSpace/active RoomInstance, không phải
terminal lifecycle. Start/end/cancel, lock/unlock và recovery dùng expected version cùng stable
idempotency key. Transaction khóa theo thứ tự MediaSpace -> RoomInstance -> admission/
ParticipantSession; partial unique constraint chặn hai instance active. Retry hoặc hai request
start đồng thời không được tạo hai provider rooms.

ParticipantSession đi qua state bounded `waiting -> admitted -> joining -> connected ->
reconnecting -> left/removed/failed`; terminal state không tự quay lại connected. Admission là
resource riêng để admit/deny idempotent, không trộn transport state vào business authority.
Capacity được reserve atomically khi admission/join tạo ParticipantSession và được release bằng
terminal transition, không dùng count-then-insert ngoài transaction.

Với official classroom, source class/session/occurrence và quyền hiện tại được reload trước mỗi
mutation. One-time ClassSession được project `live/ended` khi media lifecycle commit; recurring
occurrence dùng stable occurrence identity và exception/projection riêng, không sửa toàn series.
ClassSession/StudyMeeting bị cancel không thể mở room mới; cancel khi room đang open phải chạy
explicit end flow và ghi audit thay vì chỉ đổi UI.

### 3. Official classroom và member-owned space không trộn quyền

- Official ClassSession/occurrence vẫn dùng class policy. Organization admin/teacher, implicit
  owner hoặc active co-teacher có thể start/end theo shared policy. Teaching assistant có thể
  admit và điều phối trong instance nhưng không tự biến một lịch thành official session.
- Active class access là điều kiện join; enrollment suspend/leave/remove hoặc class archive chặn
  credential mới. Source audience/RSVP có thể thu hẹp eligibility nhưng không mở rộng qua class
  policy.
- StudyMeeting và instant space thuộc creator. Owner có thể start/end và mời active member cùng
  tenant; invited actor chỉ join trong scope được lưu server-side. Ownership không cấp
  `session.schedule`, attendance hoặc quyền trên class.
- P4 MVP không cho anonymous/external participant. Guest chỉ tham gia nếu là active authenticated
  tenant membership và có explicit class/invite authority.
- Organization admin có safety end/remove/revoke capability với reason bắt buộc và audit; safety
  action không chuyển ownership hoặc cho admin đọc private chat/media content.

Shared policy tiếp tục deny-by-default. P4 implementation tái dùng `session.start`, `session.end`,
`session.join`, `media.publish`, `participant.admit` và `participant.remove`; bổ sung typed action
cho `room.create.instant`, `media.share_screen`, `media.lock` và `media.moderate` khi endpoint
tương ứng được thêm. Handler không so sánh role. Mỗi mutation reload membership, class role,
source state và ParticipantSession authoritative.

### 4. Credential và provider identity tối thiểu quyền

Canonical Phase 4 join credential được cấp cho `RoomInstance`, không cho class ID chung. Request
không nhận tenant, provider room name, role, grant hoặc participant identity từ client. Server:

1. resolve MediaSpace/source/instance trong active tenant;
2. reauthorize join, admission, feature/quota và instance state;
3. tạo hoặc reuse idempotent ParticipantSession của join attempt;
4. mint JWT TTL 1-15 phút, mặc định 5 phút, chỉ cho exact opaque provider room;
5. trả credential `Cache-Control: no-store`, giữ token trong component memory và buộc reissue sau
   reload hoặc recovery instance.

Provider room name và participant identity là opaque, server-generated và map qua database;
không encode tenant, class, user hoặc BFF session ID. JWT attribute chỉ chứa bounded display role
dùng cho UX; không endpoint nào dùng claim đó thay cho PostgreSQL/shared policy. `CanSubscribe`
chỉ true khi join hợp lệ. Camera/mic và screen-share grant tách theo server capability.
`CanPublishData` giữ false trong MVP; hand raise/reaction/moderation command đi qua Core API,
được reauthorize/rate-limit rồi mới phát tín hiệu provider-side.

Issued JWT không thể bị sửa retroactively. Lock, role revoke, enrollment revoke hoặc remove phải
chặn credential mới và dùng provider server API để remove/mute participant đang kết nối khi policy
yêu cầu. TutorHub không remote-unmute mic/camera; unmute luôn cần hành động/consent ở client.

### 5. Signed webhook map vào instance, không parse business scope từ tên phòng

LiveKit webhook vẫn phải qua official signature/body verification và event-ID idempotency. Sau
P4-02, signed event lookup provider room/SID trong authoritative RoomInstance binding. Unknown,
terminal-stale hoặc mismatched event được ignore/record bounded theo policy; nó không tự tạo tenant,
space, participant hoặc quyền.

Current `th_<tenant>_<class>` parser và class-wide receipt chỉ là P1 compatibility path. Phase 4
không suy tenant/class từ provider name. Webhook transition có compare-and-set, tolerates retry/
out-of-order event và không để provider failure rollback business transaction đã commit.

### 6. Persistence, ACL, audit và outbox

Schema target tối thiểu:

- `media_spaces`;
- `media_room_instances`;
- `media_space_members` cho explicit same-tenant invite;
- `media_admission_requests`;
- `media_participant_sessions`;
- provider webhook receipt/binding được mở rộng hoặc thay bằng table tenant-scoped tương đương.

Mọi business row có `tenant_id`; source/class/member foreign key dùng composite tenant integrity.
Core API runtime chỉ có exact DML cần thiết; không có owner, migration hoặc maintenance privilege.
Mọi query predicate tenant và foreign ID bị conceal `404`. Forward migration phải được kiểm tra
trên disposable trước shared staging; P4-00 không tạo migration.

Durable transition start/end/cancel, lock/unlock, admit/deny, promote/demote, mute/remove và safety
recovery ghi audit allowlist cùng bounded domain outbox fact trong business transaction.
Consumer registration và notification/delivery side effect vẫn giữ gate riêng; việc có outbox
fact không tự bật consumer Phase 3. Track toggle, reaction và quality sample không tạo audit
noise. Audit/log/outbox không chứa JWT, provider secret, SDP/ICE, IP, device label, raw error,
chat/media content hoặc participant email.

Không giữ PostgreSQL transaction trong lúc gọi LiveKit. Command commit intent/version trước,
provider adapter chạy idempotent ngoài transaction và kết quả được reconcile bằng compare-and-set
hoặc signed webhook. Nếu một flow cần retry nền/durable scheduler mà P3-03B chưa có, feature đó
phải giữ off hoặc dùng explicit synchronous recovery; không được giả định Render Web Service là
durable worker.

### 7. Feature, quota và rollout fail-closed

P4-01 thêm catalog keys, nhưng P4-00 chỉ khóa contract:

- feature `classroom_media_rooms`, compiled default `false`;
- feature `instant_study_rooms`, compiled default `false` và luôn phụ thuộc parent feature;
- quota `active_media_spaces`, default 10, ceiling 100;
- quota `media_participants_per_space`, default/ceiling 50;
- quota `active_media_participants`, default 100, ceiling 500;
- quota `media_space_starts_per_hour`, default 20, ceiling 200.

Deployment guardrail của cả hai feature mặc định force-off cho tới exact staging acceptance;
catalog default false một mình không đủ vì tenant override có thể bật. Guardrail chỉ có thể
force off hoặc hạ ceiling. Thiếu LiveKit URL/key/secret,
provider health prerequisite hoặc schema capability phải fail closed. Join credential có shared
rate limit riêng theo tenant/actor/session/action; baseline là tối đa 30 lần/10 phút và hard bound
60, nhưng reconnect recovery được phân loại riêng để không biến provider outage thành lockout.
Quota/rate-limit storage failure trả `503`, không mint token.

Tenant override không thể bật instant rooms khi parent off hoặc vượt deployment guardrail. UI
capability chỉ là projection; direct API luôn enforce. Không bật shared staging hoặc end-user
rollout cho tới task acceptance tương ứng.

### 8. Lobby và moderation semantics

- Host/co-host/TA vào room theo server projection; attendee mặc định vào lobby khi lobby enabled.
- `admit`/`deny` bind exact admission request, instance và version. Denied/removed participant
  không thể rejoin instance nếu chưa được explicit restore.
- Lock chặn admission/join mới nhưng không tự kick participant đang active.
- Server có thể mute published track và remove participant; không remote-unmute.
- Screen share mặc định cho host/co-host/TA; attendee share cần explicit server grant.
- Co-host promotion chỉ có hiệu lực trong RoomInstance và không sửa organization/class role.
- Ending instance thu hồi join path, disconnect participant qua provider adapter và terminalize
  outstanding admission request idempotently.

Recording, egress, transcription, E2EE policy, breakout, whiteboard và webinar/broadcast profile
không thuộc Phase 4 MVP. Recording luôn off ngoài controlled provider test có owner approval.

#### 8.1. P4-04 admission, invite và explicit restore

P4-04 dùng các aggregate đã tạo ở migration `000029`; không tạo directory/public-invite authority
mới. Explicit member grant chỉ áp dụng cho `StudyMeeting` (kể cả instant-created meeting). Request
có thể nhận normalized exact member email để lookup một active same-tenant membership, nhưng email
không được persist trong media table, response, audit, outbox hoặc log. Official ClassSession/
occurrence vẫn chỉ lấy eligibility từ class policy và enrollment hiện hành.

Mỗi lobby mutation bind exact MediaSpace, active RoomInstance, AdmissionRequest/Member version và
stable idempotency key. Receipt giữ fingerprint bounded; replay cùng key/fingerprint không tạo audit/
outbox lần hai, còn key reuse khác fingerprint bị reject. Lock order là tenant control -> MediaSpace
-> RoomInstance -> AdmissionRequest -> ParticipantSession. Admit/deny/end race chỉ được một durable
kết quả; end/timeout/cancel terminalize waiting admission và release participant capacity trong cùng
transaction.

Deny chuyển ParticipantSession hiện tại sang terminal `removed`; terminal session không được hồi
sinh. Một attempt mới của actor bị chặn cho tới khi moderator thực hiện explicit restore, được lưu
bằng timestamp/actor trên terminal participant. Restore chỉ mở quyền tạo attempt mới và vẫn phải
reauthorize membership/source/lobby hiện hành. Member revoke/restore dùng lifecycle riêng trên
`media_space_members`; revoke chặn ngay admission/credential mới. Waiting projection có bounded
expiry, self-cancel và polling; trước `admitted` browser không được mint credential hoặc connect
LiveKit.

#### 8.2. P4-07 moderation command and provider-effect boundary

P4-07 keeps one authoritative participant projection. Client commands target only the opaque
`participant_key`; user, ParticipantSession, join-attempt, provider room, provider participant and
track identifiers remain server-only. An early idempotency-receipt replay is read-only and never
takes a receipt row lock. Every write path resolves the target again under the fixed lock order
`tenant control -> MediaSpace -> RoomInstance -> ParticipantSession -> role assignment/receipt`.

Dynamic co-host authority is stored as a RoomInstance-scoped assignment. Promotion never changes an
organization or class role, and every assignment becomes ineffective when that exact instance is no
longer active. Demotion revokes the dynamic assignment and falls back to the current source role.
Signal, lobby, credential and moderation flows must all use this same effective-role resolver; a
provider claim or client-rendered role is never sufficient authority.

`lock`, `unlock`, `promote_co_host`, `demote_co_host`, `mute_microphone` and `remove_participant`
are versioned and idempotent Core API commands. Lock blocks new join credentials and admissions but
does not disconnect active participants. Remote mute is one-way: the server adapter can set a
published microphone track to muted but has no remote-unmute method. Remove terminalizes the active
ParticipantSession, releases capacity, clears the raised-hand projection and preserves the explicit
restore barrier before any later rejoin.

Provider work is a durable effect attached to the committed command receipt. Core API commits the
business result first, releases the PostgreSQL transaction, then a narrow LiveKit adapter attempts
mute/remove/delete. Responses expose only the allowlisted state `none`, `pending`, `applied`,
`retryable_failed` or `permanent_failed`; UI must not claim provider success before `applied`.
Retries claim work with compare-and-set/lease semantics. Raw provider errors and identifiers never
enter API responses, audit, outbox or logs.

The existing lifecycle `end` command remains versioned/idempotent and owns the durable LiveKit room
delete effect plus retry/reconcile state; moderation does not create a second end path.

The exact operation matrix is server-projected per target. A host may lock/unlock, promote an
attendee, demote a dynamic co-host, and mute/remove any non-host target. A co-host may mute/remove an
attendee only. A teaching assistant may mute an attendee only. An attendee has no moderation
operation. Self-targeting and host-targeting are always denied. Organization-admin safety recovery
is a separate reason-required audited path and does not silently grant ordinary room membership or
a co-host token.

### 9. Persistent chat và ephemeral signal boundary

LiveKit DataChannel không là source of truth. In-room chat phải dùng PostgreSQL REST contract và
sanitize/authorization của conversation module. ADR-0013 hiện chỉ cho `direct` và `class`, nên
P4-08 phải review/amend ADR-0013 và ADR-0025 trước khi thêm room-scoped conversation; không tạo
table chat song song hoặc nhét message vào webhook.

Hand raise, reaction, active-speaker và quality state là ephemeral. Client payload không được
quyết định moderation hoặc attendance. Nếu server/provider signal bị mất, durable room lifecycle
và chat vẫn đúng; UI có bounded resync path.

### 10. Privacy, telemetry và diagnostics

ParticipantSession phục vụ join/reconnect/support, không được mô tả là attendance. Identifiable
join-stage/quality diagnostics giữ tối đa 30 ngày ở MVP; aggregate metric có thể giữ lâu hơn sau
khi bỏ user/room dimension. Provider webhook receipt cũng có bounded retention/maintenance plan
trước activation. Tenant-configurable/legal retention thuộc phase production-readiness.

Telemetry chỉ nhận typed stage/outcome/error code, duration và coarse network/media quality;
không nhận token, SDP, ICE candidate, raw IP, device label, audio/video/screen frame hoặc exception
thô. Support export yêu cầu actor được phép, bounded time/size, audit, no-store và redaction; không
bao giờ xuất provider credential hoặc raw media.

### 11. API boundary mục tiêu

OpenAPI implementation có thể tinh chỉnh path/name nhưng không đổi semantics:

```http
POST /api/v1/media/spaces
GET  /api/v1/media/spaces/resolve
GET  /api/v1/media/spaces/{space_id}
POST /api/v1/media/spaces/{space_id}/start
POST /api/v1/media/spaces/{space_id}/end
POST /api/v1/media/spaces/{space_id}/lock
POST /api/v1/media/spaces/{space_id}/join-attempts
POST /api/v1/media/spaces/{space_id}/join-credentials
POST /api/v1/media/spaces/{space_id}/admissions/{admission_id}/admit
POST /api/v1/media/spaces/{space_id}/admissions/{admission_id}/deny
POST /api/v1/media/spaces/{space_id}/participants/{participant_key}/role
POST /api/v1/media/spaces/{space_id}/participants/{participant_key}/mute
POST /api/v1/media/spaces/{space_id}/participants/{participant_key}/remove
POST /api/v1/media/spaces/{space_id}/signals
POST /api/v1/media/spaces/{space_id}/diagnostics
```

Create accepts only a typed server-resolvable source reference or `instant`; instant atomically
creates/binds an owned StudyMeeting and never creates a third source authority. Request uses
`X-TutorHub-Expected-Tenant-ID` as a cache/workspace-race assertion while authorization still comes
only from the active server session. It never accepts tenant/owner/provider room/grants. Commands
use CSRF, expected version and idempotency key where state changes; credential/participant
responses set `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. Stable typed problems
include `feature_disabled`, `quota_exceeded`,
`room_not_open`, `room_locked`, `admission_required`, `admission_denied`, `stale_version` and
`media_provider_unavailable`, without exposing foreign resource existence.

P4-12 adds one read-only source resolver for product navigation. It accepts only the three
persisted source kinds (`class_session`, `class_session_occurrence`, `study_meeting`), never
creates a space, requires the active-tenant assertion and reuses the same source/member
authorization projection as `GET /media/spaces/{space_id}`. A missing or invisible source/space is
always concealed as `404`. This boundary lets an authorized attendee discover an already-created
official or explicitly invited room without granting `session.start`, accepting a client role, or
using the legacy class-wide room authority.

### 12. P1 compatibility and rollout

`POST /api/v1/classes/{class_id}/media-token` and deterministic class room are legacy spike
surfaces. P4 implementation must not silently treat them as lifecycle-complete. New web routes use
MediaSpace APIs. During migration the old path may remain for controlled P1 smoke only; before
P4-12 activation it is deprecated, disabled for tenants using `classroom_media_rooms`, and no
tenant may have both authorities active. The signed webhook endpoint remains but switches to
instance binding after forward migration and compatibility tests.

## Consequences

- P1 code, dependencies, provider project and smoke knowledge are reused, reducing delivery risk.
- PostgreSQL/shared policy become explicit authority for room start/join/moderation and recovery.
- The model supports official classes and member-owned rooms without granting students official
  session authority.
- More tables and lifecycle/concurrency tests are required before Phase 4 can enable media rooms.
- Provider API calls for kick/mute/server signal need a narrow adapter and outage semantics.
- Existing class-wide endpoint and room-name parser require a controlled compatibility removal.

## Alternatives rejected

- Keep one deterministic room per class: cannot bind scheduled lifecycle, admission, recovery or
  least-privilege participant sessions.
- Trust LiveKit participant metadata/role: claims become stale and are client-visible; provider
  state is not TutorHub authorization authority.
- Put tenant/class/user IDs in provider identifiers: increases metadata leakage and couples
  webhook parsing to business identity.
- Let clients publish authoritative DataChannel commands: bypasses server authorization/rate
  limit and makes moderation state non-durable.
- Add a media microservice/Redis/Kafka now: no load or ownership evidence justifies the new
  operational boundary.
- Implement recording in the MVP: expands consent, retention and privacy risk beyond Phase 4.

## P4-00 acceptance

- Phase 4 task/dependency/exit-gate backlog references this ADR.
- First implementation slice is MediaSpace lifecycle/schema/API with all media features off.
- Existing P1 spike is explicitly reusable but not accepted as Phase 4 lifecycle authority.
- Official class, StudyMeeting and instant ownership boundaries are explicit.
- Token, webhook, tenant ACL, concurrency, lobby, moderation, privacy and retention boundaries
  are implementation-ready.
- Recording and all unrelated Phase 3 carry-over remain off; P4-00 performs no migration/deploy.
