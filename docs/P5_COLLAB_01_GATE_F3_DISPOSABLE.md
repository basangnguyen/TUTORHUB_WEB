# P5-COLLAB-01 Gate F.3 — Disposable provider drill

> **Trạng thái:** `PREPARED`, chưa phải `PASS`. Automation local đã tách biệt; chưa provision hoặc
> kết nối Render/Neon/B2 thật. Shared staging và production vẫn force-off.

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
Free, region `singapore`, auto-deploy off. Control fixture dùng `/livez`; Hocuspocus runtime dùng
`/readyz`. Render Free không hỗ trợ cấu hình `maxShutdownDelaySeconds`, nên candidate dùng shutdown
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

Sau baseline:

1. Trigger manual deploy exact commit của runtime để Render gửi SIGTERM; log chỉ được giữ
   `drain_started`/`drain_complete` và duration bucket.
2. Chạy lại provider drill để chứng minh reconnect/recovery từ Neon checkpoint.
3. Thêm ba biến sau vào file local rồi chạy sustained control outage:

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

## 6. Drill cần thao tác provider

- **Neon outage:** suspend compute hoặc tạm revoke CONNECT của role riêng, giữ 600 giây, xác nhận runtime
  fail-closed; restore rồi chạy provider drill. Không chạm branch `staging`.
- **B2 outage:** revoke key disposable cũ, xác nhận B2 round-trip thất bại nhưng không làm mất Neon
  checkpoint; tạo key bucket-scoped mới, cập nhật local/Render và chạy lại.
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

Chỉ sau khi toàn bộ provider drill và owner/provider checklist trong
`P5_COLLAB_01_RUNTIME_OPERATIONS.md` đạt mới chuyển Gate F/P5-COLLAB-01 sang `DONE` và ADR-0034 sang
`Accepted`.
