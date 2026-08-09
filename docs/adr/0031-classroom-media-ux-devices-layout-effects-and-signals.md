# ADR 0031: Classroom media UX cho thiết bị, layout, effects và signals

- Status: Accepted
- Date: 2026-08-09
- Scope: P4-MEDIA-UX-00 và phần UX/signals/effects của P4-03/P4-04/P4-05/P4-06/P4-11
- Extends: ADR-0030; không thay đổi MediaSpace/RoomInstance/ParticipantSession hoặc LiveKit Cloud
- Evidence date: nguồn chính thức và local reference checkout được đối chiếu ngày 2026-08-09

## Context

ADR-0030 đã khóa authority của Classroom Media: PostgreSQL/shared policy và Core API quyết định
room lifecycle, admission, grant và moderation; LiveKit chỉ là media transport/provider state.
`CanPublishData=false` trong Phase 4 MVP, nên hand raise/reaction không thể lấy client metadata,
provider DataChannel hoặc đồng hồ client làm nguồn sự thật.

P4-MEDIA-UX-00 cần thu hẹp các câu hỏi còn mở trước khi code P4-03 đến P4-06:

- khi nào browser được phép mở camera/microphone và cách phục hồi lỗi thiết bị;
- speech/original-sound, audio output và autoplay có fallback nào;
- grid, active-speaker và presentation layout phải ổn định ra sao ở 2/5/25/50 người;
- hand queue/reaction hội tụ, chống spam và được screen reader thông báo thế nào;
- blur/static background có đủ an toàn để ship, dùng processor nào và tự tắt khi nào;
- CSP, Permissions Policy, asset/license/privacy, cleanup và rollout gate nào là bắt buộc.

Benchmark Zoom và Google Meet chỉ cung cấp product pattern, không chứng minh kiến trúc nội bộ,
không phải dependency và không phải nguồn cho các hằng số TutorHub. V1 chỉ được audit read-only để
lấy use case/test; global DOM/JCEF, local persistence, CDN MediaPipe, client-owned admission và
DataChannel authority của V1 không được port.

Repository đã khóa `@livekit/components-react@2.9.23` và `livekit-client@2.20.1`. Audit local dùng
ba shallow checkout được Git-ignore, không phải production dependency:

| Nguồn                         | Revision/package được đọc                                                  | Kết luận liên quan                                                                           |
| ----------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `livekit-examples/meet`       | `665e1cb7841ab872de0d8e5c310744009a763b08`                                 | Reference controls/responsive/autoplay; không fork ứng dụng                                  |
| `livekit/components-js`       | `2da3e59e9854cde26cbeadcf8a5732ea42163bfa`; React `2.9.23`, Core `0.12.14` | Có grid/pagination/visual-stability primitives; prefab không phải TutorHub product authority |
| `livekit/track-processors-js` | `9ef5191da7fb6d82e55876fa04d0e6048d49859b`; package `0.7.2`                | Candidate duy nhất cho optional blur/static background; chưa được phép rollout               |

Audit source của Track Processors `0.7.2` cho thấy candidate dùng
`@mediapipe/tasks-vision@0.10.14`, mặc định lấy WASM từ jsDelivr và model theo URL
`storage.googleapis.com/.../latest/...`; `segmentForVideo` chạy đồng bộ và source upstream ghi rõ
có thể block event loop hàng chục đến khoảng 100 ms. Wrapper chọn
`MediaStreamTrackProcessor`/`MediaStreamTrackGenerator` khi có, nếu không fallback sang hidden
canvas + `captureStream()`/`requestAnimationFrame`, và có lifecycle cleanup riêng. Các đặc điểm này
là lý do effect không được bật chỉ vì demo chạy.

## Decision

### 1. Progressive enhancement và ranh giới authority

Phase 4 phải hoàn thành đường tham gia/lớp học với `effect=None`. Camera, effect, speaker selection,
original sound và video chất lượng cao đều là progressive enhancement; không mục nào là điều kiện
để gọi Join nếu server cho phép listen-only/audio-only.

Prejoin state, device/effect/audio-mode/layout/pin choice chỉ tồn tại trong memory của tab và exact
RoomInstance hiện tại. Không ghi device ID, device label, effect, background, audio mode, layout,
pin, credential hoặc provider identity vào `localStorage`, `sessionStorage`, IndexedDB, URL,
history, analytics hoặc error report. Reload, logout, workspace/principal/source change và chuyển
RoomInstance phải purge state; mặc định trở lại `None` và device mặc định của browser.

