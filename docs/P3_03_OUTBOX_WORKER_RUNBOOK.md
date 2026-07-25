# P3-03 — Runbook triển khai PostgreSQL leased outbox worker

## Trạng thái và phạm vi

Tài liệu này chuẩn bị lát cắt triển khai cho [ADR-0018](./adr/0018-postgresql-leased-outbox-worker.md).
Nó không xác nhận worker đã được provision hoặc P3-03 đã đạt exit gate.

Trong thay đổi hiện tại:

- một OCI image chứa cả `/app/tutorhub-core-api` và `/app/tutorhub-worker`;
- Core API vẫn là lệnh mặc định của image;
- worker phải chạy như process độc lập trên durable host;
- không tạo, đồng bộ hoặc áp dụng `render.yaml`;
- không thay đổi live Render/Neon, secret, database role hoặc migration.
- worker chỉ claim tối đa `min(OUTBOX_BATCH_SIZE, OUTBOX_CONCURRENCY)` để không giữ lease
  cho event còn chờ semaphore;
- `OUTBOX_METRICS_INTERVAL` điều khiển heartbeat/metric log có label bounded; registry rỗng
  vẫn phát heartbeat định kỳ nhưng không claim event.

P3-03A là repository/runtime foundation và dừng ở `VERIFY`. Sau gate này, P3-04 được phép
triển khai handler đầu tiên sau registration/feature gate mặc định tắt để làm controlled
canary. P3-03B chỉ được chuyển umbrella task sang `DONE` sau khi migration `000015` đã áp
dụng trên staging, API/worker direct-LOGIN grants cùng exact ACL probes và startup smoke
đạt, durable-host gate được duyệt, worker được triển khai và crash/reclaim acceptance bên
dưới có bằng chứng đạt.
Không bật side effect tới end user khi P3-03B còn mở.

## Cấu hình runtime và invariant

| Biến | Mặc định | Vai trò |
| --- | ---: | --- |
| `OUTBOX_POLL_INTERVAL` | `2s` | Nhịp tìm work đã đến hạn |
| `OUTBOX_METRICS_INTERVAL` | `30s` | Nhịp heartbeat/metrics, kể cả registry rỗng |
| `OUTBOX_LEASE_DURATION` | `30s` | Thời gian lease trước khi replica khác reclaim |
| `OUTBOX_HANDLER_TIMEOUT` | `20s` | Deadline tối đa của một handler |
| `DATABASE_QUERY_TIMEOUT` | `5s` | Deadline cho một thao tác database |
| `OUTBOX_SHUTDOWN_TIMEOUT` | `30s` | Deadline drain khi nhận shutdown |

Validation fail-closed yêu cầu lease bao phủ ít nhất handler timeout + database query
timeout + safety margin 5 giây; shutdown timeout phải bao phủ handler + database query.
Provider shutdown delay phải lớn hơn shutdown timeout và không vượt giới hạn provider.

## Hợp đồng image và process

Dockerfile hiện hữu tại
`infrastructure/huggingface/core-api/Dockerfile` là artifact chung:

| Process | Binary | Cách chọn |
| --- | --- | --- |
| Core API | `/app/tutorhub-core-api` | `CMD` mặc định của image |
| Outbox worker | `/app/tutorhub-worker` | command override của durable host |

Phải giữ `CMD`, không dùng fixed `ENTRYPOINT`, để Render `dockerCommand` hoặc runtime
tương đương có thể chọn worker mà không tạo image thứ hai.

Image chung cố ý không khai báo Docker `HEALTHCHECK`: Core API có HTTP endpoint nhưng
worker là process không mở port. Trước lần deploy image mới, xác nhận web service Core API
vẫn dùng provider health-check path `/health`. Render chỉ hỗ trợ HTTP health checks cho
web/private services, không áp dụng cho background worker.

## Durable-host gate

Target ưu tiên cho staging/private alpha là **Render Background Worker trả phí**, một
instance, cùng repository, commit, Dockerfile và image với Core API. Background Worker
chạy liên tục và không nhận inbound traffic. Render không có Free plan cho loại service
này; dùng Free web service kèm external ping không đáp ứng gate.

Không dùng các phương án sau thay cho durable worker:

- Render Free web service vì có thể spin down;
- Render Cron Job hoặc GitHub Actions schedule vì polling/lease có thể bị ngắt hoặc trễ;
- chạy polling loop trong HTTP API;
- laptop hoặc process thủ công không có supervisor/restart policy.

Trước khi provision, owner phải duyệt đủ các điều kiện:

- [ ] chấp nhận phát sinh chi phí Render Background Worker;
- [ ] kiểm tra region của Core API và Neon; chọn region worker có độ trễ phù hợp;
- [ ] kiểm tra Neon plan, compute-hours, connection budget và khả năng scale-to-zero;
- [ ] migration, grants và rollback contract đã được review;
- [ ] `OUTBOX_SHUTDOWN_TIMEOUT` nhỏ hơn provider shutdown delay;
- [ ] metrics/alerts và crash/reclaim acceptance fixture đã sẵn sàng;
- [ ] có quyền thao tác provider và cửa sổ triển khai được phê duyệt.

Render gửi `SIGTERM` khi shutdown/deploy rồi mới `SIGKILL` sau shutdown delay. Cấu hình
`maxShutdownDelaySeconds` phải lớn hơn `OUTBOX_SHUTDOWN_TIMEOUT`; Render giới hạn giá trị
này trong khoảng 1–300 giây. Vì worker hiện cho phép timeout lớn hơn, validation ở provider
gate phải chặn cấu hình trên 300 giây. Durable ở đây nghĩa là service được supervisor duy
trì và event có thể reclaim bằng lease; không có nghĩa một process sống vĩnh viễn.

## Cấu hình candidate trên Render

Đây là checklist review, **không phải Blueprint để sync**:

| Thuộc tính | Giá trị candidate |
| --- | --- |
| Service type | Background Worker |
| Runtime | Docker |
| Plan | paid plan nhỏ nhất đáp ứng tải, dự kiến Starter; xác nhận lại lúc provision |
| Instances | 1 ban đầu; scale 2 tạm thời cho contention acceptance nếu được duyệt |
| Dockerfile | `./infrastructure/huggingface/core-api/Dockerfile` |
| Docker command | `/app/tutorhub-worker` |
| Branch/commit | cùng release commit với Core API |
| Auto deploy | off trong private alpha nếu live service hiện đang vận hành thủ công |
| Inbound port/health path | không có |
| Shutdown delay | lớn hơn `OUTBOX_SHUTDOWN_TIMEOUT`, tối đa 300 giây |

Không hardcode region trong repo khi chưa thu thập live configuration. Render không cho
đổi region hoặc service type của service hiện hữu sau khi tạo.

## Không sync live render.yaml trong P3-03

Repository hiện không có nguồn chân lý đầy đủ cho live Render service. Không thêm hoặc
apply `render.yaml` trong task này vì một Blueprint thiếu trường có thể thay đổi/reset
cấu hình, và `sync: false` không tự điền secret cho service hiện hữu.

Trước một thay đổi IaC riêng có thẩm quyền provider:

1. xuất inventory **đã redaction** của Core API: service name, type, region, plan, branch,
   Dockerfile path, Docker command, health path, auto-deploy và danh sách tên env key;
2. không ghi giá trị env/secret, connection string hoặc credential vào ticket, log hay Git;
3. đối chiếu inventory với Blueprint đầy đủ và review diff;
4. provision worker hoặc import service theo quy trình riêng;
5. chỉ sync sau khi owner duyệt chi phí và blast radius.

## DATABASE_WORKER_URL và database role

`DATABASE_WORKER_URL` là secret chỉ cấp cho process worker. URL phải:

- dùng Neon pooled endpoint và TLS;
- đăng nhập bằng role riêng, không dùng migration owner;
- không xuất hiện ở Core API, frontend, log hoặc evidence;
- không được alias sang `DATABASE_MIGRATION_URL`;
- được rotate/revoke độc lập với `DATABASE_POOL_URL` của API.

Worker fail fast khi biến này thiếu, URL không hợp lệ, không ping được database hoặc role
không khớp exact contract. Startup probe yêu cầu `session_user = current_user`, LOGIN
trực tiếp, không superuser/create-role/create-db/replication/bypass-RLS, không membership,
không sở hữu database/schema/outbox, không có CREATE ở `tutorhub` hoặc `public`, không có
quyền table/column trên bảng nghiệp vụ khác. Trên outbox chỉ cho SELECT và UPDATE đúng
allowlist; INSERT/DELETE/TRUNCATE/REFERENCES (kể cả column-level)/TRIGGER đều bị chặn.
Đừng fallback im lặng sang API hoặc migration credential.

