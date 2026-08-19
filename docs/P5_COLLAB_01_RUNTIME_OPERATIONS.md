# P5-COLLAB-01 Gate F — Runtime và vận hành Excalidraw collaboration

> **Ngày khóa candidate:** 2026-08-19
> **Owner runtime decision:** `FREE_PRIVATE_ALPHA` ngày 2026-08-19.
> **Trạng thái:** `VERIFY` — isolated runtime/operations contract `18/18 PASS`; OCI source candidate
> Gate F.1 đã triển khai, còn chờ exact image/SBOM scan, disposable Render Free/Neon/B2 drill,
> on-call owner và chấp thuận giới hạn single-instance/cold-start.
> **Phạm vi:** runbook/acceptance và OCI candidate cho collaboration data plane; không provision,
> không deploy, không tạo migration, không kết nối shared staging và không thay đổi secret.
> **Guardrail:** whiteboard production tiếp tục force-off cho tới P5-COLLAB-17 exact staging.

## 1. Topology owner đã chọn cho development/private alpha

Owner chọn profile `FREE_PRIVATE_ALPHA` với mục tiêu chi phí hạ tầng bổ sung `0 USD/tháng` khi còn
trong free allowance. Profile này không tự nâng gói, không tự scale và không provision tài nguyên trả
phí:

| Thành phần | Exact candidate | Vai trò và ranh giới |
| --- | --- | --- |
| Runtime | Node.js `24.15.0` LTS, image Linux immutable | Chạy collaboration gateway; không dùng floating tag. Image digest còn phải được điền từ build đã scan trước deploy. |
| Collaboration server | `@hocuspocus/server@4.6.0` | WebSocket transport, auth hooks, document load/store và drain; không tạo document/history authority thứ hai. |
| Canonical authority | `yjs@13.6.27` | Một `Y.Doc` cho exact `{tenant, document, generation}`; Excalidraw chỉ là projection. |
| Compute | Render Web Service **Free**, region Singapore, đúng **1 instance** | Chạy data plane private alpha. Chấp nhận spin-down/cold-start, restart và khoảng gián đoạn khi deploy; không có HA hoặc horizontal scaling. |
| Coordination | **Không Redis trong profile free** | Một instance không cần cross-node pub/sub. Runtime không load Redis extension và không yêu cầu Redis secret. |
| Durable current checkpoint | Neon PostgreSQL Singapore trong free allowance, Yjs binary trong `BYTEA` | Giữ full-state checkpoint có generation/watermark/fencing; không lưu live operation log cạnh tranh. |
| Portable recovery | Backblaze B2 private bucket trong free allowance, immutable versioned object + checksum | Giữ portable canonical snapshot/export; không làm live writer. Object key opaque, không chứa tenant/user/document có thể đoán. |
| Business/control plane | Core API/PostgreSQL hiện hữu | Tenant, membership, lifecycle, capability, current generation, one-time grant và revoke authority. |

`24.15.0` là baseline acceptance có release LTS chính thức. Trước mỗi deploy phải kiểm tra Node 24 LTS
security release mới hơn. Nếu cần vá bảo mật, pin exact patch mới cùng image digest và chạy lại Gate F;
không giữ một patch cũ chỉ để bảo toàn tài liệu.

Topology logic:

```text
Browser (one-time grant, memory-only)
       |
       v
Render Free Singapore (one instance)
       |
       v
Hocuspocus + one canonical Y.Doc authority
       |
       +------------------------+
       |                        |
       v                        v
Neon BYTEA checkpoint     B2 immutable portable snapshot
(current generation)      (recovery/provider exit)
```

Render Free spin down sau thời gian không có inbound traffic và cần cold-start khi có HTTP/WebSocket
mới; free service chỉ chạy một instance và không hỗ trợ scaling. Vì vậy profile này chỉ dành cho
development/private alpha, **không phải production HA**. Local filesystem/instance RAM không phải
durable authority; restart phải phục hồi từ Neon checkpoint và B2 last-good snapshot. Không dùng ping
giả để che cold-start hoặc biến free service thành always-on không được đo lường.

### 1.1 Đường nâng cấp production đã hoãn

Trước public beta hoặc khi cần SLO/HA, candidate phải quay lại Gate F với **Render Standard Singapore
x2 + Redis Cloud paid Multi-AZ** (hoặc ADR mới có evidence tương đương). Đây chỉ là upgrade path, chưa
được mua, provision hay tính vào exit gate của `FREE_PRIVATE_ALPHA`. Hai-node/Redis isolated tests hiện
có được giữ làm invariant cho đường nâng cấp, không được dùng để quảng bá profile free là HA.

### 1.2 Những gì không được suy diễn từ topology

- Một Render Free instance không đảm bảo continuous availability, rolling deploy không gián đoạn hoặc
  giữ phòng khi instance sleep/restart.
- Không có Redis trong profile free; không được giả lập coordination bằng local filesystem hoặc memory
  rồi gọi đó là multi-node durability.
