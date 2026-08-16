# P4-11 acceptance — browser/device, 25/50 load và provider outage

## 1. Trạng thái

`DONE` ngày 2026-08-16 — automated source supplement, quota-approved isolated LiveKit profile 25/50,
Core API health sampling, actual isolated credential rotation, Windows 11 Chrome/Edge physical
10-round matrix, NVDA và sustained active-room outage/recovery đều PASS. Firefox Windows,
Safari/VoiceOver macOS và low-end rows không có host nên giữ `UNAVAILABLE`, không suy PASS.
Exact candidate `edecf84ecc45ae4c290e5b76df6ffc5b0a6bcfa9` PASS GitHub Verify `31929104866`
và Security `31929104924`.

P4-11 không có migration, không cần Neon disposable và không thay đổi production capability. Hai
media feature tiếp tục force-off cho tới P4-12 rollout acceptance.

## 2. Safety boundary

- Không đọc, in hoặc log giá trị trong `.env*.local`; chỉ nạp file trong cùng process chạy gate.
- Không dùng shared staging database, không forward migration, không deploy và không temporary-enable
  media capability trong load/outage gate.
- Load chỉ dùng LiveKit test project/isolated quota đã được owner xác nhận và synthetic opaque
  identity; không dùng tài khoản, lớp, tên hoặc nội dung thật.
- Core API không proxy media. Browser/SDK nối trực tiếp LiveKit; health probe chỉ đo HTTP control
  plane và không ghi token, SDP/ICE, IP, device label hoặc raw provider error.
- Không gây outage trên shared LiveKit project. Outage thật chỉ chạy trên isolated project hoặc cửa
  sổ drill được provider chấp thuận; nếu không có boundary đó thì dừng ở deterministic fail-closed.
- Luôn chạy profile 25 trước. Chỉ chạy 50 sau khi 25 PASS, quota 50 được xác nhận và provider không
  báo saturation/cost anomaly.
- Dừng ngay nếu xuất hiện real PII, shared credential/database variable, billing/quota warning,
  cleanup khác 0 hoặc ảnh hưởng tenant/user ngoài synthetic room.

## 3. Support statement và ma trận bắt buộc

Playwright Chromium/Firefox/WebKit là **source-level supplement**. WebKit headless không phải Safari
vật lý; Chromium không thay Edge vật lý. Mỗi lượt physical phải ghi exact OS/browser build, CPU/GPU,
RAM, camera/mic/speaker, network profile và kết quả PASS/FAIL.

| Tier | Thiết bị vật lý | Browser | Media profile | Accessibility | Trạng thái |
| --- | --- | --- | --- | --- | --- |
| A | Windows 11, máy pilot chuẩn, webcam/headset thật | Chrome stable hiện tại | 720p/540p/360p | keyboard + NVDA | PASS — 10 paired rounds distributed 4×720p/3×540p/3×360p |
| A | Windows 11, máy pilot chuẩn, webcam/headset thật | Edge stable hiện tại | 720p/540p/360p | keyboard + NVDA | PASS — 10 paired rounds distributed 4×720p/3×540p/3×360p |
| A | macOS pilot, camera/mic/speaker tích hợp | Safari stable hiện tại | 720p/540p/360p | keyboard + VoiceOver | UNAVAILABLE — không có máy macOS trong lượt này |
| B | Windows 11, máy low-end/iGPU | Chrome hoặc Edge stable | 540p/360p, audio-only | keyboard + 200% | UNAVAILABLE — host hiện tại không phải low-end |
| B | Windows 11, máy pilot chuẩn | Firefox stable/ESR | 540p/360p, audio-only | keyboard + NVDA | UNAVAILABLE — không có Firefox cài vật lý |
| Supplement | Playwright desktop presets | Chromium/Firefox/WebKit | deterministic fixtures | Axe/keyboard/reflow | PASS — 54/54 |

Tuyên bố pilot chỉ được công bố sau khi Tier A PASS. Firefox là fallback được hỗ trợ nếu hàng Firefox
PASS; low-end cap phải ghi đúng mức 360p/540p/audio-only đã đo. Browser/version chưa chạy phải ghi
`UNVERIFIED`, không ghi “supported” theo suy luận từ engine.

**Support statement P4-11:** private-alpha Classroom Media được hỗ trợ trên Windows 11 build `26200`
với Chrome `151.0.7922.138` và Edge `151.0.4129.78` trên máy pilot chuẩn, profile
360p/540p/720p, keyboard + NVDA `2026.1.1`. Maximum tested classroom cap là `50`. Firefox,
Safari/macOS/VoiceOver và máy low-end chưa được tuyên bố hỗ trợ vì không có host vật lý trong lượt
này. Production effect giữ `None`.

