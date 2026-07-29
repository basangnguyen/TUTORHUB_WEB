# ADR-0020: Calendar invitation, RSVP, iCalendar và AWS SES

- Trạng thái: **Proposed**
- Ngày đề xuất: 2026-07-26
- Phạm vi: P3-CAL-02, P3-02C, P3-05A/P3-05B và mọi Calendar email về sau
- Bổ sung: ADR-0018 và ADR-0019

## Trạng thái quyết định

ADR này cố ý giữ `Proposed` trong khi Gate C2 chưa có đủ bằng chứng. Việc tài liệu khóa
contract không đồng nghĩa AWS SES, domain hay đường gửi runtime đã production-ready.
Chỉ chuyển sang `Accepted` sau khi:

1. deterministic renderer và golden create/update/cancel đạt;
2. SES sandbox/sink cô lập đạt bằng identity do owner kiểm soát;
3. Gmail/Google Calendar, Outlook và Apple Calendar đạt create/update/cancel trong phạm
   vi được nêu dưới đây;
4. topology provider event, suppression và runbook có bằng chứng vận hành;
5. các blocker domain/DNS được ghi rõ, không bị gọi nhầm là đã hoàn tất.

Ngay cả khi ADR được chấp nhận ở cấp decision spike, delivery tới end user vẫn bị khóa
bởi P3-03B durable worker, P3-02C và P3-05A.

## Bối cảnh

TutorHub cần gửi invitation, update, cancellation và reminder có thể được Calendar
client hiểu, nhưng lịch TutorHub và RSVP trong TutorHub vẫn là source of truth. Lỗi email
không được rollback lịch đã commit. Worker chạy at-least-once theo ADR-0018, trong khi
AWS SES v2 `SendEmail` không có caller idempotency token và một timeout có thể xảy ra
sau khi request đã rời process. Vì vậy không thể tuyên bố exactly-once ở ranh giới SMTP.

Recipient còn có thể đến từ roster động, attendee thủ công hoặc external guest. Nếu
worker tự đọc roster hiện tại lúc gửi, một lần retry có thể gửi cho audience khác với
lần publish. Nếu MIME/ICS được render lại theo clock hoặc boundary ngẫu nhiên, cùng một
logical effect có thể thành nhiều message khác nhau và Calendar client có thể tạo item
trùng.

AWS SES chỉ xác nhận đã nhận request khi trả về thành công. Provider event `Delivery`
chỉ cho biết máy chủ thư đích đã chấp nhận message, không chứng minh message đã vào inbox
hoặc được người nhận đọc. Open/click tracking còn có thể rewrite capability link và phá
security contract của RSVP.

Phase 3 chưa triển khai inbound mailbox/iTIP parser hoặc Google/Microsoft two-way sync.
Do đó native Accept/Decline trong email client không được hứa là sẽ cập nhật TutorHub.

## Mục tiêu và tiêu chí

- Audience, RSVP và iCalendar identity ổn định qua retry, reschedule và cancel.
- Không lộ roster/email/capability giữa recipient.
- MIME/ICS deterministic ở mức byte mà ứng dụng gửi cho SES.
- Domain không phụ thuộc AWS SDK; provider có thể thay bằng adapter khác.
- Provider call chỉ xảy ra sau commit qua leased worker.
- Ambiguous acceptance, duplicate, out-of-order event, bounce, complaint và suppression
  có state machine, metric và runbook.
- Có đường nâng cấp sang domain/DNS production mà không đổi domain contract.

## Quyết định

### 1. Aggregate, organizer và audience snapshot

`created_by_user_id` là audit identity bất biến. `organizer_user_id` là principal nội bộ
đang chịu trách nhiệm cho invitation và phải có capability trên source event; creator và
organizer có thể khác nhau. Sender mailbox kỹ thuật không được dùng thay cho organizer
business identity.

Khi publish, reschedule, change audience hoặc cancel, command service resolve audience
authoritative **trong business transaction** rồi lưu một revision snapshot:

- invitation/source/tenant/class/series/occurrence identity và version;
- organizer display name/address snapshot đã allowlist;
- recipient identity, loại internal/external và delivery address bảo vệ;
- `required` hoặc `optional`, nguồn `roster` hoặc `manual`;
- `response_requested`, guest permission, locale và viewer timezone;
- canonical IANA event/viewer timezone và version của tzdata/container đã pin;
- `ical_uid`, `ical_sequence`, source lifecycle (`published|updated|cancelled`) và canonical render inputs đã mã hóa; các input này là business snapshot, không phải rendered MIME bytes hay per-recipient delivery method;
- audience revision, created/updated/cancelled reason và actor.

