# P3-CAL-02 — AWS SES sandbox và interoperability runbook

## Mục đích và trạng thái

Runbook này dùng để kiểm chứng AWS SES v2 target của
[ADR-0020](../adr/0020-calendar-invitation-rsvp-icalendar-and-ses.md) trong sandbox cô
lập. Nó không provision production, không nối Core API/outbox và không cho phép gửi
email nghiệp vụ.

- Trạng thái: **PREPARED — chưa có live evidence**
- Provider call: SES v2 `SendEmail` với `Content.Raw`
- Recipient: đúng một owner-controlled verified identity mỗi call
- Tracking: bắt buộc tắt
- Runtime delivery: bị khóa cho tới P3-03B, P3-02C và P3-05A

## Quy tắc an toàn tuyệt đối

1. Chỉ dùng nội dung synthetic và identity/hộp thư do owner kiểm soát.
2. Không dùng roster, email học sinh, dữ liệu lớp thật hoặc business invitation.
3. Không paste AWS access key, secret, session token, email, raw capability, account ID,
   queue URL hoặc DNS verification value vào terminal transcript, Git, ticket hay chat.
4. Credential chỉ nằm trong AWS-authenticated operator session hoặc provider secret
   store. Ưu tiên workload identity/short-lived credential.
5. Không dùng SES `Simple` hoặc `Template`; calendar flow chỉ dùng canonical
   `Content.Raw`.
6. Mỗi submission có đúng một `To`; không `CC/BCC`.
7. Không bật open/click tracking, redirect/URL shortener hoặc remote tracking asset.
8. Không gửi lại ngay khi timeout/reset có thể đã submit; chuyển
   `outcome_unknown`, chờ grace/reconcile.
9. Không gọi `accepted` là delivered/inbox/read.
10. Nếu bất kỳ preflight nào không đạt, dừng. Không nới gate để “test cho xong”.

## Vai trò

| Vai trò | Trách nhiệm |
| --- | --- |
| Owner/operator AWS | xác nhận account/region, identity, quota, billing, configuration set |
| Spike operator | tạo synthetic canonical MIME, gọi adapter/sandbox, ghi evidence redact |
| Client reviewer | quan sát Gmail/Google Calendar, Outlook và Apple Calendar |
| Reviewer bảo mật | kiểm identity/privacy, tracking, log/error và artifact trước sign-off |

Một người có thể giữ nhiều vai trò ở môi trường cá nhân, nhưng evidence phải ghi rõ ai
đã thực hiện review; không ghi địa chỉ email.

## Biến danh nghĩa trong evidence

Dùng alias, không dùng giá trị live:

```text
aws-account-A
ses-region-A
configuration-set-A
sender-A
recipient-A
effect-A-create
effect-A-update
effect-A-cancel
```

Provider MessageId/effect ID chỉ lưu dưới dạng opaque reference đã redact hoặc hash phục
vụ correlation. Không dùng email, tenant, user, class, event hay title làm provider tag.

## Preflight AWS SES

### 1. Account, region và sandbox

- [ ] Chọn đúng một AWS account và một SES region; mọi identity, configuration set,
      quota và provider event đều region-specific.
- [ ] Xác nhận operator đang ở đúng account/region bằng giao diện/CLI đã đăng nhập mà
      không in credential.
- [ ] Ghi `sandbox=true/false` và timestamp vào evidence; không ghi account ID.
- [ ] Xem current daily send quota và maximum send rate.
- [ ] Kiểm tra billing budget/alarm và kill switch trước submission.
- [ ] Xác nhận account-level suppression policy; không auto-unsuppress complaint.

Nếu account đã production-access, lượt này vẫn phải dùng allowlist owner-controlled và
quy tắc sandbox của runbook; không tận dụng quyền production để gửi business email.

### 2. Verified identities

- [ ] `sender-A` là personal email identity do owner kiểm soát, status verified trong
      đúng region.
- [ ] `recipient-A` là personal email identity do owner kiểm soát, status verified
      trong đúng region khi account còn sandbox.
- [ ] Không chụp/lưu địa chỉ thật trong evidence repository.
- [ ] Không dùng identity của học sinh, khách hàng hoặc mailbox không được đồng ý.

