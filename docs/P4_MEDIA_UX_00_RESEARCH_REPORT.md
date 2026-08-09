# P4-MEDIA-UX-00 — Báo cáo nghiên cứu Classroom Media UX

- Ngày chốt nghiên cứu: 2026-08-09
- Phạm vi: prejoin, lobby, device/audio, layout 2-50 người, screen share, giơ tay,
  reaction, background/effect, degraded mode và accessibility
- Authority nền: ADR-0030; LiveKit chỉ là media transport, Core API/PostgreSQL giữ quyền
- Tài liệu quyết định: ADR-0031
- Mức bằng chứng: tài liệu chính thức hiện hành, audit source V1/V2, source checkout được pin,
  prototype cô lập và automated browser probe trên máy Windows hiện tại

## 1. Kết luận điều hành

P4 có đủ quyết định để triển khai P4-03 đến P4-06 mà không phải sao chép Zoom/Meet hoặc port
runtime V1. Các quyết định chính:

1. Prejoin không capture camera/mic khi mới render. Mỗi probe cần hành động rõ ràng; lỗi camera,
   effect hoặc speaker test không được chặn Join nếu server policy vẫn cho audio-only/listen-only.
2. Tách `PREJOIN` khỏi `WAITING`. Participant chưa được Core API admit thì chưa nhận LiveKit
   credential và chưa kết nối provider.
3. Giữ LiveKit Components/client đã pin làm primitive. TutorHub tự sở hữu shell, state machine,
   stable ordering, pagination, focus restore và authorization projection; không fork LiveKit Meet.
4. P4 MVP có ba mode layout: `Grid`, `Active speaker`, `Presentation`. DOM/focus order ổn định;
   speaker hoặc raised hand không liên tục đảo tile.
5. Hand raise là state authoritative theo server sequence, giữ tới self/moderator/lifecycle lower.
   Reaction là ephemeral, allowlist sáu emoji, TTL 10 giây, grouping và rate-limit ở Core API.
   `CanPublishData=false` không được mở lại.
6. Audio giọng nói dùng WebRTC echo cancellation/noise suppression/auto gain dạng `ideal`, rồi
   đọc `getSettings()` để nói đúng trạng thái. Có `Original sound/music` tắt ba xử lý khi browser
   hỗ trợ; không thêm Krisp trong P4 MVP.
7. Production baseline là `None`. `@livekit/track-processors@0.7.2` chỉ là candidate duy nhất cho
   blur/nền tĩnh và giữ feature off cho tới khi self-host immutable WASM/model, xử lý MediaPipe
   metrics/consent, đạt browser/device/low-end budgets và không có outbound chưa phê duyệt.
8. Không tích hợp MediaPipe trực tiếp thành processor thứ hai; không dùng native blur như contract.
   Native blur chỉ có thể là progressive enhancement sau feature detection.
9. Degrade luôn theo thứ tự: tắt effect -> 360p/15fps -> tắt camera/audio-only; audio/control và
   server authority phải tồn tại lâu hơn video.
10. Bốn background V1 không được nhập: file là JPEG gắn đuôi `.png`, có C2PA/SynthID provenance
    nhưng không tìm thấy hồ sơ license/source quyền phân phối.

Kết quả này không tuyên bố effect đã sẵn sàng production. Nó chốt một no-effect path an toàn và
chuyển các gate vật lý Safari/macOS, máy yếu, camera/mic/output thật, NVDA và tải LiveKit 25/50
sang acceptance bắt buộc của P4-03/P4-05/P4-11 trước khi bật subfeature tương ứng.

## 2. Phương pháp và thang bằng chứng

### 2.1 Bốn lớp bằng chứng

| Mức | Ý nghĩa | Được dùng để |
| --- | --- | --- |
| A | Standard hoặc tài liệu chính thức hiện hành | Khóa constraint, privacy, a11y và product pattern |
| B | Source chính thức pin exact revision/version | Audit lifecycle, asset URL, browser fallback, license |
| C | Prototype/automated probe tái lập trên máy hiện tại | Xác nhận capability và phát hiện rủi ro thực thi |
| D | Giả thuyết product nội bộ | Chọn constant ban đầu; bắt buộc đo lại, không gán cho đối thủ |

Google Meet/Zoom chỉ cung cấp pattern UX. Help Center là tài liệu mutable, không phải protocol
contract. Mọi source được truy cập ngày 2026-08-09; exact behavior không có tài liệu được ghi là
“chưa xác minh”, không suy diễn thành fact.

### 2.2 Reference checkout

| Dự án | Revision/version | License | Vai trò |
| --- | --- | --- | --- |
| LiveKit Meet | `665e1cb7841ab872de0d8e5c310744009a763b08` | Apache-2.0 | Reference UX/source, không fork |
| LiveKit Track Processors | `9ef5191da7fb6d82e55876fa04d0e6048d49859b` / 0.7.2 | Apache-2.0 + NOTICE | Candidate effect cô lập |
| LiveKit Components | `2da3e59e9854cde26cbeadcf8a5732ea42163bfa` | Apache-2.0 | Audit grid/hooks/primitives |
| MediaPipe Tasks Vision | exact dependency 0.10.14 trong Track Processors | Apache-2.0 | Segmentation transitively used |