Revision và recipient row là **neutral, immutable business snapshot**. P3-02C persist encrypted
canonical business snapshot/render input, nhưng không persist rendered MIME bytes, provider
attempt/reference, delivery state, delivery effect, recipient-specific delivery method hay outbox
delivery handler và không coi snapshot là email đang chờ gửi. Worker không đọc roster hiện tại
để quyết định người nhận. P3-05A sẽ materialize **sau commit** một delivery effect riêng cho
từng recipient snapshot, derive `REQUEST` hoặc `CANCEL` từ immutable audience diff và source
lifecycle, cố định đúng một địa chỉ `To`, không dùng `CC/BCC`. ICS
mặc định chỉ chứa organizer và recipient hiện tại. Guest list chỉ được thêm khi server trả
`can_see_guest_list`, policy cho phép và interoperability test chứng minh không lộ dữ liệu.
External guest không bao giờ nhận roster lớp chỉ vì có invitation.

Migration `000021` giữ an toàn dữ liệu thử nghiệm trước boundary bằng cách không xóa value
cũ. Encrypted canonical business snapshot (`canonical_payload_*` và key version) vẫn là runtime
input bất biến cho renderer về sau; chỉ revision-level `method` legacy không phải runtime input
và không được consumer đọc. Mọi revision tạo sau boundary bắt buộc có `method = NULL`; P3-05A
sẽ derive method, tạo rendered MIME payload/effect chuyên dụng khi delivery runtime thật được mở.

Recipient nội bộ được định danh bằng `user_id`. External guest chỉ lưu dữ liệu giao hàng
tối thiểu trong bảng có quyền hạn chế; lookup/dedupe dùng keyed fingerprint, giá trị có
thể gửi phải được mã hóa bằng key ngoài database. Không ghi raw email vào outbox, log,
metric, audit detail hoặc provider tag. Retention/redaction theo tenant policy và legal
requirement; delivery history giữ opaque recipient reference thay vì giữ email vô hạn.

Snapshot có thể ghi `rsvp_state`/`rsvp_source` như historical value tại thời điểm revision
được tạo, nhưng không bao giờ là current RSVP. Current RSVP chỉ nằm ở attendee authority của
PostgreSQL; response sau đó không update snapshot và không tạo delivery effect trước P3-05A.

#### Audience diff

Mỗi command tạo diff deterministic theo stable recipient identity:

| Diff | Delivery effect (P3-05A, chưa tạo ở P3-02C) | RSVP/capability |
| --- | --- | --- |
| `added` | materialize `REQUEST` ở sequence hiện tại | `needs_action`; capability mới |
| `removed` | materialize `CANCEL` cùng UID với sequence mới | đóng response; revoke capability; giữ RSVP cũ chỉ làm history |
| `unchanged` | chỉ materialize update khi thay đổi user-visible hoặc policy bắt buộc | retain/reset theo bảng dưới |
| `role_change` | materialize `REQUEST` với role mới | reset `needs_action`; rotate capability |

`REQUEST`/`CANCEL` trong bảng là delivery method của **recipient effect**, không phải field của
revision snapshot. Một revision lifecycle `updated` có thể đồng thời materialize `REQUEST` cho
`added` và `CANCEL` cho `removed`; lifecycle `cancelled` materialize `CANCEL` cho effect phù hợp.

Re-add sau removal là `added`, không khôi phục RSVP/capability cũ. Duplicate giữa roster
và manual được collapse theo stable identity; policy có độ ưu tiên rõ và không tạo hai
effect.

RSVP được giữ khi chỉ đổi metadata không làm thay đổi cam kết tham dự, như description
allowlist, deep link hoặc formatting. RSVP reset về `needs_action` khi đổi:

- start/end/duration/timezone hoặc recurrence membership;
- required/optional role;
- organizer;
- occurrence/series identity do split;
- policy `response_requested`.

Title/location đổi không tự reset, nhưng UI phải đánh dấu “đã cập nhật”. Organizer có
thể explicit request-response lại bằng audited command; command này tăng sequence và
rotate external capability.

#### Organizer transition và archive

- Transfer organizer là command có authorization, expected version và audit; giữ UID,
  tăng sequence, reset RSVP và gửi `REQUEST` từ organizer snapshot mới.
- Organizer bị disable/rời scope không được tự động giả mạo bằng người khác. Publish,
  resend và update bị pause ở trạng thái `organizer_attention_required` cho tới khi
  admin/class owner chuyển organizer bằng audited command.
- Cancel do safety/admin hoặc archive được phép dùng system actor nhưng vẫn dùng last
  published organizer snapshot trong ICS; lý do/actor nằm trong audit, không lộ vào
  provider tag.