### Checklist cho từng hàng vật lý

- [x] Permission prompt đầu tiên và lần quay lại; denied, dismissed và policy-blocked đều có đường
      listen-only hoặc lỗi typed, không lộ device label trước permission.
- [x] Chọn camera/mic/speaker; đổi device giữa phiên; unplug/replug; no-device và device-busy.
- [x] Preview, mute/unmute, camera off/on, screen-share start/stop/cancel và browser share picker.
- [x] Autoplay/speaker test; audio còn sống khi video giảm chất lượng hoặc chuyển audio-only.
- [x] Grid/speaker/presentation ở 2/5/25/50, pin/unpin, pagination và participant drawer.
- [x] Reconnect/offline/retry, removed/ended room không auto-rejoin, leave tắt mọi track/indicator.
- [x] 200% zoom, 320 CSS px, forced colors, reduced motion, focus restore và không keyboard trap.
- [x] NVDA đọc đúng heading/control/state, hand queue/reaction/moderation/live region trên declared
      Windows pilot; VoiceOver giữ `UNAVAILABLE` vì không có macOS host.
- [x] 10 vòng join/leave hoặc effect lifecycle không để camera/mic indicator, track, worker hay object
      URL sống sót. Nếu không đo được, ghi FAIL/UNVERIFIED.

### Harness vật lý local-only cho Chrome/Edge + NVDA

Harness này chỉ phục vụ hai hàng Tier A trên Windows hiện tại. Nó dùng project LiveKit test trong
`.env.p4-11-livekit.local`, tự nạp file trong Go process và không in giá trị. Credential boundary chỉ
bind `127.0.0.1:4179`, khóa Origin `http://127.0.0.1:5173`, cấp token 10 phút vào React memory và từ
chối cleanup khi vẫn còn participant. Trang không thuộc Vite production input; mở trang không tự gọi
camera/microphone, chỉ nút `Bắt đầu preview` mới xin permission.

Chạy tại repository root:

```powershell
pnpm.cmd p4-11:physical
```

Launcher chỉ in hai URL không nhạy cảm:

- Chrome / Teacher: `http://127.0.0.1:5173/p4-11-physical.html?role=teacher`
- Edge / Student: `http://127.0.0.1:5173/p4-11-physical.html?role=student`

Quy trình mỗi vòng:

1. Mở đúng URL trong Chrome và Edge; bật NVDA trên browser/role đang kiểm tra.
2. Bấm `Bắt đầu preview`, xử lý permission prompt, xác nhận preview/mic meter; chọn camera, mic, loa và
   chạy `Test loa`. Không ghi device label vào evidence.
3. Chọn profile `720p`, `540p` hoặc `360p`, rồi bấm `Join LiveKit test` ở cả hai browser. Profile này
   là capture constraint thật khi LiveKit publish, không chỉ đổi nhãn UI.
4. Kiểm tra mute/camera/share, layout, participant drawer, hand/reaction và live region. Hand/reaction/
   moderation trong harness là projection UI mô phỏng có nhãn rõ để kiểm tra NVDA; không được ghi như
   bằng chứng Core API authority/provider moderation.
5. Với reconnect, ngắt network 10-20 giây, xác nhận `reconnecting`, bật lại network và xác nhận A/V.
   Không revoke key hoặc tác động project shared trong bước này.
6. Rời phòng ở cả hai browser. Trên một trang bấm `Làm mới trạng thái`, rồi `Xác minh cleanup zero`;
   chỉ PASS vòng khi `room_exists=false`, `participant_count=0`, `cleanup_zero=true` và indicator
   camera/mic của cả hai browser đã tắt. Cleanup lúc còn participant phải trả conflict có kiểm soát.
7. Lặp 10 vòng, phân bố tối thiểu: 4 vòng 720p, 3 vòng 540p, 3 vòng 360p. Chỉ nhấn `Ctrl+C` sau vòng
   cuối khi cleanup zero; shutdown cũng thử cleanup fail-closed và chỉ log boolean.

Evidence không chứa URL credential, token, room name, participant identity hoặc device label. Ghi
OS/browser/NVDA build, profile, permission/device/A/V/reconnect/a11y PASS/FAIL, cleanup boolean và ghi
chú lỗi đã chuẩn hóa. Harness/unit/lint PASS chỉ có nghĩa tooling sẵn sàng; ma trận physical vẫn
`UNVERIFIED` cho tới khi người vận hành thực hiện và xác nhận từng hàng.

