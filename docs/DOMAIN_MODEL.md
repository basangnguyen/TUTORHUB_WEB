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
đang hoạt động; class role thuộc enrollment của đúng lớp. Một role ở tenant hoặc lớp
khác không tham gia quyết định hiện tại.

### 3.1. Organization role

| Permission | `org_admin` | `teacher` | `student` | `guest` |
|---|:---:|:---:|:---:|:---:|
| `tenant.manage` | Có | Không | Không | Không |
| `class.create` | Có | Có | Không | Không |
| `class.update` | Có | Có | Không | Không |
| `class.view` | Có | Có | Có | Không |
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

### 3.2. Class role

| Permission | `owner` | `co_teacher` | `teaching_assistant` | `student` |
|---|:---:|:---:|:---:|:---:|
| `tenant.manage` | Không | Không | Không | Không |
| `class.create` | Không | Không | Không | Không |
| `class.update` | Có | Có | Không | Không |
| `class.view` | Có | Có | Có | Có |
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
- Unique constraint thường gồm `tenant_id`.
- File path/key không dựa vào tên file người dùng; dùng opaque object ID.
- Background job mang tenant context và actor/service identity.
- Platform Admin là luồng riêng, có step-up authentication và audit bắt buộc.

## 5. Trạng thái quan trọng

### Class

`draft -> active -> archived`

### Class session

`scheduled -> live -> ended`, hoặc `scheduled -> cancelled`.

### Enrollment

`invited -> active -> removed`; yêu cầu tham gia có thể thêm `pending/rejected` theo policy tenant.

Mọi chuyển trạng thái phải được kiểm tra trong domain service, không cho controller cập nhật status tùy ý.
