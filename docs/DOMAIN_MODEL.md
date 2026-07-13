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

## 3. Role và permission MVP

| Permission | Org Admin | Teacher | Student | Guest |
|---|---:|---:|---:|---:|
| tenant.manage | Yes | No | No | No |
| class.create | Yes | Yes | No | No |
| class.update | Yes | Own/assigned | No | No |
| class.view | Yes | Member | Enrolled | Invited session |
| enrollment.manage | Yes | Assigned class | No | No |
| session.start/end | Yes | Assigned class | No | No |
| session.join | Yes | Assigned | Enrolled | Valid invite |
| participant.admit/remove | Yes | Assigned class | No | No |
| media.publish | Policy | Policy | Policy | Restricted |
| chat.send | Policy | Policy | Policy | Restricted |

Không dùng role trực tiếp rải rác trong handler. Handler gọi authorization policy với subject, action, resource và tenant context.

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