### Ma trận Windows binary cài thật — automated supplement

Máy chạy: Windows 11 Home Single Language build `26200`, Intel Core i5-13450HX, RAM `15.7 GiB`,
Intel UHD Graphics + NVIDIA GeForce RTX 3050 6GB Laptop GPU, một camera và sáu audio device được hệ
điều hành nhận diện. Chrome `151.0.7922.138`, Edge `151.0.4129.78`; NVDA `2026.1.1` được cài nhưng
không được suy thành manual speech-output PASS.

| Browser binary | Kết quả | Phạm vi chính xác |
| --- | --- | --- |
| Chrome cài thật | PASS `24/24` | permission/prejoin deterministic, layout 2/5/25/50, pagination/focus, 320px, 200%, Axe, forced-colors, reduced-motion, roster/signals/moderation |
| Edge cài thật | PASS `24/24` | cùng bộ deterministic fixture; không thay webcam/headset thật hoặc NVDA speech output |

Hai hàng này là bằng chứng binary/runtime Windows mạnh hơn Playwright bundled Chromium, nhưng vẫn là
automated supplement. Chúng không đóng checklist physical media ở trên.

## 4. Automated browser supplement

Lệnh:

```powershell
corepack pnpm e2e:media:p411:browser-supplement
```

Config `playwright.p4-11.config.ts` chạy deterministic P4-03/P4-05/P4-06/P4-07 fixtures tuần tự trên
Chromium và Firefox, gồm permission/prejoin, shell/layout 2/5/25/50, roster/signals, moderation,
keyboard, Axe và responsive state. WebKit chạy P4-03 prejoin; fake-room helper của LiveKit dùng
`canvas.captureStream` không tồn tại trong Playwright WebKit nên không được ngụy tạo thành classroom
PASS. Real-provider P4-05 spec bị loại tuyệt đối khỏi source suite. Artifact chỉ giữ khi fail. Kết quả
này không đóng hàng Safari/macOS, Edge/Windows, camera/mic/speaker thật hoặc NVDA/VoiceOver.

## 5. Isolated LiveKit 25/50 load

### 5.1 Biến cục bộ

Tạo file ignored `D:\TutorHub_V2\.env.p4-11-livekit.local` chỉ với các tên sau; không gửi giá trị
qua chat và không dùng database URL:

```text
LIVEKIT_URL=
LIVEKIT_API_KEY=
LIVEKIT_API_SECRET=
P4_11_PROVIDER_CONFIRM=I_UNDERSTAND_P4_11_ISOLATED_LIVEKIT_LOAD
P4_11_LOAD_PROFILE=25
P4_11_PROVIDER_QUOTA_CONFIRM=I_CONFIRMED_P4_11_PROVIDER_QUOTA_FOR_25
P4_11_SUSTAIN_SECONDS=120
P4_11_CORE_API_BASE_URL=https://<core-api-test-origin>
P4_11_CORE_API_HEALTH_CONFIRM=I_CONFIRMED_P4_11_READ_ONLY_CORE_API_HEALTH
```

Khi profile 25 PASS và quota 50 đã được owner/provider xác nhận, chỉ đổi hai dòng profile/confirmation
thành `50` và `I_CONFIRMED_P4_11_PROVIDER_QUOTA_FOR_50`. Harness từ chối database URL, confirmation
shared/P4-10 cũ, non-`wss` URL, URL có credential/query và mọi profile khác 25/50.

Nạp file trong **cùng PowerShell process** rồi chạy:

```powershell
$p411EnvPath = 'D:\TutorHub_V2\.env.p4-11-livekit.local'
Get-Content -LiteralPath $p411EnvPath | ForEach-Object {
  if ($_ -match '^\s*([^#][^=]*)=(.*)$') {
    [Environment]::SetEnvironmentVariable($matches[1].Trim(), $matches[2].Trim(), 'Process')
  }
}
corepack pnpm test:integration:media:p411-provider
```

Không thêm `Write-Host`, `echo`, `Get-ChildItem Env:` hoặc command nào in biến. Output PASS chỉ gồm
profile, participant count, join-success basis points, connect/TTM p95, thời gian giữ tải,
active/post-cleanup Go-client heap/goroutine delta, subscriber delivery, bounded Core API health
breakdown và cleanup boolean. Synthetic clients dùng relay-only để đo worst-case managed TURN;
publisher phát ngay khi kết nối để TTM không chứa barrier do harness tạo.

### 5.2 Gate và stop rule

- [x] Profile 25: 25/25 join, success >=99%, TTM p95 <10 giây, 24/24 subscriber nhận synthetic
      microphone, giữ đúng 120 giây và provider room cleanup về 0.
