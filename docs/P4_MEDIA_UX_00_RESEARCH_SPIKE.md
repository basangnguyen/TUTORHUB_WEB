# P4-MEDIA-UX-00 — Prejoin, lobby và media-effects research spike

> Decision spike cho **Phase 4 - Classroom Media MVP**. Task có thể chạy song song với
> P4-01/P4-02, nhưng phải `DONE` trước khi triển khai UX/effects của P4-03/P4-04/P4-05.
> Task không đổi media provider, không cài production dependency, không tạo migration/service
> hoặc deploy. Mọi dependency/effect mới chỉ được phép sau evidence và ADR/amendment được chấp nhận.

| Thuộc tính         | Giá trị                                                                       |
| ------------------ | ----------------------------------------------------------------------------- |
| Trạng thái         | `TODO`                                                                        |
| Phase              | Phase 4 - Classroom Media MVP                                                 |
| Dependency         | P4-00                                                                         |
| Có thể chạy        | Song song P4-01/P4-02                                                         |
| Phải hoàn thành    | Trước phần UX/effects của P4-03/P4-04/P4-05                                  |
| Quyết định bị chặn | Prejoin/lobby UX, virtual background, audio processing và degraded fallback  |
| Không bị chặn      | P4-01/P4-02 authority/schema/credential work và Phase 3 deferred carry-over   |
| Cập nhật           | 2026-08-09                                                                    |

## 1. Mục tiêu

Chọn phạm vi và cách triển khai prejoin/lobby/effects có bằng chứng cho lớp học 2-50 người,
thay vì sao chép UI đối thủ hoặc mang nguyên prototype V1 sang V2. Spike phải trả lời:

1. green room cần preview, mic meter, speaker test, device selector và recovery nào;
2. `None`, blur và curated static background có đủ an toàn/nhẹ cho P4 MVP hay không;
3. processor/capability/fallback nào dùng trước và khi nào phải tự tắt effect;
4. browser/WebRTC audio processing nào bật mặc định; Krisp có đáng làm tùy chọn sau hay không;
5. privacy, CSP, asset license, accessibility và performance budget nào chặn rollout.

## 2. Nguồn benchmark sản phẩm

Chỉ rút pattern và acceptance criteria; không sao chép UI, icon, text hoặc asset của đối thủ.

- Google Meet: green-room self-check, device selection/test, background/effects,
  troubleshooting và accessibility.
  - [Connect video and audio](https://support.google.com/meet/answer/10409699?hl=en)
  - [Change backgrounds and effects](https://support.google.com/meet/answer/10058482)
  - [Troubleshoot video and audio quality](https://support.google.com/meet/answer/10620583)
  - [Google Meet accessibility](https://support.google.com/accessibility/answer/16175468)
- Zoom: prejoin preview, camera test, background hardware requirements, waiting-room behavior
  và accessibility.
  - [Video preview before joining](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061118)
  - [Testing video](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061836)
  - [Virtual background requirements](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060007)
  - [Waiting-room patterns](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0057887)
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
| `src/main/resources/html/tldraw_board_v2.html` | Lobby layout/mock và danh sách control | JCEF/monolithic markup, inline handler/style và CDN runtime |
| `src/main/resources/html/img/backgrounds/*` | Candidate curated backgrounds | Chưa được dùng nếu thiếu provenance/license rõ ràng |
| `livekit-manager.js` / `media-device-manager.js` | Track/device/reconnect failure inventory | URL/client metadata/DataChannel làm authority, token/raw-error logging hoặc global state |
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
| Browser WebRTC audio processing | Mặc định cho echo cancellation, noise suppression và auto gain khi supported | Có music/original-sound escape hatch nếu xử lý làm hỏng nội dung |
| [LiveKit Krisp](https://docs.livekit.io/transport/media/noise-cancellation/) | Tùy chọn nâng cao sau feature flag/entitlement | Xác minh LiveKit Cloud support, browser, license/terms, runtime model và chi phí trước quyết định |

Không ghép nhiều segmentation stack trong MVP. Beauty, makeup, avatar, sticker, AI-generated
background, video background và user-uploaded background là non-goal của spike trừ khi evidence
chứng minh chúng không làm tăng đáng kể privacy, moderation, asset và performance scope.

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

## 6. Cách thực hiện

1. Khóa decision questions và test fixture trước khi prototype.
2. Audit V1 read-only; ghi reuse/reject matrix, không chạy script/token fixture legacy.
3. Benchmark Zoom/Meet bằng tài liệu chính thức hiện hành và cùng năm flow: permission, device
   selection/test, effect unsupported/overload, waiting/admit/deny, keyboard/NVDA.
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
- [ ] CSP/self-host/model/WASM, license/NOTICE, privacy/telemetry và V1 background provenance rõ.
- [ ] Keyboard, NVDA, 200% zoom, forced-colors và reduced-motion gate có evidence.
- [ ] ADR/amendment `Accepted` chọn đúng một processor path, fallback/degrade order và P4 MVP scope.
- [ ] P4-03/P4-04/P4-05/P4-11 acceptance được cập nhật theo quyết định; nếu effect bị loại, UI vẫn
      có no-effect fallback và không tuyên bố feature chưa ship.

Cho tới khi checklist trên đạt, background/effects chỉ là candidate. `@livekit/track-processors`,
MediaPipe hoặc Krisp không được coi là production decision và không được thêm chỉ vì prototype chạy.