Core API vẫn sở hữu admission, join attempt, signal authorization, FIFO, rate limit và resync
snapshot. Provider active-speaker/quality là media state tức thời; local pin/layout preference chỉ
là UX state, không cấp grant hoặc đổi role. Không thay đổi `CanPublishData=false`.

### 2. Prejoin là state machine local, không capture khi render

Route/prejoin component render ở trạng thái `idle` và **không** gọi `getUserMedia`, không mount
prefab nào tự tạo preview track và không cấp join credential. Người dùng phải kích hoạt action có
nhãn rõ như “Kiểm tra camera và micro” hoặc từng nút “Bật bản xem trước”/“Kiểm tra micro”. Chỉ sau
action đó browser mới hiện permission prompt và tạo local track.

State machine tối thiểu:

```text
idle -> probing -> preview-ready
idle -> probing -> recoverable-error
preview-ready -> switching-device -> preview-ready/recoverable-error
preview-ready/recoverable-error -> joining
any local state -> cleaning-up -> idle
```

`joining` chỉ bắt đầu sau khi người dùng explicit submit Join và server chạy authority flow; probe
local không reserve capacity, không tạo ParticipantSession và không kết nối LiveKit room. Các trạng
thái lobby `waiting`, `admitted`, `denied`, `cancelled`, `meeting-ended`, `provider-unavailable` và
`timeout` là server projection riêng; client không được kết nối provider trước admission.

Nếu tái dùng LiveKit `PreJoin`, implementation phải bọc nó trong TutorHub state machine, đặt
`persistUserChoices={false}`, camera/microphone mặc định off và không mount phần tạo track trước
action người dùng. Ưu tiên dùng hooks/device primitives nhỏ thay vì nhận nguyên prefab behavior.

Sau permission, UI có camera preview, mic meter, input selector và speaker-test action. Trước
permission chỉ được hiện nhãn generic do browser cung cấp; không cố fingerprint hay khôi phục label.
`devicechange` làm rescan bounded khi prejoin/room đang mở, giữ selection nếu còn hợp lệ, nếu không
chuyển về browser default và báo trạng thái. Camera/effect lỗi không tự bật lại camera.

### 3. Error taxonomy, audio output và autoplay

DOM exception không được trả nguyên văn lên API/log. Client map vào stable bounded codes:

| Stable code                                   | Browser evidence thường gặp                                                          | Recovery và ảnh hưởng Join                                                       |
| --------------------------------------------- | ------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| `media_permission_denied_or_blocked`          | `NotAllowedError`; có thể do user, OS, browser policy hoặc insecure context          | hướng dẫn theo browser/OS; retry chỉ sau action; listen-only nếu policy cho phép |
| `media_device_not_found`                      | `NotFoundError`                                                                      | rescan/default device; audio-only hoặc listen-only                               |
| `media_device_busy_or_unreadable`             | `NotReadableError`                                                                   | đóng app/tab đang giữ device, retry; không chặn listen-only                      |
| `media_constraints_unavailable`               | `OverconstrainedError`                                                               | bỏ exact device/constraint, retry với default/relaxed profile                    |
| `media_probe_aborted`                         | `AbortError` hoặc generation cũ bị hủy                                               | cleanup rồi retry; không log raw cause                                           |
| `media_capture_unsupported_or_policy_blocked` | API/secure-context/Permissions-Policy thiếu; `SecurityError`/`TypeError` tùy browser | feature-off và browser guidance; listen-only nếu được phép                       |
| `audio_output_selection_unsupported`          | thiếu `selectAudioOutput` hoặc `setSinkId`                                           | dùng output mặc định; không chặn Join                                            |
| `media_playback_blocked`                      | `play()` reject hoặc `AudioContext` suspended do autoplay policy                     | hiện action “Phát âm thanh”; không auto-loop retry                               |
| `media_device_unknown`                        | lỗi còn lại                                                                          | request ID + code bounded; không gửi name/message/stack/device label             |

Không được tuyên bố phân biệt chắc chắn “user denied” với “system blocked” nếu browser chỉ trả cùng
`NotAllowedError`. Guidance chi tiết chỉ hiện khi capability/permission API của browser cho bằng
chứng tương ứng.