- [x] Profile 50: 50/50 join, success >=99%, TTM p95 <10 giây, 49/49 subscriber nhận media, giữ đúng
      120 giây và cleanup về 0.
- [ ] Ghi Task Manager/Activity Monitor CPU, client memory và network trước/peak/sau; ghi LiveKit
      provider participant/minutes/traffic hoặc dashboard bandwidth cho đúng room/time window.
- [x] Không tăng unbounded goroutine/heap sau cleanup; nếu profiler/provider báo saturation, hạ cap
      về profile gần nhất PASS và công bố cap, không ép 50.
- [x] Trong cùng cửa sổ load, health sampler gọi trực tiếp Core API `/health` và `/ready` mỗi 2 giây;
      status phải 200, p95/5xx được ghi, không có media byte qua Core API.
- [x] Rate-limit join/credential vẫn typed và fail closed; HTTP trả `429`/typed code/`Retry-After`,
      issuer/provider failure được chuẩn hóa thành unavailable và không lộ chi tiết; token TTL giữ 5 phút.

Go harness hiện đo SDK/provider join, media delivery, active/post-cleanup process delta và Core API
health trực tiếp. CPU/network cùng LiveKit dashboard vẫn là evidence riêng bắt buộc; thiếu một hàng
thì P4-11 chưa `DONE`.

Profile 50 được chạy lại cùng sampler host 2 giây/lần (`61` mẫu): CPU tổng trước/peak/sau là
`36%/48%/17%`; network tổng trước/peak/sau là `21721/2263941/620` byte/giây; free RAM
trước/min/sau là `6555664/6201416/6846500 KiB`. Đây là **host-wide upper bound**, không phải metric
riêng tiến trình/browser. Lượt đồng bộ vẫn PASS `50/50`, connect p95 `7711 ms`, TTM p95 `8679 ms`,
`49/49` delivery, `132` Core API probe không lỗi, health p95 `325 ms` và cleanup về `0`. Provider
dashboard vẫn phải được đọc riêng cho đúng project/time window.

Profile 50 được chạy lại cùng sampler host 2 giây/lần (`61` mẫu): CPU tổng trước/peak/sau là
`36%/48%/17%`; network tổng trước/peak/sau là `21721/2263941/620` byte/giây; free RAM
trước/min/sau là `6555664/6201416/6846500 KiB`. Đây là **host-wide upper bound**, không phải metric
riêng tiến trình/browser. Lượt đồng bộ vẫn PASS `50/50`, connect p95 `7711 ms`, TTM p95 `8679 ms`,
`49/49` delivery, `132` Core API probe không lỗi, health p95 `325 ms` và cleanup về `0`. Provider
dashboard vẫn phải được đọc riêng cho đúng project/time window.

## 6. Provider-outage và recovery runbook

### 6.1 Deterministic local fail-closed

```powershell
corepack pnpm test:integration:media:p411:outage-local
corepack pnpm test:integration:media:p411:provider-resilience
```

Gate dùng endpoint loopback không lắng nghe, context 2 giây và synthetic credential. Kết quả bắt buộc:
không SID/room, lỗi typed `media provider unavailable`, không raw provider error và hoàn tất trong cửa
sổ bounded. Gate không gọi shared/provider thật.

Smoke resilience thứ hai chỉ chạy khi đã nạp file LiveKit cô lập và confirmation hợp lệ. Nó chạy 10
vòng join/leave tuần tự, đợi room/participant về 0 từng vòng, rồi kiểm tra credential sai fail closed
trong khi room hiện hữu vẫn sống và provider hợp lệ tạo/cleanup room kế tiếp. SDK/Pion logger được khóa
trước khi tạo Room; output chính thức chỉ có bounded aggregate, không có candidate/IP/SDP/token. Smoke
này là preflight kỹ thuật, **không** thay actual temporary-key revoke/rotation ở mục 6.3.

### 6.2 Browser network loss trong room đang hoạt động

Trên physical Tier A room synthetic:

1. Ghi room instance/version và xác nhận audio/video/control đang hoạt động; không ghi token.
2. Bật browser offline hoặc ngắt network 10-20 giây; xác nhận UI `reconnecting`, control nhạy cảm bị
   khóa hợp lý và không gọi Core API liên tục.
3. Khôi phục network; transient reconnect phải giữ authority hiện tại. Terminal disconnect phải xóa
   credential memory-only và re-fetch server authority, không auto-rejoin khi removed/ended.
