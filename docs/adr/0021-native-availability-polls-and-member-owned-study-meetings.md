# ADR-0021: Native Availability Poll và member-owned Study Meeting

- Trạng thái: Accepted
- Ngày: 2026-07-23
- Làm rõ sau readiness review: 2026-07-23
- Phạm vi: P3-02D, P3-05B và contract tích hợp Classroom Media ở Phase 4

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

- poll chỉ được bind `class_id` khi creator là active member của class và có `class.view`;
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
draft -------------> cancelled
open --------------> cancelled
closed ------------> cancelled
closed ------------> open
```

`close` có thể do owner/safety admin hoặc deadline worker chạy idempotent. `reopen` chỉ
được phép khi chưa finalized/cancelled, deadline mới hợp lệ và tập slot không đổi; giữ
response, tăng version và ghi audit/outbox. Slot/timezone/duration sửa tự do ở draft;
sau khi poll open và có response thì các field làm đổi nghĩa answer là immutable. Muốn
đổi phải đóng/cancel và tạo revision mới.

`cancel` là command idempotent từ `draft`, `open` hoặc `closed` bởi owner/safety admin;
`finalized` không chuyển ngược thành `cancelled`. Muốn hủy outcome đã finalize phải dùng
command cancel riêng của ClassSession/StudyMeeting để giữ audit, invitation và delivery
semantics đúng nguồn.

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

Với `anyone_with_link`, aggregate chỉ hiện sau explicit response và đạt minimum cohort
theo tenant policy; dưới ngưỡng trả trạng thái chưa đủ phản hồi. Đây là mitigation, không
thể bảo đảm chống differencing/Sybil. Public projection dùng coarse bucket và không lộ
exact responder count; exact count/individual response chỉ dành cho organizer hoặc
capability nội bộ. Anonymous identity dùng response handle/edit capability hash riêng;
broad share token không sửa response của người khác. Dedupe chỉ bảo đảm retry idempotent
theo response handle/idempotency key, không thể tuyên bố one-human-one-response.
Retention/purge, prefix/token/poll rate limit, abuse signal và uniform error phải được
chốt cùng hard cap ở P3-02D.

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
Flag, hard cap và kill switch phải được thêm ngay trong P3-02D; P3-13 chỉ hợp nhất
catalog/dashboard, không trì hoãn enforcement của poll.

### 7. Public capability exchange

Link bên ngoài có dạng:

```text
/availability/{public_id}#token=<opaque-secret>
```

Fragment không được gửi trong HTTP request/referrer. SPA:

1. đọc token từ fragment vào biến memory;
2. xóa fragment đồng bộ bằng `history.replaceState` trước mọi network call;
3. POST token từ memory trong JSON tới endpoint exchange;
4. xóa biến token ngay sau exchange và nhận short-lived, purpose-bound response
   session/handle;
5. không lưu token trong `localStorage`, session storage hoặc IndexedDB.

Route đặt `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `noindex`; không chạy
analytics/click tracking trước exchange. Landing dùng CSP chặt:
`default-src 'none'`, chỉ allow self script/style/connect cần thiết,
`base-uri 'none'`, `form-action 'none'`, `frame-ancestors 'none'`; không tải third-party
script/resource trước exchange. App, proxy, audit, outbox, metric và error không được chứa
raw token, public link secret, raw email hay response detail.

Token phải có entropy cao, expiry, revoke, purpose/scope binding, rate limit và replay
policy. Broad public token không làm identity chung để sửa response người khác; anonymous
responder nhận response handle/edit secret riêng và database chỉ lưu hash.

Tối thiểu dùng 128-bit CSPRNG, token versioned, HMAC/hash-at-rest và constant-time
comparison. Link dựng từ canonical HTTPS origin, không tin `Host`/`X-Forwarded-Host`.
Landing GET/security-scanner prefetch không được vote/finalize; mutation chỉ sau explicit
POST + confirm. Short-lived handle dùng Secure/HttpOnly/SameSite cookie hoặc memory-only
handle cùng Origin/CSRF protection phù hợp. Không chạy analytics, service worker hoặc
email click tracking trước capability exchange.

