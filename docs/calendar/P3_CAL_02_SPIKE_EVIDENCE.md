# P3-CAL-02 invitation/iCalendar/provider spike evidence

- Ngày mở evidence: `2026-07-26`
- ADR: [ADR-0020](../adr/0020-calendar-invitation-rsvp-icalendar-and-ses.md)
  **Proposed**
- Phạm vi: deterministic invitation/audience/RSVP contract, iCalendar/MIME renderer,
  provider port và AWS SES v2 adapter cô lập
- Trạng thái decision gate: **VERIFY**
- Runtime delivery: **BLOCKED** bởi P3-03B, P3-02C, P3-05A và các gate provider/domain
  bên dưới

Tài liệu này là sổ bằng chứng, không phải tuyên bố TutorHub đã gửi email production.
Kết quả `PASS` chỉ được ghi khi có lệnh, artifact hoặc review thực tế. Chưa có bằng chứng
thì giữ `PENDING`, `VERIFY` hoặc `BLOCKED`; không suy `PASS` từ code review.

Commit cuối chứa evidence được xác định bởi Git history của file này; không chèn SHA dự
đoán trước khi commit.

## Ranh giới spike

P3-CAL-02 được phép:

- render deterministic synthetic invitation/update/cancel vào sink cô lập;
- kiểm tra byte/hash, MIME/iCalendar parser, audience diff và provider mapping;
- submit chính canonical raw MIME tới SES sandbox bằng identity do owner kiểm soát và
  đã verify, sau khi preflight trong runbook đạt;
- chạy interoperability thủ công bằng hộp thư thử nghiệm do owner kiểm soát.

P3-CAL-02 không được:

- nối Core API, consume outbox hoặc đăng ký handler runtime;
- gửi roster, dữ liệu học sinh hoặc email nghiệp vụ tới end user;
- bật `Simple`/`Template`, `CC/BCC`, open/click tracking hoặc inbound
  `METHOD:REPLY`;
- gọi SES bên trong business transaction hoặc gọi thành công `accepted` là “đã vào
  inbox”;
- gọi Gate C2 `DONE` khi domain/DNS, event topology hoặc client matrix còn thiếu.

## Candidate và dữ liệu kiểm thử

| Thuộc tính | Giá trị |
| --- | --- |
| Candidate commit | lấy từ Git history của lần chạy; chưa ghi trước |
| Nội dung fixture | synthetic, không dùng roster hoặc dữ liệu học sinh |
| Recipient/call | đúng một owner-controlled verified identity |
| MIME | `multipart/alternative`, plain -> HTML -> một calendar part |
| Provider flow | SES v2 `SendEmail` với `Content.Raw` |
| Tracking | bắt buộc tắt |
| Runtime wiring | không có trong spike |

Không ghi email, credential, raw capability, raw MIME, AWS account ID, queue URL,
provider payload hoặc DNS verification value vào tài liệu này. Evidence dùng alias
`sender-A`, `recipient-A`, opaque effect reference và giá trị đã redact.

## Ma trận gate tổng hợp

| Gate | Evidence cần có | Trạng thái hiện tại |
| --- | --- | --- |
| ADR-0020 contract | ADR khóa audience, RSVP, RFC subset, SES và topology | PASS document |
| Audience diff/lifecycle | unit test added/removed/unchanged/role-change + source precedence | PASS local |
| Stable UID/sequence | golden create/update/reschedule/cancel/split/override | PASS local |
| RFC subset | required fields + timezone/recurrence/master/override test | PASS local |
| Deterministic MIME/ICS | exact bytes/hash, CRLF, folding, escaping, method parity | PASS local |
| Retry canonical bytes | cùng immutable snapshot sinh đúng bytes/hash | PASS local |
| Provider abstraction | domain không import AWS type; adapter dùng SES v2 Raw | PASS local |
| SES error mapping | accepted/retryable/permanent/outcome-unknown, SDK no-retry | PASS local |
| Sink isolation | không Core API/outbox/runtime/business send | PASS local |
| SES sandbox | owner-controlled verified identities + configuration set | BLOCKED live evidence |
| EventBridge/SQS/DLQ/inbox | allowlist, IAM, dedupe, out-of-order, redrive | BLOCKED live topology |
| Bounce/complaint/suppression | synthetic local fixture + live policy evidence | VERIFY |
| Gmail/Google Calendar | create/update/cancel/timezone/CTA/privacy | VERIFY manual |
| Outlook | create/update/cancel/timezone/CTA/privacy | VERIFY manual |
| Apple Calendar | create/update/cancel/timezone/CTA/privacy | VERIFY manual |
| Sending domain | owner-controlled domain + identity | BLOCKED domain |
| Easy DKIM | SES-issued CNAME verified đúng region | BLOCKED domain/DNS |
| SPF/DMARC alignment | alignment evidence và DMARC monitor | BLOCKED domain/DNS |
| Custom MAIL FROM | MX/SPF + fail-closed policy | BLOCKED domain/DNS |
| SES production access/quota | access, current quota/rate, billing alarms | BLOCKED provider |
| Runtime delivery | P3-03B + P3-02C + P3-05A + feature gate | BLOCKED dependencies |

