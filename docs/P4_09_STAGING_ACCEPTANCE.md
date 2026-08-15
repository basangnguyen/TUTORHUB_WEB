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

- [x] Review diff/secret scan; commit/push exact candidate lên `main`.
- [x] GitHub Verify và Security PASS trên exact SHA.
- [x] Sau quyền riêng: shared owner preflight và forward-only/idempotent `34 -> 35` PASS.
- [x] Shared exact ACL/read-only zero-side-effect snapshot PASS trước deploy.
- [x] Render và Cloudflare triển khai exact SHA; health/readiness/status PASS.
- [x] Live feature-off/privacy/concealment/accessibility probe PASS, không temporary-enable media.
- [x] Post-live read-only snapshot PASS; PROJECT_STATE/backlog/master/coordination chuyển P4-09 DONE.

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

- Status: `PASS — P4-09 DONE`.
- Exact final candidate `fe33ffaba19d8f82f2034ddeb4b16d4e919e5014` PASS GitHub Verify
  `31868991020`, Security `31868991007`, Browser E2E và Cloudflare Pages check `94974692563`.
  Dependency review skip đúng policy của push; mọi security job còn lại đều success.
- Shared owner preflight PASS ở `34 dirty=false`. Migration chạy forward-only/idempotent
  `34 false -> 35 false -> 35 false`; exact runtime/PUBLIC/maintenance/dependency ACL PASS.
  Final read-only snapshot giữ `35 dirty=false`, media force-off và `recovery_side_effects=0`.
- Render deployment `dep-da00d7dbedkc739jgt60` đạt `Live` đúng exact SHA. Cloudflare Pages preview
  `https://2a8eed45.tutorhub-web.pages.dev` deploy thành công cùng SHA.
- Direct Render và Pages-proxied health/readiness/status cùng anonymous MediaSpace/recovery privacy
  đạt `10/10`: expected status, `no-store`, request ID, không Set-Cookie, JSON typed và không lộ
  credential/provider/device field.
- Organization Admin xác nhận “Phòng học trực tuyến” và “Phòng học nhóm tức thời” đều đang tắt;
  synthetic MediaSpace được conceal. Workspace có `53/53`, concealment có `12/12` exposed controls
  mang accessible name; mỗi trang có đúng một `main`, `h1`, `nav`, không duplicate ID, media/effect
  resource hoặc console warning/error.
- Post-live read-only snapshot tiếp tục PASS tại `35 dirty=false`, media force-off và
  `recovery_side_effects=0`. Không rollback, không temporary-enable capability; disposable branch
  tiếp tục được giữ lại. P4-09 chuyển `IN PROGRESS -> VERIFY -> DONE`; physical browser/device,
  25/50 load, provider-outage và optional-effect gates vẫn `UNVERIFIED — P4-11`.
