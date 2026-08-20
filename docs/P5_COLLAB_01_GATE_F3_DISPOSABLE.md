# P5-COLLAB-01 Gate F.3 — Disposable provider drill

> **Trạng thái:** `DONE` — provider drill và owner/quota sign-off đều `PASS` ngày 2026-08-20.
> Render/Neon/B2 disposable đã được kiểm tra với bounded evidence; shared staging và production vẫn
> force-off.

## 1. Phạm vi và ranh giới trung thực

Gate này chỉ dùng resource disposable, hard cap `0 USD`:

- một Hocuspocus runtime thật trên Render Free Singapore;
- một control-plane fixture riêng trên Render Free Singapore;
- một Neon branch và role runtime riêng;
- một B2 private bucket và application key riêng;
- không Redis, không shared staging, không production feature flag.

Fixture chỉ cung cấp exact runtime-state/grant contract để test provider. Nó không thay Core API thật.
P5-COLLAB-02 vẫn sở hữu migration/ACL chính thức, P5-COLLAB-04 sở hữu control-plane endpoint chính
thức và P5-COLLAB-07 sở hữu snapshot scheduling/catalog/restore generation swap. Gate F.3 chỉ kiểm
tra OCI candidate và adapter thật; không được ghi nhận ba task tương lai là đã hoàn thành.

## 2. Candidate và guard

- Render Blueprint: `infrastructure/render/p5-collab-01-gate-f3.render.yaml`.
- Disposable control fixture: `scripts/p5-collab-01-gate-f3-control.mjs`.
- Secret-safe preflight: `scripts/require-p5-collab-01-gate-f3-confirm.mjs`.
- Neon schema/ACL fixture: `scripts/p5-collab-01-gate-f3-neon.mjs`.
- Provider round-trip: `services/whiteboard-runtime/provider-drill.mjs`.
- Sustained control outage: `scripts/p5-collab-01-gate-f3-control-outage.mjs`.

Exact automation tree `def10c0` PASS GitHub Verify `32255600426` và Security `32255600491` ngày
2026-08-19. Kết quả này cho phép bắt đầu provision disposable resource; chưa được tính là provider PASS.

Automation từ chối pooler, shared-stage credential, Redis, region khác Singapore, spend cap khác `0`,
resource quá 7 ngày, URL chứa credential ở HTTP origin và owner/runtime database dùng cùng role. Output
chỉ gồm boolean, duration bucket và bounded failure code; không in URL, token, password, document body.

## 3. Resource cần tạo thủ công

### 3.1 Neon disposable

1. Tạo child branch từ `staging`, tên `p5-collab-01-f3-disposable-YYYYMMDD`, auto-delete tối đa 7 ngày.
2. Tạo role riêng `tutorhub_collab_f3` trên branch disposable; không dùng owner hoặc
   `tutorhub_runtime` dùng chung.
3. Lấy hai direct, non-pooled URL của cùng branch/database:
   - owner URL cho `P5_F3_DATABASE_OWNER_URL`;
   - role riêng cho `DATABASE_COLLABORATION_URL`.
4. Cả hai URL phải có `sslmode=require`; hostname không chứa `-pooler`.

Provision script chỉ tạo bảng fixture `public.collaboration_document_checkpoints`, revoke `PUBLIC` và
grant đúng `SELECT, INSERT, UPDATE` cho role riêng. Đây không phải forward migration P5-COLLAB-02.

### 3.2 B2 disposable

1. Tạo private bucket riêng, không bật Object Lock trong drill chưa được owner duyệt.
2. Đặt lifecycle xóa object sau tối đa 7 ngày.
3. Tạo application key chỉ giới hạn bucket này, đủ list/read/write cho S3 `HeadBucket`, `PutObject` và
   `GetObject`; không cấp delete/admin/master capability.
4. Ghi endpoint, region, bucket, key ID và application key vào file local ignored.

### 3.3 Render Free Singapore

Chỉ sync Blueprint sau khi commit chứa harness đã PASS GitHub Verify/Security. Tạo đúng hai web service
Free, region `singapore`, auto-deploy off. Cả hai Render health check dùng `/livez` để chỉ đo
liveness; Hocuspocus runtime vẫn dùng `/readyz` cho dependency readiness và fail-closed gate.
Không cấu hình Render health check vào `/readyz`, vì sustained dependency outage sẽ tạo
restart-loop trên single Free instance. Render Free không hỗ trợ cấu hình `maxShutdownDelaySeconds`, nên candidate dùng shutdown
window mặc định 30 giây và application drain budget 25 giây để giữ 5 giây safety margin. Nếu Dashboard
yêu cầu plan trả phí, dừng gate, không provision.
Blueprint khóa tên disposable có hậu tố `bs-20260819`, control origin tương ứng và synthetic client
origin `https://p5-f3-client.invalid`; nếu tên Render đã bị chiếm thì đổi đồng thời service name,
`COLLAB_CONTROL_PLANE_URL` và file local trước khi provision.