- Archive/cancel source đóng response, revoke capability và gửi `CANCEL` cho mọi
  recipient từng nhận revision còn active. Snapshot được giữ đủ để cancel/reconcile rồi
  redaction theo retention policy.

### 2. RSVP source of truth và external capability

Source of truth là PostgreSQL TutorHub, không phải trạng thái hiển thị của Gmail,
Outlook, Apple Calendar hay SES. Trạng thái:

```text
needs_action | accepted | tentative | declined
```

Mỗi response giữ `responded_at`, invitation sequence và source bounded:

```text
tutorhub_authenticated | tutorhub_external_capability | organizer_override
```

Organizer override cần capability riêng, reason và audit. RSVP không thay attendance,
enrollment, grade hoặc quyền join.

Phase 3 mặc định là **CTA-only**:

- ICS luôn phát `RSVP=FALSE`;
- email nói rõ “Phản hồi trong TutorHub”;
- internal user POST bằng authenticated session;
- external guest POST bằng recipient-bound capability;
- không parse `METHOD:REPLY`, không đọc mailbox và không tuyên bố native email RSVP
  parity.

`RSVP=TRUE` hoặc inbound `METHOD:REPLY` chỉ được bật bằng ADR bổ sung có parser bounded,
sender/authentication mapping, spoof/replay protection, quarantine và Gmail/Outlook/
Apple end-to-end evidence.

External capability:

- raw token có ít nhất 128-bit entropy từ CSPRNG;
- database chỉ lưu versioned HMAC/hash, purpose, invitation/recipient/sequence scope,
  issued/expiry/revoked timestamps và bounded use metadata;
- token của sequence cũ không mutate sequence mới;
- mặc định hết hạn tại `event end + 7 ngày`, nhưng không quá 90 ngày từ lúc phát;
- cancel, removal, organizer transfer, response closure và supersede đều revoke/rotate;
- broad resolve scope và respond scope tách biệt; capability không cấp quyền đọc lớp,
  file, roster, guest list hoặc join room;
- URL dùng canonical HTTPS origin và token ở fragment. SPA xóa fragment đồng bộ bằng
  `history.replaceState` trước network call rồi POST token trong JSON;
- public route dùng `Cache-Control: no-store`, `Referrer-Policy: no-referrer`,
  `X-Robots-Tag: noindex`, CSP chặt và không tải analytics/third-party resource trước
  exchange;
- GET, prefetch hoặc security scanner không được accept/decline; mutation cần explicit
  POST, Origin/CSRF check và confirm UI;
- compare constant-time, response lỗi generic để không enumerate recipient.

Rate limit ban đầu:

- resolve: `10/phút/token-fingerprint` và `30/phút/IP`;
- respond: `5/10 phút/token-fingerprint` và `20/10 phút/IP`;
- thêm tenant/global quota, exponential penalty và abuse alert.

Các giá trị có thể giảm bằng configuration, nhưng tăng vượt mức trên cần security
review. Raw token, email và IP không dùng làm metric label.

### 3. iCalendar/iTIP/iMIP subset

#### Identity và sequence

- `ical_uid` sinh đúng một lần bằng UUID ngẫu nhiên có global uniqueness và serialize
  canonical dạng `urn:uuid:<lowercase-uuid>`; không phụ thuộc domain gửi email.
- UID bất biến suốt invitation lineage, kể cả reschedule và cancel.
- First publish dùng `SEQUENCE:0`. Mỗi externally visible revision tăng đúng một lần
  trong transaction; sequence không giảm, không tái sử dụng và không tăng cho idempotent
  no-op.
- Mọi recipient effect được P3-05A materialize từ cùng revision dùng cùng sequence. P3-05A
  derive `REQUEST`/`CANCEL` theo audience diff và source lifecycle; revision snapshot sau
  boundary không lưu một delivery method. Worker không sở hữu việc tăng sequence; P3-02C không
  tạo effect.
- Effect update/reschedule dùng cùng UID, sequence mới và `METHOD:REQUEST`.
- Effect cancel dùng cùng UID, sequence mới, `METHOD:CANCEL` và `STATUS:CANCELLED`.
- Occurrence override/cancel dùng UID của series và stable `RECURRENCE-ID`.

Với `edit following`, series cũ được truncate bằng một `REQUEST` sequence mới; series
mới nhận UID mới và bắt đầu sequence `0`. Exception trước split giữ UID cũ; exception
từ boundary được rebase sang series mới theo ADR-0019 hoặc transaction bị reject. Không
gán UID cũ cho hai active master.

#### RFC subset

Gate C2 hỗ trợ subset có chủ đích của:

- RFC 5545 cho `VCALENDAR`/`VEVENT`;
- RFC 5546 cho outbound `REQUEST` và `CANCEL`;
- RFC 6047 cho iMIP outbound qua MIME email.

Một `VCALENDAR` có đúng một `METHOD` và tối thiểu:

```text
BEGIN:VCALENDAR
PRODID:-//TutorHub//Calendar Invitation 1.0//EN
VERSION:2.0
CALSCALE:GREGORIAN
METHOD:REQUEST|CANCEL
BEGIN:VEVENT
UID
DTSTAMP
DTSTART/DTEND
SEQUENCE
SUMMARY
ORGANIZER
ATTENDEE
...
END:VEVENT
END:VCALENDAR
```

Timed event luôn dùng IANA `TZID` và `VTIMEZONE` deterministic; không phát floating
time. Recurrence dùng đúng subset ADR-0019 với `RRULE`, `RECURRENCE-ID` và `EXDATE`.
`ORGANIZER`, recipient-specific `ATTENDEE`, `ROLE`, `PARTSTAT`, `RSVP=FALSE`,
`LOCATION`, canonical HTTPS deep link và description allowlist được phát khi có dữ liệu
hợp lệ. `STATUS:CANCELLED` chỉ dùng cho cancellation.

Không tuyên bố full RFC 6047 security conformance, S/MIME hoặc inbound iTIP. Unknown
property/repetition, unbounded recurrence và dữ liệu không qua validator bị reject.
Tên host-local như `Local` bị cấm. Build/container pin tzdata; snapshot lưu version
được render policy kiểm tra. Sau lần render đầu, retry dùng bytes đã persist nên upgrade
tzdata không được âm thầm render lại effect cũ.

### 4. Deterministic MIME và canonical bytes

P3-05A renderer nhận immutable invitation/recipient snapshot rồi materialize effect riêng;
P3-02C không render hay persist canonical MIME bytes. Renderer không nhận raw outbox payload
hoặc clock trực tiếp. `DTSTAMP`, locale, subject, boundary và mọi render input được persist
cùng effect trước provider call. Kết quả gồm canonical bytes và SHA-256; retry cùng
effect/revision phải dùng **đúng bytes đã persist**, không render lại.

Canonical bytes nằm trong bảng/record `calendar_delivery_payload` chuyên dụng, mã hóa
at-rest bằng envelope key ngoài database, có `effect_id`, hash, key version, size,
created/expiry/redacted timestamps và quyền đọc chỉ dành cho delivery worker/operator
được audit. Business transaction atomically tạo payload + effect ledger + outbox
reference; outbox chỉ giữ opaque payload/effect ID, không chứa raw MIME. Worker claim
effect rồi đọc payload theo ID, kiểm hash trước provider call. Payload giữ tới khi
effect terminal cộng cửa sổ reconcile/incident tối đa 30 ngày, sau đó redact bytes nhưng
giữ hash/opaque metadata theo retention; legal hold là policy riêng có audit. Không copy
payload vào audit detail, DLQ, log hay support ticket.

Canonical message là `multipart/alternative` với đúng ba part theo thứ tự:

1. `text/plain; charset=UTF-8`;
2. escaped `text/html; charset=UTF-8`;
3. một `text/calendar; charset=UTF-8; method=REQUEST|CANCEL`,
   `Content-Disposition: inline; filename="invite.ics"`.

Không đính kèm calendar part thứ hai. MIME `method` phải bằng `VCALENDAR METHOD`.
Invitation/update/cancel dùng SES v2 `SendEmail` với `Content.Raw`/`RawMessage` chứa
chính canonical bytes; `Simple` và `Template` bị cấm cho flow có iCalendar.

Quy tắc byte:

- header/property chống CRLF injection; subject/display name dùng RFC 2047 UTF-8 khi
  cần;
- body HTML encode, URL scheme/origin allowlist và không nhúng remote tracking asset;
- ICS escape `\\`, `,`, `;`, newline theo RFC;
- mọi line kết thúc CRLF;
- content line fold ở 75 octet, continuation bắt đầu bằng một space, không cắt giữa
  UTF-8 code point;
- ba MIME body dùng base64 deterministic, wrap 76 ký tự và CRLF;
- boundary, app-supplied Date và part order ổn định theo persisted snapshot; TutorHub
  không dựa vào transport `Message-ID` để nhận dạng lịch;
- golden bytes riêng cho create, metadata update, reschedule, role/audience change,
  recurrence override, split và cancel;
- tracking open/click luôn tắt; provider không được rewrite CTA capability URL.

