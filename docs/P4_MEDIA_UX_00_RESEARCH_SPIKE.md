# P4-MEDIA-UX-00 — Classroom media UX research spike

> Decision spike cho **Phase 4 - Classroom Media MVP**. Task có thể chạy song song với
> P4-01/P4-02, nhưng phải `DONE` trước khi triển khai UX/signals/effects của
> P4-03/P4-04/P4-05/P4-06.
> Task không đổi media provider, không cài production dependency, không tạo migration/service
> hoặc deploy. Mọi dependency/effect mới chỉ được phép sau evidence và ADR/amendment được chấp nhận.

| Thuộc tính         | Giá trị                                                                       |
| ------------------ | ----------------------------------------------------------------------------- |
| Trạng thái         | `TODO`                                                                        |
| Phase              | Phase 4 - Classroom Media MVP                                                 |
| Dependency         | P4-00                                                                         |
| Có thể chạy        | Song song P4-01/P4-02                                                         |
| Phải hoàn thành    | Trước phần UX/signals/effects của P4-03/P4-04/P4-05/P4-06                    |
| Quyết định bị chặn | Prejoin/lobby, classroom layout, hand raise/reaction, effects và fallback     |
| Không bị chặn      | P4-01/P4-02 authority/schema/credential work và Phase 3 deferred carry-over   |
| Cập nhật           | 2026-08-09                                                                    |

## 1. Mục tiêu

Chọn phạm vi và cách triển khai prejoin/lobby/layout/signals/effects có bằng chứng cho lớp học
2-50 người,
thay vì sao chép UI đối thủ hoặc mang nguyên prototype V1 sang V2. Spike phải trả lời:

1. green room cần preview, mic meter, speaker test, device selector và recovery nào;
2. `None`, blur và curated static background có đủ an toàn/nhẹ cho P4 MVP hay không;
3. processor/capability/fallback nào dùng trước và khi nào phải tự tắt effect;
4. browser/WebRTC audio processing nào bật mặc định; Krisp có đáng làm tùy chọn sau hay không;
5. grid, active-speaker, focus/pin và presentation layout chuyển trạng thái thế nào ở mốc
   2/5/25/50 người mà không gây nhảy tile hoặc làm mất điều khiển;
6. vòng đời giơ tay, thứ tự hàng đợi, host lower/lower-all và resync nào là dễ hiểu, idempotent;
7. reaction palette, thời lượng, grouping, rate-limit và thông báo accessibility nào đủ biểu cảm
   nhưng không gây spam hoặc che nội dung;
8. privacy, CSP, asset license, accessibility và performance budget nào chặn rollout.

Authority của hand raise/reaction không phải câu hỏi mở: P4-06 dùng Core API đã xác thực và
rate-limit, `CanPublishData=false`. Spike chỉ quyết định interaction/projection/resync, không được
đổi client metadata hoặc provider DataChannel thành nguồn sự thật.

## 2. Nguồn benchmark sản phẩm

Chỉ rút pattern và acceptance criteria; không sao chép UI, icon, text hoặc asset của đối thủ.