## 4. File local ignored

Tạo `.env.p5-collab-01-gate-f3.local`; không gửi nội dung qua chat, không commit và không chụp secret.
File cần đúng các tên biến sau:

```dotenv
P5_F3_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY
P5_F3_DISPOSABLE_EXPIRES_AT=<ISO-8601 trong 7 ngày>
P5_F3_RENDER_REGION=singapore
P5_F3_HARD_CAP_USD=0
P5_F3_NEON_BRANCH_ID=<opaque branch id>
P5_F3_B2_BUCKET_ID=<opaque bucket id>

P5_F3_RUNTIME_URL=<credential-free HTTPS origin>
P5_F3_CONTROL_URL=<credential-free HTTPS origin>
P5_F3_ALLOWED_ORIGIN=<disposable client HTTPS origin>
P5_F3_PROVIDER_DOCUMENT_NAME=wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1

COLLAB_RUNTIME_PROFILE=FREE_PRIVATE_ALPHA
COLLAB_INSTANCE_COUNT=1
COLLAB_ALLOWED_ORIGINS=<same P5_F3_ALLOWED_ORIGIN>
COLLAB_CONTROL_PLANE_URL=<same P5_F3_CONTROL_URL>
COLLAB_CONTROL_PLANE_TOKEN=<same P5_F3_CONTROL_TOKEN_CURRENT>
COLLAB_METRICS_TOKEN=<random secret>
COLLAB_BUILD_ID=<exact deployed commit>

P5_F3_CONTROL_TOKEN_CURRENT=<random secret>
P5_F3_CONTROL_TOKEN_NEXT=<optional distinct random secret>
P5_F3_CONTROL_ADMIN_TOKEN=<distinct random secret>

P5_F3_DATABASE_OWNER_URL=<direct owner URL>
DATABASE_COLLABORATION_URL=<direct runtime-role URL>

B2_ENDPOINT=<credential-free HTTPS origin>
B2_REGION=<region>
B2_BUCKET=<private disposable bucket>
B2_KEY_ID=<bucket-scoped key id>
B2_APPLICATION_KEY=<bucket-scoped application key>
```

Không đặt `DATABASE_POOL_URL`, `DATABASE_MIGRATION_URL`, maintenance URL, LiveKit credential, Redis URL
hoặc shared-stage confirmation trong process này.

## 5. Trình tự chạy

Mỗi command tự nạp file local trong cùng process; không `Get-Content`, `echo` hoặc log biến môi trường.

```powershell
node --env-file=.env.p5-collab-01-gate-f3.local scripts/require-p5-collab-01-gate-f3-confirm.mjs
node --env-file=.env.p5-collab-01-gate-f3.local scripts/p5-collab-01-gate-f3-neon.mjs
pnpm --filter @tutorhub/whiteboard-runtime build
node --env-file=.env.p5-collab-01-gate-f3.local services/whiteboard-runtime/provider-drill.mjs
```

Baseline PASS phải trả bounded result có `hocuspocus_sync`, `checkpoint_recovery`,
`b2_read_after_write`, `cleanup_zero` đều `true` và cold-start duration bucket.

### Baseline provider checkpoint — 2026-08-20

Exact candidate `7febbce` đã PASS GitHub Verify `32335427818` và Security `32335427799`, sau đó được
deploy `live` trên runtime Render Free Singapore. Preflight, Neon schema/exact ACL và provider baseline
đều PASS mà không log secret. Bounded result được giữ:

```text
hocuspocus_sync=true
checkpoint_recovery=true
b2_read_after_write=true
cleanup_zero=true
cold_start_bucket=lt_5s
```

Checkpoint này chỉ đóng baseline. Chưa suy PASS cho real SIGTERM/drain, post-redeploy recovery,
sustained control/Neon/B2 outage, credential rotation/restore hoặc owner/provider checklist.

### SIGTERM/drain + post-redeploy recovery checkpoint — 2026-08-20

Manual deploy exact candidate `7febbce` tạo deploy `dep-da39i0ibkg8c7381o8i0`. Instance cũ `[tnmnl]`
ghi `drain_started`, rồi `drain_complete` với `outcome=ok`, `duration_bucket=lt_100ms`; instance mới
`[kz697]` lên `live` và `/readyz=200`. Provider drill sau redeploy PASS:

```text
hocuspocus_sync=true
checkpoint_recovery=true
b2_read_after_write=true
cleanup_zero=true
cold_start_bucket=lt_5s
```