Personal identities chỉ phục vụ pre-domain spike. Chúng không chứng minh sending
domain, DKIM/SPF/DMARC, custom MAIL FROM hoặc production readiness.

### 3. Configuration set

- [ ] Tạo/chọn `configuration-set-A` trong đúng region.
- [ ] Open tracking: disabled.
- [ ] Click tracking: disabled.
- [ ] Không có remote pixel, redirect hoặc provider template rewrite.
- [ ] Event destinations nếu đã provision chỉ trỏ tới resource đã review.
- [ ] Configuration set name live không được ghi vào public artifact; evidence dùng alias.

Nếu không chứng minh tracking tắt, dừng trước gửi.

### 4. IAM và adapter

- [ ] Principal chỉ có send permission cần thiết cho identity/configuration set đã duyệt.
- [ ] Không dùng migration/database credential cho SES.
- [ ] Adapter đã ép AWS SDK no-retry/`MaxAttempts=1`.
- [ ] Deadline, rate/concurrency limiter không vượt current SES quota/rate.
- [ ] Error trả ra đã sanitize; không chứa recipient/raw MIME/provider payload.
- [ ] Domain/application package không import AWS SDK.

## Preflight canonical fixture

Trước SES, chạy sink/local tests và ghi kết quả vào
[P3_CAL_02_SPIKE_EVIDENCE.md](./P3_CAL_02_SPIKE_EVIDENCE.md):

```powershell
$env:GOCACHE = 'D:\TutorHub_V2\.gocache-temp'
Push-Location services/core-api
go test ./internal/spikes/calendarinvitation/... -count=1 -cover
go vet ./internal/spikes/calendarinvitation/...
Pop-Location
```

Chỉ tiếp tục khi:

- [ ] create/update/cancel golden đạt;
- [ ] cùng effect retry dùng exact canonical bytes/hash;
- [ ] MIME có plain, HTML và đúng một inline calendar part;
- [ ] MIME method bằng `VCALENDAR METHOD`;
- [ ] UID ổn định, sequence tăng đơn điệu;
- [ ] CRLF, 75-octet folding, UTF-8/escaping đạt;
- [ ] one-recipient/không CC-BCC đạt;
- [ ] `RSVP=FALSE`, CTA-only và không lộ attendee khác;
- [ ] fixture không chứa PII/secret/capability thật.

Không lấy live email làm golden. Canonical bytes có thể chứa synthetic address cố định
và chỉ được lưu trong testdata nếu đã qua privacy scan.

## Trình tự sandbox submission

### Bước 1 — Initial invitation

1. Chọn fixture `effect-A-create` với `METHOD:REQUEST`, stable UID và `SEQUENCE:0`.
2. Render một lần; persist/đọc lại canonical bytes theo spike contract và kiểm SHA-256.
3. Gọi provider adapter với:
   - canonical raw MIME bytes;
   - `sender-A`;
   - đúng một `recipient-A`;
   - `configuration-set-A`;
   - bounded opaque effect reference.
4. Xác nhận request dùng `SendEmail` + `Content.Raw`; không `Simple`/`Template`.
5. Khi SES trả MessageId, ghi state `accepted`; không ghi “delivered”.
6. Ghi evidence redact: timestamp, region alias, effect alias, result class, latency,
   canonical hash và provider reference đã redact.

### Bước 2 — Update/reschedule

1. Dùng cùng UID, tăng sequence đúng một lần và `METHOD:REQUEST`.
2. Xác nhận retry của effect create không bị dùng thay cho effect update.
3. Submit đúng một recipient với cùng configuration set/tracking policy.
4. Ghi `accepted` hoặc failure class theo cùng quy tắc redaction.
5. Reviewer xác nhận client giữ một item, không tạo item thứ hai.

### Bước 3 — Cancellation

1. Dùng cùng UID, sequence mới, `METHOD:CANCEL` và `STATUS:CANCELLED`.
2. Submit đúng một recipient.
3. Ghi kết quả redact.
4. Reviewer xác nhận cancel tác động item cũ theo behavior của client và không tạo item
   mới.

Mỗi bước dùng nội dung synthetic và không gửi quá số lần cần thiết để đóng matrix.

