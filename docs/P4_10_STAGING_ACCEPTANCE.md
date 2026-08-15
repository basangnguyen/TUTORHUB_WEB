# P4-10 staging acceptance — join telemetry, privacy và diagnostics export

## 1. Trạng thái

`DONE` — contract, implementation, local/disposable, exact candidate CI, shared staging,
Render/Cloudflare và live acceptance đều PASS ngày 2026-08-15.

## 2. Safety boundary

- Không đọc, in hoặc log giá trị trong `.env*.local`.
- Chỉ nạp URL trong cùng command chạy gate; output chỉ chứa boolean/count/bounded status.
- Chỉ forward migration `35 false -> 36 false`; không rollback `000036`.
- Không xóa disposable branch trước khi toàn bộ database gate PASS.
- Không forward shared staging, deploy hoặc bật media capability trước khi disposable và exact
  candidate CI PASS.
- Ba principal phải tách biệt trên cùng database: migration owner, Core API runtime và maintenance.

## 3. Implementation/local gates

- [x] ADR-0033 khóa closed telemetry schema, privacy, retention, export và aggregate contract.
- [x] OpenAPI/client chỉ nhận enum hữu hạn, duration `0..600000`, range tối đa 31 ngày và limit
  `1..1000`; không có token/SDP/ICE/IP/device/provider/raw exception field.
- [x] Core API re-resolve exact active tenant/actor/participant/join-attempt; replay event idempotent.
- [x] Organization Admin export dùng expected-tenant + CSRF, pseudonym tenant-scoped, no-store và
  fail closed nếu audit không ghi được.
- [x] Web phát best-effort join/credential/connect/media/reconnect/disconnect/leave diagnostics;
  không tự rotate CSRF, không chặn room flow và không lưu token vào URL/storage.
- [x] Admin diagnostics panel có loading/error/retry, aggregate 24 giờ và redacted JSON download.
- [x] Full `pnpm verify`, focused integration-tag compile và diff formatting gates PASS.

## 4. Neon disposable gates

- [x] Owner preflight: đúng disposable branch, ledger `35 dirty=false`, ba URL cùng branch/database
  và ba role tách biệt.
- [x] Forward-only `35 false -> 36 false`; migrate lại giữ `36 false`.
- [x] Provision exact Core API runtime/PUBLIC/maintenance ACL cho ledger 36.
- [x] Actor/tenant/room/join-attempt binding, idempotency và foreign-tenant concealment PASS.
- [x] Join-success/p95/reconnect metric math và 30-day retention constraint PASS.
- [x] Runtime không DELETE/EXECUTE; maintenance chỉ EXECUTE purge và không SELECT table.
- [x] Bounded purge `1..1000` và `FOR UPDATE SKIP LOCKED` concurrency PASS.
- [x] Final read-only snapshot giữ `36 dirty=false`, media features force-off.

### Lệnh gate đã khóa confirmation

Nạp `DATABASE_MIGRATION_URL`, `DATABASE_POOL_URL` và `DATABASE_POLL_MAINTENANCE_URL` từ file
disposable trong cùng PowerShell process, đặt đúng ba confirmation sau rồi chạy
`corepack pnpm test:integration:media:p410`:

- `P4_10_DISPOSABLE_CONFIRM=I_UNDERSTAND_P4_10_DISPOSABLE_ONLY`
- `P4_10_ACL_PROVISION_CONFIRM=I_UNDERSTAND_P4_10_ACL_PROVISION_DISPOSABLE_ONLY`
- `P4_10_FINAL_SNAPSHOT_CONFIRM=I_UNDERSTAND_P4_10_FINAL_SNAPSHOT_READ_ONLY`

Không echo environment hoặc connection string. Harness tự fail closed nếu boundary/ledger/role sai.

## 5. Candidate, shared staging và live gates

- [x] Review diff/secret scan; commit/push exact candidate lên `main`.
- [x] GitHub Verify/Security PASS trên exact SHA.
- [x] Sau quyền riêng: shared owner preflight và forward-only/idempotent `35 -> 36` PASS.
- [x] Shared exact ACL và read-only zero-side-effect snapshot PASS trước deploy.
- [x] Render/Cloudflare deploy exact SHA; health/readiness/status PASS.
- [x] Live feature-off/privacy/concealment/accessibility probe PASS; không temporary-enable media.
- [x] Post-live shared snapshot PASS; cập nhật state/backlog/master/coordination sang P4-10 `DONE`.

Shared gates dùng `P4_10_SHARED_CONFIRM=I_UNDERSTAND_P4_10_SHARED_STAGING_ONLY` và chỉ đặt
một action confirmation trong mỗi process:

- owner preflight: `P4_10_SHARED_OWNER_PREFLIGHT=I_UNDERSTAND_P4_10_SHARED_OWNER_PREFLIGHT_READ_ONLY`;
- forward migration: `P4_10_SHARED_MIGRATION_CONFIRM=I_UNDERSTAND_P4_10_FORWARD_SHARED_STAGING_ONLY`;
- exact ACL: `P4_10_SHARED_ACL_PROVISION_CONFIRM=I_UNDERSTAND_P4_10_ACL_PROVISION_SHARED_STAGING_ONLY`;
- final snapshot: `P4_10_SHARED_FINAL_CONFIRM=I_UNDERSTAND_P4_10_SHARED_FINAL_SNAPSHOT_READ_ONLY`.

Harness fail closed nếu confirmation disposable/P4-09 cũ hoặc action P4-10 khác còn tồn tại.

## 6. Evidence ledger

### Local candidate

- Status: `PASS`.
- Evidence: full `pnpm verify` PASS gồm format, generated contract drift, local/E2E infra,
  GitHub Actions security, lint, typecheck, all package test/build, Storybook, bundle security và
  toàn bộ Go test/vet. API client `7/7` files, `53/53` tests; web `69/69` files, `437/437` tests.
  Focused media integration-tag compile, `git diff --check` và candidate secret-pattern scan cũng
  PASS. Exact final candidate là `c960f77753fa14475b84e7f0e0242bfcc458dacc`.

### Disposable Neon

- Status: `PASS — DISPOSABLE` ngày 2026-08-15 trên branch `p4-10-disposable-20260815`.
- Owner/runtime/maintenance URL boundary PASS với ba principal tách biệt trên cùng exact Neon
  endpoint/database. Forward-only/idempotent giữ chuỗi `35 false -> 36 false -> 36 false`; không
  rollback và branch tiếp tục được giữ lại.
- Focused P4-10 PostgreSQL đạt `4/4`: exact runtime/PUBLIC/maintenance ACL, tenant/actor/room/
  join-attempt binding, idempotency/concealment, join-success/p95/reconnect metric math, retention
  đúng 30 ngày và bounded `SKIP LOCKED` purge đều PASS.
- Retained media regression đạt PASS cho toàn bộ `11/11` nhóm. Lần chạy đầu phát hiện harness cũ
  ghim ledger 35; sau khi nâng exact latest ledger lên 36, `10/11` nhóm PASS và gate signal cuối phát
  hiện rate-window fixture chưa cô lập. Cleanup đầu ma trận đã được bổ sung; focused hand/reaction
  rerun PASS. Final read-only snapshot PASS tại `36 dirty=false`, diagnostics/expired/retention
  violation đều `0`; media deployment guard vẫn force-off. Retained enabled overrides được báo cáo,
  không xóa.

### Shared staging/live

- Status: `PASS — P4-10 DONE`.
- Exact candidate `c960f77753fa14475b84e7f0e0242bfcc458dacc` PASS GitHub Verify
  `31881117029`, Security `31881116916`, Browser E2E và Cloudflare Pages check
  `95003818195`. Dependency review skip đúng push policy; các security job còn lại đều success.
- Shared owner preflight PASS tại `35 dirty=false`, ba principal tách biệt/cùng exact database và
  media force-off. Forward-only/idempotent đạt `35 false -> 36 false -> 36 false`; exact
  runtime/PUBLIC/maintenance/dependency ACL và final read-only snapshot trước deploy đều PASS.
- Render deployment `dep-da04pjegekts7395k1h0` đạt `Live` đúng exact SHA. Cloudflare Pages
  deployment `b8a42033-ce9a-486b-aa1b-f9d0302452c7` success cùng SHA; preview
  `https://b8a42033.tutorhub-web.pages.dev`.
- Direct Render và Pages-proxied health/readiness/status cùng anonymous diagnostics record/export
  privacy đạt `10/10`: expected status, `no-store`, request ID, không Set-Cookie, JSON typed và
  không lộ token/SDP/ICE/IP/device/provider/participant/join-attempt field.
- Organization Admin xác nhận “Phòng học trực tuyến” và “Phòng học nhóm tức thời” đều đang tắt;
  synthetic MediaSpace bị conceal. Workspace có `52/52`, concealment có `11/11` exposed controls
  mang accessible name; mỗi trang có đúng một `main`, `h1`, `nav`, không duplicate ID,
  media resource hoặc console warning/error.
- Post-live read-only snapshot tiếp tục PASS tại `36 dirty=false`, media force-off,
  `diagnostics=0`, `expired_diagnostics=0`, `retention_violations=0` và
  `retained_enabled_media_overrides=0`. Không rollback, không temporary-enable capability;
  disposable branch tiếp tục được giữ lại. Physical browser/device, 25/50 load,
  provider-outage và optional-effect gates vẫn `UNVERIFIED — P4-11`.