Các checkout nằm trong `.tmp/research/`, bị Git ignore, không phải submodule hoặc production
dependency. Không dùng credential LiveKit/Neon và không đọc file secret. Revision, lệnh và kết quả
được lưu ở báo cáo này, nhưng script effect/capability là exploratory local evidence, không tái lập
được trực tiếp từ clean clone. Vì vậy số đo chỉ hỗ trợ quyết định force-off; P4-11 phải tạo harness
reviewable và chạy lại trước mọi activation.

### 2.3 Lệnh evidence cô lập

```text
pnpm --ignore-workspace install --frozen-lockfile
pnpm --ignore-workspace lint
pnpm --ignore-workspace build
pnpm --ignore-workspace build-sample
node .tmp/research/browser-capability-probe.cjs
node .tmp/research/effect-benchmark-client.cjs chrome
node .tmp/research/effect-benchmark-client.cjs msedge
```

Track Processors 0.7.2 build và sample build PASS; lint có 0 error/2 duplicate-import warning.
Script `test` upstream không chạy vì package khai báo `jest` script nhưng không có `jest` trong
dependency/devDependency. Đây là gap của revision được audit, không phải test failure của TutorHub.
Sample bundle tạo một JS chunk 567,98 kB, gzip 155,83 kB; production bắt buộc dynamic import.

## 3. Benchmark Zoom và Google Meet

### 3.1 Pattern được giữ và khác biệt TutorHub

| Flow | Evidence đối thủ | Quyết định TutorHub |
| --- | --- | --- |
| Green room | Meet có mic/camera/speaker selector, mic meter và speaker test; Zoom có video/audio preview | Một prejoin local, probe explicit; không capture ở initial render |
| Join without video | Zoom cho join có/không video; Meet hướng dẫn tắt video khi máy/mạng yếu | Camera/effect failure không phải join gate |
| Waiting room | Meet/Zoom có queue, admit one/all, deny/return-to-waiting | Tách waiting khỏi provider; không auto-admit/bypass trong MVP |
| Effects | Cả hai có none/blur/static; Meet có cloud effect, Zoom có hardware matrix | Chỉ local candidate; không cloud frame processing, upload/AI/video background |
| Grid lớn | Meet từng cho tiled tới 49; Zoom dùng 25/49 có điều kiện và pagination | Không xem 49 là capacity proof; TutorHub cap visible video theo viewport |
| Speaker/layout | Meet Auto thay đổi theo context; Zoom tách speaker/gallery/custom order | Grid chỉ highlight; Active speaker có hysteresis; local pin thắng auto speaker |
| Presentation | Cả hai giữ presentation chính và participant context | Stage + bounded rail; stop/cancel/leave phục hồi layout trước |
| Pin/spotlight | Cả hai phân biệt local pin với host-visible spotlight | P4-05 chỉ local pin; moderated stage thuộc P4-07 |
| Hand | Meet có FIFO/lower one/all và auto-lower; Zoom giữ đến self/host lower | Server FIFO; không auto-lower theo audio/active speaker |
| Reaction | Meet grouping/announcement controls; Zoom reaction biến mất sau 10 giây | Sáu emoji, TTL 10 giây, grouping, rate-limit, reduced-motion |
| Toolbar | Meet giữ bottom controls; Zoom cần fixed toolbar cho keyboard workflow | Toolbar desktop luôn hiện; Leave tách khỏi mic/camera |
| Shortcut/a11y | Meet cảnh báo `Ctrl+Alt` có thể xung đột AltGr; Zoom có screen-reader alert settings | UI equivalent cho mọi shortcut; không chiếm bare Space toàn app |
| Degraded network | Hai sản phẩm khuyên giảm/tắt video trước | Coarse status; effect/video degrade trước audio/control |

Không sao chép cloud-based effects của Meet, preference persistence của Zoom, full emoji palette,
auto-lower hand hoặc layout desktop app của Zoom. Google Meet gần target web hơn, nhưng ngay cả
Meet cũng không phải bằng chứng rằng TutorHub + LiveKit chịu được 49 video track.

### 3.2 Product behavior chưa có source chính thức đủ mạnh

- Thuật toán/hysteresis chính xác của active speaker.
- TTL reaction của Google Meet; chỉ Zoom xác nhận 10 giây.
- Grouping window và rate-limit nội bộ của Meet/Zoom.
- Ordering khi nhiều raise cùng mili-giây hoặc qua reconnect.
- Speaker-output parity trên Firefox/Safari và exact UI ở 320 CSS px/200%.
- Ngưỡng CPU/FPS đối thủ dùng để tự tắt effects.

TutorHub constants ở ADR-0031 là initial product constants có test, không phải claim sao chép.

## 4. Audit TutorHub V1: reuse/reject

V1 hữu ích như inventory use case, nhưng authority và frontend runtime không an toàn để port.