## Failure semantics

| Quan sát | State/action |
| --- | --- |
| SES success + bounded MessageId | `accepted`; chờ provider/client evidence |
| Validation/identity/configuration chắc chắn sai | permanent/suppressed/dead-letter theo mapping |
| Definite pre-submit local failure | retryable theo bounded policy |
| Timeout/network reset sau possible submit | `outcome_unknown`; không retry ngay |
| Bounce/complaint event hợp lệ | project severity/suppression; không auto-unsuppress |
| Revision mới xuất hiện | supersede effect cũ chưa gửi |

`outcome_unknown`:

1. bắt đầu grace mặc định 30 phút;
2. reconcile bằng opaque effect reference, provider MessageId/event evidence và current
   sequence;
3. nếu hết grace, không có evidence và effect vẫn current, chỉ một controlled resend
   bằng đúng persisted canonical bytes;
4. ambiguous lần hai vào `dead_letter`; không loop;
5. nếu duplicate-risk proxy đạt alert threshold, pause ambiguous resend và điều tra.

Không cố tình tạo live timeout bằng cách làm gián đoạn mạng nếu thao tác có thể gửi
duplicate không kiểm soát. Kiểm error mapping bằng fake/sink; live ambiguous test chỉ
thực hiện khi có procedure correlation và owner duyệt.

## Provider-event topology acceptance

Target production:

```text
SES Configuration Set
  -> EventBridge default event bus
  -> allowlisted rule
  -> encrypted SQS Standard queue
  -> SQS DLQ
  -> provider-event consumer
  -> PostgreSQL provider_event_inbox + history/projection
```

Không thay bằng SNS HTTPS trong Phase 3.

### Provision/review checklist

- [ ] Rule allowlist đúng AWS source/detail type/account/region/configuration set.
- [ ] Queue policy chỉ cho đúng EventBridge rule/account.
- [ ] Queue/DLQ encryption, retention, visibility timeout và redrive đã review.
- [ ] Visibility timeout dài hơn consumer handler deadline.
- [ ] Consumer IAM chỉ receive/delete resource cần thiết.
- [ ] Unexpected source, schema, region, configuration set, oversize hoặc malformed event
      vào quarantine/DLQ.
- [ ] Consumer chỉ delete SQS message sau PostgreSQL inbox/history commit.
- [ ] Inbox dedupe và out-of-order severity state machine đạt.
- [ ] Backlog age, oldest message, provider-event lag và DLQ alarms hoạt động.
- [ ] Replay/redrive yêu cầu authorization, bounded batch, reason và audit.

### Acceptance fixture

1. Dùng provider event synthetic đã sanitize để test duplicate/out-of-order local.
2. Khi live topology đã provision, gửi một synthetic sandbox invitation theo trình tự
   trên.
3. Correlate bằng opaque effect reference/provider reference; không dùng email trong log.
4. Re-deliver cùng event để chứng minh inbox idempotency.
5. Gửi/đưa event out-of-order có kiểm soát để chứng minh Delivery không hạ
   Bounce/Complaint severity.
6. Đưa malformed synthetic event để chứng minh quarantine/DLQ.
7. Controlled redrive một record được duyệt và xác nhận audit.

Nếu chưa có AWS resource hoặc PostgreSQL consumer, ghi `BLOCKED`; local fixture không
thay thế live topology evidence.

## Bounce, complaint và suppression

- Local spike dùng normalized synthetic provider events; không dùng email thật trong
  fixture.
- SES account-level suppression phải được review cho bounce/complaint.
- TutorHub suppression projection dùng keyed recipient fingerprint, không raw email.
- Complaint/hard bounce không được retry bằng template/channel khác.
- Unsuppress cần owner/recipient verification, capability riêng, reason và audit.
- Raw DLQ/provider payload không copy vào ticket/chat/evidence.

Không kích hoạt complaint/bounce tới mailbox người khác chỉ để test. Nếu live
suppression acceptance cần AWS simulator hoặc phương pháp khác, phải có review riêng
theo tài liệu AWS hiện hành, nội dung synthetic và owner approval; không trộn với
cross-client matrix.

## Cross-client interoperability

