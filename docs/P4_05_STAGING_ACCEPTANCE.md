# P4-05 — Classroom shell, media controls và layouts staging acceptance

## 1. Trạng thái và ranh giới

- Trạng thái hiện tại: `IN PROGRESS`.
- Tài liệu này là runbook/evidence ledger cho P4-05; không checklist nào bên dưới được coi là
  `PASS` nếu chưa có exact command, candidate SHA hoặc browser/provider evidence tương ứng.
- P4-05 dùng canonical MediaSpace/RoomInstance flow của P4-03/P4-04. Legacy class media-token và
  deterministic provider room không phải authority của task.
- Theo scope hiện tại, P4-05 không cần OpenAPI, backend, PostgreSQL relation, runtime ACL hoặc
  migration mới. Disposable và shared phải giữ ledger mục tiêu `31 false`; không chạy
  `db:migrate`, rollback hoặc ACL provisioning chỉ để nghiệm thu UI.
- `classroom_media_rooms` và `instant_study_rooms` tiếp tục deployment-force-off.
  `effect=None` là production baseline và `CanPublishData=false` không thay đổi.
- Actual 25/50 participant provider load, browser/hardware thật, manual NVDA/VoiceOver, low-end/
  thermal soak và provider-outage matrix thuộc P4-11. P4-05 vẫn phải đạt fixture layout 2/5/25/50,
  automated accessibility và isolated LiveKit control/cleanup gates; không được suy các manual gate
  P4-11 là `PASS`.
- Không temporary-enable shared/live capability để tạo demo. Actual media acceptance dùng isolated
  fixture và LiveKit test project được phép.

Authority:

- [Phase 4 backlog](PHASE_4_BACKLOG.md), mục P4-05;
- [ADR-0030](adr/0030-authoritative-classroom-media-spaces-lifecycle-and-livekit-grants.md);
- [ADR-0031](adr/0031-classroom-media-ux-devices-layout-effects-and-signals.md);
- [P4-MEDIA-UX-00 research report](P4_MEDIA_UX_00_RESEARCH_REPORT.md).

## 2. Candidate phải triển khai

### 2.1 Canonical classroom shell và lazy boundary

- Canonical route là
  `/app/media/spaces/{space_id}/instances/{room_instance_id}/room`, dùng one-time in-memory handoff
  của exact tenant/user/MediaSpace/RoomInstance.
- Tách production room module khỏi prejoin để route không dùng classroom room, kể cả canonical
  prejoin, không tải LiveKit client/components bundle. Không fork LiveKit Meet, không nhập nguyên
  prefab `VideoConference` và không dùng provider metadata/role làm business authority.
- TutorHub sở hữu stage, toolbar, media rail, responsive drawer, layout state machine, focus
  restoration, capability projection, degraded mode, style và accessibility semantics.
- TutorHub-owned `Room`/`RoomContext` được tạo một lần sau committed StrictMode lifecycle cho exact
  RoomInstance; đổi layout không remount room hoặc mint/reissue credential. Không dùng prefab
  `LiveKitRoom` có unmount cleanup ngoài quyền sở hữu của TutorHub.
- Lobby panel P4-04 vẫn hoạt động cho actor có quyền và không làm thay đổi layout/media authority.

### 2.2 Deterministic layout controller

Ba mode user-visible chỉ tồn tại trong memory của exact RoomInstance:

| Mode             | Contract P4-05                                                                                                                       |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `grid`           | Canonical order ổn định; join append, leave compact khi cần; không reorder theo active speaker/hand; page action và page count rõ    |
| `active_speaker` | Một main stage + rail tối đa 6; local pin thắng auto speaker; speaker event không đổi DOM/focus order                                |
| `presentation`   | Một share stage + rail tối đa 6; share stop/cancel/replace, presenter leave hoặc track end restore deterministic mode/page/pin/focus |

Capacity ban đầu:

- desktop 1280x720: tối đa 12 camera tile/page;
- medium: tối đa 6 camera tile/page;
- compact/320 CSS px/200% reflow: tối đa 4 camera tile/page;
- hard bound grid desktop lớn: không attach hơn 16 camera video element;
- active-speaker/presentation rail: tối đa 6 participant.

Grid không đổi thứ tự theo audio activity. Active speaker dùng initial tunable constants với fake
clock: candidate dominant `800 ms`, minimum stage hold `2500 ms`, silence release `1500 ms`. Nếu pin
target rời/unpublish thì clear pin và chọn fallback xác định. Khi page biến mất, clamp về page hợp lệ
gần nhất và phục hồi focus tới page control hoặc tile còn tồn tại.