Phân quyền mục tiêu:

| Principal | Quyền cần có trên outbox | Không được có |
| --- | --- | --- |
| Core API role | INSERT đúng các cột enqueue nghiệp vụ | claim, ack, retry, dead-letter, replay hoặc purge |
| Worker role | SELECT pending rows và UPDATE đúng các cột lease/fencing/attempt/result | DDL, migration, tenant business-table mutation không cần thiết, replay/purge operator |
| Migration owner | schema migration và grant/revoke trong release window | runtime credential |
| Operator/replay role | thao tác break-glass được audit nếu được thiết kế | chạy thường trực trong worker |

Tên cột/grant chính xác phải theo migration P3-03. Sau migration, xác minh bằng chính
credential của từng LOGIN role, gồm positive test và negative tests cấp tạm quyền dư trên
bảng nghiệp vụ/schema rồi xác nhận startup bị từ chối. Migration-owner `SET ROLE` không
thay thế direct-login test vì có thể `RESET ROLE` lấy lại quyền.

Không inject các secret OIDC, B2, LiveKit hoặc API-only khác vào worker nếu handler P3-03
không cần chúng. Lease owner có thể chứa instance identifier đã chuẩn hóa để quan sát,
nhưng fencing phải dựa trên lease token được PostgreSQL tăng và trả về atomically trong
claim SQL. Worker chỉ mang token đã claim vào ack/retry/dead-letter; worker không tự tạo
token và không dựa riêng vào `RENDER_INSTANCE_ID`.

## Gate chi phí và capacity Render/Neon

Hai chi phí phải được xét độc lập:

1. Render Background Worker không có Free plan, nên provision tạo chi phí mới.
2. Polling thường xuyên có thể giữ Neon compute active. Neon ghi nhận background
   activity/connection/query có thể ngăn scale-to-zero.

Tại thời điểm review 2026-07-24, Neon Free công bố 100 CU-hours mỗi project mỗi tháng.
Min compute 0.25 CU chạy liên tục trong tháng 750 giờ là khoảng 187.5 CU-hours, chưa tính
Core API. Đây chỉ là phép tính capacity minh họa; phải kiểm tra lại pricing, min/max CU,
usage thực tế và plan tại thời điểm provision.

Capacity worksheet phải ghi:

- API replica × API max connections;
- worker replica × worker max connections;
- migration/operator headroom;
- Neon pooled connection/compute limit;
- poll interval, batch size, lease duration và backlog-age SLO;
- compute-hours quan sát trong canary.

Nếu Free plan không đủ, owner phải duyệt Neon plan phù hợp hoặc điều chỉnh polling/SLO
có bằng chứng. Không kéo dài poll interval chỉ để né chi phí nếu làm vi phạm due-lag SLO
hoặc lease/retry semantics.

## Trình tự release được phép

1. Build, test và scan **đúng commit**; xác nhận image chứa cả hai binary.
2. Xác nhận Core API provider health path trước khi deploy image không có Docker
   `HEALTHCHECK`.
3. Áp dụng migration bằng migration role theo runbook database.
4. Áp dụng và chạy positive/negative privilege tests cho API/worker role.
5. Set `DATABASE_WORKER_URL` trực tiếp trong secret store của provider, không in giá trị.
6. Deploy Core API và worker từ cùng commit/image; worker dùng command override.
7. Ghi lại immutable commit/deploy IDs và worker instance ID đã redaction.
8. Xác nhận startup fail-fast, metrics và backlog không xử lý event type/version chưa đăng ký.
9. Chạy crash/reclaim acceptance. Chỉ sau khi đạt mới cân nhắc P3-03 DONE.

Migration runner preflight trước khi down qua version 15. Nếu có bất kỳ row mang retained
lease state hoặc dead-letter, lệnh phải fail và version vẫn là `15 false`. Không force
metadata, không blanket-mark published và không dùng một lệnh repair tự động; dừng worker,
reconcile dữ liệu và review backup/restore trước khi cân nhắc database down.