## Local automated evidence

### Lệnh tái lập

Chạy từ repository root. Dùng cache nằm trong workspace nếu máy không cho Go ghi cache
mặc định; không đưa cache vào Git.

```powershell
$env:GOCACHE = 'D:\TutorHub_V2\.gocache-temp'
Push-Location services/core-api
go test ./internal/spikes/calendarinvitation/... -count=1 -cover
go test ./internal/spikes/calendarinvitation -run=^$ `
  -fuzz=FuzzFoldContentLine -fuzztime=10s
go vet ./internal/spikes/calendarinvitation/...
go test ./... -count=1
go vet ./...
Pop-Location
```

Fuzz target chỉ nhận tối đa 8 KiB mỗi corpus input và bỏ input có CR/LF vì renderer chỉ
fold logical content line đã validate.

### Kết quả

| Check | Kết quả terminal | Trạng thái |
| --- | --- | --- |
| Focused unit/golden | PASS; domain `85,4%`, SES adapter `81,0%` statement coverage | PASS |
| Fuzz/resource cap | PASS; `FuzzFoldContentLine`, 10 giây, `1.288.460` executions | PASS |
| `go vet` focused packages | exit `0` | PASS |
| Full Core API tests | `go test ./... -count=1`, mọi package xanh | PASS |
| Full Core API vet | `go vet ./...`, exit `0` | PASS |
| Secret/PII scan cho diff | không có secret pattern; mọi address fixture thuộc reserved `example.com`, `example.edu`, `example.net` hoặc `example.test` | PASS |

Root agent cập nhật đúng output terminal sau khi chạy. Không cộng kết quả từ nhiều lần
partial thành một full-run giả.

## Deterministic artifact matrix

Mỗi fixture phải giữ cùng `UID` trong cùng lineage, chỉ tăng `SEQUENCE` ở revision thay
đổi externally visible và sinh đúng một canonical MIME/hash cho từng
effect/recipient/sequence.

| Fixture | METHOD | Identity expectation | Golden hash/byte result |
| --- | --- | --- | --- |
| Initial create | `REQUEST` | stable UID, sequence `0` | PASS golden hash |
| Metadata update | `REQUEST` | same UID, monotonic sequence | PASS golden hash |
| Reschedule | `REQUEST` | same UID, new sequence; RSVP reset | PASS golden hash |
| Role/audience change | `REQUEST`/`CANCEL` | recipient-specific effect | PASS golden hash/diff |
| Recurrence override | `REQUEST` | same series UID + recurrence ID | PASS golden hash |
| Edit-following split | `REQUEST` | old series truncated, new UID starts at `0` | PASS golden hash |
| Cancellation | `CANCEL` | same UID, new sequence, cancelled status | PASS golden hash |
| Exact retry | unchanged | exact persisted canonical bytes/hash | PASS deterministic test |

Golden artifact trong repository chỉ dùng synthetic address/content. Không lưu raw
external capability hoặc live provider header.

## MIME/iCalendar acceptance

| Invariant | Trạng thái |
| --- | --- |
| To đúng một recipient; không `CC/BCC` | PASS local |
| Plain, escaped HTML, đúng một inline `text/calendar` part | PASS local |
| MIME `method` bằng `VCALENDAR METHOD` | PASS local |
| `PRODID`, `VERSION:2.0`, `CALSCALE:GREGORIAN`, `UID`, `SEQUENCE` | PASS local |
| Timed event có IANA `TZID` và deterministic `VTIMEZONE` | PASS local |
| Bounded `RRULE`, `RECURRENCE-ID`, `EXDATE` | PASS local |
| `RSVP=FALSE`; CTA nói phản hồi trong TutorHub | PASS local |
| CRLF toàn bộ và content line fold tối đa 75 octet | PASS local + fuzz |
| UTF-8 không bị cắt giữa code point; ICS/header/HTML escaping | PASS local |
| Dangerous control/format injection và disallowed URL bị reject | PASS local |
| Ba MIME body base64 deterministic, wrap 76 ký tự | PASS local |
| Không remote tracking asset hoặc rewritten CTA | PASS renderer; tracking live BLOCKED |

## Provider adapter/sink acceptance

| Invariant | Trạng thái |
| --- | --- |
| Domain/application package không import AWS SDK | PASS local |
| SES adapter dùng `SendEmail` + `Content.Raw` | PASS local |
| `Simple`/`Template` không có trong calendar flow | PASS local |
| SDK retry bị tắt; effect ledger sở hữu retry | PASS local |
| Configuration set bắt buộc, tracking tắt | PASS pin; tracking live BLOCKED |
| Provider tag chỉ là bounded opaque reference | PASS local |
| Success có MessageId -> `accepted`, không gọi là delivered | PASS local |
| Timeout/reset sau possible submit -> `outcome_unknown` | PASS local |
| Definite validation/identity error -> permanent/suppressed | PASS local mapping |
| Sink không gọi network và trả behavior deterministic | PASS local |
| Raw MIME, email, credential/provider payload không vào log/error | PASS negative tests |

## SES sandbox evidence

Chỉ thực hiện theo
[P3_CAL_02_SES_SANDBOX_RUNBOOK.md](./P3_CAL_02_SES_SANDBOX_RUNBOOK.md).

| Preflight/observation | Evidence được phép ghi | Trạng thái |
| --- | --- | --- |
| Account/region pin | alias region, không account ID | BLOCKED |
| Sandbox status | `sandbox=true/false` tại thời điểm test | BLOCKED |
| `sender-A` verified | alias + verified yes/no | BLOCKED |
| `recipient-A` verified | alias + verified yes/no | BLOCKED |
| Configuration set | alias + tracking disabled | BLOCKED |
| Current send quota/rate | số liệu không chứa identity | BLOCKED |
| Billing alarm/kill switch | enabled yes/no | BLOCKED |
| Initial `REQUEST` accepted | redacted attempt/effect/provider refs | BLOCKED |
| Update accepted | redacted refs; same UID/new sequence | BLOCKED |
| `CANCEL` accepted | redacted refs; same UID/new sequence | BLOCKED |
| Recipient rendering | manual client matrix bên dưới | VERIFY |

`accepted` chỉ xác nhận SES nhận request. Chỉ provider event `Delivery` mới được diễn
đạt là `delivered_to_recipient_server`, vẫn không phải inbox/read evidence.

## Provider-event topology evidence

Production target:

```text
SES Configuration Set
  -> EventBridge
  -> encrypted SQS Standard queue
  -> SQS DLQ
  -> provider-event consumer
  -> PostgreSQL provider_event_inbox + delivery projection