Speaker selection chỉ hiện sau explicit user action và feature detection cho cả
`navigator.mediaDevices.selectAudioOutput` và `HTMLMediaElement.setSinkId`. Permission/output ID
không được persist. Nếu unsupported/denied, audio phát qua output mặc định; speaker test có trạng
thái playing/success/blocked/unsupported và nút dừng.

Remote audio/autoplay failure phải giữ classroom shell usable và đưa focus đến một action rõ ràng
để bật âm thanh. Không mô phỏng success trước khi `play()` resolve.

### 4. Hai audio mode, không thêm Krisp trong MVP

P4 có đúng hai profile client-side:

1. `speech` là mặc định. Chỉ request `echoCancellation=true`, `noiseSuppression=true` và
   `autoGainControl=true` khi browser khai báo supported constraint.
2. `original_sound` là lựa chọn explicit cho nhạc/nội dung cần dynamic range. Nó request ba
   constraint trên bằng `false`, kèm cảnh báo dùng tai nghe để tránh echo/feedback.

Sau `getUserMedia`/`applyConstraints`, UI và diagnostics chỉ dựa vào `track.getSettings()` để mô tả
giá trị thực tế; không hứa mọi browser/device tuân cùng một cách. `OverconstrainedError` phải quay về
profile relaxed/browser default mà không làm mất Join. Chuyển profile trong room dùng track
replacement/lifecycle của LiveKit và không tạo đồng thời hai microphone track ngoài thời gian
bounded cần cho switch.

Không thêm direct DSP, MediaPipe audio hoặc LiveKit Krisp trong P4 MVP. Krisp cần package/model,
browser/support check, entitlement/cost, privacy và performance gate riêng; WebRTC audio processing
đủ làm baseline hiện tại. ADR mới chỉ được xem lại quyết định này khi speech metrics thật chứng minh
nhu cầu.

### 5. Dùng LiveKit primitives trong TutorHub shell, không fork prefab

P4-05 tiếp tục dùng package đã pin `@livekit/components-react@2.9.23` và
`livekit-client@2.20.1`. Có thể tái dùng hooks/primitives `ParticipantTile`, `GridLayout`/
`useGridLayout`, `usePagination`, `useVisualStableUpdate`, focus-layout primitives và track attach,
nhưng TutorHub sở hữu:

- layout state machine và page capacity;
- stable participant ordering, local pin và focus restoration;
- toolbar/roster/announcement semantics;
- server capability projection và degraded mode;
- test fixture, style và accessibility contract.

Không fork LiveKit Meet, không nhập nguyên `VideoConference`, không dùng component/provider state
làm business authority. `LiveKitRoom` không được remount chỉ để đổi layout; credential và exact
RoomInstance lifecycle vẫn theo ADR-0030.

LiveKit adaptive stream/dynacast được bật theo provider capability. Video element phải attach theo
official track API để visibility/size phản ánh subscription. Audio của participant hợp lệ được ưu
tiên; video chỉ subscribe/attach cho current page, main stage và bounded rail. Hidden/off-page video
không tiếp tục render vô hạn.

### 6. Ba layout mode và profile 2/5/25/50

P4 có ba user-visible mode: `grid`, `active_speaker`, `presentation`. Lựa chọn chỉ giữ trong exact
RoomInstance memory và không đổi khi provider tự báo active speaker.

#### 6.1 Grid

Grid không reorder theo active speaker. Canonical order dùng server roster sequence; tie hiếm dùng
opaque participant key, không dùng display name/client time/provider identity. Participant mới append;
duplicate/out-of-order event được fold theo projection version. Leave/unpublish chỉ compact khi cần;
không di chuyển tile chỉ vì audio activity.

Capacity ban đầu, là **hằng số sản phẩm TutorHub có thể tune sau evidence**, không phải behavior của
Zoom/Meet:

| Fixture        | 1280 x 720 target | Hard bound trên desktop lớn | 320 CSS px hoặc 200% reflow          |
| -------------- | ----------------- | --------------------------- | ------------------------------------ |
| 2 participant  | 2 tile/page       | 2                           | tối đa 2 tile/page                    |
| 5 participant  | 5 tile/page       | 5                           | tối đa 4 tile/page                    |
| 25 participant | 12 tile/page      | tối đa 16 video tile/page   | tối đa 4 tile/page + paged roster    |
| 50 participant | 12 tile/page      | tối đa 16 video tile/page   | tối đa 4 tile/page + paged roster    |