### 8. API boundary

Authenticated contract dự kiến:

```http
POST  /api/v1/calendar/availability-polls
GET   /api/v1/calendar/availability-polls/{poll_id}
PATCH /api/v1/calendar/availability-polls/{poll_id}
POST  /api/v1/calendar/availability-polls/{poll_id}/open
POST  /api/v1/calendar/availability-polls/{poll_id}/close
POST  /api/v1/calendar/availability-polls/{poll_id}/reopen
PUT   /api/v1/calendar/availability-polls/{poll_id}/responses/me
GET   /api/v1/calendar/availability-polls/{poll_id}/summary
POST  /api/v1/calendar/availability-polls/{poll_id}/finalize
POST  /api/v1/calendar/availability-polls/{poll_id}/cancel
POST  /api/v1/calendar/availability-polls/{poll_id}/capabilities
POST  /api/v1/calendar/availability-polls/{poll_id}/capabilities/{capability_id}/revoke
GET   /api/v1/calendar/study-meetings
POST  /api/v1/calendar/study-meetings
GET   /api/v1/calendar/study-meetings/{meeting_id}
PATCH /api/v1/calendar/study-meetings/{meeting_id}
POST  /api/v1/calendar/study-meetings/{meeting_id}/cancel
```

External exchange/respond contract dự kiến:

```http
POST /api/v1/calendar/availability-polls/resolve
POST /api/v1/calendar/availability-polls/respond
```

Đây là architecture boundary; OpenAPI review có thể tinh chỉnh path/name mà không đổi
semantics. Token không nằm trong query/path. External response chỉ nhận projection
allowlist và không thể dùng client-supplied tenant/class/role.

Active member có `study_meeting.schedule_own` được tạo StudyMeeting trực tiếp hoặc từ
poll. Owner được update/cancel; safety admin chỉ recovery/revoke với reason/audit.
StudyMeeting Phase 3 là timed scheduling intent, có version/conflict check và không tự
mint LiveKit token. Audience/RSVP/email chỉ bật sau ADR-0020 có contract tương ứng.

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

P3-05B gửi sau commit:

- poll opened;
- poll reopened với recipient snapshot và deadline/version mới;
- deadline reminder;
- poll cancelled;
- poll finalized;
- direct StudyMeeting scheduled/rescheduled/cancelled;
- invitation/ICS nếu outcome là session/meeting có delivery contract phù hợp.

Manual close mặc định chỉ ghi audit + in-app organizer, không tự broadcast. Deadline
auto-close phải do durable worker P3-03 claim theo due time và phát đúng một lifecycle
event. Mỗi recipient có một effect và capability riêng để không lộ roster; effect dedupe
theo `(source_type, source_id, recipient_id, effect_type, source_version, channel)`.
Provider failure không rollback poll/session/meeting; retry/dead-letter/idempotency tuân
ADR-0018, deliverability và suppression tuân ADR-0020.

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
  `class.view`; foreign/inaccessible `class_id` bị conceal `404`.
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
- Close/deadline-auto-close/reopen/edit-after-response tuân state machine và không làm
  mất hoặc tái diễn giải response âm thầm.
- Active member tạo/update/cancel StudyMeeting trực tiếp hoặc từ poll; student không thể
  nâng nó thành ClassSession/attendance.
- Public aggregate dưới minimum cohort bị suppress; trên ngưỡng chỉ trả coarse bucket/
  không lộ exact responder count. Tài liệu/UI nói rõ đây là mitigation, không bảo đảm
  chống differencing/Sybil hoặc one-human-one-response; retention, purge, rate/quota và
  abuse/load test đạt.
- Production bundle/runtime không có request, iframe, scrape hay code copy từ When2meet.
