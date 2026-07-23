# ADR-0021: Native Availability Poll và member-owned Study Meeting

- Trạng thái: Accepted
- Ngày: 2026-07-23
- Phạm vi: P3-02D, P3-05 và contract tích hợp Classroom Media ở Phase 4

## Bối cảnh

TutorHub cần một luồng tìm giờ học phù hợp chuyên nghiệp hơn việc organizer tự xem
free/busy rồi chọn giờ. TutorHub V1 đã có ý tưởng Availability Poll với link chia sẻ và
heatmap, nhưng dữ liệu ngày/slot được lưu bằng chuỗi/JSON, link dùng mã ngắn và không có
tenant policy, capability lifecycle hoặc privacy model đủ mạnh để chuyển thẳng sang V2.

When2meet có mental model đơn giản, quen thuộc: organizer chia sẻ link và người tham gia
đánh dấu thời gian trên heatmap. Tuy nhiên TutorHub không được phụ thuộc vào một website
bên ngoài cho dữ liệu lịch, quyền, SLA hoặc trải nghiệm cốt lõi; cũng không được dùng API
không chính thức, iframe, scrape hay sao chép giao diện/mã nguồn/nhãn hiệu.

Owner đồng thời yêu cầu student và các tài khoản TutorHub đang hoạt động được tạo khảo
sát và tổ chức buổi học nhóm online của chính mình. Quyền này phải tách khỏi quyền
`session.schedule`, vì một poll hoặc study meeting do student tạo không được mạo danh
buổi học chính thức, ghi attendance hay thay đổi lịch lớp authoritative.

ADR này chốt ownership, authorization, privacy, security và ranh giới phase. ADR-0019
vẫn sở hữu quyết định renderer/recurrence/conflict; ADR-0020 vẫn sở hữu
invitation/RSVP/iCalendar/email provider.

## Quyết định

### 1. TutorHub tự xây Availability Poll

Availability Poll là một capability native:

- React/TypeScript sở hữu editor và heatmap;
- Go modular monolith sở hữu command, policy, ranking và finalize;
- Neon PostgreSQL sở hữu trạng thái normalized, tenant-scoped;
- FullCalendar Standard có thể render lịch chính sau ADR-0019, nhưng không làm poll
  domain hoặc poll heatmap;
- When2meet chỉ là comparator về mental model link chia sẻ và drag/paint heatmap.

Production bundle/runtime không được:

- gọi When2meet;
- nhúng iframe hoặc scrape trang;
- dùng API không chính thức;
- fork/copy mã, UI, asset hoặc trademark của When2meet;
- biến object của thư viện renderer thành database schema.

### 2. Tách `ClassSession` và `StudyMeeting`

TutorHub duy trì hai outcome khác nhau:

- `ClassSession` là buổi học chính thức, luôn gắn class authoritative. Chỉ actor có
  `session.schedule` mới tạo/sửa/hủy và có thể nối attendance/lifecycle giáo dục.
- `StudyMeeting` là lịch học nhóm không chính thức, do một member sở hữu. Nó không được
  ghi attendance/grade, thay lịch lớp hay tự nhận nhãn buổi học chính thức.

P3-02D triển khai poll, kết quả xếp hạng và scheduling intent của `StudyMeeting`. Phase 4
mới triển khai `MediaRoom`, LiveKit token, start/join, lobby, moderation, reconnect và
room lifecycle. Nút “Mở phòng ngay” chỉ được mô tả là hoạt động sau khi vertical slice
Phase 4 đạt; không âm thầm kéo LiveKit lifecycle vào P3-02D.

### 3. Permission và ownership

Shared policy theo ADR-0013 thêm các capability deny-by-default:

- `availability.poll.create`;
- `availability.poll.manage_own`;
- `study_meeting.schedule_own`;
- `availability.poll.publish_to_class` cho fan-out tới roster;
- `room.create.instant` là authorization target của Phase 4.

Mọi active authenticated tenant member, gồm student và guest có tài khoản, được tạo poll,
quản lý poll của mình và lên lịch Study Meeting của mình nếu feature/quota cho phép.
External hoặc anonymous responder không được tạo poll/meeting nếu chưa có account và
active membership.

Quyền sở hữu poll không nâng quyền:

- poll chỉ được bind `class_id` khi creator là active member của class và có `class.read`;
  foreign/inaccessible class bị conceal `404`;
- organizer thiếu `session.schedule` chỉ finalize thành `StudyMeeting`;
- organizer có `session.schedule` và quyền trên class đích mới được chọn
  `ClassSession`;
- external responder không được finalize;
- student không được tạo arbitrary organization event hoặc broadcast tới roster chỉ vì
  đã tạo poll;
- fan-out tới class roster cần `availability.poll.publish_to_class`; một member vẫn có
  thể chia sẻ link class-only mà không enumerate email hay tự gửi hàng loạt;