CSS Grid/container query chọn hàng/cột trong bound trên. Không attach hơn 16 camera video element
đồng thời trong grid. Page change bằng action rõ, có số trang, không swipe-only. Khi page biến mất do
leave, clamp về page hợp lệ gần nhất và restore focus tới page control hoặc tile ổn định.

#### 6.2 Active speaker

Active-speaker mode có một main stage và rail tối đa 6 participant; viewport hẹp dùng main stage +
drawer. Local pin luôn thắng auto speaker cho main stage. Auto speaker chỉ đổi khi:

- candidate liên tục dominant ít nhất `speaker_enter_ms=800`;
- stage hiện tại đã giữ ít nhất `speaker_min_hold_ms=2500`;
- silence giữ stage cũ `speaker_silence_release_ms=1500` trước fallback.

Ba giá trị trên là initial tunable product constants được test bằng fake clock; **không** được ghi là
benchmark fact của đối thủ. Speaker event không đổi DOM/focus order và không tạo screen-reader live
announcement liên tục. Nếu pin target rời/unpublish, clear pin và chọn deterministic fallback.

#### 6.3 Presentation

Khi screen share thật bắt đầu, `presentation` có một share stage và rail tối đa 6; camera stage/video
subscription còn lại bị bound. Trước transition, client lưu in-memory `previousMode`, page, local pin
và focused control. Share stop/cancel, presenter leave hoặc track end phải:

1. detach/cleanup share track đúng một lần;
2. restore exact mode/page/pin/focus nếu target còn hợp lệ;
3. nếu không, fallback về grid page chứa local participant hoặc page 1 và focus layout control.

Presenter đổi không làm mất toolbar. Screen share permission phải được yêu cầu lại bằng explicit
browser picker mỗi lần; không persist permission/source. Local pin khác moderated spotlight; remote
spotlight nếu thêm sau thuộc P4-07 contract riêng và không được suy từ provider metadata.

### 7. Degrade layout có thứ tự và không hy sinh audio/control

Khi resource/network kém, thứ tự classroom shell là:

1. giảm số video đang visible/subscribe trong page;
2. chuyển sang bounded page/rail và hạ remote video quality;
3. dừng remote camera video ngoài stage nhưng giữ audio/roster/control;
4. audio-only nếu video vẫn không ổn định.

Presentation share được ưu tiên hơn camera video nhưng không hơn audio/control. Client không được tự
tăng server quota hoặc subscribe toàn bộ 50 video. P4-11 phải công bố profile thực đo; nếu 50 không
đạt, deployment cap được hạ thay vì giả lập PASS.

### 8. Hand raise là authoritative FIFO có snapshot/resync

P4-06 gửi typed command qua Core API. Server reauthorize exact tenant/RoomInstance/
ParticipantSession và gán monotonically increasing `signal_sequence` cùng server `accepted_at`.
Mỗi participant có tối đa một active hand state. Canonical queue sort theo sequence; client time,
arrival order hoặc display name không tham gia FIFO.

- Raise/lower dùng stable idempotency key. Raise lặp trả cùng active state; lower lặp trả trạng thái
  đã hạ, không tạo queue entry mới.
- Participant chỉ tự hạ tay mình. Actor có `media.moderate` mới lower-one/lower-all; target và
  authority reload tại mutation.
- Hand giữ active tới self-lower, moderator lower, participant left/removed hoặc RoomInstance
  terminal. Active speaker không tự hạ tay và hand không reorder video grid.
- Snapshot có projection version + last sequence. Client fetch khi connect/reconnect/tab resume,
  sau sequence gap hoặc sau command timeout; snapshot thắng packet/event cũ.
- `403`, `409`, `429`, offline và retry có feedback typed. Bounded display name được render; email,
  provider identity và ParticipantSession ID không được đưa vào announcement/telemetry.

Initial rate limit, là TutorHub safety constant cần stress-test 25/50 người:

- self raise/lower: tối đa 6 mutation/actor/60 giây và 120/RoomInstance/60 giây;
- moderator lower-all: tối đa 6/actor/60 giây;
- storage/rate-limit failure trả `503`; vượt limit trả `429` + bounded `Retry-After`.

Các limit phải chạy cross-Core-API-instance; process-local map không hợp lệ. Hand state không phải
attendance/grade và không tạo audit noise; durable moderation action vẫn theo allowlist ADR-0030.

