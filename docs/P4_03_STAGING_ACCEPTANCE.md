# P4-03 — Prejoin device/network và join-attempt staging acceptance

## 1. Trạng thái và ranh giới

- Ngày cập nhật: `2026-08-10`.
- Trạng thái hiện tại: `DONE`.
- Implementation, local verification, Chromium E2E, Neon disposable, LiveKit test-provider,
  exact CI/security, shared ACL, exact deploy và live acceptance đều `PASS`.
- P4-03 không có migration mới. Ledger disposable và shared cuối cùng đều là `30 false`; không
  forward migration và không rollback.
- Hai feature `classroom_media_rooms` và `instant_study_rooms` tiếp tục force-off theo rollout
  guardrail. `effect=None` tiếp tục là baseline; không thêm processor production dependency.
- Disposable branch tiếp tục được giữ lại; không xóa trong lượt acceptance này.

## 2. Candidate đã triển khai

### 2.1 Canonical join-attempt contract

- Thêm `POST /api/v1/media/spaces/{space_id}/join-attempts` vào OpenAPI, generated TypeScript client
  và Core API.
- Client chỉ gửi `join_attempt_id`, `expected_room_instance_id` và `expected_space_version`; tenant,
  actor, source authority, role và LiveKit grants đều do server suy ra.
- Join-attempt idempotent theo exact actor/space/room/id; stale room/version và inaccessible source
  bị chặn trước mutation.
- Participant `waiting` không nhận credential và không kết nối LiveKit. Credential endpoint chỉ
  chấp nhận exact participant attempt đang `admitted` hoặc `joining`.
- Credential recheck role/source/lobby hiện hành; role downgrade không thể dùng admitted attempt cũ
  để bypass lobby.

### 2.2 Prejoin và local media ownership

- Initial render không gọi `getUserMedia()` hoặc `enumerateDevices()`; camera/mic chỉ mở sau explicit
  action của người dùng.
- Mic/camera/speaker selection, preview, mic meter, speaker test, speech mode EC/NS/AGC dạng `ideal`
  và explicit original-sound mode được tách vào controller có lifecycle rõ.
- Device labels chỉ xuất hiện sau permission. Không lưu label, device ID, token hoặc provider
  credential vào URL, history, `localStorage` hay `sessionStorage`.
- Permission denied, policy block, no-device, device busy, overconstraint, abort và autoplay đều map
  vào taxonomy hữu hạn; listen-only vẫn khả dụng khi policy cho phép.
- Device change được debounce; switch/cancel/unmount dừng mọi owned track, AudioContext, oscillator,
  audio element, animation frame, timer và listener.

### 2.3 Network, handoff và room boundary

- Network probe chỉ dùng `navigator.onLine` và coarse same-origin `/api/health`; không thu SDP/ICE,
  raw IP, device label hoặc raw provider error.
- Credential chỉ tồn tại trong memory escrow một lần. Reload, workspace/principal/source/room change,
  disconnect, error hoặc leave đều purge credential và ngăn auto-reconnect ngoài ý muốn.
- LiveKit room dùng stable callbacks/options, output-device progressive enhancement và `StartAudio`
  recovery khi browser chặn autoplay.
- Standalone room/recovery routes có landmark chính và bounded loading/error/retry/leave states.

## 3. Security và database invariants

- Source authorization chạy ngay sau exact MediaSpace lookup, trước feature/version/status/lock
  observation; cùng-tenant outsider nhận concealed not-found thay vì state oracle.
- Tenant advisory lock giữ ordering cho join-attempt; quota/capacity và unique join-attempt constraint
  chặn double reservation.
- Runtime role không có table-wide privilege, schema `CREATE`, ownership hay migration-role
  membership.
- Exact ACL của `media_participant_sessions` không cho `joining_at` trong `INSERT`; trường này chỉ
  được runtime cập nhật trong credential transition.
- Không log database URL, LiveKit key/secret, access token, device label, SDP/ICE hoặc raw provider
  error.

## 4. Local verification evidence