- Nếu production HA path được kích hoạt, Redis chỉ coordination và **không persist document**;
  Neon/B2 vẫn là recovery boundary.
- Instance không được dùng local filesystem làm durable state.
- PostgreSQL lưu full Yjs checkpoint, không append từng operation và không tạo TutorHub history thứ hai.
- B2 snapshot không được ghi đè; restore luôn tạo generation mới rồi atomic swap ở control plane.
- Không fallback sang Excalidraw demo relay/Firebase, tldraw sync, LiveKit DataChannel hoặc Core API REST
  operation transport khi dependency lỗi.
- Không tự đổi sang plan trả phí, thêm replica hoặc Redis. Mọi thay đổi plan/region/replica cần capacity
  evidence, owner approval và cập nhật ADR.

## 2. Runtime contract

### 2.1 Pin và build

Artifact production tương lai phải khóa:

```text
node=24.15.0
@hocuspocus/server=4.6.0
yjs=13.6.27
pg=8.23.0
@aws-sdk/client-s3=3.1113.0
oci_image_digest=<PENDING_SCANNED_BUILD_DIGEST>

# Deferred production HA only; không load trong FREE_PRIVATE_ALPHA
@hocuspocus/extension-redis=4.6.0
```

Không chấp nhận `latest`, caret/tilde, image tag không digest hoặc runtime cài dependency khi startup.
Build phải dùng lockfile, SBOM, production vulnerability scan và provenance của đúng commit. App bind
public traffic qua một port Render; WebSocket và probe HTTP cùng process nhưng khác path.

### 2.1.1 Gate F.1 OCI source candidate — 2026-08-19

Runtime thực tế nằm tại `services/whiteboard-runtime`, tách khỏi editor bundle. Candidate đã có:

- Hocuspocus HTTP/WebSocket server cùng `/livez`, `/readyz` và bearer-protected `/metrics`;
- exact one-time grant exchange, Origin/capability/generation checks và fail-closed control polling;
- Neon `BYTEA` checkpoint adapter với checksum, full-state Yjs validation, writer fence/watermark và
  least-privilege preflight;
- B2 private immutable content-addressed snapshot adapter với checksum, `If-None-Match` và
  read-after-write verification;
- bounded connection/frame/message limits, privacy-safe telemetry và SIGTERM drain tối đa 45 giây;
- multi-stage Dockerfile pin exact public Node image digest, non-root user, readiness healthcheck và
  isolated production deploy chỉ có bốn dependency production exact-pin;
- CI job build image, scan Trivy HIGH/CRITICAL, tạo CycloneDX SBOM, kiểm tra SBOM và upload SARIF/
  artifact bằng action pin SHA.

Local source/package acceptance PASS: runtime unit/integration `9/9`, lint, typecheck, build, OCI
static guard, production dependency audit không có known vulnerability và isolated production
package (`runtime_dependencies=4`, không có test provider).
Máy hiện tại chưa có Docker/Trivy/Syft nên **chưa** có actual image digest, SBOM hoặc vulnerability
result; các mục đó chỉ được PASS sau CI/exact builder chạy thật. Candidate cũng cố ý chưa ready trước
P5-COLLAB-02 schema/ACL và P5-COLLAB-04 control-plane endpoints; không tạo contract giả để ép xanh.

Candidate server limits phải fail closed và không thấp hơn Gate C:

- pre-auth deadline và idle health interval có bound;
- `maxUnauthenticatedQueueSize`, `maxUnauthenticatedQueueMessages`, `maxPendingDocuments` không dùng
  giá trị vô hạn;
- `websocketOptions.maxPayload`, Yjs update bytes, object/depth, awareness bytes/rate, connection per
  actor/document/tenant và reconnect storm đều có exact cap;
- persistence `debounce` có `maxDebounce`; không để document dirty vô hạn;
- auth/Origin/generation/capability được kiểm tra server-side trước khi nhận mutation.

### 2.2 Process và dependency startup

Startup chỉ chuyển `ready=true` sau khi:

1. config schema hợp lệ và tất cả secret bắt buộc hiện diện, nhưng không log giá trị;
2. exact runtime/dependency version khớp allowlist;
3. Neon direct login đạt least-privilege preflight và đọc được schema version expected;
4. active profile đúng `FREE_PRIVATE_ALPHA`, instance count đúng `1` và runtime không yêu cầu/load
   Redis extension;
5. B2 config metadata hợp lệ; B2 outage không chặn process liveness nhưng chặn snapshot/export job;
6. kill switch/feature catalog được đọc từ control authority; authority outage fail closed;
7. service chưa ở trạng thái drain.

Không fallback sang migration owner, API database role hoặc B2 admin key. Nếu cấu hình production HA
được bật thì Redis TLS trở lại dependency bắt buộc; không có fallback Redis không TLS. Một dependency
quan trọng thiếu/sai phải làm readiness đỏ, không khởi động ở chế độ permissive.

## 3. Health, readiness và drain

