# Mô hình miền và phân quyền ban đầu

## 1. Aggregate chính

| Aggregate | Entity chính | Ghi chú |
|---|---|---|
| Identity | User, ExternalIdentity, Session | User toàn cục; identity provider có thể thay đổi |
| Tenancy | Tenant, Membership, RoleAssignment | Membership nối user với tenant |
| Classroom | Class, Enrollment, Invitation | Class luôn thuộc một tenant |
| Scheduling | ClassSession | Phiên học có trạng thái scheduled/live/ended/cancelled |
| Media | MediaRoom, Recording | Mapping phiên học với LiveKit room |
| Messaging | Conversation, Message | Tin nhắn bền vững, có moderation/audit |
| Content | FileObject, Folder, ShareGrant | Binary ở object storage |
| Assessment | QuestionBank, Question, Exam, Attempt, Result | Chưa thuộc MVP đầu tiên |

## 2. Quan hệ lõi

```mermaid
erDiagram
    USER ||--o{ MEMBERSHIP : has
    TENANT ||--o{ MEMBERSHIP : contains
    TENANT ||--o{ CLASS : owns
    CLASS ||--o{ ENROLLMENT : contains
    USER ||--o{ ENROLLMENT : joins
    CLASS ||--o{ CLASS_SESSION : schedules
    CLASS_SESSION ||--o| MEDIA_ROOM : opens
    CLASS_SESSION ||--o{ MESSAGE : contains
```

## 3. Mô hình role và permission

TutorHub phân biệt hai phạm vi role. Organization role thuộc membership của workspace
đang hoạt động; class role thuộc enrollment của đúng lớp. Trong P2-04,
`classes.owner_user_id` là nguồn owner implicit cho policy cho đến khi enrollment/roster
được triển khai ở P2-05/P2-06; không tạo enrollment sớm chỉ để biểu diễn owner. Một role
ở tenant hoặc lớp khác không tham gia quyết định hiện tại.

### 3.1. Organization role

| Permission | `org_admin` | `teacher` | `student` | `guest` |
|---|:---:|:---:|:---:|:---:|
| `tenant.view` | Có | Có | Có | Có |
| `tenant.manage` | Có | Không | Không | Không |
| `class.create` | Có | Có | Không | Không |
| `class.update` | Có | Có | Không | Không |
| `class.view` | Có | Có | Có | Không |
| `class.archive` | Có | Không | Không | Không |
| `class.transfer_ownership` | Có | Không | Không | Không |
| `enrollment.manage` | Có | Có | Không | Không |
| `session.start` | Có | Có | Không | Không |
| `session.end` | Có | Có | Không | Không |
| `session.join` | Có | Có | Có | Có |
| `participant.admit` | Có | Có | Không | Không |
| `participant.remove` | Có | Có | Không | Không |
| `media.publish` | Có | Có | Có | Không |
| `chat.send` | Có | Có | Có | Có |

Ma trận organization giữ tương thích với các luồng Phase 1. Khi enrollment và roster
được triển khai ở P2-05/P2-06, class role bổ sung ràng buộc theo đúng lớp thay vì làm
phát sinh kiểm tra role riêng trong từng module.

`tenant.view` cho phép mọi membership active đọc metadata của chính active tenant;
`tenant.manage` chỉ dành cho `org_admin` để update/archive. Danh sách workspace là
user-membership scoped để vẫn chọn được tenant khi session chưa có active tenant.

### 3.2. Class role

| Permission | `owner` | `co_teacher` | `teaching_assistant` | `student` |
|---|:---:|:---:|:---:|:---:|
| `tenant.view` | Không | Không | Không | Không |
| `tenant.manage` | Không | Không | Không | Không |
| `class.create` | Không | Không | Không | Không |
| `class.update` | Có | Có | Không | Không |
| `class.view` | Có | Có | Có | Có |
| `class.archive` | Có | Không | Không | Không |
| `class.transfer_ownership` | Có | Không | Không | Không |
| `enrollment.manage` | Có | Có | Không | Không |
| `session.start` | Có | Có | Không | Không |
| `session.end` | Có | Có | Không | Không |
| `session.join` | Có | Có | Có | Có |
| `participant.admit` | Có | Có | Có | Không |
| `participant.remove` | Có | Có | Không | Không |
| `media.publish` | Có | Có | Có | Có |
| `chat.send` | Có | Có | Có | Có |

### 3.3. Effective permission

1. Server chỉ đọc membership active của `active_tenant_id` trong session; tenant ID do
   client gửi không tạo thêm quyền.
2. Effective permission là hợp của các organization role hợp lệ trong active tenant và
   các class role active của đúng resource class. Phép hợp loại trùng và trả thứ tự ổn
   định để response/cache có tính xác định.
3. Role không nhận diện, membership không active, actor/tenant thiếu hoặc action không
   được khai báo đều bị từ chối theo nguyên tắc deny-by-default.
4. Ràng buộc trạng thái tài nguyên được áp dụng sau phép hợp quyền. Ví dụ lớp archived
   vẫn được xem nhưng không thể join room hoặc publish media; quyền role không vượt qua
   state machine.