### 9. Reaction dùng allowlist, TTL và bounded grouping

Payload API chỉ nhận enum:

```text
thumbs_up, clap, heart, celebrate, laugh, surprised
```

Glyph/illustration là presentation; enum, accessible name và i18n text mới là contract. Server gán
opaque event ID, sequence, `accepted_at` và `expires_at`. TTL là **10 giây từ server accepted time**;
con số này lấy 10 giây của Zoom làm benchmark dễ hiểu nhưng là quyết định TutorHub độc lập. Snapshot
chỉ trả reaction chưa hết TTL; expired reaction không trở thành chat, attendance, grade hoặc audit.

Projection grouping:

- reaction cùng enum trong cửa sổ server 750 ms được gộp một cluster và count;
- tối đa 3 cluster animation cùng lúc; count hiển thị `99+` thay vì tạo một DOM node/event;
- cluster vượt visual cap chỉ cập nhật bounded summary/count, không tạo animation hoặc hàng đợi DOM
  không giới hạn;
- TTL không kéo dài vô hạn do thêm reaction mới: mỗi cluster giữ expiry mới nhất nhưng một event
  riêng không sống quá 10 giây;
- reduced-motion bỏ travel/burst animation nhưng giữ icon + count + text status.

Initial cross-instance limits:

- actor burst tối đa 3 reaction/5 giây và hard limit 20/60 giây;
- aggregate tối đa 100/RoomInstance/5 giây;
- unknown enum/malformed payload bị reject trước fan-out; `429` có bounded `Retry-After`, storage
  failure trả `503`.

Các số này là initial TutorHub safety constants nhằm giữ 50-person storm bounded, không phải claim
về competitor. P4-06 được phép hạ limit sau usability/load evidence nhưng không được tăng qua hard
ceiling nếu chưa review ADR/security.

Screen reader không nghe từng remote reaction. Client gom summary tối đa một lần/2 giây, ví dụ
“5 người đã vỗ tay”, không đọc danh sách tên. Action của chính người dùng có status riêng ngay sau
server ACK. Hand queue announcement chỉ phát cho own action/own queue-position change hoặc explicit
roster navigation; active-speaker/reaction animation không spam assertive live region.

### 10. Effect baseline `None`; một optional processor path duy nhất

P4 MVP luôn ship `None`. Optional effect scope chỉ gồm `blur` và 3-4 curated static backgrounds.
Không có beauty/makeup/avatar/sticker, generated/video/user-uploaded background. Chọn effect không
tự bật camera; nếu camera off, UI lưu choice trong current component memory nhưng chỉ start processor
sau explicit camera action.

Nếu và chỉ nếu rollout gates ở phần 11-13 đạt, candidate duy nhất là
`@livekit/track-processors@0.7.2` qua LiveKit TrackProcessor lifecycle. Không viết compositor trực
tiếp bằng MediaPipe, không chạy song song native `backgroundBlur` path và không thêm Krisp. Direct
MediaPipe Image Segmenter bị loại khỏi P4 vì sẽ tạo processor/lifecycle thứ hai trong khi official
web guide xác nhận `segmentForVideo` đồng bộ và có thể block UI thread.

Package/model/WASM phải được pin cùng nhau. Runtime **không** được dùng URL mặc định jsDelivr,
Google Storage `latest` hoặc CDN khác. Trước production candidate phải:

1. self-host exact WASM/model/background assets trên origin do TutorHub kiểm soát;
2. lưu version, license/NOTICE, provenance và SHA-256 manifest trong repository/release artifact;
3. khóa CSP không cho runtime fallback sang remote asset;
4. audit network chứng minh camera frame/token/device label/raw error không rời browser tới bất kỳ
   processor/asset endpoint nào;
5. hoàn thành privacy/telemetry/consent review; nếu không chứng minh được network/telemetry boundary
   thì effect giữ off;
6. xác minh V1 background provenance trước khi dùng; thiếu provenance thì tạo/licence asset mới.

Effect có deployment subfeature/kill switch riêng (target name `classroom_media_effects`), compiled
default và deployment clamp đều `false`. Parent `classroom_media_rooms` off luôn ép effect off.
Tenant/client không bypass clamp. Dependency không được thêm vào production package trước isolated
bundle/CSP/license/performance review.

### 11. Performance gate, auto-disable và cleanup

Không có số đo hardware/browser nào được coi là PASS trong ADR này. Các ngưỡng sau là rollout gate
ban đầu, phải đo lại bằng exact build ở P4-05/P4-11:

| Tier/profile          | Gate tối thiểu trong sample 120 giây                                                                     |
| --------------------- | -------------------------------------------------------------------------------------------------------- |
| Standard, 720p target | processed video >=24 FPS; p95 processor <=30 ms/frame; effect-attributable long-task time <=1% wall time |
| Low-end, 540p target  | processed video >=20 FPS; p95 processor <=40 ms/frame; audio/control không drop                          |
| Fallback, 360p/15     | processed video >=12 FPS; join/control usable; nếu không đạt thì camera off/audio-only                   |

Cold asset load, warm load, time-to-preview, bundle/model/WASM bytes, long task, processor time,
memory trend, received bitrate và subscribed-video count phải được ghi cùng browser/OS/hardware.
CPU/GPU chỉ báo nếu measurement API tin cậy; không suy số từ Task Manager cảm tính.

Trong runtime effect tự chuyển về raw `None` khi ba cửa sổ 5 giây liên tiếp có processed FPS <12,
p95 processing >50 ms, processor error lặp hoặc context/model mất. Người dùng nhận thông báo
non-blocking và có retry explicit. Nếu raw camera tiếp tục quá tải/network degraded, thứ tự là:

```text
effect off -> 360p/15fps -> camera off/audio-only
```

Không tự bật lại effect trong cùng incident. Reconnect/recovery instance chỉ reattach processor sau
new raw track + capability check; lỗi quay về `None`.

Mỗi leave/unmount/logout/workspace/principal/source/RoomInstance change, failed switch và aborted
probe phải idempotently:

- stop toàn bộ owned local track và captured fallback track;
- detach/destroy TrackProcessor/transformer và close MediaPipe segmenter;
- cancel RAF/timer/video-frame callback, terminate worker nếu có;
- remove hidden canvas/video, close `VideoFrame`/`ImageBitmap`/AudioContext và revoke object URL;
- bỏ `devicechange`/room listeners và invalidate async generation cũ.

Gate cleanup: sau 10 vòng enable/switch/disable và sau leave không còn live capture track,
device indicator phải tắt, không còn owned canvas/worker/timer/listener; memory sau GC-capable test
phải trở về trong 10% baseline hoặc có browser-specific bound được review. Heap API thiếu không được
biến thành tuyên bố “không leak”; phải dùng observable resource assertions cộng soak test.

### 12. CSP, Permissions Policy và privacy

Media route phải chạy secure context và không được nhúng trong third-party iframe. Header target:

- `Permissions-Policy`: camera, microphone, display-capture và speaker-selection chỉ cho `self`;
- `script-src`, `worker-src`, `media-src`, `img-src` và `connect-src` dùng exact self/LiveKit origins
  cần thiết, không wildcard và không processor CDN;
- model/WASM/worker là versioned same-origin immutable asset; CSP/offline/404/hash mismatch đều fail
  về `None`, không retry vô hạn;
- screen capture luôn qua browser picker/action mới, không persist grant/source.

Không có camera/screen/audio frame đi qua Core API. Diagnostics chỉ nhận typed code, stage,
duration, coarse settings/quality và declared browser profile; không nhận device label/ID, raw
exception/stack, token, SDP/ICE, IP hoặc media. Background/effect choice không được dùng làm
profiling dimension.

### 13. Accessibility là rollout gate, không phải polish

Toolbar dùng WAI-ARIA toolbar pattern: Tab đi giữa logical regions, arrow key đi trong toolbar,
visible focus luôn có; nút Leave tách khỏi media toggles. Không chiếm global Space bắt buộc vì có thể
xung đột screen reader/push-to-talk/scroll. Mọi toggle có accessible name + pressed/state text;
mic/camera/effect/quality không chỉ dùng màu.

DOM focus order theo canonical roster/layout control, không đổi theo visual active speaker. Local
pin/page/layout/share transition phải restore focus. 320 CSS px và 200% zoom giữ Join/Leave/mute/
camera/layout/roster usable, dùng drawer thay vì horizontal page overflow. Forced colors có border/
text state; reduced motion bỏ tile/reaction animation. Screen reader không được nghe continuous
active-speaker/quality update.

Status/live-region dùng `polite` và batching mặc định; chỉ lỗi ngăn action hiện tại mới cần alert.
Shared-screen pixels không được mô tả là screen-reader-readable; controls, presenter label, chat/
roster và text artifact đi kèm vẫn phải accessible.