| Endpoint/tín hiệu | Thành công | Thất bại | Dữ liệu được phép trả |
| --- | --- | --- | --- |
| `GET /livez` | Process/event loop còn phản hồi | Deadlock, fatal shutdown | Chỉ `ok` + bounded build version; không query dependency. |
| `GET /readyz` | Không drain, control authority usable và Neon checkpoint read/write probe đạt trong deadline; Redis chỉ được probe ở production HA profile | Bất kỳ dependency bắt buộc hoặc kill switch write bị lỗi | Bounded dependency code; không hostname, connection string, tenant/document hay raw error. |
| `GET /metrics` | Chỉ qua private/authorized scrape path | Public/không auth bị deny | Bounded metrics ở mục 8. |
| `SIGTERM` | Đặt not-ready trước, reject upgrade mới, drain, flush checkpoint rồi exit 0 | Quá deadline để Render `SIGKILL` | Log event code + duration, không content. |

Render `healthCheckPath` trỏ vào `/readyz`. Candidate `maxShutdownDelaySeconds=60`; application drain
budget tối đa 45 giây để còn 15 giây safety margin:

1. atomically đặt `draining=true`, `/readyz` trả `503` và upgrade mới trả bounded retryable error;
2. ngừng cấp/refresh collaboration grant trên replica đang drain;
3. yêu cầu client reconnect bằng exponential backoff; client không phụ thuộc sticky session;
4. ngừng nhận document mutation mới, flush dirty full-state checkpoint có fencing;
5. đóng socket bằng application close code đã chuẩn hóa, không gửi raw provider error;
6. hủy Neon connection sau checkpoint; xác nhận active connection/document về `0`;
7. exit trước 45 giây. Quá deadline thì exit non-zero để stale process không tiếp tục nhận write.

Drain PASS khi instance đặt not-ready trước, flush acknowledged state và đạt `connections=0`,
`documents=0`, `dirty_documents=0` trước exit. Vì profile free không có replica sống sót, client sẽ có
khoảng gián đoạn và chỉ reconnect sau khi instance mới sẵn sàng; recovery phải giữ semantic hash từ
Neon checkpoint. Render gửi `SIGTERM` và có shutdown window; không dùng sleep thủ công hoặc external
ping để giả lập supervisor.

## 4. Persistence, backup và restore

### 4.1 Neon current checkpoint — không phải op log

Hocuspocus Database extension dùng:

- `fetch`: trả lại đúng `Uint8Array` đã lưu cho exact opaque document + current generation;
- `store`: encode full `Y.Doc` state và upsert một current checkpoint `BYTEA` trong transaction;
- fencing condition gồm exact `{tenant_id, document_id, generation, expected_watermark}`;
- catalog chỉ advance sau khi byte length, checksum và semantic envelope đã validate;
- writer stale/generation cũ update `0 row` và socket bị đóng;
- checksum mismatch hoặc unsupported schema bị quarantine, không tự tạo empty document che mất dữ liệu.

Conceptual record, **không phải migration trong Gate F**:

```text
tenant_id, document_id, generation, schema_version,
provider_version, causal_watermark, yjs_state BYTEA,
byte_length, checksum, updated_at, writer_fence
```

Không lưu email, display name, snapshot body hoặc operation content trong audit/log. RLS/ACL, exact
columns, retention và migration thuộc task implementation riêng và phải qua disposable Neon trước
shared staging.

### 4.2 B2 immutable portable snapshot

Snapshot là canonical portable JSON/envelope từ Gate D, không phải chỉ Yjs native binary. Quy trình:

1. lấy fenced read của checkpoint current generation;
2. validate byte/object/depth/schema cap và deterministic semantic hash;
3. tạo opaque content-addressed key, upload vào **private** B2 bucket;
4. bật Object Lock/retention theo policy đã owner duyệt; không overwrite key;
5. read-after-upload, verify byte length/checksum rồi mới publish snapshot catalog;
6. nếu upload/verify lỗi, giữ catalog cũ và retry có bound; không xóa last-good.

Object Lock governance/compliance mode và retention days phải được owner chọn trước provision. B2 nêu
rõ Object Lock đã bật trên bucket thì không thể tắt; vì vậy không tự bật trên live bucket trong Gate F.

### 4.3 Restore

1. bật write kill switch cho document, ngừng grant mới và revoke generation hiện tại;
2. lấy last-good B2 object bằng least-privilege key, verify signature/checksum/schema/caps;
3. corrupt/unsupported artifact đi quarantine; không thay current generation;
4. import offline vào Yjs authority mới, chạy semantic round-trip và convergence smoke;
5. ghi Neon checkpoint của generation mới;
6. Core API atomic swap current generation; stale writer bị deny;
7. mở lại `view`, rồi `edit` chỉ sau probe; giữ generation cũ immutable tới hết retention/incident review.

Không restore trực tiếp toàn bộ Neon branch chỉ để sửa một board. Neon project backup/PITR là lớp DR
database, còn board restore bình thường phải dùng generation swap nêu trên.

### 4.4 Mục tiêu RPO/RTO candidate

Đây là **application acceptance objective**, không phải SLA của provider:

| Sự cố/phạm vi | RPO mục tiêu | RTO mục tiêu | Ghi chú |
| --- | ---: | ---: | --- |
| Render Free sleep/restart/deploy | `<=15s` acknowledged Yjs state | Candidate `<=2 phút` kể từ request đánh thức đến reconnect/hội tụ | Không có replica dự phòng; phải đo cold-start/restart thật. |
| Corrupt current checkpoint | last-good portable snapshot `<=5 phút` | `<=30 phút` cho board pilot <=2.000 object | Restore generation mới, không overwrite corrupt input. |
| B2 outage | Neon current checkpoint `<=15s`; portable RPO tạm tăng và phải alert | Snapshot/export phục hồi `<=30 phút` sau provider hồi | Live collaboration có thể tiếp tục nếu Neon đạt, nhưng restore/export bị disable. |
| Neon outage | Không hứa giữ mutation chỉ trong RAM; write dừng | `<=15 phút` sau Neon hồi và checkpoint validate | Read-only local projection không được quảng bá là durable edit. |
| Toàn region Singapore lỗi | last verified B2 portable snapshot `<=5 phút` | manual recovery objective `<=4 giờ` | Không có multi-region hot standby trong candidate; owner phải ký nhận residual risk. |
| Production HA Redis failover | `DEFERRED` | `DEFERRED` | Chỉ đo khi owner kích hoạt paid HA upgrade path. |

Gate không PASS chỉ từ số mục tiêu trên. Failure drill phải đo actual `observed_rpo_seconds` và
`observed_rto_seconds`; vượt budget thì giữ feature force-off.

## 5. Failure matrix và drill bắt buộc

| Drill | Fault injection | Hành vi bắt buộc | Evidence tối thiểu | Trạng thái |
| --- | --- | --- | --- | --- |
| Render Free cold-start | Để service spin down theo provider rồi mở HTTP/WebSocket mới | Service thức dậy, client hiển thị reconnect, phục hồi checkpoint và hội tụ; không mất acknowledged state | opaque run ID, cold-start/reconnect timings, hash equality | `NOT RUN` |
| Graceful deploy/drain | `SIGTERM` instance với dirty doc | Not-ready trước, không socket mới, checkpoint flush, disconnect, instance mới phục hồi và reconnect <= budget | drain timeline + cleanup-zero + recovery hash | `NOT RUN` |
| Neon sustained outage | Chặn Neon 10 phút | Readiness đỏ, write fail closed, không tuyên bố mutation RAM là saved; phục hồi từ last-good | checkpoint age, no-lost-ack evidence | `NOT RUN` |
| B2 sustained outage | Chặn B2 10 phút | Snapshot/export bounded retry; live state không mất; alert snapshot age; last-good không bị xóa | retry count, alert, later checksum verify | `NOT RUN` |
| Control authority outage | Core API/grant/revoke authority lỗi | Không cấp grant/refresh và không elevate reader; cached permissive role bị cấm | fail-closed response taxonomy | `NOT RUN` |
| Kill switch | Chuyển write/connection off giữa phiên | Grant mới deny; writer socket đóng <=1s; client sang bounded view/export path | revoke timing + no post-switch mutation | `NOT RUN` |
| Rotation | Rotate từng secret theo mục 7 | Current/next overlap bounded; new path đạt trước revoke; credential cũ deny sau revoke | key IDs/role names redacted, boolean probes | `NOT RUN` |
| Backup/restore | Tạo snapshot, mutate, corrupt một artifact, restore last-good | Corrupt bị quarantine; generation mới giữ expected semantic hash; stale writer deny | checksum, generation swap, hash | `NOT RUN` |
| Provider exit | Export portable, khởi tạo clean authority không dựa Redis/Hocuspocus state | Round-trip giữ supported semantic hash; không dual-write | artifact version + before/after hash | `NOT RUN` |
| Quota/reconnect storm | Vượt actor/doc/tenant/rate/payload cap | Bounded reject, không OOM, tenant khác không bị starvation | rejection metric + heap/CPU + cleanup-zero | `NOT RUN` |
| Regional tabletop | Giả lập toàn bộ Singapore unavailable | Feature off; dùng B2 last-good; operator thực hiện documented manual recovery | signed tabletop timeline và gaps | `NOT RUN` |
| Production HA two-node/Redis | Kill một replica và chặn Redis 10 phút | Hai-node coordination/failover không divergence | two-node/Redis evidence | `DEFERRED_PRODUCTION_HA` |

Sustained outage nghĩa là fault được giữ tối thiểu 10 phút, không phải một request fail ngắn. Drill không
được dùng credential live, không làm shared staging và không log secret/content. Provider dashboard
drill chỉ chạy sau owner duyệt chi phí và blast radius; trước đó dùng isolated disposable harness.

## 6. Kill switch và degraded behavior

Cần ba server-owned state, mặc định fail closed:

| State | Grant mới | Socket hiện hữu | UI |
| --- | --- | --- | --- |
| `off` | Deny tất cả | Revoke/close | Whiteboard force-off; chỉ thông báo bounded. |
| `read_only` | Chỉ grant `view` | Đóng writer rồi reconnect bằng view grant | Last-good board/semantic fallback và export nếu artifact verified. |
| `enabled` | Theo capability current | Theo exact grant/generation | Collaboration bình thường. |

Không downgrade quyền của một socket đang mở chỉ bằng client flag. Khi chuyển `enabled -> read_only/off`,
server tăng revoke generation và đóng writer; client phải exchange grant mới. Kill switch không xóa
Neon checkpoint/B2 snapshot và không tự động chuyển sang provider writer khác.

## 7. Secret và credential rotation

Mọi secret ở provider secret store; không trong repo, frontend, URL, evidence hay support export.
Rotation chạy **từng dependency một**, giữ kill switch sẵn sàng:

### 7.1 Grant signing/verification key

1. tạo `kid_next`, chỉ server biết private/signing material;
2. deploy verifier chấp nhận `kid_current` + `kid_next`, issuer chỉ phát `kid_next`;
3. chờ tối đa `2 x grant TTL` (TTL gate tối đa 60 giây) và active old grant về 0;
4. bỏ `kid_current`, probe old grant deny/new grant pass/replay deny;
5. xóa/revoke old secret ở provider, ghi chỉ key ID + timestamp.

### 7.2 Neon runtime role

1. migration owner tạo/rotate một direct LOGIN runtime credential least-privilege trong release window;
2. cập nhật secret cho Render Free service, chạy direct-login positive/negative ACL + checkpoint smoke;
3. restart bounded và xác nhận instance mới dùng credential mới; chấp nhận khoảng gián đoạn đã công bố;
4. revoke old credential; old direct login phải fail, runtime vẫn ready;
5. migration owner rời runtime; không giữ owner URL làm fallback.

### 7.3 B2 và Redis production HA đã hoãn

- B2 key chỉ private bucket/prefix cần thiết; quyền retention/administration tách khỏi runtime key;
- tạo B2 application key mới, cập nhật service, kiểm tra snapshot read-write-checksum rồi revoke key cũ
  và chạy negative probe;
- profile `FREE_PRIVATE_ALPHA` không có Redis credential;
- nếu paid HA path được kích hoạt, Redis credential chỉ có coordination database, TLS và không có admin
  subscription permission; rotation phải canary từng replica rồi mới revoke key cũ.

Rotation fail ở bất kỳ bước nào: dừng rollout, giữ current credential còn hiệu lực, force read-only nếu
consistency không chắc chắn; không in connection string để debug.

## 8. Observability, privacy và alert

### 8.1 Bounded metrics

Cho phép label hữu hạn như `outcome`, `error_code`, `capability`, `dependency`, `runtime_version`,
`coarse_profile`. Cấm `tenant_id`, `user_id`, `document_id`, socket ID, grant/JTI, object key, email,
exception text hoặc provider host làm label.

Metric tối thiểu:

- `collab_connections_current{capability}` và `collab_documents_current`;
- `collab_connection_total{outcome,error_code}`;
- `collab_update_total{outcome,error_code}` cùng histogram byte/latency theo bucket cố định;
- `collab_checkpoint_total{outcome}` và `collab_checkpoint_age_seconds`;
- `collab_snapshot_total{outcome}` và `collab_snapshot_age_seconds`;
- `collab_dependency_up{dependency}` với dependency allowlist;
- `collab_drain_active`, `collab_dirty_documents`, `collab_reconnect_total{outcome}`;
- `collab_quota_rejection_total{dimension}` với dimension allowlist;
- process CPU/RSS/heap/event-loop lag/restart/cold-start, Neon pool saturation và B2 retry; Redis metric
  chỉ bật trong production HA profile.

### 8.2 Log policy

Structured log chỉ có timestamp, severity, bounded event code, request/run ID ngẫu nhiên ngắn hạn,
build ID, duration bucket và outcome. Không log:

- board element/text/image/file, Yjs update, snapshot/import/export body;
- raw tenant/user/document/provider ID, email/display name;
- grant, cookie, token, Authorization header, Redis/Neon/B2 URL/key;
- raw provider/database error có thể chứa endpoint/query/value;
- stack trace gửi sang client.

Raw error được map sang taxonomy bounded trước log/client. Debug sampling phải mặc định off và vẫn qua
redaction test; không có chế độ “tạm log payload”. Retention log/metric và quyền dashboard cần owner
duyệt; evidence chỉ giữ đoạn đã redaction.

### 8.3 Alert candidate