“Canonical” ở đây là bytes TutorHub submit cho SES. SES hoặc downstream mail server có
thể thêm/đổi transport header; ADR không tuyên bố raw bytes cuối cùng trong inbox giống
tuyệt đối.

### 5. Provider abstraction và AWS SES v2

Domain/application định nghĩa port kiểu:

```text
Send(ctx, oneRecipient, canonicalRawBytes, configurationSet, opaqueEffectRef)
  -> accepted(providerMessageID) | retryable | permanent | outcomeUnknown
```

AWS SDK chỉ xuất hiện trong infrastructure adapter. Domain model, renderer và handler
không import AWS type. Adapter:

- dùng SES v2 `SendEmail` + `Content.Raw`;
- pin một account và region qua config; identity, sandbox/quota, suppression và
  Configuration Set đều region-specific;
- verified From identity và Configuration Set tracking-off được pin trong adapter
  configuration; caller/outbox chỉ được trùng khớp, không được tự chọn sender hoặc
  configuration set khác;
- một call/một recipient, không `CC/BCC`;
- gắn configuration set đã pin, enum `notification_type` bounded và opaque effect
  reference không chứa tenant, user, class, event hay email;
- có deadline, rate/concurrency limiter theo SES send quota và metric bounded;
- không log canonical body, email, credential hoặc provider payload;
- không tự render/rewrite MIME.
- ép AWS SDK `MaxAttempts=1`/no-retry cho `SendEmail`; mọi retry do effect ledger,
  grace/reconcile và worker policy quyết định, không để SDK âm thầm submit lần hai.

Giá/quota là provider configuration vận hành, không hard-code snapshot giá vào domain.
Runbook phải kiểm tra current send quota/rate và AWS billing alert trước rollout.

#### Accepted, timeout và idempotency

Application effect có unique key:

```text
(invitation_id, recipient_id, effect_type, ical_sequence, channel)
```

Attempt và state `sending` được persist trước provider call bằng lease/fencing của
ADR-0018.

- SES success + MessageId chuyển `accepted`; đây không phải inbox/delivery.
- Lỗi chắc chắn trước khi request rời process có thể vào `retry_wait`.
- Validation/identity/suppression lỗi permanent vào `suppressed` hoặc `dead_letter`.
- Timeout/network reset sau khi bytes có thể đã rời process vào `outcome_unknown`; không
  retry tức thì.

`outcome_unknown` có grace mặc định **30 phút**. Trong grace, consumer reconcile bằng
opaque effect reference, provider message/event evidence và current sequence. Sau grace,
nếu không có evidence và effect vẫn là revision hiện hành, chỉ cho **một** controlled
resend dùng cùng canonical submission bytes. SES/downstream có thể thay `Date` và sinh
transport `Message-ID` mới cho mỗi provider submission; Calendar dedupe/update dựa vào
stable `UID` + `SEQUENCE`, còn reconcile dựa effect ledger/provider MessageId. Ambiguous
lần thứ hai đi `dead_letter` để
operator xử lý; không loop gửi vô hạn.

SLO rủi ro duplicate bên ngoài: dưới **0,1% logical effects/30 ngày** có nhiều hơn một
provider submission do ambiguous outcome; alert ở `0,05%`. Đây là proxy đo từ attempt/
MessageId, không phải bằng chứng hai email đã vào inbox. Vi phạm SLO thì pause resend
ambiguous, điều tra event lag/provider health và không che bằng “exactly-once”.

Update/cancel sequence mới supersede effect cũ chưa gửi. Provider event trễ vẫn được
append history, nhưng không được làm revision cũ sống lại.

### 6. Provider event topology và state projection

Chọn đường production:

```text
SES Configuration Set
  -> EventBridge default event bus
  -> rule allowlist source/detail-type/account/region/configuration-set
  -> encrypted SQS Standard queue
  -> SQS DLQ
  -> P3-03 provider-event consumer
  -> PostgreSQL provider_event_inbox + delivery history/projection
```

Không chọn SNS HTTPS trong Phase 3. Vì không có public webhook, SNS
`SignatureVersion` không áp dụng. Trust boundary dùng AWS IAM/resource policy:

- queue chỉ nhận từ đúng EventBridge rule/account/region;
- consumer dùng least-privilege receive/delete;
- message phải qua schema/source/account/region/configuration-set allowlist;
- unexpected/oversize/malformed event vào quarantine/DLQ, không update delivery.

Nếu tương lai đổi sang SNS HTTPS, phải có ADR superseding và bắt buộc
`SignatureVersion=2`, HTTPS certificate-chain verification, TopicArn/account/region
allowlist, replay/timestamp protection, safe SubscriptionConfirmation và chỉ trả `2xx`
sau PostgreSQL inbox commit.