Degrade order:

1. giảm video visible/subscribed trong page;
2. dùng bounded page/rail và hạ remote video quality;
3. dừng remote camera video ngoài stage nhưng giữ audio/lobby/control;
4. audio-only nếu video vẫn không ổn định.

Presentation share được ưu tiên hơn camera video nhưng không hơn audio/control. Fixture 25/50 không
được biến thành tuyên bố real provider capacity; P4-11 mới công bố profile thực đo.

### 2.3 Exact grants và media controls

- Camera/microphone chỉ được render/enable và gọi SDK khi
  `can_publish_camera_microphone=true`.
- Screen share chỉ được render/enable và gọi SDK khi `can_share_screen=true`; không suy quyền share
  từ camera/microphone grant hoặc display role.
- Subscribe-only actor vẫn vào được listen-only khi `can_subscribe=true`; hidden/disabled UI không
  thay provider-token/direct-server deny test.
- Camera, microphone, screen share và device switch là explicit user actions. Screen capture luôn
  mở browser picker mới; không persist source/grant.
- Toolbar dùng TutorHub semantics; nút Leave tách khỏi media toggles và có confirm khi room active.
- Autoplay failure không làm mất shell; có action bật classroom audio rõ ràng và keyboard-accessible.
- Device switch không tạo hai owned local track kéo dài; device biến mất phải fallback bounded và
  không tự bật lại camera/microphone ngoài consent hiện tại.

### 2.4 Effect, privacy và cleanup

- P4-05 ship `None` only. Không thêm `@livekit/track-processors`, MediaPipe stack, model/WASM,
  background asset, processor worker hoặc remote CDN/telemetry endpoint vào production bundle.
- Không thêm `classroom_media_effects` vào public tenant catalog hoặc tạo migration chỉ để biểu diễn
  một subfeature chưa được phép rollout. Nếu scope này thay đổi, phải dừng và review ADR/migration/
  privacy/supply-chain trước.
- Credential, provider room/participant identity, device ID/label, layout, pin, effect và audio mode
  chỉ ở component/tab memory cần thiết cho exact RoomInstance. Không ghi chúng vào URL, history,
  `localStorage`, `sessionStorage`, IndexedDB, analytics hoặc error report.
- Không render/log raw token, SDP/ICE, IP, provider error/stack, media frame, device label hoặc
  participant email/provider identity.
- Leave, unmount, logout, workspace/principal/source/RoomInstance change, terminal disconnect,
  failed switch và aborted async generation phải idempotently:
  - stop/unpublish toàn bộ owned local camera/microphone/screen track;
  - detach video/audio element và stop obsolete subscription;
  - clear timer/fake-clock generation, room/device listener và pending async callback;
  - disconnect room đúng một lần và purge one-time in-memory handoff;
  - tắt browser device indicator sau khi không còn owned capture.

### 2.5 Accessibility và performance contract

- Toolbar theo WAI-ARIA toolbar pattern: Tab chuyển logical region; arrow/Home/End di chuyển trong
  toolbar; mọi action có button thật, accessible name và pressed/state text; không dùng color-only.
- Không chiếm global bare Space. Dialog/drawer/layout/page/pin/share transition phục hồi focus; active
  speaker không cướp focus hoặc tạo continuous screen-reader announcement.
- 320 CSS px và 200% zoom vẫn dùng được Join/Leave/mic/camera/layout/lobby; không horizontal control
  trap. Forced colors giữ border/text state; reduced motion bỏ non-essential transition.
- Automated Axe, keyboard, focus và semantic screen-reader contract là gate P4-05. Manual NVDA/
  VoiceOver vẫn được ghi `UNVERIFIED — P4-11`, không bị automated Axe thay thế.
- Giữ global performance targets: app-shell LCP dưới `2.5s`, INP dưới `200ms`, classroom route lazy
  riêng và route không dùng media SDK không tải LiveKit. Trước `VERIFY` phải ghi exact production
  chunk/gzip sizes và chốt numeric classroom-room chunk budget; không suy budget từ research sample.
- Browser fixture phải chứng minh video attach/subscription bounded theo page/stage/rail. Audio và
  control phải sống lâu hơn video trong degraded state.

## 3. Local verification — `AUTOMATED PASS`, commit/CI còn mở

- [x] Layout controller unit tests bao phủ Grid/Active speaker/Presentation với profile 2/5/25/50,
      join/leave, duplicate/out-of-order projection, page clamp và stable canonical order.