| Gate                                                                                        | Kết quả                                                  |
| ------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| `pnpm verify`                                                                               | PASS trên exact candidate tree                           |
| Web Vitest                                                                                  | `57` files, `289` tests PASS                             |
| API client Vitest                                                                           | `7` files, `47` tests PASS                               |
| Focused prejoin/room Vitest                                                                 | `2` files, `27` tests PASS                               |
| P4-03 Chromium Playwright trên isolated Vite                                                | `6/6` PASS trong `9.2s`                                  |
| Go full test + vet                                                                          | PASS toàn bộ `services/core-api/...`                     |
| Media integration build-tag compile                                                         | PASS; không chạy test khi thiếu explicit disposable gate |
| Format, generated OpenAPI check, lint, typecheck, build, Storybook, bundle/actions security | PASS                                                     |
| `git diff --check`                                                                          | PASS                                                     |

Sáu Playwright scenario bao phủ zero initial capture + keyboard/Axe, waiting listen-only không mint
credential, admitted attempt trước memory-only credential, permission denial, offline không health/
media probe, và forced-colors + reduced-motion + `320 CSS px` reflow. Unit stress chạy `20` explicit
device-switch cycles và xác nhận mọi track được dừng đúng một lần.

## 5. Neon disposable và LiveKit test-provider evidence

Ba nhóm URL/key được nạp trực tiếp từ ignored local env file trong cùng command. Raw test output bị
giữ kín; chỉ tên gate, action và elapsed time được in. Không giá trị secret nào được hiển thị.

| Gate                                                | Kết quả        |
| --------------------------------------------------- | -------------- |
| Exact ACL provision + ledger/role safety            | PASS `11.92s`  |
| Runtime exact ACL                                   | PASS `11.10s`  |
| Lifecycle/authority/concurrency/quota/privacy       | PASS `179.99s` |
| Join-attempt/credential/signed-webhook binding      | PASS `222.98s` |
| LiveKit provider create/token/connect/cleanup smoke | PASS `16.41s`  |
| Final read-only ledger                              | `30 false`     |

P4-02 owner preflight cũ không được chạy lại vì gate đó cố ý chỉ chấp nhận ledger đầu vào
`29 false` trước migration `000030`. P4-03 không có migration; exact ACL provision gate đã xác nhận
hai role tách biệt, cùng database, runtime-role safety và ledger `30 false` trước khi provision.

Lần chạy đầu của join-attempt integration phát hiện fixture timestamp rơi đúng ranh giới Unix
second sau hai bước `250ms`; runtime không sai. Fixture được đổi thành bước lệch `125ms`, thêm
boolean-only diagnostic, unit gate PASS và disposable rerun PASS. Không rollback được chạy.

## 6. Exact candidate, shared và live evidence

### 6.1 Exact candidate CI/security

- Exact candidate `e49a8cc38f464e3ec56655823bcbb1ee77cbc651` được review, secret-scan, commit và
  push trực tiếp lên `main`; `.env*.local` cùng `.tmp-gocache/` không nằm trong candidate.
- GitHub Verify `31330663644` PASS: Quality and integration, Browser E2E và local environment smoke
  đều xanh.
- GitHub Security `31330663663` PASS: secret scan, CodeQL Go/JavaScript-TypeScript, repository
  vulnerability scan và Core API container scan đều xanh; Dependency Review skip đúng vì đây là
  push event.
- CI giữ exact media ACL probe trên runtime role hẹp. Các test fixture cross-module dùng role CI
  riêng `NOSUPERUSER/NOCREATEDB/NOCREATEROLE/NOINHERIT/NOBYPASSRLS`, không có `DELETE`, DDL,
  ownership hoặc đường `SET ROLE` tới migration owner.

### 6.2 Shared staging ACL

- Shared ledger trước và sau gate đều giữ `30 false`.
- Exact media runtime ACL được reprovision idempotent và PASS trong khoảng `215s`; focused read-only
  ACL probe PASS trong khoảng `20s`.
- `media_admission_requests` và `media_participant_sessions` giữ allowlist exact; runtime không được
  `INSERT(joining_at)` và chỉ được `UPDATE(joining_at)` trong credential transition. Schema/PUBLIC,
  role membership, ownership và broad table privileges đều không mở rộng.
- Không chạy migration, rollback, fixture lifecycle/concurrency hoặc provider smoke trên shared.

### 6.3 Exact deploy và live acceptance