5. Quyết định thiếu quyền trong scope trả `403`. Resource ID thuộc tenant/lớp khác hoặc
   thiếu scope trả `404`, giống resource không tồn tại, nhằm tránh dò tìm định danh.

Authorization input thống nhất gồm actor, active tenant, trạng thái membership,
organization/class roles, action, resource tenant, resource class và resource state.
Handler chỉ chuyển principal đã xác thực; `identity`, `classroom` và `media` cùng dùng
`internal/policy.Authorizer`, không so sánh role hoặc permission cục bộ.

## 4. Tenant isolation

- `tenant_id` được lấy từ session/context đã xác thực, không tin giá trị tùy ý từ body.
- Repository bắt buộc nhận tenant context cho truy vấn tenant-scoped.
- Tenant list chỉ dựa trên user ID đã xác thực và membership active; tenant detail,
  update và archive bắt buộc resource tenant trùng active tenant trong session.
- Class list/detail/mutation luôn lấy tenant từ active session. Repository mutation
  khóa và đọc lại membership authoritative trước khi dùng shared policy; ID thuộc tenant
  khác được che như resource không tồn tại.
- Unique constraint thường gồm `tenant_id`.
- File path/key không dựa vào tên file người dùng; dùng opaque object ID.
- Background job mang tenant context và actor/service identity.
- Platform Admin là luồng riêng, có step-up authentication và audit bắt buộc.

## 5. Trạng thái quan trọng

### Tenant

Luồng quản trị thông thường là `active -> archived`; `suspended` dành cho policy/platform
operation, không được PATCH trực tiếp từ tenant API. Update/archive yêu cầu
`expected_version`, tăng `version` atomic và trả conflict khi client dùng dữ liệu stale.
Archive là soft state transition, giữ membership/class/outbox history và bị chặn nếu
actor không còn tenant active khác với role `org_admin`.

Create, switch và archive là privilege-context mutation. Session dùng
`context_version` compare-and-swap; mọi switch hợp lệ, kể cả chọn lại tenant hiện tại,
đều xoay session/CSRF. Archive xóa active tenant khỏi các session liên quan để request
sau không tiếp tục dùng permission của workspace đã archive.

### Membership invitation

`pending -> accepted/revoked/expired`; terminal state không quay lại `pending`. Re-invite
tạo row/token mới sau `revoked` hoặc `expired`, không tái kích hoạt row cũ. Mỗi
tenant/normalized-email chỉ có một row pending; bất kỳ membership row hiện hữu nào của
email/verified linked identity đều chặn create. Accept replay chỉ idempotent cho chính
acceptor khi membership active còn đúng intended role.

Chỉ active `org_admin` có `tenant.manage_members`. Invitation flow P2-03 chỉ cấp
`teacher/student/guest`; grant/promotion `org_admin` là mutation nhạy cảm riêng cần
step-up policy. Accept yêu cầu active verified linked identity khớp exact normalized
provider email và không tự đổi active tenant của session.

### Class

Class được tạo ở `draft` với `version=1`. Metadata công khai gồm code duy nhất trong
tenant, title, description, timezone, status, `version` và `archived_at`; `code` tiếp
tục là định danh thân thiện, không thêm `slug` trùng nghĩa.

- Transition trực tiếp duy nhất qua update là `draft -> active`; không quay active về
  draft. Class archived là read-only cho metadata cho đến khi restore; ownership
  transfer vẫn là mutation riêng được phép khi đủ quyền/recent-auth.
- Archive áp dụng cho draft hoặc active, lưu trạng thái trước trong
  `archived_from_status`; restore trả chính xác về draft/active trước đó.
- Update/archive/restore/transfer ownership đều yêu cầu `expected_version`, tăng version
  atomic và trả conflict khi snapshot stale.
- List có filter status và opaque keyset cursor, sắp xếp ổn định theo
  `(created_at DESC, id DESC)`.
- `class.archive` và `class.transfer_ownership` chỉ thuộc active `org_admin` hoặc owner
  implicit của đúng class; organization teacher và co-teacher vẫn có thể update metadata
  theo ma trận nhưng không được lifecycle/transfer.
- Ownership target phải là active member cùng tenant có effective permission
  `class.create`. Transfer là mutation riêng, dùng recent authentication tối đa 10 phút
  và được ghi vào transactional outbox cùng thay đổi business.
- Media token và media event mới chỉ hợp lệ khi class active. Archive không thể thu hồi
  JWT LiveKit đã cấp hoặc tự kick participant đang kết nối.

Class invite code chưa tồn tại trước P2-05. Lifecycle guard đã sẵn sàng để chặn tạo/join
code mới khi class không active mà không tuyên bố code hiện đã được đóng hoặc revoke.

### Class session

`scheduled -> live -> ended`, hoặc `scheduled -> cancelled`.

### Enrollment

`invited -> active -> removed`; yêu cầu tham gia có thể thêm `pending/rejected` theo policy tenant.

Mọi chuyển trạng thái phải được kiểm tra trong domain service, không cho controller cập nhật status tùy ý.