- [x] Fake-clock tests xác nhận `800/2500/1500 ms`, local pin precedence, deterministic fallback và
      speaker event không reorder Grid/DOM.
- [x] Presentation tests xác nhận share start/stop/cancel/replace, track end và presenter leave
      restore exact valid mode/page/pin/focus; cleanup chỉ chạy một lần.
- [x] Exact grant matrix bao phủ subscribe-only, camera/microphone-only, share denied và full allowed;
      denied control không gọi LiveKit publish/screen-share/device APIs.
- [x] Focused Go tests xác nhận canonical join credential/provider JWT vẫn tách camera/microphone và
      screen-share grants, room-scoped, TTL-bounded và `CanPublishData=false`.
- [x] Automated leave/unmount/error/scope-change tests xác nhận disconnect once, listener/timer/
      pending generation bị loại, late callback không đổi scope mới và in-memory credential bị purge;
      actual track/device-indicator gate vẫn nằm ở isolated LiveKit mục 4.
- [x] Privacy regression xác nhận token/provider identity/device ID/layout/pin không xuất hiện trong
      URL, history, storage, console, analytics payload hoặc production bundle; device label chỉ hiện
      trong explicit authorized device drawer.
- [x] Production build chứng minh canonical prejoin/non-room routes không static-import LiveKit;
      exact chunk/raw/gzip sizes và numeric room-route budget được lưu vào evidence.
- [x] P4-05 Playwright chạy 2/5/25/50 fixture ở 1280x720, 320 CSS px và 200%; không overlap,
      horizontal overflow hoặc video/subscription vượt bound.
- [x] Playwright keyboard/Axe/focus restore/forced-colors/reduced-motion/autoplay/listen-only gates
      đều PASS; toolbar và Leave luôn usable.
- [x] P4-03 explicit probe/memory-only handoff và P4-04 waiting/lobby/invite E2E regression đều PASS
      trên Vite-only Chromium fixture; cùng P4-05 classroom shell đạt tổng `16/16`.
- [x] Bundle/network audit của no-effect candidate không có jsDelivr, Google model storage, processor
      telemetry hoặc media frame tới Core API.
- [x] `pnpm verify`, full web `62` files/`376` tests, web build và client bundle security đều PASS
      trên local tree.
- [ ] Full committed Playwright regression suite và exact candidate CI/security đều PASS.

## 4. Disposable và LiveKit test project — `AUTOMATED PASS`, không đổi database

P4-05 không có schema/ACL change. Nếu dùng Neon disposable để giữ evidence, chỉ được chạy read-only
identity/ledger/feature/ACL checks. URL/credential được nạp từ ignored local env file trong cùng
command, không in giá trị; structural output chỉ là boolean/status/count an toàn.

- [x] Xác nhận disposable endpoint khác shared mà không in hostname/URL; direct/pooled URL cùng exact
      disposable database và dùng owner/runtime role tách biệt.
- [x] Read-only probe xác nhận ledger `31 false`, `dirty=false`, hai media feature effective false và
      runtime role không có broad table/DDL/schema-create/migration-role privilege. Disposable vẫn
      giữ `53` historic enabled override rows; deployment guard tiếp tục force-off nên không xóa/sửa row.
- [x] Không chạy `db:migrate`, rollback, ACL provision, mutation fixture hoặc data seed trên
      disposable cho P4-05.
- [x] Isolated LiveKit test project chạy actual two-participant matrix: subscribe-only,
      camera/microphone allowed, share denied và share allowed theo exact signed grants.
- [x] Actual camera/microphone/screen actions không remote-unmute, không mở DataChannel và không
      publish ngoài grant; screen picker là explicit action.
- [x] Chromium fake-device gate xác nhận Leave làm toàn bộ camera/microphone/screen capture track
      chuyển `ended`, hai participant disconnect, token không vào URL/DOM/storage và exact room bị
      xóa với count về `0`; không log provider secret.
- [ ] Physical device indicator, browser/hardware thật và provider-outage cleanup vẫn
      `UNVERIFIED — P4-11`; không suy PASS từ Chromium fake-device gate.
- [x] Layout 2/5/25/50 chỉ dùng deterministic fixture ở P4-05. Real 25/50 LiveKit subscription/load
      được ghi `UNVERIFIED — P4-11`.
- [x] Disposable branch/test project được giữ cho tới khi database/provider gate liên quan được báo
      cáo; không rollback hoặc xóa tự động.