Automated Axe/keyboard/focus/fake-media tests là bắt buộc nhưng không tự thay thế toàn bộ manual
NVDA/device acceptance. P4-11 phải ghi rõ tool/version/browser/OS và người thực hiện cho manual gate
được yêu cầu.

### 14. Rollout và những gate chưa được xác minh

ADR được `Accepted` để P4-03/P4-05/P4-06 có contract implementation ổn định; nó **không** tuyên bố
effect/browser/hardware acceptance đã PASS.

Rollout theo bốn nấc:

1. `None` only: prejoin/layout/signals chạy với media effect subfeature force-off.
2. Internal isolated pilot: exact pinned/self-hosted assets, synthetic/test account, không shared
   end-user activation.
3. Small tenant canary: chỉ browser/hardware profile đã đạt, kill switch và fallback `None` đã probe.
4. Declared pilot: chỉ sau P4-11 matrix/load/outage và P4-12 exact acceptance.

Các gate còn mở phải được ghi là unverified/deferred, không được suy từ source audit hoặc prototype:

- Safari/macOS và Chrome/Edge/Firefox trên exact declared versions;
- máy low-end và standard thật ở 360p/540p/720p, thermal/long-session và unplug/device-busy cases;
- speaker-output/autoplay parity từng browser;
- manual NVDA, 200% zoom, forced colors và reduced motion trên production route;
- exact model/WASM/background license, provenance, hash, CSP/offline và network/privacy audit;
- effect performance/cleanup thresholds và five-minute/ten-cycle soak;
- 25/50 participant hand/reaction storm, real video subscription/load và provider quota approval.

Nếu bất kỳ gate effect/privacy/license/browser nào không đạt, Phase 4 vẫn ship `None`; không hạ
join/layout/signal gate để giữ effect. Feature off/kill switch không thay server authorization test.

## Consequences

- P4-03 có prejoin/device/error/audio contract rõ và không vô tình capture hoặc persist thiết bị.
- P4-05 có một layout controller deterministic trên LiveKit primitives, bounded video subscription
  và restore semantics cụ thể cho screen share.
- P4-06 có exact hand/reaction product contract để thiết kế API, rate-limit, snapshot và a11y test;
  `CanPublishData=false` không bị mở lại.
- Blur/static background có một candidate duy nhất nhưng không nằm trên critical join path; privacy,
  supply-chain, browser và performance failure đều fail về `None`.
- Không thêm Krisp/direct MediaPipe/processor stack thứ hai trong MVP, giảm bundle và lifecycle risk.
- P4-11 phải làm phần hardware/browser/manual/load còn thiếu; source audit không được ghi thành PASS.

## Alternatives rejected

- **Capture camera/mic khi prejoin render:** vi phạm consent, có thể bật device indicator trước khi
  người dùng hiểu hành động và làm Join phụ thuộc preview.
- **Persist device/effect bằng LiveKit PreJoin mặc định hoặc localStorage:** tăng privacy/fingerprint
  risk và làm state cũ rò qua workspace/principal/RoomInstance.
- **Fork LiveKit Meet hoặc dùng prefab làm product authority:** kéo theo layout/toolbar/state không
  thuộc TutorHub và không thay Core API authorization.
- **Reorder grid liên tục theo active speaker:** gây tile jump, focus instability và screen-reader
  noise; active speaker chỉ điều khiển stage có hysteresis.
- **Render/subscribe cả 25-50 camera cùng lúc:** không có evidence thiết bị/network; bounded page/
  rail + adaptive subscription an toàn hơn.
- **Đưa hand/reaction qua client DataChannel:** client có thể forge actor/order, packet không là
  authoritative resync path và trái ADR-0030.
- **Direct MediaPipe compositor song song Track Processors:** nhân đôi processor/lifecycle/test
  matrix; web API hiện có synchronous main-thread risk.
- **Bật CDN/model `latest`:** không reproducible, phá CSP/offline/rollback và supply-chain review.
- **Thêm Krisp ngay:** chưa có need/cost/privacy/browser/performance evidence; native WebRTC audio
  profile đủ cho MVP baseline.
- **Buộc effect đạt mới cho Join:** làm optional visual enhancement hạ reliability của core flow.

## Implementation and acceptance mapping