SQS Standard là at-least-once và có thể out-of-order. Consumer:

1. claim message;
2. normalize/redact;
3. insert PostgreSQL inbox idempotently;
4. append delivery history và project state bằng expected version/severity;
5. commit;
6. mới delete SQS message.

Inbox dedupe dùng canonical hash của:

```text
(provider_message_id, event_type, recipient_fingerprint,
 event_timestamp, provider_event_id_if_present)
```

Không dùng arrival time làm last-write-wins. Transition chính:

```text
pending/retry_wait -> sending
sending -> accepted | outcome_unknown | retry_wait | suppressed | dead_letter
accepted -> delivered_to_recipient_server | bounced | complained
delivered_to_recipient_server -> complained
outcome_unknown -> accepted | delivered_to_recipient_server | bounced | complained
                   | retry_wait | dead_letter
pending/retry_wait -> superseded
```

`complained` và suppression có severity cao, không bị event Delivery đến trễ hạ cấp.
UI chỉ gọi `delivered_to_recipient_server` là “máy chủ nhận đã chấp nhận”; không hiển thị
“đã vào inbox”, “đã đọc” hoặc “đã xem”.

Queue visibility timeout phải dài hơn handler deadline; DLQ redrive, backlog age,
oldest-message age và provider-event lag có alarm. Replay/redrive cần authorization,
reason và audit; raw DLQ payload không được copy vào ticket/chat.

### 7. Bounce, complaint và suppression

- SES account-level suppression được bật cho bounce/complaint theo policy.
- TutorHub giữ suppression projection riêng bằng keyed recipient fingerprint; external
  API/UI không tiết lộ email có bị suppress ở tenant khác.
- Complaint và hard bounce tạo global technical suppression để tránh tiếp tục gửi tới
  địa chỉ hỏng/không mong muốn. Soft/transient bounce dùng retry bounded theo effect.
- Resend kiểm tra current sequence, app suppression và provider suppression trước call.
- Không auto-unsuppress complaint. Unsuppress cần xác minh owner/recipient, capability
  riêng, reason và audit.
- Cancellation không được bypass complaint suppression bằng cách đổi template/channel;
  in-app notification vẫn có thể phản ánh lịch đã cancel.
- Provider event có nhiều recipient bất thường bị reject/quarantine vì TutorHub gửi một
  recipient/call.

### 8. Sending domain và email authentication

Production dùng domain owner kiểm soát:

- SES identity: `mail.<owned-domain>`;
- From mặc định: `calendar@mail.<owned-domain>`;
- Easy DKIM bằng CNAME do SES cấp;
- custom MAIL FROM: `bounce.mail.<owned-domain>` với MX/SPF do SES cấp;
- DMARC trên organizational domain, ban đầu monitor `p=none`, sau evidence mới nâng
  `quarantine/reject`;
- alignment phải pass bằng DKIM aligned; custom MAIL FROM còn cho SPF aligned/fallback.

Không tạo nhiều SPF record cạnh tranh. DNS value luôn lấy từ SES console/IaC của đúng
region; không hard-code token DNS vào repository. Custom MAIL FROM failure behavior được
chọn là reject/fail closed cho production thay vì âm thầm dùng MAIL FROM không aligned.

Trước khi có domain:

- chỉ dùng personal sender và recipient identity do owner kiểm soát, đã verify trong
  cùng SES sandbox/region;
- không gửi business email tới end user;
- không coi personal identity, SES sandbox hoặc mailbox simulator là domain/DNS/
  production-access readiness;
- không nới exit gate Easy DKIM, SPF/DMARC alignment, custom MAIL FROM và production
  access.

SPF/DKIM/DMARC chỉ là authentication/deliverability; không thay S/MIME và không đủ để
tuyên bố full iMIP security.

### 9. Security, privacy và abuse controls

- IAM credential/secret chỉ ở provider secret store/runtime environment, không vào
  source, frontend, canonical bytes, fixture hoặc log. Ưu tiên workload identity/short
  lived credential khi host hỗ trợ.
- IAM role chỉ có quyền gửi từ identity/configuration set đã duyệt và đọc queue cần
  thiết; key rotation có runbook và dual-key window bounded.
- Header/HTML/ICS injection, oversized text, invalid address, dangerous Unicode
  bidi/format/zero-width controls, disallowed URL và recurrence exhaustion đều có
  negative test. Không tuyên bố phát hiện mọi Unicode homograph/confusable.
- Subject, description, location và organizer name có byte/line cap sau encoding.
- Fan-out có tenant quota, per-event audience cap và invitation-bombing protection.
- Không dùng email, recipient, token, title, description hoặc provider payload làm
  metric label.