| Alert | Ngưỡng candidate | Hành động đầu tiên |
| --- | --- | --- |
| No ready instance | 1 phút sau request đánh thức | `SEV-1`, force-off grant, kiểm tra Render/Neon. Cold-start expected vẫn phải được đo riêng. |
| Checkpoint age | `>30s` trong 2 phút | `SEV-1`, chuyển read-only; không giữ write chỉ trong RAM. |
| Snapshot age | `>10 phút` trong active room | `SEV-2`, disable restore/export mới và kiểm tra B2. |
| Neon dependency down | 1 phút | Dừng write/grant, chạy dependency runbook. Redis alert chỉ áp dụng cho production HA profile. |
| Error/reconnect rate | `>5%` trong 5 phút | Dừng rollout, kiểm tra provider/load; chống reconnect storm. |
| CPU/heap | `>70% CPU` hoặc `>75% heap/RSS budget` trong 10 phút | Giảm admission, xem coarse profile; không thêm high-cardinality debug. |
| Quota | 70/85/100% per agreed capacity | Notify/throttle/deny theo mức; không noisy-neighbor. |
| Cost forecast | 70/90/100% monthly owner budget | Notify/freeze scale/force-off nếu vượt hard cap. |

Ngưỡng phải được hiệu chỉnh bằng Gate E 2/10/50 và private-alpha canary; không nới error/RPO threshold
chỉ để làm dashboard xanh.

## 9. TCO và capacity worksheet

### 9.1 Profile đã chọn: hard cap `0 USD/tháng`

Owner chọn dùng free allowance, không mua Redis và không cho tự động nâng Render/Neon/B2. Mục tiêu:

```text
C_free_private_alpha =
  C_render_free
  + C_neon_within_free_allowance
  + C_b2_within_free_allowance
  = 0 USD/month target
```

Giá trị `0` chỉ đúng khi usage còn trong quota hiện hành. Provider quota/usage phải được chụp redacted
trong ngày approval. Khi đạt warning threshold, hệ thống giảm admission rồi force-off trước khi cần tài
nguyên trả phí; không tự mua add-on, scale instance hoặc đổi plan.

| Khoản active | Chi phí mục tiêu | Ràng buộc |
| --- | ---: | --- |
| 1 x Render Free Singapore | `0 USD` | Single instance, spin-down/cold-start, 750 free instance hours/workspace theo policy hiện hành, không scaling. |
| Redis | `0 USD` | Không provision/không dùng trong profile free. |
| Neon | `0 USD` | Chỉ khi compute/storage/transfer còn trong free allowance; vượt ngưỡng thì force-off. |
| B2 | `0 USD` | Chỉ khi storage/transaction/egress còn trong free allowance; vượt ngưỡng thì force-off snapshot/export mới. |
| **Approved hard monthly cap** | **`0 USD`** | Không automatic paid upgrade. |

### 9.2 Deferred production HA worksheet

Render Standard x2 + Redis Cloud Multi-AZ là đường nâng cấp, không phải chi phí đã duyệt. Khi có trigger
public beta/SLO/capacity, owner phải lấy quote dashboard mới và phê duyệt trước provision:

```text
C_paid_ha =
  (2 * C_render_standard_instance)
  + C_render_workspace_or_org
  + C_redis_cloud_singapore_multi_az
  + C_neon_incremental_compute
  + C_neon_incremental_storage_and_backup
  + C_b2_snapshot_storage
  + C_b2_restore_egress_and_transactions
  + C_observability
  + C_support
  + C_tax_fx_buffer
```

Chi phí lao động/on-call và incident rehearsal ghi riêng, không che trong cloud total. B2 Object Lock
không có phí riêng theo tài liệu provider nhưng object vẫn phát sinh storage/egress/transaction theo
pricing hiện hành.

| Khoản deferred | Quote/tháng | Currency | Quote date | Evidence redacted |
| --- | ---: | --- | --- | --- |
| 2 x Render Standard Singapore | `PENDING_OWNER_DASHBOARD_QUOTE` | `PENDING` | `PENDING` | `PENDING` |
| Render workspace/org requirement | `PENDING_OWNER_DASHBOARD_QUOTE` | `PENDING` | `PENDING` | `PENDING` |
| Redis Cloud paid Multi-AZ Singapore | `PENDING_OWNER_DASHBOARD_QUOTE` | `PENDING` | `PENDING` | `PENDING` |
| Neon incremental compute/storage/backup | `PENDING_USAGE_MODEL` | `PENDING` | `PENDING` | `PENDING` |
| B2 storage/restore traffic | `PENDING_USAGE_MODEL` | `PENDING` | `PENDING` | `PENDING` |
| Observability/support/tax buffer | `PENDING_OWNER_DECISION` | `PENDING` | `PENDING` | `PENDING` |
| **Paid HA hard monthly cap** | `PENDING_FUTURE_OWNER_APPROVAL` | `PENDING` | `PENDING` | owner sign-off |

### 9.3 Capacity input bắt buộc

- active rooms average/peak, room-minutes/tháng và profile 2/10/50;
- 500/500/2.000 objects, update bytes/second và snapshot bytes;
- connections/instance, CPU/RSS/heap và Neon connection pool; Redis ops/memory chỉ áp dụng cho paid HA;
- snapshot frequency/retention, restore drills, B2 stored GB/download GB;
- expected reconnect/error headroom và growth 3 tháng.