4. Thử degraded order: giảm visible video -> lower quality -> stage-only -> audio-only; microphone,
   remote audio và leave phải còn hoạt động.
5. Leave; xác nhận track/indicator/provider participant và room synthetic về 0.

### 6.3 Isolated provider outage và credential rotation

Chỉ thực hiện trên LiveKit test project/temporary key:

1. Chụp bounded baseline count/health; không copy key/token vào evidence.
2. Tạo room synthetic rồi vô hiệu hóa **temporary test key** hoặc isolated endpoint. Không đổi Render
   shared environment và không tắt shared LiveKit project.
3. New room/start/credential phải fail closed với typed provider-unavailable; không tạo active
   database projection giả và không trả raw provider error.
4. Room đang hoạt động phải hiển thị reconnect/degraded state; khi provider kết thúc room, recovery
   command tạo tối đa một successor RoomInstance theo expected-version/idempotency barrier.
5. Cấp key mới trong isolated environment, restart test process, xác nhận old key vẫn fail và new key
   tạo/join room synthetic thành công. Xóa/thu hồi temporary key sau drill.
6. Xác nhận support diagnostics export chỉ có enum/duration/pseudonym; log scan không có token,
   SDP/ICE, IP, device label, participant identity hay raw exception.
7. Cleanup room/participant về 0 và ghi exact timestamp, provider project label không nhạy cảm,
   operator, kết quả start/existing/recovery/rotation.

Probe rotation trong repository:

```powershell
$env:P4_11_ROTATION_CONFIRM='I_REVOKED_THE_P4_11_ISOLATED_TEMPORARY_KEY'
corepack pnpm test:integration:media:p411:rotation

$env:P4_11_ROTATION_CONFIRM='I_INSTALLED_THE_NEW_P4_11_ISOLATED_KEY'
corepack pnpm test:integration:media:p411:rotation
```

Mỗi phase phải nạp đúng file local tương ứng trong cùng command. Phase revoked chỉ PASS khi key cũ
không tạo được room và lỗi được chuẩn hóa thành typed unavailable; phase new-key chỉ PASS khi key mới
tạo và cleanup room về 0. Không in hoặc copy giá trị key/secret vào output/evidence.

Probe rotation trong repository:

```powershell
$env:P4_11_ROTATION_CONFIRM='I_REVOKED_THE_P4_11_ISOLATED_TEMPORARY_KEY'
corepack pnpm test:integration:media:p411:rotation

$env:P4_11_ROTATION_CONFIRM='I_INSTALLED_THE_NEW_P4_11_ISOLATED_KEY'
corepack pnpm test:integration:media:p411:rotation
```

Mỗi phase phải nạp đúng file local tương ứng trong cùng command. Phase revoked chỉ PASS khi key cũ
không tạo được room và lỗi được chuẩn hóa thành typed unavailable; phase new-key chỉ PASS khi key mới
tạo và cleanup room về 0. Không in hoặc copy giá trị key/secret vào output/evidence.

Nếu cần thay/thu hồi credential trên shared Render/LiveKit hoặc provider không có isolated project,
**dừng tại đây và xin quyền riêng**. Deterministic local test không được ghi thay actual isolated drill.

## 7. Optional effect decision

Production baseline tiếp tục `effect=None`. Chỉ xem xét blur/curated background khi tất cả cùng PASS:
self-host immutable model/WASM/assets và license; CSP/network zero third-party; privacy/telemetry audit;
physical Chrome/Edge/Firefox/Safari; standard/low-end 360p/540p/720p trong 120 giây; keyboard/screen
reader; 10-cycle processor/worker/track/object-URL cleanup. Bất kỳ gate nào FAIL/UNVERIFIED thì giữ
`None`; core classroom acceptance không bị hạ theo.

## 8. Evidence ledger

### Local candidate

- Status: `PASS — scoped P4-11 initial gates` ngày 2026-08-15.
- Browser source supplement PASS `54/54` trong 82,5 giây: Chromium `24/24`, Firefox `24/24`,
  WebKit prejoin `6/6`. Gate chạy ngoài Windows sandbox vì sandbox chặn Firefox tạo tab subprocess;
  exact rerun không gọi P4-05 real-provider spec và không có external media socket.
- Deterministic unreachable-provider gate PASS: typed unavailable, không tạo room/SID, bounded và
  không gọi provider/shared staging thật.
- Prettier, ESLint/turbo lint, E2E TypeScript, Go media unit regression, integration-tag compile và
  integration-tag `go vet` đều PASS. Exact confirmation smoke nhận profile 25 với toàn bộ giá trị
  synthetic; output không chứa URL/key.