Evidence ngày 2026-08-11, toàn bộ command nạp credential từ ignored local file trong cùng process và
chỉ xuất status/count không bí mật:

- `TestPostgresP405DisposableReadOnlySnapshot` và
  `TestPostgresMediaLifecycleRuntimeExactACLDisposableReadOnly`: PASS, ledger `31 false`, exact ACL;
- `TestLiveKitP405TwoParticipantGrantMatrix`: `2/2 PASS` trong `60.38 s`, denied source/data không tới
  subscriber và mỗi exact room được cleanup về `0`;
- `p4-05-livekit-provider.spec.ts`: `1/1 PASS`; publisher và subscribe-only subscriber đều mount real
  `ClassroomLiveKitRoom`. Subscriber nhận remote camera video + audio, chỉ nhận screen share sau
  explicit canvas-screen action; Leave kết thúc mọi capture track, gỡ remote media và Go cleanup room PASS;
- disposable/shared endpoint chỉ được so sánh offline theo boolean và xác nhận khác nhau; không có
  shared network connection trong gate này.

## 5. Exact candidate CI/security — `PENDING`

- [x] Review diff chỉ chứa P4-05 source/test/docs cần thiết; loại `.env*.local`, token, artifact,
      browser profile, screenshot riêng tư và `.tmp-gocache/`.
- [x] Local full verification chạy lại trên exact tree trước commit.
- [ ] Commit/push exact candidate lên `main` không force-push; ghi full SHA vào mục Evidence.
- [ ] GitHub Verify PASS: quality/integration, committed Browser E2E và local-environment smoke.
- [ ] GitHub Security PASS: secret scan, CodeQL Go/JavaScript-TypeScript, repository/dependency scan
      và container scan theo workflow hiện hành.
- [ ] CI test output/artifact không chứa credential, provider room/participant identity, device label
      hoặc private media content.
- [ ] Shared/deploy gate không bắt đầu trước exact candidate Verify/Security PASS.

## 6. Shared staging read-only — `PENDING`

Chỉ bắt đầu sau exact candidate CI/security PASS và quyền shared staging riêng. P4-05 không có
forward migration hoặc ACL delta.

- [ ] Secret-safe endpoint comparison xác nhận shared khác disposable; không in URL/credential.
- [ ] Read-only before-live snapshot xác nhận ledger `31 false`, `dirty=false`, hai media feature
      false, không có enabled media override và runtime ACL vẫn exact.
- [ ] Không chạy migration, rollback, ACL provision, mutation/concurrency fixture, provider smoke
      hoặc temporary feature override trên shared.
- [ ] Không tạo MediaSpace/RoomInstance/ParticipantSession/audit/outbox row để nghiệm thu shell.
- [ ] Read-only after-live snapshot giống before-live snapshot trên ledger, feature state và bounded
      aggregate counts.

## 7. Exact deploy và live acceptance — `PENDING`

- [ ] Cloudflare Pages deploy exact accepted candidate SHA sau CI/shared read-only gate; deployment
      status và URL được ghi nhưng không chứa credential.
- [ ] Vì P4-05 dự kiến không đổi Core API, ghi rõ Render giữ compatible backend SHA hay được
      owner-authorized exact-SHA redeploy; không tuyên bố Render chạy candidate nếu không có evidence.
- [ ] Render và Pages health/ready/status endpoints đều `200`, semantic status đúng, no-store và có
      request ID theo baseline.
- [ ] Anonymous matrix trên canonical MediaSpace/prejoin/join paths trả typed `401`/concealment,
      no-store/no-referrer/nosniff, không Set-Cookie ngoài auth flow và không lộ resource/provider ID.
- [ ] `/app/media/*` giữ no-store, no-referrer, frame denial và `Permissions-Policy` chỉ cho camera,
      microphone, display-capture, speaker-selection từ `self`.
- [ ] Authenticated Organization Admin xác nhận `classroom_media_rooms` và
      `instant_study_rooms` vẫn off; inaccessible synthetic route fail closed và không capture/
      connect provider.
- [ ] No-effect production network audit không có processor/model/WASM/background/CDN/telemetry
      request; route không dùng media không tải LiveKit SDK.
- [ ] Actual shell/control/layout acceptance lấy từ committed isolated Playwright + allowed LiveKit
      test project; không temporary-enable shared/live capability.
- [ ] Shared read-only after-live snapshot bằng before-live; live probe không tạo database/provider
      side effect.
- [ ] Browser console không có uncaught error, token/provider identifier, device label hoặc raw
      exception.