Không giữ raw log hoặc secret; chỉ giữ event code, outcome, duration bucket và boolean/bucket result.
Checkpoint này đóng hai bước đầu dưới đây, nhưng không suy PASS cho sustained outage, rotation/restore
hoặc owner/provider closure.

Các bước tiếp theo sau baseline:

1. [x] Trigger manual deploy exact commit của runtime để Render gửi SIGTERM; log chỉ được giữ
       `drain_started`/`drain_complete` và duration bucket.
2. [x] Chạy lại provider drill để chứng minh reconnect/recovery từ Neon checkpoint.
3. [x] Thêm ba biến sau vào file local rồi chạy sustained control outage:

```dotenv
P5_F3_OUTAGE_TARGET=control
P5_F3_OUTAGE_SECONDS=600
P5_F3_OUTAGE_CONFIRM=I_UNDERSTAND_P5_F3_CONTROL_WILL_BE_UNAVAILABLE_FOR_600_SECONDS
```

```powershell
node --env-file=.env.p5-collab-01-gate-f3.local scripts/p5-collab-01-gate-f3-control-outage.mjs
```

Script giữ control unavailable đúng 600 giây, xác nhận runtime `/readyz=503` mỗi 60 giây, khôi phục
control và yêu cầu `/readyz=200`. Nếu process bị ngắt, kiểm tra control fixture về `enabled` trước khi
tiếp tục.

Kết quả 2026-08-20: PASS đủ 600 giây với 10 probe mỗi 60 giây; `/livez=200`,
`/readyz=503` trong outage và `/readyz=200` sau khôi phục. Render health check đã dùng `/livez`, nên
dependency outage không tạo restart loop.

Neon sustained outage dùng đúng role disposable `tutorhub_collab_f3`, không suspend compute và không
chạm branch `staging`:

```powershell
$env:P5_F3_OUTAGE_TARGET = "neon"
$env:P5_F3_OUTAGE_SECONDS = "600"
$env:P5_F3_OUTAGE_CONFIRM = "I_UNDERSTAND_P5_F3_NEON_ROLE_WILL_BE_UNAVAILABLE_FOR_600_SECONDS"
node --env-file=.env.p5-collab-01-gate-f3.local scripts/p5-collab-01-gate-f3-neon-outage.mjs
```

Harness chỉ `NOLOGIN` role trên, terminate session của chính role đó trong database disposable, giữ
fail-closed 600 giây và tự `LOGIN` lại trong `finally`. Output chỉ có target, elapsed, liveness,
fail-closed và recovery boolean; không có URL, role credential hoặc raw provider error.

### Sustained Neon outage checkpoint — 2026-08-20

- Preflight xác nhận exact role disposable `tutorhub_collab_f3` đang `LOGIN`, direct runtime database
  kết nối được và Runtime `/livez=200`, `/readyz=200`.
- Harness chuyển đúng role này sang `NOLOGIN`, terminate session của chính role trên database
  disposable và xác nhận fail closed. Mười mốc `60, 120, 180, 240, 300, 360, 420, 480, 540, 600`
  giây đều PASS với `/livez=200`, `/readyz=503`.
- `finally` khôi phục `LOGIN`; direct database recovery và `/readyz=200` đều PASS. Final bounded result
  giữ `duration_seconds=600`, `fail_closed=true`, `liveness_preserved=true`, `recovered=true`.
- Post-restore provider drill PASS Hocuspocus sync, Neon checkpoint recovery, B2 r2 read-after-write,
  snapshot dependency readiness và cleanup-zero.

## 6. Drill cần thao tác provider

- **Neon outage:** suspend compute hoặc tạm revoke CONNECT của role riêng, giữ 600 giây, xác nhận runtime
  fail-closed; restore rồi chạy provider drill. Không chạm branch `staging`.
- **B2 outage:** revoke key disposable cũ, xác nhận B2 round-trip thất bại nhưng không làm mất Neon
  checkpoint; tạo key bucket-scoped mới, cập nhật local/Render và chạy lại.

Sustained B2 outage dùng key đã bị revoke và giữ đúng 600 giây. Harness chỉ xuất boolean/bounded stage,
không xuất key, endpoint, bucket ID, object key hoặc raw provider error:

```powershell
$env:P5_F3_OUTAGE_TARGET = "b2"
$env:P5_F3_OUTAGE_SECONDS = "600"
$env:P5_F3_OUTAGE_CONFIRM = "I_UNDERSTAND_P5_F3_B2_KEY_WILL_REMAIN_UNAVAILABLE_FOR_600_SECONDS"
node --env-file=.env.p5-collab-01-gate-f3.local scripts/p5-collab-01-gate-f3-b2-outage.mjs
```