- **P4-03:** explicit probe/no capture on render; no persistence; error taxonomy; speaker/autoplay;
  speech/original-sound; cleanup; listen-only path.
- **P4-04:** waiting state chỉ là server admission projection; không connect provider trước admit;
  effect/device state không đổi capacity/authority.
- **P4-05:** custom deterministic grid/active-speaker/presentation controller, page/rail bounds,
  hysteresis, adaptive subscription, presentation restore, cleanup và effect flag boundary.
- **P4-06:** Core API signal commands, FIFO/version/snapshot, exact allowlist/TTL/grouping/rate
  limit, unknown payload rejection và bounded announcements.
- **P4-11:** exact browser/OS/hardware/performance/accessibility/load/outage matrix cùng unverified
  manual gates ở phần 14.

## Evidence and official sources

Các nguồn dưới đây được đọc ngày 2026-08-09. Product Help là mutable; implementation phải pin
package/revision và P4-11 phải ghi exact browser/provider version thực đo.

### Product patterns

- Google Meet: [connect video/audio and green room](https://support.google.com/meet/answer/10409699?hl=en),
  [layouts](https://support.google.com/meet/answer/10550593?hl=en),
  [hand raise](https://support.google.com/meet/answer/10159750?hl=en),
  [reactions](https://support.google.com/meet/answer/13151720?hl=en),
  [background/effects](https://support.google.com/meet/answer/10058482?hl=en) và
  [screen-reader guidance](https://support.google.com/meet/answer/15738543?hl=en).
- Zoom: [prejoin video preview](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0061118),
  [meeting layouts](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063672),
  [hand raise](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0068290),
  [reactions/non-verbal feedback](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0063325)
  và [accessibility FAQ](https://www.zoom.com/en/accessibility/faq/).

### Standards and browser behavior

- W3C: [Media Capture and Streams](https://www.w3.org/TR/mediacapture-streams/),
  [Audio Output Devices API](https://www.w3.org/TR/audio-output/),
  [Media Capture Transform](https://www.w3.org/TR/mediacapture-transform/) và
  [Screen Capture](https://www.w3.org/TR/screen-capture/).
- Mozilla: [`MediaStreamTrackProcessor` limited availability](https://developer.mozilla.org/en-US/docs/Web/API/MediaStreamTrackProcessor/MediaStreamTrackProcessor),
  [`applyConstraints`](https://developer.mozilla.org/en-US/docs/Web/API/MediaStreamTrack/applyConstraints)
  và [`getCapabilities`](https://developer.mozilla.org/en-US/docs/Web/API/MediaStreamTrack/getCapabilities).
- WebKit: [Safari WebRTC/media capture/privacy](https://webkit.org/blog/7763/a-closer-look-into-webrtc/)
  và [MediaStreamTrack processing in Safari 18](https://webkit.org/blog/15443/news-from-wwdc24-webkit-in-safari-18-beta/).

### LiveKit and processing candidates

- LiveKit: [PreJoin](https://docs.livekit.io/reference/components/react/component/prejoin/),
  [GridLayout](https://docs.livekit.io/reference/components/react/component/gridlayout/),
  [component building blocks](https://docs.livekit.io/reference/components/react/concepts/building-blocks/),
  [adaptive stream/subscription](https://docs.livekit.io/transport/media/subscribe/),
  [data packet semantics](https://docs.livekit.io/transport/data/packets/) và
  [noise cancellation/Krisp](https://docs.livekit.io/transport/media/noise-cancellation/).
- Official OSS: [`livekit/components-js`](https://github.com/livekit/components-js),
  [`livekit/track-processors-js`](https://github.com/livekit/track-processors-js) và
  [Track Processors releases](https://github.com/livekit/track-processors-js/releases).
- Google AI Edge: [MediaPipe Image Segmenter for web](https://developers.google.com/edge/mediapipe/solutions/vision/image_segmenter/web_js)
  và [official MediaPipe repository](https://github.com/google-ai-edge/mediapipe).

### Accessibility

- W3C: [WCAG 2.2](https://www.w3.org/TR/WCAG22/),
  [Reflow understanding](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html),
  [Focus Order understanding](https://www.w3.org/WAI/WCAG22/Understanding/focus-order.html),
  [ARIA toolbar pattern](https://www.w3.org/WAI/ARIA/apg/patterns/toolbar/) và
  [ARIA alert guidance](https://www.w3.org/WAI/ARIA/apg/patterns/alert/).