## 8. P4-11 manual/physical deferrals

Các mục sau phải được giữ rõ `UNVERIFIED — P4-11`; chúng không chặn P4-05 no-effect classroom shell
nếu toàn bộ automated/isolated gate của tài liệu này xanh, nhưng không được ghi là đã PASS:

- manual NVDA/Chrome Windows và VoiceOver/Safari macOS;
- exact Safari/macOS, Firefox và declared Chrome/Edge versions trên thiết bị thật;
- máy standard/low-end thật ở 360p/540p/720p, thermal/long-session và device busy/unplug/change;
- real 25/50 participant provider subscription/load, join storm và provider quota approval;
- provider outage/TURN/reconnect long-loss matrix ngoài P4-05 terminal cleanup contract;
- optional blur/static-background model/WASM/license/provenance/hash/CSP/privacy/performance;
- five-minute/ten-cycle effect soak và memory/resource cleanup thresholds của optional processor.

Nếu bất kỳ future effect gate nào không đạt, Phase 4 tiếp tục ship `None`; không hạ shell/layout/
grant/cleanup/accessibility gate để giữ effect.

## 9. Evidence ledger

| Evidence                                            | Trạng thái/kết quả |
| --------------------------------------------------- | ------------------ |
| Local full verify                                   | `PASS`             |
| Focused layout/grant/cleanup/privacy tests          | `76/76 PASS`       |
| Full web tests                                      | `62 files / 376 tests PASS` |
| P4-05 Playwright 2/5/25/50 + a11y                   | `7/7 PASS`         |
| Room application chunk raw/gzip; budget             | `38.02/11.93 kB; <=45/15 PASS` |
| LiveKit vendor + CSS raw/gzip                       | `604.88/161.73 kB` |
| Total room increment raw/gzip; budget               | `642.90/173.66 kB; <=700/190 PASS` |
| App entry/prejoin LiveKit static-import guard       | `PASS`             |
| Exact local security suite                          | `24/24 PASS`       |
| P4-03/P4-04/P4-05 Vite-only fixture regression      | `16/16 PASS`       |
| Browser LiveKit real publisher/subscriber delivery gate | `1/1 PASS`      |
| Candidate diff/secret-marker audit                  | `28 files PASS`    |
| Disposable read-only ledger/ACL probe               | `31 false; exact ACL PASS` |
| Isolated LiveKit test-project grant/control/cleanup | `Go 2/2 + real publisher/subscriber browser 1/1 + cleanup PASS` |
| Exact candidate SHA                                 | `PENDING`          |
| GitHub Verify run                                   | `PENDING`          |
| GitHub Security run                                 | `PENDING`          |
| Shared before/after read-only snapshots             | `PENDING`          |
| Cloudflare Pages exact deployment                   | `PENDING`          |
| Render compatibility/exact deployment decision      | `PENDING`          |
| Live health/privacy/feature-off/no-side-effect      | `PENDING`          |

## 10. Chuyển trạng thái

P4-05 chỉ được chuyển `IN PROGRESS -> VERIFY` khi implementation, focused unit/security tests, full
local verify, committed Playwright fixture matrix, isolated provider/cleanup evidence và
disposable-no-DB read-only boundary đều xanh trên cùng candidate tree.

P4-05 chỉ được chuyển `VERIFY -> DONE` khi:

1. [ ] exact candidate đã commit/push và GitHub Verify/Security PASS;
2. [ ] shared read-only `31 false`/feature-off snapshots trước và sau live giống nhau, không có
       migration/rollback/ACL/mutation;
3. [ ] exact deploy và live health/privacy/header/authenticated feature-off acceptance PASS;
4. [ ] actual grant/control/cleanup evidence đến từ isolated LiveKit test project, không shared/live
       temporary activation;
5. [ ] performance/lazy-boundary và automated accessibility evidence đầy đủ;
6. [ ] các manual/physical/load/effect gate P4-11 vẫn được ghi rõ `UNVERIFIED`, không suy PASS;
7. [ ] Project State, Phase 4 backlog, coordination, roadmap, database/security state và README được
       đồng bộ; P4-06 trở thành task runnable tiếp theo.

Local automated implementation/performance/accessibility, disposable read-only và isolated LiveKit
actual grant/control/cleanup checkpoint đã PASS. Browser fixture/harness chưa được commit; exact
candidate CI/security và shared/deploy/live gates chưa được xác nhận, nên trạng thái hợp lệ của P4-05
vẫn là `IN PROGRESS`.
