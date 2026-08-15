# P4-10 staging acceptance — join telemetry, privacy và diagnostics export

## 1. Trạng thái

`VERIFY` — contract, implementation, local deterministic và Neon disposable gates đã hoàn thành.
Exact candidate CI, shared staging, deploy và live acceptance chưa chạy nên P4-10 chưa phải `DONE`.

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

- [ ] Review diff/secret scan; commit/push exact candidate lên `main`.
- [ ] GitHub Verify/Security PASS trên exact SHA.
- [ ] Sau quyền riêng: shared owner preflight và forward-only/idempotent `35 -> 36` PASS.
- [ ] Shared exact ACL và read-only zero-side-effect snapshot PASS trước deploy.
- [ ] Render/Cloudflare deploy exact SHA; health/readiness/status PASS.
- [ ] Live feature-off/privacy/concealment/accessibility probe PASS; không temporary-enable media.
- [ ] Post-live shared snapshot PASS; cập nhật state/backlog/master/coordination sang P4-10 `DONE`.

## 6. Evidence ledger

### Local candidate

- Status: `PASS — VERIFY`.
- Evidence: full `pnpm verify` PASS gồm format, generated contract drift, local/E2E infra,
  GitHub Actions security, lint, typecheck, all package test/build, Storybook, bundle security và
  toàn bộ Go test/vet. API client `7/7` files, `53/53` tests; web `69/69` files, `437/437` tests.
  Focused media integration-tag compile, `git diff --check` và candidate secret-pattern scan cũng
  PASS. Exact SHA review vẫn được chốt trước push.

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

- Status: `PENDING` — bị khóa sau disposable và exact candidate CI.
