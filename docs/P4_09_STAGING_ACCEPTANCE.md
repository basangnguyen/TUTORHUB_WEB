# P4-09 staging acceptance — reconnect, recovery và degraded audio-only

## 1. Safety boundary

- Không đọc, in hoặc log giá trị trong `.env*.local`.
- Chỉ nạp URL trong cùng command chạy gate; output chỉ là boolean/count/bounded status.
- Không rollback migration `000035` và không xóa Neon disposable trước khi toàn bộ database gate PASS.
- Không forward shared staging, deploy hoặc bật capability trước khi disposable và candidate CI PASS.

## 2. Implementation gates

- [x] ADR-0032 và OpenAPI khóa transient reconnect, terminal reauthorization và recovery authority.
- [x] Core API tạo recovery chỉ từ exact latest failed instance; replay idempotent.
- [x] Signed provider finish chuyển active/provisioning instance và participant sang failed bounded.
- [x] Provider call nằm ngoài transaction và retry hội tụ cùng RoomInstance.
- [x] Web không gọi token/recovery API trong `Reconnecting -> Reconnected`.
- [x] Terminal disconnect xóa credential; removed/deleted/ended không auto-rejoin.
- [x] Audio-only tắt local camera, giữ microphone/remote audio/leave và không tự bật lại camera.
- [x] OpenAPI generated client, Go/web tests, lint/typecheck/build/security gates PASS.

## 3. Neon disposable gates

- [x] Owner preflight: đúng disposable branch, ledger `34 dirty=false`, ba URL cùng branch/database.
- [x] Forward-only `34 false -> 35 false`; migrate lại giữ `35 false`.
- [x] Exact Core API runtime ACL và PUBLIC zero privilege PASS.
- [x] Recovery failed-only, stale-version, idempotency và foreign-tenant concealment PASS.
- [x] Hai recovery đồng thời tạo đúng một successor; một-active-instance invariant PASS.
- [x] End/source terminal/enrollment or participant revocation thắng reconnect/recovery.
- [x] Provider-finished cascade chuyển participants/admissions terminal đúng và không lộ raw data.
- [x] Các PostgreSQL integration regression liên quan PASS.
- [x] Final read-only snapshot giữ `35 dirty=false`, media features force-off.

## 4. Candidate, shared staging và live gates

- [ ] Review diff/secret scan; commit/push exact candidate lên `main`.
- [ ] GitHub Verify và Security PASS trên exact SHA.
- [ ] Sau quyền riêng: shared owner preflight và forward-only/idempotent `34 -> 35` PASS.
- [ ] Shared exact ACL/read-only zero-side-effect snapshot PASS trước deploy.
- [ ] Render và Cloudflare triển khai exact SHA; health/readiness/status PASS.
- [ ] Live feature-off/privacy/concealment/accessibility probe PASS, không temporary-enable media.
- [ ] Post-live read-only snapshot PASS; PROJECT_STATE/backlog/master/coordination chuyển P4-09 DONE.

## 5. Evidence ledger

### Local candidate

- Status: `PASS`.
- Evidence: `pnpm verify` PASS; web `67/67` files, `431/431` tests; API client `7/7`
  files, `52/52` tests; toàn bộ format, generated-contract drift, lint, typecheck, build,
  Storybook, security bundle, Go test/vet PASS. Focused integration-tag P4-09 compile gate và
  `git diff --check` cũng PASS.

### Disposable Neon

- Status: `PASS`.
- Evidence: owner preflight PASS ở `34 dirty=false`, ba principal tách biệt/cùng disposable
  boundary và effective media feature force-off. Migration chạy forward-only/idempotent
  `34 false -> 35 false -> 35 false`. Recovery race PASS `exactly_one=true`, replay idempotent,
  foreign tenant concealed và lock thắng. Exact runtime/PUBLIC/maintenance/dependency ACL cùng
  toàn bộ `9/9` media PostgreSQL regression programs PASS, gồm credential/signed webhook đã được
  căn lại với P4-09 `room_finished -> failed` và active participant cascade `failed`; participant
  đã `left` không bị sửa lại. Final read-only snapshot PASS tại `35 dirty=false`, effective media
  feature force-off, `recovery_receipts=1`, `recovery_events=1`,
  `retained_enabled_media_overrides=13`, `retained_active_intents=3`. Không rollback; disposable
  branch vẫn được giữ lại.

### Shared staging/live

- Status: `BLOCKED UNTIL CANDIDATE CI PASS`.