- org admin có safety capability để revoke/close poll theo audit policy, không mặc nhiên
  trở thành owner của response.

Frontend không suy quyền từ role. API trả viewer capability projection; mọi mutation
reauthorize bằng principal, active tenant/membership, optional class và resource state
authoritative.

### 4. Share mode

Poll có đúng ba mode:

1. `class_members`: mặc định khi poll gắn class; chỉ active class member đủ quyền xem/
   respond. Link không cấp class membership.
2. `invited_only`: mỗi recipient có purpose-bound capability riêng; mặc định cho poll
   không gắn class.
3. `anyone_with_link`: organizer phải bật rõ ràng; unlisted, chỉ cấp projection và
   response scope tối thiểu.

`anyone_with_link` không phải public booking:

- không có public directory;
- không giữ/chốt chỗ;
- không capacity/payment/auto-confirm;
- responder không thể finalize;
- hệ thống không tự tạo session/meeting từ một response.

Đổi share mode phải revoke/rotate capability không còn hợp lệ.

### 5. Poll, slot và response semantics

Poll lưu:

- tenant, optional class, owner, title/description tối thiểu;
- timezone IANA, date range, working hours;
- meeting duration, slot granularity, deadline;
- participant/audience và share mode;
- optimistic version, lifecycle và timestamps.

Lifecycle là:

```text
draft -> open -> closed -> finalized
                 \-----> cancelled
open ------------------> cancelled
```

Response cho mỗi slot là `preferred`, `available` hoặc `unavailable`; chưa trả lời là
`unknown`, không được tự suy thành unavailable. Instant UTC và civil-time intent tuân
ADR-0017. Server xếp hạng bằng rule deterministic, công bố score/explanation bounded và
không biến ranking thành mutation tự động.

Desktop dùng drag/paint heatmap. Mobile dùng list/card theo ngày. Keyboard và screen
reader có action tương đương; state không truyền nghĩa chỉ bằng màu mà có text/icon/count.

Participant thông thường thấy response của mình và aggregate privacy-safe. Organizer và
teacher/admin đủ capability có thể xem individual response trong phạm vi poll; public
projection không trả roster, email, class detail, file, individual availability hoặc
calendar private detail.

### 6. Persistence

Schema normalized dự kiến:

- `availability_polls`;
- `availability_poll_slots`;
- `availability_poll_participants`;
- `availability_poll_responses`;
- `availability_poll_answers`;
- `availability_poll_capabilities`;
- `study_meetings`.

Slot/answer là row có constraint/index phù hợp, không lưu `date_list` hay
`available_slots` bằng JSON/string như V1. Mọi business row có `tenant_id`; optional
`class_id` phải dùng composite tenant/class integrity. Repository luôn predicate tenant
và conceal foreign resource bằng `404`.

Capability chỉ lưu hash, purpose, scope, expiry, revoked state và metadata tối thiểu.
Raw token chỉ trả một lần. Feature catalog/quota theo ADR-0015 phải giới hạn ít nhất số
ngày, slot, participant, poll đang mở, capability/invitation tạo theo giờ và fan-out.

### 7. Public capability exchange

Link bên ngoài có dạng:

```text
/availability/{public_id}#token=<opaque-secret>
```

Fragment không được gửi trong HTTP request/referrer. SPA:

1. đọc fragment;
2. POST token trong JSON tới endpoint exchange;
3. nhận short-lived, purpose-bound response session/handle;
4. xóa fragment bằng `history.replaceState`;
5. không lưu token trong `localStorage`.