Free profile phải giữ admission dưới evidence thực đo; profile 2/10/50 isolated không tự chứng minh
Render Free capacity. Vượt cap hoặc không đạt RPO/RTO thì giữ feature off. Không provision paid HA cho
tới khi expected và peak model đều dưới hard cap tương lai với headroom đã ghi.

## 10. On-call và incident flow

| Vai trò | Owner | Trách nhiệm |
| --- | --- | --- |
| Primary on-call | `PENDING_OWNER_ASSIGNMENT` | Nhận alert, kích hoạt kill switch, giữ timeline/evidence. |
| Backup on-call | `PENDING_OWNER_ASSIGNMENT` | Review restore/rotation, liên hệ provider khi primary unavailable. |
| Security incident owner | `PENDING_OWNER_ASSIGNMENT` | Grant replay/credential/content exposure; quyết định revoke toàn cục. |
| Cost owner | `PENDING_OWNER_ASSIGNMENT` | Duyệt quote/budget/scale; không có quyền xem secret runtime. |

Incident tối thiểu:

1. xác nhận alert bằng bounded metrics, không bật payload logging;
2. nếu consistency/auth không chắc chắn, chuyển `read_only` hoặc `off` trước điều tra;
3. ghi UTC/Vietnam timestamp, build/deploy ID, dependency taxonomy và coarse affected profile;
4. dừng rollout/rotation đang chạy; không rollback schema mù;
5. xác minh last-good Neon checkpoint và B2 checksum trước restore;
6. phục hồi generation mới, chạy convergence/authorization/cleanup smoke;
7. chỉ mở edit sau owner incident approval; rotate credential nếu có khả năng lộ;
8. post-incident cập nhật RPO/RTO actual, cost và corrective gate; không lưu content/secret.

`SEV-1`: auth/revoke bypass, divergence, no ready replica, checkpoint vượt hard RPO hoặc nghi lộ secret.
`SEV-2`: dependency degraded, snapshot/restore/export delayed hoặc quota/cost 90%. Profile free không có
redundancy; instance unavailable sau cold-start budget là `SEV-1`.

## 11. Acceptance commands và evidence

Checkpoint cô lập ngày 2026-08-19 đã wire runtime model vào source candidate. Model dùng hai process
logic dùng chung atomic coordinator để kiểm chứng one-time consume, global quota, opaque HMAC document
name, writer lease/fencing, watermark/checkpoint, drain, dependency outage, kill switch, credential
rotation và bounded telemetry. Đây là contract test, **không giả làm Redis Cloud/Neon/Render thật**.
Hai-process evidence được giữ để kiểm tra invariant và đường nâng cấp production; provider profile được
owner chọn để đóng private alpha là single-instance Render Free không Redis.

```powershell
# PASS: 2 files, 18 tests
pnpm --filter @tutorhub/whiteboard-spike gate:excalidraw-runtime

# Chưa wire/chưa chạy: chỉ thực hiện trên disposable Render Free/Neon/B2 sau quyền riêng
pnpm --filter @tutorhub/whiteboard-spike test:excalidraw:runtime-operations
pnpm --filter @tutorhub/whiteboard-spike test:excalidraw:runtime-failures

# Existing regression gates that must stay green
pnpm --filter @tutorhub/whiteboard-spike test
pnpm --filter @tutorhub/whiteboard-spike lint
pnpm --filter @tutorhub/whiteboard-spike typecheck
pnpm --filter @tutorhub/whiteboard-spike build:excalidraw
pnpm --filter @tutorhub/whiteboard-spike gate:excalidraw-dependencies
pnpm --filter @tutorhub/whiteboard-spike structure:excalidraw-bundle
pnpm --filter @tutorhub/whiteboard-spike security:excalidraw-bundle

# Gate F.1 real runtime candidate
pnpm --filter @tutorhub/whiteboard-runtime typecheck
pnpm --filter @tutorhub/whiteboard-runtime lint
pnpm --filter @tutorhub/whiteboard-runtime test
pnpm --filter @tutorhub/whiteboard-runtime build
pnpm security:whiteboard-runtime-oci
```

Checkpoint result: full unit/integration `53/53`, lint, typecheck, Excalidraw-only build,
dependency/license, structure `184 assets` và security `182 text assets` đều PASS. Build vẫn có
expected large-chunk warning đã được Gate E ghi nhận; không đổi production force-off.

Provider-level acceptance sau này phải chạy từ exact scanned commit/image trên disposable resources,
tự nạp secret trong cùng process nhưng chỉ xuất boolean/bounded result. Không echo env, không lưu URL có
credential, không dùng shared staging trước khi disposable gate và owner approval đạt.

## 12. Exit checklist Gate F

### Automated/isolated

- [x] Exact local Node `24.15.0`, Hocuspocus `4.6.0`, Yjs `13.6.27` và Excalidraw `0.18.1` pin PASS.
- [x] Hai-node shared coordinator contract: atomic grant/quota, single writer lease, fencing, takeover
      và cleanup-zero PASS.