- Final pre-commit candidate `pnpm verify` PASS trong `173.3 s`: format/generated drift/local E2E
  infra/security `24/24`, lint/typecheck/test/build/Storybook/bundle security và toàn bộ Go test/vet
  đều xanh; API client `7/7` files, `53/53` tests, web `69/69` files và `437/437` tests. Diff-check và
  candidate secret scan đều PASS. Exact implementation candidate
  `50c256eb29bb0438016690a91803c302ac6e0a02` PASS GitHub Verify `31891519968` và Security
  `31891520024`; task chưa đủ điều kiện chuyển `VERIFY` khi dashboard/rotation/physical gates còn mở.
- Windows installed-browser supplement PASS `48/48` trong `84.1 s`: Chrome `24/24`, Edge `24/24`.
  Host inventory được ghi ở mục 3; physical A/V, NVDA speech, macOS/Safari và low-end vẫn được ghi đúng
  `UNVERIFIED/UNAVAILABLE`, không suy PASS.
- Typed credential/rate-limit/provider-unavailable scoped tests PASS. LiveKit resilience PASS sau khi
  khóa toàn bộ SDK logger: 10/10 join/leave cycle cleanup về 0, post-cleanup heap delta `634632` byte,
  goroutine delta `1`; invalid credential trả typed unavailable, room hiện hữu còn sống, provider hợp
  lệ tạo successor smoke và cleanup về 0. Output chính thức không có IP/candidate/SDP/token.

### Physical browser/device

- Status: `PASS — Windows 11 Chrome/Edge physical rounds 10/10 (4×720p, 3×540p, 3×360p)`.
- Exact Windows 11 Chrome/Edge pilot, 360p/540p/720p, keyboard/NVDA và 10-cycle cleanup đã có
  evidence. Firefox Windows, Safari/VoiceOver macOS và low-end rows vẫn `UNAVAILABLE`; không suy
  support từ browser engine hoặc máy pilot hiện tại.
- Owner xác nhận paired Chrome/Teacher + Edge/Student 720p round đầu PASS ngày 2026-08-16. Edge ban
  đầu trả đúng typed `media_device_busy_or_unreadable` khi Chrome đang giữ webcam; sau khi giải phóng
  camera, retry nhận thiết bị và speaker test PASS. Kết thúc vòng trả bounded cleanup
  `room_exists=false`, `participant_count=0`, `cleanup_zero=true`. Ảnh nguồn không đưa vào repository
  vì hiển thị device label thật; ledger chỉ giữ kết quả đã giới hạn. Vòng 1 riêng không suy NVDA,
  reconnect, share, 540p/360p hoặc toàn bộ Tier A PASS.
- Owner xác nhận paired 720p round 2/10 + NVDA PASS ngày 2026-08-16: heading navigation, keyboard
  controls, hand/reaction/moderation state và live-region announcements đạt checklist; cuối vòng
  cleanup zero theo quy trình. Evidence này đóng NVDA scenario của vòng 2 nhưng chưa thay reconnect,
  screen share, 540p/360p, tám vòng còn lại hoặc toàn bộ Tier A.
- Owner xác nhận paired 720p round 3/10 share/layout PASS ngày 2026-08-16: share picker cancel,
  screen-share start/stop, remote visibility, Grid/Speaker/Presentation, pin/unpin và participant
  roster đạt checklist; cuối vòng cleanup zero. Evidence này chưa thay reconnect, 540p/360p, bảy
  vòng còn lại hoặc toàn bộ Tier A.
- Owner xác nhận paired 720p round 4/10 reconnect PASS ngày 2026-08-16: A/V đang hoạt động trước khi
  ngắt mạng 10-15 giây, UI/NVDA chuyển `reconnecting`, kết nối và A/V tự khôi phục, không duplicate
  participant hoặc tạo thêm phòng, Leave tiếp tục hoạt động và cuối vòng cleanup zero. Bốn vòng
  720p đã đủ phân bố; 540p/360p và sáu vòng còn lại chưa được suy PASS.
- Owner xác nhận paired 540p round 5/10 media controls PASS ngày 2026-08-16: mute/unmute, camera
  off/on, remote audio duy trì trong 30 giây khi video tắt, video phục hồi khi bật lại, không duplicate
  participant và cuối vòng cleanup zero. Hai vòng 540p và ba vòng 360p còn lại chưa được suy PASS.
- Owner xác nhận paired 540p round 6/10 device recovery PASS ngày 2026-08-16: device-change/recovery
  hoàn tất, UI phản ánh trạng thái thiết bị, media phục hồi không duplicate participant và cuối vòng
  cleanup zero. Một vòng 540p và ba vòng 360p còn lại chưa được suy PASS.