| Nguồn V1 | Reuse dưới dạng scenario | Reject/viết lại |
| --- | --- | --- |
| `PreJoinDialog.java:11-107` | Xem lớp, cancel/continue | Swing modal, thiếu device test, debug/focus gap |
| `lobby-manager.js:128-348` | Device selector, preview, camera-off | Auto xin camera/mic, global state, query role, `localStorage`, raw error |
| `lobby-manager.js:433-497,726-745` | Mic meter, speaker tone | AudioContext/rAF thủ công, output mặc định, thiếu autoplay recovery |
| `lobby-manager.js:37-125,378-421` | None/blur/static use case | CDN MediaPipe, main-thread loop, CSP/license/cleanup/a11y gap |
| `lobby-manager.js:531-619` | waiting/join/retry states | Student kết nối LiveKit trước admission |
| `livekit-manager.js:60-197` | adaptive/dynacast/screen-share cases | Log token/raw error, provider identity làm business identity |
| `livekit-manager.js:199-342,394-466` | Signal/moderation inventory | Client metadata/DataChannel quyết định role/hand/admit/mute/kick/reaction |
| `video-layout.js:1-45,126-243` | Grid/speaker/presentation/pin scenarios | Sqrt grid không cap, không hysteresis/stable focus, unsafe embedded iframe |
| `roster.js:137-438` và `room-roster-manager.js:4-60` | Queue/lower/reaction palette | Client timestamp/metadata, arbitrary emoji/sender, no limit/TTL/resync |
| `tldraw_board_v2.html` và `board.css` | Control/roster/modal inventory | Inline handler, label/dialog/live-region/reduced-motion/forced-color gaps |
| `WaitingRoomDialog.java`, `ClassroomDAO.java` | Admit transaction scenarios | Email exposure, V1 protocol/schema không tenant-aware |
| `ClassroomFeatureSmokeTest.java` | Wiring inventory | Reflection/string smoke không chứng minh ACL/concurrency/media/a11y |

### 4.1 Asset V1

| File | Byte thực | Kích thước | SHA-256 |
| --- | --- | --- | --- |
| `bg-classroom.png` | JPEG | 1024x1024 | `571DEC05FEC3B9AAA4E14E8D7B841D33268EE63E27E6E71D9EC0B03F0A70904A` |
| `bg-library.png` | JPEG | 1024x1024 | `B13B76BFB95BDF66B245FF0594F16894028CAEEC1AD423263378C8870A4ACE18` |
| `bg-nature.png` | JPEG | 1024x1024 | `D99A6DDED40A8C2B32F28DF41B62AF31AF2690B44ECF3D145901858E03EAEE89` |
| `bg-space.png` | JPEG | 1024x1024 | `C974D54B15E83678C90F6389EA64B2BC1D9C38CB424B9B86CA4D3D5D0E9B8520` |

Các file chứa C2PA claim về Google Generative AI/SynthID nhưng không có license/source declaration
được tìm thấy. C2PA string không tự chứng minh quyền phân phối. P4 chỉ được dùng asset mới do dự án
sở hữu hoặc có license manifest, MIME/extension đúng, SHA-256 và provenance rõ.

## 5. Audit baseline V2

### 5.1 Có thể giữ

- `@livekit/components-react@2.9.23`, styles 1.2.0 và `livekit-client@2.20.1` đã pin.
- `Room` được tạo một lần với `adaptiveStream` và `dynacast`.
- Credential chỉ giữ trong navigation memory và route state được xóa.
- `PreJoin.persistUserChoices=false`, listen-only path, bounded alert và reconnect observer.
- Backend P4-02 ép `CanPublishData=false`.

### 5.2 Phải thay ở P4-03/P4-05

- `<PreJoin>` hiện dùng defaults camera/mic bật; chưa chứng minh initial render không capture.
- Web vẫn gọi legacy class token, nhận/hiển thị `room_name` và `participant_identity`; canonical P4
  phải dùng RoomInstance join credential/opaque ParticipantSession và không hiển thị provider ID.
- Speaker test chỉ phát oscillator ra system default; chưa chọn output/recover autoplay.
- Error taxonomy thiếu `OverconstrainedError`, `AbortError`, OS/system-blocked và insecure context.
- Một boolean `canPublish` đang điều khiển mic/camera/screen share; canonical grants phải tách.
- Chỉ có một `GridLayout`; chưa có mode, rail, cap/pagination, stable focus/subscription budget.
- Chưa có classroom-scoped CSP/Permissions Policy, effect lifecycle hoặc actual cleanup tests.

## 6. Browser/capability matrix

### 6.1 Automated probe thực chạy ngày 2026-08-09

Probe chạy trên secure localhost, headless, Windows hiện tại. `backgroundBlur` dưới đây là media
constraint native, không phải CSS blur.

| Runtime thực chạy | gUM/enumerate | selectAudioOutput | setSinkId | TrackProcessor/Generator | VideoFrame/canvas fallback | native backgroundBlur |
| --- | --- | --- | --- | --- | --- | --- |
| Chrome 151.0.7922.77 / Windows | Có | Không trong headless probe | Có | Có/Có | Có/Có | Không |
| Edge 151.0.4129.72 / Windows | Có | Không trong headless probe | Có | Có/Có | Có/Có | Không |
| Playwright Chromium 149 / Windows | Có | Không | Có | Có/Có | Có/Có | Không |
| Playwright Firefox 151 / Windows | Có | Có | Có | Không/Không | Có/Có | Không |
| Playwright WebKit 26.5 / Windows | Media API không được expose trong runtime này | Không | Không | Không/Không | Không/Không | Không |

