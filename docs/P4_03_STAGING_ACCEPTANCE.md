# P4-03 — Prejoin device/network và join-attempt staging acceptance

## 1. Trạng thái và ranh giới

- Ngày cập nhật: `2026-08-09`.
- Trạng thái hiện tại: `VERIFY`.
- Implementation, local verification, Chromium E2E, Neon disposable và LiveKit test-provider smoke
  đã `PASS`.
- P4-03 không có migration mới. Ledger disposable cuối cùng là `30 false`; không rollback, không
  migrate shared staging và không deploy trong lượt này.
- Hai feature `classroom_media_rooms` và `instant_study_rooms` tiếp tục force-off theo rollout
  guardrail. `effect=None` tiếp tục là baseline; không thêm processor production dependency.
- Disposable branch được giữ lại cho tới khi exact candidate CI/security, shared acceptance và live
  acceptance hoàn tất.

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

| Gate | Kết quả |
| --- | --- |
| `pnpm verify` | PASS trong `215.2s` |
| Web Vitest | `57` files, `289` tests PASS |
| API client Vitest | `7` files, `47` tests PASS |
| Focused prejoin/room Vitest | `2` files, `27` tests PASS |
| P4-03 Chromium Playwright trên isolated Vite | `6/6` PASS trong `9.2s` |
| Go full test + vet | PASS toàn bộ `services/core-api/...` |
| Media integration build-tag compile | PASS; không chạy test khi thiếu explicit disposable gate |
| Format, generated OpenAPI check, lint, typecheck, build, Storybook, bundle/actions security | PASS |
| `git diff --check` | PASS |

Sáu Playwright scenario bao phủ zero initial capture + keyboard/Axe, waiting listen-only không mint
credential, admitted attempt trước memory-only credential, permission denial, offline không health/
media probe, và forced-colors + reduced-motion + `320 CSS px` reflow. Unit stress chạy `20` explicit
device-switch cycles và xác nhận mọi track được dừng đúng một lần.

## 5. Neon disposable và LiveKit test-provider evidence

Ba nhóm URL/key được nạp trực tiếp từ ignored local env file trong cùng command. Raw test output bị
giữ kín; chỉ tên gate, action và elapsed time được in. Không giá trị secret nào được hiển thị.

| Gate | Kết quả |
| --- | --- |
| Exact ACL provision + ledger/role safety | PASS `11.92s` |
| Runtime exact ACL | PASS `11.10s` |
| Lifecycle/authority/concurrency/quota/privacy | PASS `179.99s` |
| Join-attempt/credential/signed-webhook binding | PASS `222.98s` |
| LiveKit provider create/token/connect/cleanup smoke | PASS `16.41s` |
| Final read-only ledger | `30 false` |

P4-02 owner preflight cũ không được chạy lại vì gate đó cố ý chỉ chấp nhận ledger đầu vào
`29 false` trước migration `000030`. P4-03 không có migration; exact ACL provision gate đã xác nhận
hai role tách biệt, cùng database, runtime-role safety và ledger `30 false` trước khi provision.

Lần chạy đầu của join-attempt integration phát hiện fixture timestamp rơi đúng ranh giới Unix
second sau hai bước `250ms`; runtime không sai. Fixture được đổi thành bước lệch `125ms`, thêm
boolean-only diagnostic, unit gate PASS và disposable rerun PASS. Không rollback được chạy.

## 6. Acceptance matrix hiện tại

- [x] Initial capture bằng `0`; explicit keyboard action mới mở camera/mic.
- [x] Automated keyboard, Axe, landmark, forced-colors, reduced-motion và 200%-equivalent reflow PASS.
- [x] Permission/no-device/degraded state vẫn giữ listen-only khi policy cho phép.
- [x] Device label chỉ sau permission; credential không xuất hiện trong URL/history/storage/error.
- [x] Bounded error taxonomy và 20-cycle resource cleanup PASS.
- [x] Canonical attempt trước credential; waiting participant có zero provider credential/connection.
- [x] Current-role/lobby reauthorization, quota, tenant concealment và exact runtime ACL PASS trên
      PostgreSQL disposable.
- [x] Real LiveKit test-provider smoke và exact cleanup PASS.
- [ ] Exact candidate commit/push và GitHub Verify/Security PASS.
- [ ] Shared staging ACL reprovision/read-only gates, exact deploy và live acceptance PASS.

Physical microphone/camera/speaker diversity, Safari/Firefox, real network degradation/TURN matrix,
manual screen-reader matrix và join-load profile vẫn thuộc P4-11. Chúng không được suy PASS từ
Chromium synthetic automation của P4-03.

## 7. VERIFY -> DONE

1. Review exact diff và secret scan; loại `.tmp-gocache/` cùng mọi local env/test artifact.
2. Commit/push exact candidate lên `main`; chờ GitHub Verify và Security cùng PASS.
3. Sau khi được owner cho phép, giữ shared ledger `30 false`, reprovision exact media runtime ACL
   để loại `joining_at` khỏi `INSERT`, rồi chạy read-only/focused shared database gates. Không forward
   migration và không rollback.
4. Deploy exact candidate lên Render/Cloudflare; xác nhận exact SHA, health/readiness, feature-off
   guardrail, anonymous privacy và authenticated prejoin route behavior.
5. Chỉ temporary-enable tenant capability nếu có approval rollout riêng; nếu không, actual join-flow
   evidence tiếp tục lấy từ isolated Chromium E2E + disposable/provider gate và live phải chứng minh
   force-off không side effect.
6. Khi toàn bộ evidence trên được ghi lại, chuyển P4-03 `VERIFY -> DONE`; sau đó P4-04 mới là task
   runnable chính.

## 8. Quyết định trạng thái

P4-03 đạt implementation, local, browser automation, disposable database và provider gates nên được
chuyển `TODO -> IN PROGRESS -> VERIFY`. Chưa có exact candidate CI/security, shared staging hoặc live
deployment trong lượt này, vì vậy chưa được đánh dấu `DONE`.