Migration phải backward-compatible với API/worker release trước đó. Không rollback schema
mù khi worker gặp sự cố; có thể tạm dừng worker như incident action để giữ event trong
PostgreSQL, sau đó phân tích và deploy bản sửa.

Mọi handler đăng ký từ P3-04 trở đi phải tôn trọng `context.Context`, dừng prompt khi
timeout/cancel và có test timeout riêng. Go không thể kill an toàn một goroutine bỏ qua
context; supervisor/provider shutdown delay là lớp cưỡng bức cuối cùng, còn event chưa
ack sẽ được reclaim sau lease expiry.

## Crash/reclaim acceptance

### Tiền điều kiện

- Dùng staging tenant và handler đầu tiên của P3-04 đã đăng ký sau controlled-canary gate;
  registration/feature gate mặc định tắt ngoài cửa sổ acceptance.
- Handler/sink của probe phải an toàn, idempotent và quan sát được.
- Không publish hàng loạt các row lịch sử từ Phase 1/2.
- Có cách giữ handler đủ lâu hoặc failpoint chỉ bật trong acceptance để kill process sau
  claim nhưng trước ack; failpoint không được dùng trong production.
- Ghi mốc thời gian database/server, lease duration và poll interval.

### Kịch bản bắt buộc

1. Enqueue một probe event mới và ghi opaque event ID.
2. Worker A claim event; xác nhận PostgreSQL claim SQL atomically ghi owner, tăng
   `lease_token`, đặt `leased_until` và trả về token mới cho worker.
3. Dừng cưỡng bức Worker A trước ack.
4. Trước `leased_until`, Worker B không được claim event đó.
5. Sau `leased_until`, Worker B reclaim với lease token mới.
6. Ack/retry/dead-letter giả lập bằng token cũ phải update 0 row; integration test phải
   chứng minh fencing này ngay cả khi process A thực tế đã chết.
7. Worker B hoàn tất; outbox row được đánh dấu published và idempotent sink chỉ có một
   logical effect.
8. Chạy poison probe: retry theo backoff đến max attempts, sau đó chuyển dead-letter;
   không polling vô hạn.
9. Gửi `SIGTERM` khi handler đang chạy: worker ngừng claim mới, drain trong deadline;
   nếu bị kill trước completion, event phải được reclaim sau lease expiry.
10. Nếu được duyệt chi phí, scale tạm lên hai worker để kiểm chứng
    `FOR UPDATE SKIP LOCKED` không tạo concurrent ownership, rồi trả về một instance.

### Tiêu chí đạt

- không mất event và không có hai lease còn hiệu lực cho cùng event;
- stale token không thể thay đổi state;
- duplicate delivery không tạo duplicate logical effect;
- retry/dead-letter có bound và schedule đúng;
- shutdown không claim thêm việc và không ack handler chưa hoàn tất;
- row lịch sử/unregistered event không bị xử lý;
- log/metrics không chứa payload riêng tư, credential hoặc label cardinality cao.

Evidence cần giữ: commit/deploy IDs, test name/result, opaque event ID, mốc claim/crash/
expiry/reclaim/ack, bounded log excerpt và query result đã redaction. Không chụp hoặc lưu
connection string.

## Quan sát và cảnh báo tối thiểu

- pending backlog count và oldest pending age;
- due lag;
- claim, success, retry, dead-letter và reclaim totals;
- handler duration;
- database pool saturation/error;
- process restart và heartbeat/last-success timestamp.

Label chỉ dùng bounded dimensions như event type/version, outcome và error code chuẩn hóa;
không dùng tenant ID, event ID, payload hoặc exception text làm label. Vì Background Worker
không có HTTP health check, startup failure, heartbeat và backlog-age alert là tín hiệu vận
hành chính.

## Nguồn chính thức

- [Render Background Workers](https://render.com/docs/background-workers)
- [Render Free instances](https://render.com/docs/free)
- [Render Docker command override](https://render.com/docs/docker)
- [Render deploys and graceful shutdown](https://render.com/docs/deploys)
- [Render health checks](https://render.com/docs/health-checks)
- [Render Blueprint specification](https://render.com/docs/blueprint-spec)
- [Neon pricing](https://neon.com/pricing)
- [Neon compute endpoints and scale-to-zero](https://neon.com/docs/manage/endpoints/)
- [Neon scale-to-zero](https://neon.com/docs/introduction/scale-to-zero)