- Audit/outbox chỉ giữ opaque IDs và bounded reason; không giữ raw MIME/capability.
- CTA tracking tắt; không dùng URL shortener hoặc redirect domain chưa được duyệt.
- Sink/sandbox fixture dùng synthetic content, không dùng roster hay dữ liệu học sinh.

### 10. Observability và runbook

Metric bounded tối thiểu:

- render count/error/latency/byte size;
- effect pending/sending/accepted/outcome_unknown/retry/dead-letter/superseded;
- provider call latency/error/quota throttle;
- EventBridge/SQS backlog age, consumer lag, inbox duplicate/out-of-order;
- delivered-to-recipient-server/bounce/complaint/suppression;
- ambiguous resend và duplicate-risk SLO;
- RSVP accepted/tentative/declined/latency và invalid/expired/revoked/rate-limited ở
  aggregate bounded.

Runbook bắt buộc có:

- pause/kill switch không làm mất business mutation;
- inspect effect/inbox/DLQ bằng opaque ID;
- controlled replay/redrive và supersede;
- ambiguous outcome grace/reconcile;
- bounce/complaint/suppression/unsuppress;
- SES quota/sandbox/production access và regional outage;
- rotate IAM key, DKIM và custom MAIL FROM;
- domain/DNS drift, DMARC report và deliverability incident;
- data redaction/retention và incident response.

### 11. Rollout, rollback và dependency gate

Rollout theo thứ tự:

1. ADR `Proposed`, renderer + deterministic sink/golden cô lập.
2. SES sandbox với owner-controlled verified identities và tracking off.
3. Gmail/Google Calendar, Outlook, Apple create/update/cancel interoperability.
4. Domain/Easy DKIM/SPF/DMARC/custom MAIL FROM, SES production access, quota/alarm và
   durable EventBridge/SQS/DLQ acceptance.
5. P3-03B durable worker + P3-02C audience/RSVP + P3-05A runtime implementation, feature
   gate mặc định tắt.
6. Internal canary, private alpha allowlist, rồi tăng fan-out theo metric/SLO.

Rollback vận hành là tắt registration/send feature flag và dừng provider consumer/call,
không rollback lịch đã commit. Effect chưa gửi được giữ/supersede có audit; không blanket
mark success. Disable sender/configuration set không xóa delivery/inbox history. Down
migration không chạy trên shared staging/production chỉ để rollback application.

Runtime bị chặn rõ:

- P3-CAL-02 spike không nối Core API và không consume outbox;
- provider call runtime phải sau commit qua ADR-0018 worker;
- P3-03B phải chứng minh durable host, lease/reclaim/duplicate/dead-letter;
- P3-02C phải triển khai audience/attendee/RSVP contract;
- P3-02C chỉ commit neutral invitation/recipient snapshot; P3-05A mới được tạo
  per-recipient delivery effect/payload, đăng ký handler invitation/reminder và consume
  delivery outbox;
- mọi gate domain/deliverability còn thiếu giữ feature flag tắt.

## Interoperability acceptance

Fixture synthetic phải chứng minh trên Gmail/Google Calendar, Outlook và Apple Calendar:

1. create tạo đúng một item;
2. update/reschedule giữ một item, cùng UID và sequence tăng;
3. cancel tác động item cũ theo client policy, không tạo item mới;
4. timezone/DST hiển thị đúng;
5. recurrence exception gắn đúng occurrence;
6. CTA TutorHub còn nguyên, không bị tracking rewrite;
7. không lộ recipient/guest khác;
8. native RSVP không được mô tả là đã đồng bộ TutorHub khi `RSVP=FALSE`.

Client không có sẵn để test phải được ghi `BLOCKED`, không suy PASS từ parser/unit test.

## Phương án đã cân nhắc

### Dùng SES `Simple`/`Template`

Không chọn cho Calendar. Provider sẽ sở hữu MIME assembly và có thể làm đổi calendar
part, boundary hoặc method; không đáp ứng canonical retry bytes.

### Dùng CC/BCC hoặc một ICS có toàn bộ attendee

Không chọn vì lộ roster/email/capability và làm recipient-specific RSVP khó idempotent.

### Bật `RSVP=TRUE` và nhận native reply ngay

Không chọn vì Phase 3 chưa có inbound parser, sender authentication mapping và spoof/
replay gate. Hiển thị native buttons nhưng không cập nhật TutorHub là UX sai.

### Retry ngay khi SES timeout

Không chọn vì request có thể đã được accepted và SES không có caller idempotency token.
Retry tức thì làm tăng duplicate không kiểm soát.

### SNS HTTPS webhook