Dùng cùng lineage synthetic create -> update/reschedule -> cancel. Reviewer ghi kết quả
vào evidence matrix, không chụp địa chỉ email.

Với từng Gmail/Google Calendar, Outlook và Apple Calendar:

- [ ] create tạo đúng một item;
- [ ] update giữ một item, same UID/new sequence;
- [ ] cancel tác động item cũ và không tạo item mới;
- [ ] timezone/DST hiển thị đúng;
- [ ] recurrence exception gắn đúng occurrence;
- [ ] CTA TutorHub còn nguyên, không bị tracking rewrite;
- [ ] không thấy recipient/guest khác;
- [ ] UI/copy không hứa native RSVP đồng bộ TutorHub khi ICS `RSVP=FALSE`.

Client không có sẵn ghi `BLOCKED`, không suy từ parser/unit test.

## Evidence được phép lưu

Được phép:

- commit/deploy/test command và terminal PASS/FAIL không chứa secret;
- timestamp, OS/tool version;
- alias region/configuration set/identity;
- canonical SHA-256 của synthetic fixture;
- opaque effect/provider reference đã redact;
- bounded state/result/latency/quota số học;
- screenshot đã che hoàn toàn address/account/resource/token.

Không được phép:

- AWS key/session token, email hoặc account ID;
- raw capability/deep-link token;
- raw live MIME/provider event/DLQ payload;
- queue URL/ARN nếu policy yêu cầu giữ kín;
- DNS verification token/CNAME value;
- roster, class/event title thật hoặc dữ liệu học sinh.

Trước commit, chạy diff review và secret/PII scan. Nếu evidence từng lộ secret, dừng,
rotate/revoke trước rồi mới sanitize; không chỉ xóa khỏi commit mới.

## Kill switch, rollback và cleanup

Khi phát hiện gửi sai, tracking rewrite, privacy leak, quota spike hoặc provider incident:

1. dừng sandbox sender/adapter call;
2. giữ runtime registration/send feature flag tắt;
3. không rollback business mutation/lịch đã commit;
4. đánh dấu effect chưa gửi là retained/superseded có audit, không blanket success;
5. với ambiguous attempt, giữ `outcome_unknown` và reconcile; không retry tức thì;
6. kiểm suppression/provider events bằng opaque ID;
7. rotate credential nếu có dấu hiệu lộ và ghi incident ngoài repository bảo mật phù hợp;
8. chỉ resume sau owner/security review.

Sau spike:

- không xóa identity/configuration set hoặc event resource đang dùng bởi workload khác;
- remove/revoke chỉ resource cô lập đã xác định và có owner approval;
- xóa local temporary raw MIME/live transcript; giữ synthetic golden/hash cần thiết;
- giữ tracking disabled;
- không bật production access/domain/runtime flag chỉ vì sandbox PASS.

## Domain/DNS follow-up bắt buộc

Sandbox personal identities không đóng các mục sau:

- owner-controlled sending domain;
- SES identity `mail.<owned-domain>`;
- Easy DKIM CNAME;
- custom MAIL FROM `bounce.mail.<owned-domain>` với MX/SPF;
- DKIM/SPF alignment và DMARC monitor;
- custom MAIL FROM failure behavior fail closed;
- SES production access, quota/rate/capacity và deliverability canary.

DNS value phải lấy trực tiếp từ SES console/IaC của đúng region và không đưa vào
repository.

## Điều kiện kết thúc runbook

Runbook chỉ đạt khi:

1. local deterministic/sink gates PASS;
2. SES sandbox create/update/cancel đạt bằng owner-controlled verified identities;
3. tracking được chứng minh tắt;
4. cross-client matrix Gmail/Google Calendar, Outlook và Apple Calendar đạt hoặc có
   explicit blocker được decision owner chấp nhận;
5. provider-event topology/suppression evidence đạt hoặc còn explicit blocker giữ ADR
   `Proposed`;
6. domain/DNS/production-access vẫn được ghi đúng trạng thái, không bị sandbox che lấp;
7. không có business send, runtime wiring, secret hoặc PII trong evidence.

PASS của runbook không tự bật delivery. P3-03B, P3-02C và P3-05A vẫn là dependency bắt
buộc.