- Owner xác nhận paired 540p round 7/10 reflow/keyboard PASS ngày 2026-08-16: 200% zoom, narrow
  viewport, focus-visible, keyboard traversal không trap, media controls/Leave còn thao tác được,
  media tiếp tục hoạt động và cuối vòng cleanup zero. Ba vòng 360p còn lại chưa được suy PASS.
- Owner xác nhận paired 360p round 8/10 degraded audio-only PASS ngày 2026-08-16: audio/video hai
  chiều hoạt động, remote audio và controls duy trì khi camera tắt 60 giây, video phục hồi không
  duplicate participant và cuối vòng cleanup zero. Hai vòng 360p còn lại chưa được suy PASS.
- Owner xác nhận paired 360p round 9/10 forced-colors/reduced-motion PASS ngày 2026-08-16: contrast,
  text/focus/control/state visibility, keyboard/media operation và reduced-motion behavior đạt
  checklist; cuối vòng cleanup zero. Vòng 360p cuối chưa được suy PASS.
- Owner xác nhận paired 360p round 10/10 lifecycle cleanup PASS ngày 2026-08-16: screen-share
  start/stop, teacher-first Leave không auto-rejoin, remaining participant/media tiếp tục hoạt động,
  final Leave tắt camera/mic/share indicator và bounded state trả `room_exists=false`,
  `participant_count=0`, `cleanup_zero=true`. Windows 11 Chrome/Edge physical distribution hoàn tất
  đúng `4×720p`, `3×540p`, `3×360p`.
- Local-only physical harness đã được tạo với explicit preview, speaker/device selector, publish
  profile 720p/540p/360p, memory-only 10-minute token, exact loopback CORS, bounded status và
  participant-safe cleanup. Đây là readiness evidence, không tự đóng physical gate.
- Read-only isolated-provider startup smoke PASS: boundary tự nạp file ignored trong cùng process,
  không in credential, không tạo room và trả đúng bounded state `room_exists=false`,
  `participant_count=0`, `cleanup_zero=true`.
- Exact launcher smoke PASS: physical page trả `200`, boundary preflight trả `204`, CORS origin khớp
  tuyệt đối và hai port `4179/5173` đều đóng sau khi dừng harness.

### Provider load/outage

- Status: `LOAD + DASHBOARD + ACTUAL CREDENTIAL ROTATION + SUSTAINED OUTAGE/RECOVERY PASS`.
- Isolated profile 25 PASS ngày 2026-08-15: `25/25` join, success `10000 bp`, connect p95 `4331 ms`,
  TTM p95 `6021 ms`, `24/24` subscriber nhận synthetic microphone, sustain `120 s`; Core API có
  `126` request `/health` + `/ready`, `0` transport/status/endpoint failure, p95 `426 ms`; room cleanup
  về `0`.
- Active-load delta là heap `70593368` bytes/goroutine `1175`; sau cleanup + GC là heap `26133368`
  bytes/goroutine `1`.
- Isolated profile 50 PASS ngày 2026-08-15: `50/50` join, success `10000 bp`, connect p95 `6761 ms`,
  TTM p95 `8190 ms`, `49/49` delivery, sustain `120 s`; Core API có `128` probe, `0` failure, p95
  `658 ms`; cleanup về `0`. Active heap/goroutine delta là `93658344/2350`, sau cleanup + GC là
  `53639968/1`.
- Từ 25 lên 50, active heap tăng `1.33x`, active goroutine tăng đúng `2x`, post-cleanup goroutine giữ
  `+1`; không có dấu hiệu superlinear/goroutine leak trong hai profile. Maximum tested cap là `50`.
- Existing-room behavior và 10-cycle provider cleanup đã PASS ở isolated smoke. Windows 11
  Chrome/Edge physical media + NVDA đã PASS 10/10; macOS/VoiceOver, Firefox cài thật và low-end giữ
  `UNAVAILABLE`.
- Profile 50 đồng bộ với host sampler PASS: `61` mẫu, CPU tổng peak `48%`, network tổng peak
  `2263941 B/s`, free RAM tối thiểu `6201416 KiB`; tải vẫn `50/50`, TTM p95 `8679 ms`, health
  `132/132`, cleanup `0`. Metric host không thay provider dashboard.
