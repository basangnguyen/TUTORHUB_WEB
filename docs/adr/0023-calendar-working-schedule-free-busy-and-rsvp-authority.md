# ADR-0023: Calendar working schedule, free/busy và RSVP authority

- Trạng thái: **Accepted**
- Ngày: 2026-07-28
- Phạm vi: P3-02C và nền authority cho P3-02D/P3-05A
- Bổ sung: ADR-0017, ADR-0019 và phần audience/RSVP domain của ADR-0020

## Bối cảnh

P3-02A đã cung cấp Calendar shell/read projection và P3-02B đã khóa recurrence cùng
class conflict. P3-02C cần thêm working schedule, attendee, privacy-safe free/busy,
suggested time và RSVP mà không tạo một generic Calendar event owner mới hoặc để client
suy quyền từ role.

ADR-0019 yêu cầu exact range/participant/interval/candidate cap và resource authority
trước implementation. ADR-0020 đã mô tả audience snapshot, RSVP source of truth và
external capability, nhưng cố ý còn `Proposed` vì SES/provider/interoperability chưa đạt
gate. Core P3-02C không được chờ email delivery, đồng thời cũng không được gọi phần
provider của ADR-0020 là production-ready.

## Quyết định

### 1. Ownership và ranh giới domain

1. Calendar module sở hữu `WorkingSchedule`, privacy-filtered availability projection và
   deterministic suggested-time ranking. Nó không sở hữu mutation của ClassSession hay
   ClassSessionSeries.
2. Classroom source domain tiếp tục sở hữu organizer, audience mutation, lifecycle,
   class/teacher conflict và invitation revision của session/series/occurrence.
3. PostgreSQL TutorHub là source of truth duy nhất cho RSVP. Email/ICS/provider status
   không phải business response và không được cập nhật attendance, enrollment, grade
   hoặc join permission.
4. P3-02C chỉ commit encrypted revision/recipient snapshot trung tính: `lifecycle` vẫn là
   source truth, còn revision-level `method` là `NULL` và không có per-recipient MIME/effect.
   P3-05A mới materialize per-recipient delivery effect/payload, derive `REQUEST`/`CANCEL`
   từ audience diff và lifecycle, phân phối CTA/ICS đã commit và gọi lại RSVP command của
   P3-02C. Worker không đọc roster hiện tại để tự dựng hoặc thay đổi audience.
5. Các clause audience diff, organizer transition, RSVP retain/reset và external
   capability tại ADR-0020 được dùng làm contract domain cho P3-02C. ADR-0020 vẫn
   `Proposed` cho SES, MIME/iCalendar interoperability, provider event và production
   deliverability.

### 2. WorkingSchedule

1. Mỗi user có một schedule versioned trong một IANA timezone. Không có user override
   nghĩa là dùng tenant default và response phải ghi rõ `tenant_default`; không được giả
   là user đã cấu hình.
2. Weekly interval dùng local civil time, half-open `[start_local, end_local)`,
   `start_local < end_local`, cùng ngày và không overlap. Tối đa **8 interval/weekday**,
   tức tối đa 56 interval cho một weekly schedule.
3. Một schedule giữ tối đa **366 date exception**. Exception có ngày local và một trong:
   `holiday`, `out_of_office`, `special_hours`. `holiday`/`out_of_office` thay working
   intervals của ngày bằng rỗng; `special_hours` thay toàn bộ weekly intervals của ngày
   bằng danh sách interval cùng validation/cap như weekday.
4. PUT là full replacement có `expected_version`; stale version trả 409. IANA zone sai,
   interval overlap, cap/range sai hoặc duplicate exception trả validation error mà
   không ghi một phần.
5. Owner thấy schedule chi tiết. Scheduling Assistant chỉ nhận projection đã lọc theo
   policy; tenant admin hoặc organizer không mặc nhiên có quyền đọc schedule riêng của
   người khác.

### 3. Canonical free/busy và conflict authority

1. Availability interval chỉ có status:

   ```text
   free | tentative | busy | out_of_office | unknown
   ```

   cùng range và opaque participant/resource reference. Không trả title, description,
   class, file, roster hoặc source event detail.
   Availability request phải có `class_id` đã được server authorize; internal participant
   chỉ là class owner hoặc active roster của class đó, external participant chỉ là active
   audience của class đó. ID không tồn tại và ID không đủ scope đều bị conceal cùng 404.