- Cloudflare Pages check `93288377456` PASS exact SHA; deployment detail
  `963d891f-653e-407b-924c-a9d8bbed4cd2` trỏ tới candidate trên.
- Render deployment `dep-d9sd4ne7bikc739bf7l0` được tạo bằng **Deploy a specific commit**, đạt
  `live` trên exact full SHA; không dùng native rollback.
- Direct Render và Pages proxy đạt `6/6` cho health/ready/status: HTTP `200` và
  `Cache-Control: no-store`.
- Anonymous GET MediaSpace và POST join-attempt qua direct/Pages đạt `4/4` HTTP `401`,
  `application/problem+json`, `no-store`, `no-referrer`, `nosniff`, safe `Vary`, request ID hiện diện,
  không `Set-Cookie` và body không lộ token/provider/device/tenant/room projection.
- Organization Admin trên workspace KMA xác nhận `classroom_media_rooms` và
  `instant_study_rooms` đều `Đang tắt`. Route prejoin của inaccessible MediaSpace hiển thị bounded
  unavailable alert + Retry, vẫn fail-closed sau retry, có zero video/audio element, URL không chứa
  credential và browser console sạch. Không temporary-enable capability.
- Transaction read-only trước và sau toàn bộ live probes giữ nguyên ledger `30 false` và cả bảy
  relation `media_spaces`, `media_room_instances`, `media_space_members`,
  `media_admission_requests`, `media_participant_sessions`, `media_space_mutation_receipts`,
  `media_provider_webhook_receipts` đều `0 -> 0`. Vì vậy acceptance không tạo join-attempt,
  credential, provider connection hoặc database side effect.

Actual join-flow live không được chạy vì hai deployment guardrail vẫn force-off. Bằng chứng flow thật
được lấy từ isolated Chromium E2E cùng disposable PostgreSQL/LiveKit test-provider gate; đây là ranh
giới có chủ đích, không phải gate bị bỏ qua.

## 7. Acceptance matrix cuối cùng

- [x] Initial capture bằng `0`; explicit keyboard action mới mở camera/mic.
- [x] Automated keyboard, Axe, landmark, forced-colors, reduced-motion và 200%-equivalent reflow PASS.
- [x] Permission/no-device/degraded state vẫn giữ listen-only khi policy cho phép.
- [x] Device label chỉ sau permission; credential không xuất hiện trong URL/history/storage/error.
- [x] Bounded error taxonomy và 20-cycle resource cleanup PASS.
- [x] Canonical attempt trước credential; waiting participant có zero provider credential/connection.
- [x] Current-role/lobby reauthorization, quota, tenant concealment và exact runtime ACL PASS trên
      PostgreSQL disposable.
- [x] Real LiveKit test-provider smoke và exact cleanup PASS.
- [x] Exact candidate commit/push và GitHub Verify/Security PASS.
- [x] Shared staging ACL reprovision/read-only gates, exact deploy và live acceptance PASS.

Physical microphone/camera/speaker diversity, Safari/Firefox, real network degradation/TURN matrix,
manual screen-reader matrix và join-load profile vẫn thuộc P4-11. Chúng không được suy PASS từ
Chromium synthetic automation của P4-03.

## 8. VERIFY -> DONE

1. [x] Review exact diff và secret scan; loại `.tmp-gocache/` cùng mọi local env/test artifact.
2. [x] Commit/push exact candidate và đạt GitHub Verify + Security.
3. [x] Giữ shared ledger `30 false`, reprovision exact ACL và chạy focused read-only gate; không
       migration/rollback/fixture.
4. [x] Deploy exact SHA lên Render/Cloudflare và đạt public + anonymous + authenticated live probes.
5. [x] Giữ capability force-off, dùng Chromium/disposable/provider cho actual join-flow và chứng minh
       live không side effect.
6. [x] Ghi đầy đủ evidence và chuyển P4-03 `VERIFY -> DONE`; P4-04 trở thành task runnable chính.

## 9. Quyết định trạng thái

Toàn bộ implementation, local/browser, disposable/provider, exact CI/security, shared ACL, exact
deploy và live feature-off/privacy/no-side-effect gate đã PASS. P4-03 chuyển
`TODO -> IN PROGRESS -> VERIFY -> DONE` ngày `2026-08-10`; task runnable chính tiếp theo là P4-04.