Kết quả WebKit trên Windows chỉ là cảnh báo capability của automation runtime, không phải Safari
macOS. Không ghi Safari PASS/FAIL từ dòng này. Safari/macOS thật vẫn là P4-11 physical gate.

### 6.2 Evidence chuẩn và hệ quả

- [Media Capture and Streams](https://www.w3.org/TR/mediacapture-streams/) quy định constraint,
  device-label privacy và EC/NS/AGC; supported constraint không bảo đảm device áp dụng, nên phải
  kiểm tra `getSettings()`.
- [Audio Output Devices API](https://www.w3.org/TR/audio-output/) yêu cầu user activation/policy
  cho output selection; fallback là system default, không chặn Join.
- [Media Capture Transform](https://www.w3.org/TR/mediacapture-transform/) yêu cầu quản lý frame/
  resource cẩn thận; [MDN](https://developer.mozilla.org/en-US/docs/Web/API/MediaStreamTrackProcessor/MediaStreamTrackProcessor)
  vẫn đánh dấu TrackProcessor là limited availability.
- [WebKit Safari 18](https://webkit.org/blog/15443/news-from-wwdc24-webkit-in-safari-18-beta/)
  và [Safari 26.4](https://webkit.org/blog/17862/webkit-features-for-safari-26-4/) cho thấy media
  processing đang thay đổi theo release; feature-detect, không browser-sniff.
- [Chrome native background blur](https://developer.chrome.com/blog/background-blur) là hướng
  progressive enhancement, không phải cross-browser P4 contract.

Hệ quả: Chrome/Edge dùng modern transform path; Firefox candidate phải chịu canvas fallback; Safari
chỉ được bật effect sau test thật. `None` luôn là baseline.

## 7. Effects: source audit và benchmark

### 7.1 Track Processors 0.7.2

Ưu điểm:

- Tích hợp đúng LiveKit `TrackProcessor` và có mode disabled/blur/virtual-background.
- Có modern path và canvas fallback, `switchTo()` để tránh tạo/destroy liên tục.
- Apache-2.0 + NOTICE; pin exact MediaPipe 0.10.14.
- Cleanup dừng captured track, RAF/canvas hoặc TrackGenerator và đóng segmenter.

Blocker:

- Default WASM từ `cdn.jsdelivr.net/npm/@mediapipe/tasks-vision@0.10.14/wasm`.
- Default model từ Google Storage với alias `latest`; không immutable.
- `segmentForVideo()` đồng bộ; source cảnh báo có thể block main thread hàng chục tới khoảng 100 ms.
- `processingTimeMs` có khả năng cộng segmentation hai lần: `filterTimeMs` bắt đầu trước
  segmentation, sau đó `processingTimeMs = segmentationTimeMs + filterTimeMs`. Benchmark phải dùng
  Long Task/callback/FPS độc lập, không chỉ metric package.
- MediaPipe nói input xử lý on-device nhưng Tasks APIs gửi performance/utilization metrics và yêu
  cầu developer xử lý informed consent trong [privacy notice chính thức](https://github.com/google-ai-edge/mediapipe#privacy-notice).
- Upstream sample còn tải Bootstrap/Font Awesome CDN; chỉ là sample, tuyệt đối không copy CSP/runtime.

### 7.2 Prototype cô lập

Harness dùng `canvas.captureStream(15fps)` ở đúng 640x360, 960x540 và 1280x720, không dùng camera
thật/credential/provider. Mỗi blur/background case lấy 30 callback; `None` khởi tạo disabled path.
Observable cleanup sau mỗi Chrome/Edge case chỉ xác nhận source track `ended` và không còn hidden
processor canvas; nó chưa chứng minh worker/timer/memory/10-cycle cleanup invariant của rollout.

#### Chrome 151 / Windows

| Mode | 360p processing p95 | 540p p95 | 720p p95 | Long task lớn nhất quan sát | Cleanup quan sát được |
| --- | ---: | ---: | ---: | ---: | --- |
| None/disabled | không có processed callback | không có | không có | 0 trong case; cold init 4,30 giây | track ended, 0 canvas |
| Blur | 9,1 ms | 7,9 ms | 10,8 ms | 108 ms | track ended, 0 canvas |
| Static background | 9,9 ms | 8,9 ms | 13,4 ms | 143 ms | track ended, 0 canvas |

#### Edge 151 / Windows

| Mode | 360p processing p95 | 540p p95 | 720p p95 | Long task lớn nhất quan sát | Cleanup quan sát được |
| --- | ---: | ---: | ---: | ---: | --- |
| None/disabled | không có processed callback | không có | không có | cold init 8,41 giây, có long task 78 ms | track ended, 0 canvas |
| Blur | 7,8 ms | 9,6 ms | 11,2 ms | 101 ms | track ended, 0 canvas |
| Static background | 7,8 ms | 9,7 ms | 12,0 ms | 172 ms | track ended, 0 canvas |

#### Firefox 151 / Windows

Static capability probe cho thấy modern TrackProcessor/Generator không có, còn `VideoFrame` và
`canvas.captureStream()` có. Tuy nhiên cả 9 case `None`/blur/static tại 360p/540p/720p đều không
đến được measurement: fallback trả `Constraints could not be satisfied`; console đồng thời báo
processor element không play được. MediaPipe graph có khởi động/đóng, nhưng harness không có đủ
bằng chứng để xác nhận processed stream hoặc cleanup invariant. Firefox Long Task API cũng không
available trong runtime probe. Vì vậy “có canvas fallback API” không được xem là effect support;
Track Processors candidate hiện FAIL gate Firefox tự động và phải giữ off.

Số `processing p95` là metric upstream có caveat double-count ở trên. Callback interval p95 phần
lớn 75-80 ms ở target 15fps; 30 frame hoàn tất trong khoảng 2,0-2,3 giây. Cold/warm cache, video
tổng hợp đơn giản và máy hiện tại không đại diện máy yếu hoặc phòng LiveKit đang tải.

Điểm quan trọng hơn median là có Long Task 80-172 ms và disabled processor vẫn init model/WASM.
Do đó P4 không attach processor trên initial render. Chỉ dynamic-import/init sau khi người dùng mở
effect picker; sau lần init có thể giữ một processor ở disabled và `switchTo()` trong cùng preview/
published-track lifecycle nếu cleanup/reconnect test đạt.

### 7.3 Quyết định effect

- MVP production bắt buộc hoạt động đầy đủ với `None` và không tải processor/model.
- Track Processors là candidate duy nhất, nhưng subfeature `classroom_media_effects`
  phải force-off cho tới P4-05/P4-11.
- Nếu được mở pilot: chỉ blur + ba hoặc bốn static project-owned background; không upload, AI,
  video background, beauty, makeup, avatar hoặc persistence.
- Self-host cùng origin exact WASM/model/background với content hash, manifest upstream/version/
  SHA-256/license/provenance; deploy wrapper/assets atomically.
- Cold init quá 3 giây, processor error, hash/fetch/CSP failure, long-task budget hoặc sustained
  over-budget đều trở về raw track, không chặn Join.
- Direct MediaPipe song song là NO-GO; native blur chỉ progressive enhancement, không tạo stack
  thứ hai; Krisp là NO-GO P4 MVP.

## 8. Prejoin/device/audio contract

### 8.1 State machine

```text
IDLE --explicit preview--> REQUESTING_PERMISSION
REQUESTING_PERMISSION --> PREVIEW_READY | DENIED | NO_DEVICE | BUSY | FAILED
PREVIEW_READY --device/effect change--> SWITCHING --> PREVIEW_READY | DEGRADED
IDLE/PREVIEW_READY/DEGRADED --join consent--> CREATING_ATTEMPT
CREATING_ATTEMPT --> WAITING | ADMITTED | JOIN_FAILED
WAITING --> ADMITTED | DENIED | CANCELLED | ENDED | TIMEOUT
ADMITTED --> CREDENTIAL_ISSUED --> CONNECTING
```

Không có transition từ initial render sang permission prompt. Không có credential/provider connect
trong `WAITING`. Retry dùng idempotency key và authority/version hiện tại.

### 8.2 Error taxonomy và recovery

| Browser error | Mã bounded | UI action |
| --- | --- | --- |
| `NotAllowedError` | `media_permission_denied_or_blocked` | Hướng dẫn browser/OS; chỉ retry sau action; cho listen-only nếu policy cho phép |
| `NotFoundError` | `media_device_not_found` | Rescan/default device; cho audio-only hoặc listen-only |
| `NotReadableError` | `media_device_busy_or_unreadable` | Đóng app/tab khác đang giữ thiết bị, retry explicit |
| `OverconstrainedError` | `media_constraints_unavailable` | Bỏ exact device/constraint và retry bằng profile mặc định |
| `AbortError` hoặc probe cũ bị hủy | `media_probe_aborted` | Cleanup thế hệ cũ rồi retry bằng hành động người dùng |
| `SecurityError`/API hoặc secure-context/policy thiếu | `media_capture_unsupported_or_policy_blocked` | Feature-off, hướng dẫn browser; cho listen-only nếu policy cho phép |
| Không có `selectAudioOutput`/`setSinkId` | `audio_output_selection_unsupported` | Dùng output mặc định, không chặn Join |
| `play()` reject/`AudioContext` suspended | `media_playback_blocked` | Hiện action “Phát âm thanh”, không auto-loop retry |
| Khác | `media_device_unknown` | Thông báo chung với request ID; không render/log raw message |

`devicechange` chỉ là tín hiệu: enumerate lại, giữ selection nếu còn, nếu mất thì về system default
và announce một lần. Không persist device ID/label/effect mặc định.

### 8.3 Audio policy

- `Speech` mặc định: yêu cầu EC/NS/AGC dạng ideal, đọc actual settings và không hứa “lọc ồn” nếu
  browser/device không áp dụng.
- `Original sound/music`: yêu cầu EC/NS/AGC false khi supported, đặt `contentHint="music"` như hint;
  cảnh báo nếu actual settings vẫn bật processing.
- Không chạy Krisp đồng thời với WebRTC noise suppression.
- Speaker selection là progressive enhancement: user gesture -> `selectAudioOutput()` nếu có ->
  `setSinkId()`; nếu thiếu/deny thì tone system default và hướng dẫn đổi output OS.
- AudioContext/autoplay suspended có nút “Bắt đầu âm thanh”; không retry vô hạn.

### 8.4 Cleanup invariant

Cancel, unmount, route change, failed join và terminal disconnect phải để lại:

- zero live preview track;
- zero processor/segmenter/worker/RAF/hidden canvas;
- zero speaker-test oscillator/AudioContext còn chạy;
- zero duplicate `devicechange`/room listener;
- zero credential/token/device label/raw provider error trong log/persistence.

## 9. Layout 2-50 và screen share

### 9.1 Mode và priority

| Mode | Main surface | Video visible ban đầu | Rule |
| --- | --- | --- | --- |
| Grid | Paged equal tiles | tối đa 12 desktop, 6 medium, 4 narrow | Không reorder theo speaker/hand; highlight/badge |
| Active speaker | Một stage + bounded rail | stage + tối đa 6 rail | local pin thắng auto speaker; DOM focus không nhảy |
| Presentation | Share stage + bounded rail | share + tối đa 6 rail | stop/cancel/presenter leave phục hồi mode/pin trước |

Prototype 1280x720 dùng cap 12; ADR cho phép một profile desktop lớn riêng với hard bound 16 chỉ sau
evidence P4-05. Fixture rollout bắt buộc: 2, 5, 25, 50 participant ở 1280x720, 320 CSS px và 200%
zoom. Off-page video
không attach/subcribe như visible tile; audio/control vẫn toàn phòng theo policy. `adaptiveStream`
chỉ tối ưu track đã attach, không thay pagination/subscription policy.

### 9.2 Stable update

- Logical roster sort ổn định theo server/session projection; join append, leave remove, reconnect
  giữ stable identity.
- Active speaker chỉ thay stage sau khi candidate nói liên tục 800 ms, giữ speaker hiện tại tối thiểu
  2,5 giây và chỉ release khi speaker hiện tại im lặng 1.500 ms. Đây là initial constants mức D, cần
  tune ở P4-05.
- Speaker event không reorder DOM trong Grid và không announce mỗi lần cho screen reader.
- Pin là local preference trong memory; participant leave/share end có deterministic fallback và
  focus trở về control/tile tồn tại gần nhất.
- Screen share start chuyển Presentation nếu user chưa khóa layout; stop/cancel/replace/leave phục
  hồi mode/pin trước, không làm mất toolbar.

LiveKit `GridLayout`, `useGridLayout`, `usePagination` và `useVisualStableUpdate` được dùng như
primitive. Source `components-js` có layout tối đa 5x5 và pagination; TutorHub dùng visible cap bảo
thủ hơn. Không gán `role="grid"` nếu không triển khai đầy đủ APG Grid keyboard model; video wall có
thể là semantic section/list.

## 10. Hand raise và reaction

### 10.1 Boundary không thay đổi

Client gửi typed command tới Core API. Core API reauthorize tenant/membership/capability/room state,
enforce idempotency/rate-limit và gắn server sequence/time. Provider projection chỉ là delivery;
reconnect lấy bounded snapshot rồi áp event có sequence mới hơn. Client không gửi actor/role/tenant/
ParticipantSession của người khác. LiveKit token giữ `CanPublishData=false`.

### 10.2 Hand raise

- One raised state/actor/RoomInstance; `raise`, self `lower`, moderator `lower-one/lower-all`.
- FIFO theo monotonic server sequence, không client clock.
- Không auto-lower khi active speaker hoặc audio detector nhận diện người đó nói.
- State tồn tại tới explicit lower hoặc room terminal; retry cùng key idempotent.
- Grid không reorder; tile badge + bounded moderator queue.
- Snapshot tối đa capacity 50, gồm bounded display name/state/sequence, không email/provider ID.

### 10.3 Reaction

- Allowlist: `👍`, `👏`, `❤️`, `🎉`, `😂`, `😮`.
- TTL: 10 giây tính từ server accepted time; không lưu như chat/attendance/grade/audit.
- Grouping UI: cùng emoji trong cửa sổ 750 ms được collapse thành count; tối đa ba group animation
  đồng thời, còn lại vẫn có tile badge/summary.
- Initial rate policy: tối đa 3 reaction/5 giây và 20/phút mỗi actor; tối đa 100/5 giây mỗi
  RoomInstance. 429 có bounded `Retry-After`; limiter phải cross-instance/shared và fail closed,
  không dùng local process làm authority. Đây là product constant cần load-test/tune.
- Unknown/oversized/malformed payload bị reject/ignore an toàn, không raw-log.
- Reduced motion bỏ animation bay; polite live region gom summary tối đa mỗi 2 giây, không đọc từng
  sender trong storm. Người dùng có mức announcement.

## 11. Accessibility contract

Target WCAG 2.2 AA, bao gồm [Reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html),
[Focus Order](https://www.w3.org/WAI/WCAG22/Understanding/focus-order.html),
[Toolbar APG](https://www.w3.org/WAI/ARIA/apg/patterns/toolbar/) và bounded live-region behavior
theo [Alert pattern](https://www.w3.org/WAI/ARIA/apg/patterns/alert/).

- Toolbar desktop luôn hiện; Leave tách khỏi mic/camera và cần confirm khi room active.
- Mọi toggle dùng button thật, accessible name và `aria-pressed`; không color-only.
- Focus ring tối thiểu 2px; target khuyến nghị 44x44, không thấp hơn WCAG 24x24.
- Keyboard tới device/effect/layout/page/pin/hand/reaction/leave; shortcut luôn có UI equivalent.
- Không chiếm bare Space toàn app; shortcut không chạy trong input/dialog ngoài scope.
- Dialog/sidebar trả focus đúng; active speaker/join/leave không cướp focus.
- 320 CSS px/200% không làm mất mic/camera/leave hoặc tạo horizontal control trap.
- Forced colors giữ border/status; reduced motion bỏ tile/reaction animation nhưng giữ state.
- NVDA/Chrome Windows, VoiceOver/Safari macOS và keyboard-only là physical P4-11 gate; Axe chỉ là
  automated supplement, không thay AT validation.

## 12. Security, privacy, CSP và supply chain

### 12.1 Production effect gate

Trước khi effect flag có thể bật:

1. Pin package và lock integrity; xuất SBOM, LICENSE/NOTICE.
2. Self-host exact WASM/model/background bằng filename content-hash; không dùng `latest`.
3. Manifest ghi upstream URL, version, SHA-256, license, provenance và owner.
4. Audit cold/warm/offline network; zero jsDelivr/Google model storage/telemetry outbound chưa duyệt.
5. Privacy/legal quyết định informed consent cho MediaPipe metrics; nếu không kiểm soát được thì
   không ship processor.
6. CSP report-only rồi enforce; fetch/hash/init lỗi fallback raw track.
7. Browser/device/low-end/performance/cleanup matrix đạt; không dựa riêng metric package.

### 12.2 Header candidate

```text
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'wasm-unsafe-eval';
  worker-src 'self';
  connect-src 'self' https://<exact-core-api> wss://<exact-livekit-host>;
  img-src 'self' blob:;
  media-src 'self' blob:;

Permissions-Policy:
  camera=(self), microphone=(self), display-capture=(self), speaker-selection=(self)
```

Không thêm `unsafe-eval`; chỉ thêm `blob:` cho worker nếu prototype chứng minh bắt buộc. WebRTC ICE
không được coi là bảo vệ chỉ bằng CSP; token grant/provider config/Core API authority vẫn quyết định.

### 12.3 Không log/persist

- LiveKit token, SDP, ICE candidate/IP.
- Device ID/label, frame/audio, processor raw error.
- Participant email/provider identity/session ID trong UI event.
- Effect preference hoặc hardware fingerprint mặc định.

## 13. Decision matrix

Điểm 1-5 là công cụ quyết định nội bộ, không phải benchmark tuyệt đối. Trọng số ưu tiên security/
privacy và khả năng fallback hơn số lượng hiệu ứng.

| Candidate | Security/authority 20 | Privacy/supply 20 | Browser 15 | Perf/degrade 15 | LiveKit/lifecycle 15 | A11y/UX 10 | Maintain/license 5 | Tổng /100 | Quyết định |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Native WebRTC + `None` | 20 | 20 | 15 | 15 | 14 | 9 | 5 | 98 | GO baseline |
| Native `backgroundBlur` | 18 | 20 | 5 | 12 | 10 | 8 | 5 | 78 | Progressive only |
| Track Processors 0.7.2 | 17 | 7 | 9 | 8 | 14 | 8 | 4 | 67 | Conditional prototype |
| Direct MediaPipe stack | 14 | 6 | 8 | 7 | 7 | 7 | 3 | 52 | NO-GO |
| Krisp web NC | 16 | 7 | 8 | 8 | 12 | 7 | 2 | 60 | NO-GO P4 MVP |
| V1 processor/assets | 5 | 2 | 5 | 4 | 3 | 3 | 1 | 23 | Reject |

## 14. Acceptance handoff

### P4-03

- Initial render `getUserMedia` count = 0; explicit preview creates đúng một stream.
- Deny/not-found/busy/system/overconstraint/autoplay/output unsupported paths; Join remains usable.
- Device change/revoke/switch 20 lần; cleanup track/AudioContext/listener đạt.
- Speech/original-sound actual settings; no device label/token/raw-error persistence.
- Canonical RoomInstance join-attempt/credential replaces legacy class token.

### P4-04

- Waiting participant has no provider credential/connection.
- waiting/admitted/denied/cancelled/ended/timeout/provider-unavailable UI and focus states.
- Concurrent admit/deny/end/revoke has one winner; stale instance cannot admit.
- Projection contains no email/session/provider identifiers.

### P4-05

- Grid/speaker/presentation fixtures 2/5/25/50 at desktop/320px/200%.
- Stable DOM/focus, pin/hysteresis, screen-share restore and bounded visible subscriptions.
- Exact mic/camera/share grants; cleanup on leave/fail/reconnect terminal.
- Effect subfeature remains off until the full gate in section 12 passes.

### P4-06

- Core API server sequence/FIFO/idempotency/lower one/all/resync.
- Reaction allowlist/TTL/grouping/rate-limit and 25/50 storm.
- Direct DataChannel command cannot mutate state; `CanPublishData=false` asserted.
- Bounded a11y summaries, reduced motion and privacy/no-log tests.

### P4-11

- Physical Chrome/Edge/Firefox Windows and Safari macOS stable with exact OS/browser/GPU/device.
- Standard + low-end device, 360p/540p/720p, screen-share, 25/50 load and provider outage.
- NVDA/Chrome and VoiceOver/Safari, forced-colors/reduced-motion/200%.
- If any effect gate fails, keep effect flag off; this does not block no-effect classroom rollout.

## 15. Nguồn chính thức

### Google Meet

- [Connect video and audio](https://support.google.com/meet/answer/10409699?hl=en)
- [Waiting rooms](https://support.google.com/meet/answer/16523457?hl=en)
- [Backgrounds and effects](https://support.google.com/meet/answer/10058482?hl=en)
- [Layouts](https://support.google.com/meet/answer/10550593?hl=en)
- [Present](https://support.google.com/meet/answer/9308856?hl=en)
- [Pin participants/presentations](https://support.google.com/meet/answer/7501121?hl=en)
- [Raise hand](https://support.google.com/meet/answer/10159750?hl=en)
- [Reactions](https://support.google.com/meet/answer/13151720?hl=en)
- [Keyboard shortcuts](https://support.google.com/meet/answer/9298571?hl=en)
- [Screen reader](https://support.google.com/meet/answer/15738543?hl=en)
- [Video/audio quality](https://support.google.com/meet/answer/10620583?hl=en)
- [Camera recovery](https://support.google.com/meet/answer/10621292?hl=en)

### Zoom

- [Prejoin preview](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061118)
- [Video test](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061836)
- [Mic/speaker test](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0062765)
- [Waiting room](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063329)
- [Virtual background requirements](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060007)
- [Meeting layouts](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063672)
- [Side-by-side share](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0067526)
- [Raise hand](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0068290)
- [Reactions](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063323)
- [Reaction policy](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0058404)
- [Accessibility FAQ](https://www.zoom.com/en/accessibility/faq/)
- [Zoom Web App VPAT](https://media.zoom.com/download/assets/Zoom%2BWeb%2BApp%2BVPAT.pdf/b0c92e0a5c9511f0b107ca751c8b772c)

### LiveKit và MediaPipe

- [LiveKit PreJoin](https://docs.livekit.io/reference/components/react/component/prejoin/)
- [LiveKit building blocks](https://docs.livekit.io/reference/components/react/concepts/building-blocks/)
- [LiveKit GridLayout](https://docs.livekit.io/reference/components/react/component/gridlayout/)
- [React best practices](https://docs.livekit.io/reference/components/react/guide/)
- [Adaptive subscription/dynacast](https://docs.livekit.io/transport/media/subscribe/)
- [Reconnect](https://docs.livekit.io/intro/basics/connect/)
- [Data packets](https://docs.livekit.io/transport/data/packets/)
- [Noise cancellation](https://docs.livekit.io/transport/media/noise-cancellation/)
- [LiveKit Components source](https://github.com/livekit/components-js)
- [Track Processors source/releases](https://github.com/livekit/track-processors-js)
- [MediaPipe Image Segmenter for Web](https://developers.google.com/edge/mediapipe/solutions/vision/image_segmenter/web_js)
- [MediaPipe source/privacy notice](https://github.com/google-ai-edge/mediapipe#privacy-notice)

### Standards/browser/accessibility

- [Media Capture and Streams](https://www.w3.org/TR/mediacapture-streams/)
- [Media Capture Transform](https://www.w3.org/TR/mediacapture-transform/)
- [Audio Output Devices API](https://www.w3.org/TR/audio-output/)
- [Screen Capture](https://www.w3.org/TR/screen-capture/)
- [MediaStreamTrack Content Hints](https://www.w3.org/TR/mst-content-hint/)
- [CSP Level 3](https://www.w3.org/TR/CSP3/)
- [Permissions Policy](https://www.w3.org/TR/permissions-policy-1/)
- [WCAG 2.2](https://www.w3.org/TR/WCAG22/)
- [MDN TrackProcessor](https://developer.mozilla.org/en-US/docs/Web/API/MediaStreamTrackProcessor/MediaStreamTrackProcessor)
- [Mozilla TrackProcessor/Generator design](https://blog.mozilla.org/webrtc/unbundling-mediastreamtrackprocessor-and-videotrackgenerator/)
- [WebKit WebRTC/media privacy](https://webkit.org/blog/7763/a-closer-look-into-webrtc/)
- [WebKit Safari 18 media changes](https://webkit.org/blog/15443/news-from-wwdc24-webkit-in-safari-18-beta/)
- [WebKit Safari 26.4 fixes](https://webkit.org/blog/17862/webkit-features-for-safari-26-4/)