- Google Meet: green-room self-check, device selection/test, background/effects, adaptive
  layouts, hand-raise queue, reactions, keyboard behavior, troubleshooting và accessibility.
  - [Connect video and audio](https://support.google.com/meet/answer/10409699?hl=en)
  - [Change backgrounds and effects](https://support.google.com/meet/answer/10058482)
  - [Troubleshoot video and audio quality](https://support.google.com/meet/answer/10620583)
  - [Change the meeting layout](https://support.google.com/meet/answer/10550593?hl=en)
  - [Raise or lower your hand](https://support.google.com/meet/answer/10159750?hl=en)
  - [Send reactions](https://support.google.com/meet/answer/13151720?hl=en)
  - [Keyboard shortcuts](https://support.google.com/meet/answer/9298571?hl=en)
  - [Use Google Meet with a screen reader](https://support.google.com/meet/answer/15738543?hl=en)
  - [Google Meet accessibility](https://support.google.com/accessibility/answer/16175468)
- Zoom: prejoin preview, camera test, background hardware requirements, waiting-room behavior
  cùng gallery/speaker/share layouts, hand raise, reaction policy và accessibility.
  - [Video preview before joining](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061118)
  - [Testing video](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061836)
  - [Virtual background requirements](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060007)
  - [Waiting-room patterns](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0057887)
  - [Meeting layouts](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063672)
  - [Screen-share layouts](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061332)
  - [Zoom Rooms display layouts](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0068420)
  - [Raise or lower a hand](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0068290)
  - [Non-verbal feedback and reactions](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063325)
  - [Meeting reaction policy](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0058404)
  - [Zoom accessibility FAQ](https://www.zoom.com/en/accessibility/faq/)

Zoom/Meet product là UX benchmark, không phải dependency/provider candidate. Zoom Video SDK,
Google Meet API/Add-ons/Media API và Jitsi không được thêm chỉ để có prejoin/effects vì sẽ trùng
hoặc làm lệch LiveKit provider boundary đã chốt ở ADR-0030.

## 3. Audit bắt buộc từ TutorHub V1

V1 chỉ đọc và chỉ dùng làm nguồn use case/test; không sao chép credential, endpoint hoặc code
authority cũ.

| Nguồn V1 | Có thể kế thừa | Không được tái sử dụng trực tiếp |
| -------- | -------------- | -------------------------------- |
| `src/main/resources/html/js/lobby-manager.js` | Preview, mic meter, speaker test, device choice, None/blur/4 nền và waiting-state cases | Global/inline DOM, localStorage mặc định, CDN MediaPipe, main-thread loop, client-owned admission |
| `src/main/resources/html/tldraw_board_v2.html` | Lobby/layout mock, screen-share, raise-hand và reaction control inventory | JCEF/monolithic markup, inline handler/style và CDN runtime |
| `src/main/resources/html/img/backgrounds/*` | Candidate curated backgrounds | Chưa được dùng nếu thiếu provenance/license rõ ràng |
| `src/main/resources/html/js/livekit-manager.js` / `media-device-manager.js` | Track/device/reconnect, active-speaker và signal failure inventory | URL/client metadata/DataChannel làm authority, token/raw-error logging hoặc global state |
| `src/main/resources/html/js/video-layout.js` | Dynamic grid, screen-share/focus và join/leave resize use cases | Không có pagination 25-50, active-speaker hysteresis, a11y/performance gate; DOM/global state gắn JCEF/Jitsi |
| `src/main/resources/html/js/roster.js` / `room-roster-manager.js` | Raised-hand queue, lower/lower-all, reaction badge/palette và roster state cases | Client clock/metadata/DataChannel làm quyền; actor/order/payload không do server xác thực |
| `PreJoinDialog.java` / `WaitingRoomDialog.java` | Label, cancel/join/waiting use cases | Swing/JCEF code, email/member exposure và smoke test không chứng minh ACL/concurrency/a11y |
| `docs/tutorhub_classroom_analysis.md` | Giả thuyết toolbar/layout và feature inventory | Claim/version/chi phí cũ chưa được nguồn chính thức hiện tại xác minh |

Kết quả audit phải tạo reuse/reject matrix có đường dẫn, lý do và test case tương ứng. Asset V1
chỉ được nhập V2 sau khi xác minh quyền sử dụng và quét metadata/nội dung phù hợp.

## 4. Candidate kỹ thuật bắt buộc so sánh

| Candidate | Vai trò | Gate |
| --------- | ------- | ---- |
| [Native browser media constraints](https://www.w3.org/TR/mediacapture-streams/) | Ưu tiên capability native cho audio và blur nếu thực sự được hỗ trợ | Feature-detect bằng API/runtime settings; luôn có fallback |
| [`@livekit/track-processors`](https://github.com/livekit/track-processors-js) | Candidate chính cho blur/static background, tích hợp LiveKit TrackProcessor | License/version, preview-to-room lifecycle, browser fallback, model/WASM và performance đạt |
| [MediaPipe Image Segmenter](https://developers.google.com/edge/mediapipe/solutions/vision/image_segmenter/web_js) | Benchmark/fallback kỹ thuật nếu wrapper LiveKit thiếu use case bắt buộc | Không tạo compositor/processor thứ hai nếu LiveKit candidate đã đạt; worker/main-thread cost rõ |
| [LiveKit Meet](https://github.com/livekit-examples/meet) | Reference cho controls/layout/autoplay/responsive behavior | Không fork nguyên app hoặc thêm làm production dependency |
| [LiveKit `GridLayout`](https://docs.livekit.io/reference/components/react/component/gridlayout/), [`FocusLayoutContainer`](https://docs.livekit.io/reference/components/react/component/focuslayoutcontainer/) và [`ParticipantTile`](https://docs.livekit.io/reference/components/react/component/participanttile/) | Primitive cho grid/focus rail/pagination và tile state trong TutorHub shell | Prototype 2/5/25/50, stable reorder, a11y và adaptive-subscription; không lấy prefab làm product authority |
| CSS Grid/container queries + bounded pagination | Candidate không thêm dependency cho responsive geometry và overflow | Có fixture quyết định, focus order ổn định và không virtualize track đang phát sai cách |
| Browser WebRTC audio processing | Mặc định cho echo cancellation, noise suppression và auto gain khi supported | Có music/original-sound escape hatch nếu xử lý làm hỏng nội dung |
| [LiveKit Krisp](https://docs.livekit.io/transport/media/noise-cancellation/) | Tùy chọn nâng cao sau feature flag/entitlement | Xác minh LiveKit Cloud support, browser, license/terms, runtime model và chi phí trước quyết định |

### 4.1 Local reference checkouts

- LiveKit Meet source: `https://github.com/livekit-examples/meet.git`; revision
  `665e1cb7841ab872de0d8e5c310744009a763b08` (shallow clone ngày 2026-08-09).
- LiveKit Meet path: `.tmp/research/livekit-meet`; `.tmp/` được Git ignore, không phải submodule hoặc
  production dependency.
- Track Processors source: `https://github.com/livekit/track-processors-js.git`; revision
  `9ef5191da7fb6d82e55876fa04d0e6048d49859b` (shallow clone ngày 2026-08-09), package
  `@livekit/track-processors@0.7.2`, khai báo `@mediapipe/tasks-vision@0.10.14`.
- Track Processors path: `.tmp/research/track-processors-js`; checkout có `LICENSE` Apache-2.0 và
  `NOTICE`, không phải production dependency.
- Cả hai checkout chưa chạy `pnpm install`, chưa tạo `.env`, chưa tải runtime/model bổ sung hoặc
  kết nối LiveKit Cloud; hiện chỉ phục vụ audit read-only.

Không ghép nhiều segmentation stack trong MVP. Beauty, makeup, avatar, sticker, AI-generated
background, video background và user-uploaded background là non-goal của spike trừ khi evidence
chứng minh chúng không làm tăng đáng kể privacy, moderation, asset và performance scope.

LiveKit active-speaker và media subscription chỉ là state media tức thời. Theo
[data-packet semantics](https://docs.livekit.io/transport/data/packets/), packet kể cả reliable vẫn
không được buffer cho client đang mất kết nối. Vì vậy signal nghiệp vụ không được chuyển sang
provider packet: Core API giữ authority/FIFO/rate-limit, UI nhận projection và bounded resync.

## 5. Evidence matrix bắt buộc

### 5.1 Green room và device recovery

- Camera preview chỉ bắt đầu sau hành động rõ ràng; không lộ device label trước permission.
- Mic activity meter, camera/mic selector và speaker test có success/error/unsupported state.
- Permission denied, device busy, unplug/change, no-device và autoplay failure có hướng dẫn/retry.
- Camera/effect lỗi không chặn Join; listen-only/audio-only hoạt động khi policy server cho phép.
- Preview track được dừng/chuyển đúng; processor preview không bị giả định tự đi theo room track.

### 5.2 Background/effect và hiệu năng

- Prototype cô lập cho `None`, blur và ít nhất ba curated static backgrounds.
- Đo 360p/540p/720p: FPS, processing time, CPU/GPU nếu đo được, memory, long task, bundle/model
  load và time-to-preview trên ít nhất một máy pilot yếu và một máy chuẩn.
- Bật/tắt/chuyển effect không flash kéo dài, leak track/canvas/worker hoặc làm hỏng reconnect.
- Degraded order mặc định: effect off -> 360p/15fps -> camera off/audio-only; người dùng được báo
  rõ và có thể retry khi điều kiện cải thiện.
- Unsupported/over-budget luôn fallback về raw/no-effect track; effect không phải join gate.

### 5.3 Browser, privacy, CSP và supply chain

- Feature-detect trên Chrome, Edge, Firefox và Safari; Windows/macOS là pilot matrix chính.
- Pin package/model/WASM; đánh giá self-host asset thay vì phụ thuộc CDN runtime mặc định.
- CSP, offline/provider-asset failure, cache/version rollback và bundle lazy-load có evidence.
- Camera frames không đi qua Core API; network audit không có media/token/device label/raw error.
- Xác minh license/NOTICE và telemetry/consent của processor, model, WASM và background assets.
- Không persist device ID/effect choice mặc định nếu privacy decision chưa được ghi rõ.

### 5.4 Audio và accessibility

- Kiểm tra `echoCancellation`, `noiseSuppression`, `autoGainControl` bằng supported constraints
  và actual track settings; không hứa cùng hành vi trên mọi browser/device.
- Music/original-sound case không bị suppression/AGC làm hỏng mà không có đường tắt rõ ràng.
- Keyboard-only, NVDA, visible focus, live-region/status, 200% zoom, forced colors và reduced
  motion đạt; trạng thái mic/camera/effect không chỉ biểu thị bằng màu.
- Không chiếm phím Space bắt buộc theo cách xung đột screen reader/push-to-talk.

### 5.5 Lobby boundary

- Waiting/admitted/denied/cancelled/meeting-ended/provider-unavailable/timeout states được mô tả.
- Host queue, admit/deny/retry UI chỉ là projection của P4-04 server-authorized admission state.
- Không mang lại V1 behavior kết nối participant vào provider room trước khi server admit.
- Background/effect state không ảnh hưởng authorization, capacity reservation hoặc credential.

### 5.6 Layout lớp học 2-50 người

- Chạy cùng fixture 2/5/25/50 cho `Grid`, `Active speaker` và `Presentation`; lưu screenshot/video,
  số tile, crop/overlap, pagination/rail và active-speaker visibility ở 1280x720, 320 CSS px và 200%.
- Screen share start/stop/cancel, presenter đổi/rời phòng, pin/unpin và speaker đổi liên tục không
  làm mất focus, controls hoặc lựa chọn layout của người dùng; có hysteresis/debounce chống nhảy tile.
- Join/leave/reconnect, track publish/unpublish và event duplicate/out-of-order cho kết quả layout
  xác định; focus order không bị đảo vô nghĩa khi tile được sắp lại.
- Profile 25/50 dùng bounded pagination/rail và adaptive subscription; không attach/render vô hạn.
  Đo CPU, memory, long task, received bitrate và subscribed-video count, rồi degrade theo thứ tự:
  ít tile -> pagination/rail -> video chất lượng thấp hơn -> audio-only, không làm rớt audio/control.
- Keyboard chuyển trang/layout/pin được, focus được phục hồi; screen reader biết participant/tile
  state mà không phải nghe active-speaker update liên tục; forced colors/reduced motion đạt.

### 5.7 Giơ tay và reaction

- Raise/lower idempotent; participant tự hạ, moderator hạ một người/hạ tất cả. FIFO dùng server
  accepted timestamp/sequence, không dùng đồng hồ client; duplicate/retry/reconnect/out-of-order
  phải hội tụ qua bounded snapshot/resync.
- Moderator và participant đều nhận feedback thành công/403/409/429/offline; thông báo chỉ dùng
  bounded display name, không chứa email, provider identity hoặc ParticipantSession ID.
- MVP dùng curated emoji allowlist và TTL tạm thời được ADR chốt; burst giống nhau được gom/collapse,
  per-actor + per-tenant/room-instance rate limit chạy cross-instance, payload lạ bị bỏ an toàn.
- Hand state tồn tại tới khi self/moderator/meeting lifecycle hạ; reaction không được lưu như chat,
  attendance, grade hoặc audit noise. Provider mất packet không được làm mất/giả authority.
- Keyboard-only có toggle rõ trạng thái; screen reader có announcement level để tránh spam, icon có
  accessible name, không color-only. Reduced motion bỏ animation nhưng vẫn giữ text/icon/tile badge.
- Chạy 25/50 participant hand/reaction storm; queue, roster, audio và classroom controls vẫn usable.

## 6. Cách thực hiện

1. Khóa decision questions và test fixture trước khi prototype.
2. Audit V1 read-only; ghi reuse/reject matrix, không chạy script/token fixture legacy.
3. Benchmark Zoom/Meet bằng tài liệu chính thức hiện hành và cùng tám flow: permission/device,
   effect fallback, waiting/admit/deny, grid/speaker/presentation, screen-share transition,
   hand queue/moderation, reaction burst/policy và keyboard/screen reader.
4. Tạo prototype cô lập dùng credential/test room riêng hoặc local mock; không nối route production.
5. Chạy cùng media fixture và performance matrix cho candidate; lưu command/version/kết quả số.
6. Viết threat/privacy/license review và decision matrix có trọng số.
7. Tạo ADR mới hoặc amendment ADR-0030 chọn scope, processor, asset hosting, fallback và rollout;
   nếu evidence không đạt, quyết định hợp lệ là ship P4 không effect.

## 7. Definition of Done

- [ ] Current official Zoom/Meet/LiveKit/browser sources và V1 reuse/reject matrix được lưu.
- [ ] Prototype `None`/blur/curated background chạy cô lập; không có production dependency/route.
- [ ] Chrome/Edge/Firefox/Safari và Windows/macOS capability/fallback matrix có evidence.
- [ ] 360p/540p/720p performance, low-end auto-disable và track/processor cleanup đạt hoặc có cap rõ.
- [ ] Permission/device/autoplay/offline/provider-asset/error recovery không chặn Join.
- [ ] WebRTC audio defaults, music/original-sound escape hatch và Krisp go/no-go được chốt.
- [ ] Grid/active-speaker/presentation đạt fixture 2/5/25/50, responsive/zoom, visual stability,
      adaptive-subscription và bounded pagination/degrade gate.
- [ ] Hand queue lifecycle/FIFO/resync và reaction allowlist/TTL/grouping/rate-limit đạt test 25/50.
- [ ] CSP/self-host/model/WASM, license/NOTICE, privacy/telemetry và V1 background provenance rõ.
- [ ] Layout, queue và reaction đạt keyboard/NVDA, 200% zoom, forced-colors, reduced-motion và
      bounded announcement gate.
- [ ] ADR/amendment `Accepted` chọn đúng một processor path, fallback/degrade order và P4 MVP scope.
- [ ] ADR/amendment cũng khóa layout modes, hand/reaction contract, server-authority boundary và
      rollout; không để research mở lại `CanPublishData=false`.
- [ ] P4-03/P4-04/P4-05/P4-06/P4-11 acceptance được cập nhật theo quyết định; nếu effect bị loại,
      UI vẫn có no-effect fallback và không tuyên bố feature chưa ship.

Cho tới khi checklist trên đạt, background/effects chỉ là candidate. `@livekit/track-processors`,
MediaPipe hoặc Krisp không được coi là production decision và không được thêm chỉ vì prototype chạy.