2. External/no-sync participant và source chưa được allowlist luôn là `unknown`, không
   phải `free`. Cancelled hoặc `show_as=free` không block. Mọi overlap dùng half-open
   interval nên hai event chạm biên không conflict.
3. Class conflict tiếp tục là hard authority của backend. Teacher/organizer conflict chỉ
   là hard conflict khi assignment/attendee authoritative đã resolve; policy có thể cho
   admin override kèm reason/audit. Student overlap là privacy-safe warning. Resource
   chưa có domain contract trả `unknown` và không được frontend biến thành hard fact.
4. Ordinary participant chỉ thấy own RSVP và aggregate được phép. Guest list hoặc
   individual status cần `can_see_guest_list`/Scheduling Assistant capability do server
   trả. Foreign tenant/class/session/series/occurrence bị conceal 404.

### 4. Suggested-time algorithm và hard cap

1. Một availability request bắt buộc có bounded range, scheduling IANA timezone,
   required/optional participant, elapsed duration, grid step và `max_candidates`.
2. Hard cap được chấp nhận:

   - range tối đa **31 calendar day** trong scheduling timezone;
   - tối đa **50 distinct participant** sau dedupe, tính cả required và optional;
   - duration từ **15 đến 480 phút**;
   - step chỉ nhận **15, 30 hoặc 60 phút**;
   - trả tối đa **20 candidate**;
   - đánh giá tối đa **2.000 candidate start** cho một request;
   - deadline xử lý tối đa **250 ms**.

   Request vượt cap bị từ chối trước expansion. Deadline/candidate-start cap bị chạm trả
   bounded partial/unavailable semantics, không âm thầm tăng ngân sách.
3. Candidate grid được dựng theo civil time của scheduling timezone. Label trong DST gap
   bị bỏ; DST overlap tạo hai instant theo offset, sort theo instant và dedupe bằng
   `(start_instant, end_instant)`. End instant bằng start instant cộng elapsed duration.
4. Candidate không bị hard conflict được sort lexicographic tăng dần theo tuple:

   ```text
   (
     required_out_of_office,
     required_busy,
     required_unknown,
     required_tentative,
     required_outside_working_schedule,
     optional_out_of_office,
     optional_busy,
     optional_unknown,
     optional_tentative,
     optional_outside_working_schedule,
     start_instant,
     stable_slot_key
   )
   ```

   Vì vậy quality order là `free > tentative > unknown > busy > out_of_office`.
5. Response có bounded reason breakdown cho từng candidate và stable
   `empty_suggestions_reason`; không chứa source event detail. Frontend chỉ render kết
   quả và explanation, không tự xếp hạng authoritative.

### 5. Audience, invitation và RSVP

1. Một source event có tối đa **128 distinct attendee recipient** sau khi collapse
   roster/manual duplicate. Organizer nếu đồng thời là recipient chỉ chiếm một row.
   P3-02C commit không quá 128 recipient snapshot; P3-05A sau đó chỉ được materialize tối đa
   128 delivery effect từ revision đó. Vượt cap fail atomically.
2. Participation `required|optional` tách khỏi business role. Recipient là internal
   `user_id` hoặc protected/minimized external guest, có source `roster|manual`,
   `show_as`, `visibility`, `response_requested` và guest permissions.
3. Audience replacement có `expected_version` và idempotency key, resolve authoritative
   trong cùng transaction rồi tạo deterministic `added|removed|unchanged|role_change`
   diff cùng immutable invitation snapshot. RSVP retain/reset, organizer transfer/
   disable và class archive tuân ADR-0020; worker không suy policy.
4. RSVP state chỉ gồm:

   ```text
   needs_action | accepted | tentative | declined
   ```

   Response giữ invitation sequence, source, `responded_at`, expected version và
   idempotency. Organizer override là command riêng có capability, reason và audit.
5. Authenticated participant chỉ thay response của chính mình. External participant dùng
   recipient/sequence/purpose-bound capability có ít nhất 128-bit CSPRNG entropy; database
   chỉ lưu versioned HMAC/hash, expiry/revoke và bounded use metadata. Token cũ không
   mutate invitation sequence mới.
6. External resolve/respond giữ rate cap ADR-0020: resolve 10/phút/token fingerprint và
   30/phút/IP; respond 5/10 phút/token và 20/10 phút/IP. Lỗi invalid/expired/revoked/
   superseded trả generic response để chống enumeration. Capability không cấp quyền đọc
   class, file, roster, guest list hoặc join room.
7. CTA chỉ POST vào cùng RSVP command. Phase 3 vẫn dùng CTA-only/`RSVP=FALSE`; không hứa
   native email reply nếu chưa có inbound parser/security ADR.