Probe thu hồi dùng `b2_authorize_account` v4 với timeout 15 giây và chỉ coi HTTP `401` là credential
đã bị revoke. HTTP `200` là fail-open; status khác hoặc lỗi mạng làm gate dừng, không được tính PASS.

Mỗi phút harness xác nhận `/livez=200`, `/readyz=200`, B2 vẫn fail-closed và last-good Neon checkpoint
vẫn đọc được. Không tạo key mới hoặc redeploy cho tới khi đủ 10 lần kiểm tra.

### Sustained B2 outage checkpoint — 2026-08-20

- Runtime Render được cập nhật sang key bucket-scoped `r1`, redeploy commit `f09333d` lên `live`; provider
  baseline sau redeploy PASS `b2_read_after_write`, checkpoint recovery, Hocuspocus sync và cleanup zero.
- Key `r1` được revoke sau positive probe. Exact revoke preflight, Runtime liveness/readiness và Neon
  checkpoint preflight đều PASS.
- Mười mốc `60, 120, 180, 240, 300, 360, 420, 480, 540, 600` giây đều PASS:
  `b2_fail_closed=true`, `liveness_preserved=true`, `neon_checkpoint_preserved=true`.
- Final bounded result: `duration_seconds=600`, `outcome=pass`, `target=b2`. Không ghi URL, credential,
  bucket/object identifier hoặc raw provider error.
- Rotation recovery sang key bucket-scoped `r2` PASS: local positive read/write trước khi cập nhật,
  Render redeploy commit `f09333d` trở lại `live`, rồi post-redeploy provider probe PASS với
  `snapshot_dependency_up=true`, checkpoint recovery, Hocuspocus sync và cleanup zero. Negative probe
  của `r1` trả `401` tại toàn bộ cửa sổ, gồm mốc cuối 600 giây; credential cũ không được tái sử dụng.

### Control credential rotation checkpoint — 2026-08-20

- Control disposable chấp nhận đồng thời `current` và `next`; hai positive probe đều trả `200` trước
  khi chuyển Runtime.
- Runtime được chuyển sang token mới và redeploy; provider drill PASS trước khi token trước bị retire.
- Sau retirement, hai token trước đều trả `401`, token replacement cuối trả `200`. Runtime `/readyz`
  trở lại `200`; final provider drill PASS. Một token test xuất hiện trong output công cụ khi kiểm tra
  DOM và đã được coi là compromised, thay thế ngay trong cùng rotation; không tái sử dụng token đó.
- File local ignored và hai Render service chỉ còn token replacement; không ghi token vào tài liệu,
  log hoặc source tree.

### Portable backup/restore adapter checkpoint — 2026-08-20

- B2 private bucket thực hiện immutable content-addressed put, read-after-write byte equality và parse
  lại portable `tutorhub.excalidraw.portable-scene` envelope.
- Exact engine/format version và semantic hash sau restore round-trip đều khớp; bounded result giữ
  `portable_restore_round_trip=true`.
- Đây là provider adapter recovery evidence. Snapshot catalog, corrupt quarantine và atomic generation
  swap vẫn thuộc P5-COLLAB-07, không được suy là đã hoàn thành ở Gate F.3.

- **Credential rotation:** control dùng current/next overlap; runtime chuyển sang next trước khi current
  bị xóa. Neon và B2 tạo credential mới, update runtime, PASS positive probe rồi mới revoke credential
  cũ. Negative probe của credential cũ phải fail.
- **Backup/restore:** B2 adapter thực hiện immutable put + read-after-write checksum. Full catalog,
  quarantine và generation swap vẫn thuộc P5-COLLAB-07, không tuyên bố hoàn thành ở F.3.

## 7. Evidence và cleanup

Được giữ: exact commit/build ID, region, plan Free, UTC/Vietnam timestamp, duration bucket, boolean,
HTTP status, redacted quota screenshot và bounded event code. Không giữ credential URL, secret, raw
provider error, scene/Yjs content hoặc student data.

Sau khi evidence được ghi:

1. revoke toàn bộ credential disposable;
2. xóa hai Render service, Neon branch và B2 bucket;
3. xóa file `.env.p5-collab-01-gate-f3.local`;
4. xác nhận hard cap actual `0 USD` và không còn resource chạy.

Provider drill và owner/provider checklist trong `P5_COLLAB_01_RUNTIME_OPERATIONS.md` đã đạt ngày
2026-08-20; Gate F/P5-COLLAB-01 đã chuyển `DONE` và ADR-0034 đã chuyển `Accepted`. Cleanup provider
resource/file local vẫn là housekeeping bắt buộc sau khi retained evidence không còn cần resource;
không tự suy rằng cleanup đã chạy.