```

| Gate | Trạng thái |
| --- | --- |
| EventBridge rule allowlist account/region/configuration-set/event type | BLOCKED |
| Queue resource policy chỉ cho đúng rule | BLOCKED |
| SQS encryption, visibility timeout, retention và DLQ redrive | BLOCKED |
| Least-privilege consumer IAM | BLOCKED |
| Inbox idempotency cùng duplicate SQS delivery | BLOCKED |
| Out-of-order Delivery/Bounce/Complaint không hạ severity | BLOCKED |
| Malformed/oversize/untrusted event vào quarantine/DLQ | BLOCKED |
| Controlled replay/redrive có authorization/reason/audit | BLOCKED |
| Backlog age, oldest message, consumer lag và DLQ alarm | BLOCKED |

Local synthetic normalized-event tests có thể đạt trước AWS topology, nhưng không được
dùng để đổi các dòng live topology thành `PASS`.

## Cross-client interoperability matrix

Reviewer dùng cùng synthetic lineage qua create -> update/reschedule -> cancel. Mỗi ô
chỉ `PASS` sau khi quan sát trực tiếp client tương ứng.

| Check | Gmail / Google Calendar | Outlook | Apple Calendar |
| --- | --- | --- | --- |
| Create tạo đúng một item | VERIFY | VERIFY | VERIFY |
| Update giữ một item, same UID/new sequence | VERIFY | VERIFY | VERIFY |
| Cancel tác động item cũ, không tạo item mới | VERIFY | VERIFY | VERIFY |
| Timezone/DST hiển thị đúng | VERIFY | VERIFY | VERIFY |
| Recurrence exception gắn đúng occurrence | VERIFY | VERIFY | VERIFY |
| CTA TutorHub còn nguyên, không tracking rewrite | VERIFY | VERIFY | VERIFY |
| Không lộ recipient/guest khác | VERIFY | VERIFY | VERIFY |
| Native RSVP không bị mô tả là sync TutorHub | VERIFY | VERIFY | VERIFY |

Client không có sẵn giữ `BLOCKED`, không suy `PASS` từ parser/unit test.

## Domain/DNS và production-readiness

| Gate | Trạng thái | Điều kiện để PASS |
| --- | --- | --- |
| Owner-controlled domain | BLOCKED | owner xác nhận quyền quản lý |
| SES identity `mail.<domain>` | BLOCKED | verified đúng account/region |
| Easy DKIM | BLOCKED | SES CNAME verified |
| Custom MAIL FROM | BLOCKED | MX/SPF đúng và fail closed |
| DKIM alignment | BLOCKED | authentication result aligned |
| SPF fallback/alignment | BLOCKED | không SPF record cạnh tranh |
| DMARC monitor | BLOCKED | record hợp lệ, report path được review |
| SES production access | BLOCKED | approved trong đúng region |
| Quota/rate/capacity | BLOCKED | workload budget + alarms |
| Deliverability canary | BLOCKED | canary allowlist + incident rollback |

Personal verified identity không thay thế bất kỳ gate domain/DNS nào.

## Security/privacy review

- [x] Diff không chứa secret, token, credential hoặc URL có credential theo targeted
      scan ngày `2026-07-26`.
- [x] Fixture/golden chỉ dùng reserved `example.*` domains; không chứa email thật,
      roster hoặc dữ liệu học sinh.
- [x] Spike không persistence raw capability và ADR khóa contract chỉ lưu versioned
      HMAC/hash, scope bounded; persistence runtime vẫn thuộc task sau.
- [ ] Restricted encrypted canonical-payload storage và opaque outbox reference chưa
      triển khai; giữ `BLOCKED` tới P3-05A/runtime wiring.
- [x] Error/provider tag negative tests không để email, raw MIME hoặc provider payload
      lọt ra.
- [x] One-recipient invariant và guest-list privacy có negative test.
- [x] Header/ICS/HTML injection, oversize và dangerous Unicode controls bị reject.
- [x] Renderer không có remote asset/shortener; tracking của configuration set vẫn
      `BLOCKED` tới live SES evidence.

## Điều kiện đóng Gate C2

ADR-0020 chỉ chuyển từ `Proposed` sang `Accepted` khi:

1. local deterministic renderer/golden/provider tests đạt;
2. SES sink/sandbox cô lập đạt bằng owner-controlled verified identities;
3. Gmail/Google Calendar, Outlook và Apple matrix đạt create/update/cancel;
4. EventBridge/SQS/DLQ/inbox, suppression và incident runbook có evidence;
5. domain/DNS/production-access được ghi PASS hoặc vẫn giữ explicit blocker phù hợp với
   tiêu chí ADR.

Ngay cả sau Gate C2, runtime end-user delivery vẫn phải chờ P3-03B, P3-02C và P3-05A;
không tự bật feature flag.