- [x] Readiness/degraded state, single-node crash, checkpoint watermark và graceful drain contract PASS.
- [x] Sustained 10 phút control/coordination/persistence outage fail closed; snapshot outage không làm
      live authority giả mất durability PASS.
- [x] Server-owned `read_only`/`export_only`/`off`, credential current/next overlap và old-credential
      negative probe PASS; snapshot verification key chỉ retire sau retention và full restore probe.
- [x] Immutable snapshot, corrupt quarantine, generation restore, portable provider exit và binding-key
      rotation với retained-key recovery PASS.
- [x] Fixed metric/label allowlist, cardinality rejection, privacy-safe event log và global quota PASS.
- [x] Cost 70/90/100 và quota 70/85/100 deterministic alert fixture; missing dashboard quote trả
      `quote_pending` PASS.
- [x] Full Gate A–E regression tiếp tục PASS tại checkpoint Gate F.
- [x] Gate F.1 source/package candidate: real Hocuspocus lifecycle, Neon/B2 adapters, non-root
      digest-pinned Dockerfile, CI Trivy/CycloneDX guard và production dependency boundary PASS.

### Disposable/provider còn bắt buộc

- [ ] OCI image digest, SBOM và production vulnerability scan của exact runtime artifact PASS.
- [ ] Một Hocuspocus instance thật trên Render Free Singapore dùng Neon binary checkpoint + B2 portable
      snapshot; cold-start, restart/deploy, reconnect và cleanup-zero PASS.
- [ ] Sustained Neon/B2/control outage, real SIGTERM/drain, credential rotation và backup/restore trên
      resource disposable riêng PASS với bounded evidence.

### Owner/provider

- [x] Owner chọn `FREE_PRIVATE_ALPHA`: Render Free Singapore một instance, không Redis, Neon/B2 trong
      free allowance; paid HA chưa provision.
- [ ] Owner chấp thuận rõ single-instance/no-HA, spin-down/cold-start, free quota và residual risk không
      có multi-region hot standby.
- [ ] Render/Neon/B2 quota dashboard được ghi redacted; expected/peak nằm trong allowance và hard cap
      `0 USD`.
- [ ] Primary/backup/security/cost owner được điền và xác nhận nhận alert.
- [ ] Retention/Object Lock mode, RPO/RTO và incident severity được owner duyệt.
- [ ] Disposable provider drill có quyền/blast radius riêng và evidence redacted.
- [ ] ADR-0034 cập nhật consequences/runtime/cost/owner và chuyển `Accepted` sau khi tất cả mục trên đạt.

### Deferred trước production/public beta

- [ ] Hai Render Standard replica + Redis Cloud paid Multi-AZ được owner duyệt ngân sách và provision.
- [ ] Two-node convergence, replica kill/rolling deploy, Redis sustained outage/failover và cleanup-zero
      PASS trên disposable paid-HA topology.

Cho tới lúc checklist hoàn tất, Gate F giữ `VERIFY`, ADR-0034 giữ `Proposed`, P5-COLLAB-01 giữ
`IN PROGRESS`; **không deploy và không bật production feature**.

## 13. Nguồn chính thức

Các nguồn được kiểm tra ngày 2026-08-19; pricing/plan/region phải refresh trong ngày owner approval:

- [Node.js 24.15.0 LTS release](https://nodejs.org/en/blog/release/v24.15.0) và
  [Node.js release lifecycle](https://nodejs.org/en/about/previous-releases).
- [Hocuspocus server configuration and resource limits](https://tiptap.dev/docs/hocuspocus/server/configuration),
  [hooks](https://tiptap.dev/docs/hocuspocus/server/hooks),
  [persistence guide](https://tiptap.dev/docs/hocuspocus/guides/persistence),
  [Database extension](https://tiptap.dev/docs/hocuspocus/server/extensions/database) và
  [Redis extension](https://tiptap.dev/docs/hocuspocus/server/extensions/redis).
- [Render regions](https://render.com/docs/regions),
  [free instances](https://render.com/docs/free),
  [WebSockets and shutdown behavior](https://render.com/docs/websocket),
  [health checks](https://render.com/docs/health-checks),
  [deploys and graceful shutdown](https://render.com/docs/deploys),
  [service scaling](https://render.com/docs/scaling),
  [instance types](https://render.com/docs/compute-plans),
  [Blueprint fields](https://render.com/docs/blueprint-spec) và
  [current pricing](https://render.com/pricing).
- [Redis Cloud high availability and replication](https://redis.io/docs/latest/operate/rc/databases/configuration/high-availability/)
  và [current pricing](https://redis.io/pricing/).
- [Neon regions](https://neon.com/docs/introduction/regions),
  [backup and restore](https://neon.com/docs/manage/backups) và
  [current pricing](https://neon.com/pricing).
- [Backblaze B2 Object Lock](https://www.backblaze.com/docs/cloud-storage-object-lock),
  [private/S3 integration constraints](https://www.backblaze.com/docs/en/cloud-storage-get-started-with-a-backblaze-integration)
  và [current B2 pricing](https://www.backblaze.com/cloud-storage/pricing).