Không chọn trong Phase 3 vì thêm public ingress, certificate/signature/subscription
confirmation và failure semantics. EventBridge -> SQS/DLQ phù hợp durable consumer hiện
có. SNS HTTPS chỉ được xem lại bằng ADR superseding.

### Đợi có domain rồi mới thiết kế

Không chọn. Provider port, deterministic renderer và sandbox identity có thể được chứng
minh trước; tuy nhiên domain/DNS gate vẫn không được bỏ.

### Gửi trực tiếp trong HTTP request

Không chọn. Nó kéo latency/failure provider vào business transaction và phá nguyên tắc
after-commit của ADR-0018.

## Hệ quả

### Tích cực

- Calendar client nhận identity/update/cancel ổn định.
- Domain không bị khóa vào AWS và retry không tự thay đổi MIME.
- Audience snapshot ngăn roster drift; one-recipient effect giảm rò rỉ dữ liệu.
- Ambiguous timeout và provider event có contract thay vì được gọi nhầm là exactly-once.
- Domain có thể bổ sung sau mà không đổi UID hay RSVP source of truth.

### Chi phí và rủi ro

- Một recipient/call tăng số provider call và ledger row; cần fan-out/quota/backpressure.
- Canonical MIME/VTIMEZONE/folding cần golden/conformance test nghiêm ngặt.
- EventBridge/SQS/DLQ thêm AWS resource, IAM, alarm và runbook.
- Không có inbound `METHOD:REPLY`, nên user phải dùng TutorHub CTA để RSVP.
- SMTP vẫn có thể duplicate hoặc bị spam filtering; SLO giảm rủi ro chứ không loại bỏ.
- Cross-client behavior khác nhau; mọi upgrade renderer/template phải lặp lại matrix.

## Non-goal

- Không triển khai code runtime Core API/outbox/handler trong P3-CAL-02.
- Không gửi business email tới end user trong spike.
- Không triển khai inbound mailbox, `METHOD:REPLY`, S/MIME hoặc full iTIP server.
- Không triển khai Google/Microsoft/Apple two-way account sync.
- Không làm marketing/bulk email, campaign, open/click analytics.
- Không mua/provision domain hoặc ghi DNS secret/token trong ADR.
- Không thay đổi recurrence/conflict authority của ADR-0019.
- Không thay đổi worker lease/at-least-once contract của ADR-0018.

## Bằng chứng cần gắn trước khi Accepted

- Link source/test của renderer, canonical snapshot và provider abstraction.
- Golden byte/hash create/update/reschedule/override/split/cancel.
- Parser/conformance, CRLF, 75-octet UTF-8 folding, MIME method-match và injection test.
- Sink proof cùng effect retry sinh exact canonical bytes.
- SES sandbox evidence đã redact: region/configuration set/identity/quota/tracking và
  accepted/outcome_unknown behavior; không ghi address/key/token.
- EventBridge/SQS/DLQ/inbox duplicate/out-of-order/replay evidence hoặc explicit blocker.
- Suppression/bounce/complaint fixture và runbook.
- Gmail/Google Calendar, Outlook, Apple matrix.
- Domain/DNS/production-access checklist ghi PASS/BLOCKED từng mục.

## Nguồn chuẩn

- [RFC 5545 — iCalendar](https://datatracker.ietf.org/doc/html/rfc5545)
- [RFC 5546 — iTIP](https://datatracker.ietf.org/doc/html/rfc5546)
- [RFC 6047 — iMIP](https://datatracker.ietf.org/doc/html/rfc6047)
- [AWS SES v2 SendEmail](https://docs.aws.amazon.com/ses/latest/APIReference-V2/API_SendEmail.html)
- [AWS SES raw email](https://docs.aws.amazon.com/ses/latest/dg/send-email-raw.html)
- [AWS SES verified identities](https://docs.aws.amazon.com/ses/latest/dg/verify-addresses-and-domains.html)
- [AWS SES event publishing](https://docs.aws.amazon.com/ses/latest/dg/monitor-using-event-publishing.html)
- [AWS SES EventBridge destination](https://docs.aws.amazon.com/ses/latest/dg/event-publishing-add-event-destination-eventbridge.html)
- [AWS SES Easy DKIM](https://docs.aws.amazon.com/ses/latest/dg/easy-dkim.html)
- [AWS SES custom MAIL FROM](https://docs.aws.amazon.com/ses/latest/dg/mail-from.html)
- [AWS SES DMARC](https://docs.aws.amazon.com/ses/latest/dg/send-email-authentication-dmarc.html)
- [AWS SES suppression](https://docs.aws.amazon.com/ses/latest/dg/sending-email-suppression-list.html)