### 6. API boundary

OpenAPI implementation phải có ít nhất:

```http
GET  /api/v1/calendar/working-schedule
PUT  /api/v1/calendar/working-schedule
POST /api/v1/calendar/availability/query
GET  /api/v1/classes/{class_id}/sessions/{session_id}/attendees
PUT  /api/v1/classes/{class_id}/sessions/{session_id}/attendees
POST /api/v1/classes/{class_id}/sessions/{session_id}/responses
POST /api/v1/calendar/invitations/resolve
POST /api/v1/calendar/invitations/respond
```

Series và occurrence phải có typed source/scope boundary tương đương, gồm stable
`occurrence_key`, `expected_version` và idempotency key; implementation không được chỉ
hỗ trợ one-time session rồi tuyên bố P3-02C hoàn tất. Path cuối có thể được OpenAPI review
tinh chỉnh nhưng không đổi semantics này.

Authenticated endpoint luôn derive tenant/user từ session, yêu cầu
`X-TutorHub-Expected-Tenant-ID`; mutation yêu cầu CSRF. Public capability nằm trong JSON
POST, không nằm trong path/query. Resolve/respond dùng `no-store`, `no-referrer`,
`noindex`, CSP chặt và generic error.

### 7. Feature control và failure semantics

1. P3-02C tái sử dụng feature `class_session_scheduling` và deployment kill switch
   `FEATURE_CONTROL_DISABLE_CLASS_SESSION_SCHEDULING`. Không thêm feature key mới.
   Feature/quota/authorization đều do server enforce; client chỉ dùng capability để ẩn
   hoặc disable UI.
2. Kill switch/feature off fail closed trước query expansion và audience mutation. Exact
   caps trong ADR này là hard safety ceiling, tenant policy chỉ được hạ thấp, không được
   tăng nếu chưa có ADR/security/performance review.
3. Validation dùng 400; foreign/inaccessible source dùng 404; stale version, closed RSVP
   hoặc idempotency reuse khác payload dùng 409; cap/rate limit dùng 429 cùng
   `Retry-After`; dependency/deadline unavailable dùng 503. Conflict detail chỉ chứa dữ
   liệu viewer được phép thấy.

## Phương án không chọn

- Thêm feature key riêng cho từng Calendar subfeature: tăng control surface chưa cần;
  `class_session_scheduling` và deployment kill switch hiện có đủ cho P3-02C.
- Coi unknown là free: tạo lịch sai và che mất provider/source chưa đồng bộ.
- Để frontend tính suggested time hoặc conflict: phá privacy và race authority.
- Worker đọc roster lúc gửi: tạo audience drift giữa publish và retry.
- Một session-only attendee contract: không đáp ứng recurrence/occurrence identity đã
  chốt ở ADR-0019.
- Mark ADR-0020 Accepted để unblock code: làm sai trạng thái provider/domain/DNS và
  interoperability chưa đạt.

## Hệ quả

- Core WorkingSchedule/free-busy/RSVP có thể triển khai và test độc lập với SES/domain.
- Query và fan-out bị bounded từ API đến repository; tenant lớn cần policy/ADR mới thay
  vì âm thầm tăng giới hạn.
- Supporting series/occurrence làm OpenAPI/repository phức tạp hơn nhưng tránh tạo source
  of truth thứ hai hoặc endpoint one-time không mở rộng được.
- External guest cần crypto/key/retention boundary trước activation; raw email/token vẫn
  bị cấm trong outbox, log, audit và metric.

## Acceptance

1. OpenAPI và migration phản ánh đúng caps, CAS, source scope và canonical enums.
2. Unit/golden tests khóa total-order tuple, unknown-vs-busy và stable reason codes.
3. DST gap/overlap, schedule exception và boundary-touching interval đạt test.
4. Concurrent RSVP/audience mutation chỉ có một authoritative result; idempotent replay
   không tạo revision/effect thứ hai.
5. Audience diff/RSVP retain-reset/organizer transfer/archive đạt ADR-0020 domain tests.
6. Cross-tenant/cross-class/guest-list IDOR bị conceal; free/busy không lộ source detail.
7. Kill switch và `class_session_scheduling` fail closed trước query/mutation; audience
   128 và availability hard caps không thể bypass ở client.
8. Scheduling Assistant có text/icon/reason, dual timezone và keyboard/screen-reader
   equivalent; heatmap color không phải kênh nghĩa duy nhất.