Route đặt `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `noindex`; không chạy
analytics/click tracking trước exchange. App, proxy, audit, outbox, metric và error không
được chứa raw token, public link secret, raw email hay response detail.

Token phải có entropy cao, expiry, revoke, purpose/scope binding, rate limit và replay
policy. Broad public token không làm identity chung để sửa response người khác; anonymous
responder nhận response handle/edit secret riêng và database chỉ lưu hash.

### 8. API boundary

Authenticated contract dự kiến:

```http
POST  /api/v1/calendar/availability-polls
GET   /api/v1/calendar/availability-polls/{poll_id}
PATCH /api/v1/calendar/availability-polls/{poll_id}
POST  /api/v1/calendar/availability-polls/{poll_id}/open
PUT   /api/v1/calendar/availability-polls/{poll_id}/responses/me
GET   /api/v1/calendar/availability-polls/{poll_id}/summary
POST  /api/v1/calendar/availability-polls/{poll_id}/finalize
POST  /api/v1/calendar/availability-polls/{poll_id}/cancel
POST  /api/v1/calendar/availability-polls/{poll_id}/capabilities
POST  /api/v1/calendar/availability-polls/{poll_id}/capabilities/{capability_id}/revoke
```

External exchange/respond contract dự kiến:

```http
POST /api/v1/calendar/availability-polls/resolve
POST /api/v1/calendar/availability-polls/respond
```

Đây là architecture boundary; OpenAPI review có thể tinh chỉnh path/name mà không đổi
semantics. Token không nằm trong query/path. External response chỉ nhận projection
allowlist và không thể dùng client-supplied tenant/class/role.

### 9. Finalize, transaction và side effect

Finalize:

1. load poll/owner/viewer/class authoritative;
2. validate lifecycle, capability, expected version và idempotency key;
3. re-expand/recheck conflict trong transaction;
4. tạo đúng một `ClassSession` hoặc `StudyMeeting` theo permission;
5. link outcome vào poll và chuyển `finalized`;
6. append audit allowlist và transactional outbox;
7. commit rồi mới phân phối notification/email.

Hai request finalize đồng thời hoặc retry không được tạo hai outcome. Frontend preview và
ranking không phải authority. Poll được finalized/cancelled không nhận response mới.

P3-05 gửi sau commit:

- poll opened;
- deadline reminder;
- poll cancelled;
- poll finalized;
- invitation/ICS nếu outcome là session/meeting có delivery contract phù hợp.

Mỗi recipient có một effect và capability riêng để không lộ roster. Provider failure
không rollback poll/session/meeting; retry/dead-letter/idempotency tuân ADR-0018,
deliverability và suppression tuân ADR-0020.

## Hệ quả

### Tích cực

- TutorHub sở hữu dữ liệu, UX, quyền, privacy và roadmap của một năng lực cốt lõi.
- Teacher và student đều có công cụ tìm giờ học mà không trao quyền tạo buổi học chính
  thức cho student.
- Link ngoài vẫn hữu ích nhưng không biến TutorHub thành public booking service.
- Schema normalized hỗ trợ ranking, concurrency, audit và query bounded tốt hơn V1.
- Boundary `StudyMeeting` cho phép Phase 4 cấp phòng media cho mọi member mà không làm
  loãng `ClassSession`.

### Chi phí và rủi ro

- TutorHub phải tự xây heatmap, capability exchange, mobile/a11y và anti-abuse.
- Public link có rủi ro token leakage, spam, roster enumeration và privacy leak; gate
  bảo mật ở trên là điều kiện rollout.
- Số slot/response có thể phình nhanh; quota, hard cap, index và load test là bắt buộc.
- Scope dễ trượt sang media; P3-02D không được tự mint LiveKit token hay tuyên bố room
  runtime đã hoàn thành.

## Phương án đã loại

### Dùng hoặc nhúng When2meet

Loại vì tạo external runtime/data/SLA dependency, khó áp shared policy, audit, tenant
privacy, email/ICS và không có integration contract được TutorHub kiểm soát.

### Fork một hệ thống polling/booking hoàn chỉnh

Loại ở Phase 3 vì tăng stack/license/security/upgrade burden và không khớp domain
ClassSession/StudyMeeting. OSS vẫn có thể được đọc làm comparator nếu license cho phép.

### Port TutorHub V1

Loại vì DAO/model V1 thiếu tenant/timezone/DST/version/audit, dùng chuỗi/JSON cho
slot/response và link capability yếu. Chỉ giữ product insight.

### Cho mọi poll finalize thành ClassSession

Loại vì poll ownership không phải quyền quản lý class và sẽ phá shared authorization.

## Acceptance bắt buộc

- Student và active member khác tạo, mở/chia sẻ và quản lý poll của mình; fan-out tới
  roster vẫn cần `availability.poll.publish_to_class`.
- Class-only link từ external bị từ chối; invited/public capability chỉ nhận projection
  tối thiểu.
- Creator không thể bind poll vào class mà mình không phải active member/không có
  `class.read`; foreign/inaccessible `class_id` bị conceal `404`.
- Link expired/revoked/rate-limited không dùng lại được; đổi share mode rotate token.
- Public view không lộ roster/email/individual availability/private calendar detail.
- Hai responder đồng thời không ghi đè nhau; broad link không sửa response người khác.
- Student thiếu `session.schedule` không thể finalize thành `ClassSession`.
- Teacher đủ quyền có thể finalize thành `ClassSession`; cả hai outcome vẫn conflict-check.
- Retry/concurrent finalize không tạo hai session/meeting.
- Cross-tenant ID bị conceal; timezone/DST tuân ADR-0017.
- Raw token không xuất hiện trong query, referrer, application/proxy log hoặc analytics.
- Email/provider lỗi không rollback nghiệp vụ và retry không tạo effect thứ hai.
- Heatmap đạt desktop drag/paint, mobile, keyboard, screen reader và forced-colors.
- Production bundle/runtime không có request, iframe, scrape hay code copy từ When2meet.