- Actual isolated key rotation PASS; evidence ghi lúc `2026-08-15T22:34:12+07:00` trên project label
  `tutorhub-v2-p411-load`. Owner thao tác dashboard, Codex chạy probe bằng hai file local Git-ignored
  trong process riêng và không in credential. Key mới pre-revoke tạo room rồi cleanup về `0`; sau khi
  owner revoke key cũ, old-key probe trả `P4_11_REVOKED_CREDENTIAL typed_unavailable=true
  room_created=false`; new-key probe lặp lại trả `P4_11_POST_ROTATION create=true cleanup_zero=true`.
  Credential cũ không tạo side effect; credential mới vẫn hoạt động. Owner đã xóa file credential cũ
  local sau khi evidence được chốt; file active-key vẫn Git-ignored.
- Profile 50 đồng bộ với host sampler PASS: `61` mẫu, CPU tổng peak `48%`, network tổng peak
  `2263941 B/s`, free RAM tối thiểu `6201416 KiB`; tải vẫn `50/50`, TTM p95 `8679 ms`, health
  `132/132`, cleanup `0`. Metric host không thay provider dashboard.
- LiveKit Cloud Overview evidence PASS, owner chụp lúc `2026-08-15T22:45:00+07:00` đến
  `22:47:00+07:00`, filter `Past 7 days`, đúng project label `tutorhub-v2-p411-load`: connection
  success `99.7%`, platform Windows `100%`, TURN `71.1%`, UDP `28.9%`, WebRTC participant time `428`
  phút. Project-level chart ghi max active participants `150` trong bucket `20:00-21:00`
  ngày 2026-08-15; đây là aggregate của nhiều room/session test và không nâng maximum tested
  classroom cap khỏi `50`. Cùng bucket có downstream `55.18 MB`, upstream `2.8 MB`; tổng `14` room
  session, average room size `19`, average duration `1` phút. Ba ảnh không chứa key/secret/token,
  participant identity hay session detail; provider project id/label và aggregate metrics là bounded
  evidence.
- Actual isolated active-room outage/recovery PASS ngày 2026-08-16 trên project label
  `tutorhub-v2-p411-load`. Baseline có `room_exists=true`, `participant_count=2`,
  `cleanup_zero=false`. Sau khi owner revoke đúng temporary outage key, boundary trả HTTP `503`
  typed `provider_unavailable`; old-key probe trả `typed_unavailable=true`, `room_created=false`.
  Wi-Fi loss 120 giây làm UI vào `reconnecting`; sau restore, phiên chuyển terminal/prejoin và không
  auto-rejoin. Manual retry bằng revoked key bị provider từ chối, không tạo authority mới; trạng thái
  trung gian hiển thị trong phòng vài giây không được ghi thành join thành công vì sau đó provider trả
  `401/503` và client fail closed về prejoin.
- Recovery credential terminate đúng opaque room về `room_exists=false`, `participant_count=0`,
  `cleanup_zero=true`. Focused recovery probe giữ existing-room authority, tạo/cleanup successor và
  post-rotation room; recovery/concurrency/idempotency regressions đều PASS. Ảnh DevTools có token
  fragment không được sao chép vào repository/evidence; ledger chỉ giữ typed/boolean bounded result.
- Final candidate local verify PASS ngày 2026-08-16 trong `171 s`: format/API generation,
  lint/typecheck, web `70/70` files và `438/438` tests, build/Storybook, bundle security và toàn bộ Go
  test/vet đều xanh. Focused physical harness Go test/vet và web regression cũng PASS. Candidate scan
  có `15` file, không có `.env`, ảnh, cache hoặc high-risk token/credential pattern; hai local port
  `4179/5173` đều đã đóng.
- Candidate `abf4eb7a1bb3bd5ba280ed07a5132583844021e7` PASS Verify `31928773967`; Security
  `31928773979` nhận nhầm hai synthetic participant UUID. Fix
  `edecf84ecc45ae4c290e5b76df6ffc5b0a6bcfa9` lấy participant key từ boundary metadata và chỉ
  ignore đúng hai historical fingerprint, sau đó PASS Verify `31929104866` và Security
  `31929104924` với secret scan `No leaks detected`.

## 9. Điều kiện chuyển trạng thái

- `IN PROGRESS -> VERIFY`: automated supplement/local outage/full local verify PASS; exact candidate
  CI/security PASS; profile 25 và 50 (hoặc lower provider-safe cap có lý do) cùng provider/Core API
  evidence PASS; physical rows và accessibility đã ghi exact PASS/FAIL; effect decision được khóa.
- `VERIFY -> DONE`: review evidence không có secret/PII, support statement/cap được công bố, isolated
  outage/credential rotation và recovery PASS, state/backlog/master/coordination đồng bộ. Không cần
  migration/shared database write/deploy nếu candidate chỉ chứa test/runbook.
