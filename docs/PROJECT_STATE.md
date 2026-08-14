# TutorHub V2 - Trạng thái dự án

> Điểm vào nhanh để tiếp tục phát triển. Cập nhật sau mỗi task hoặc thay đổi hạ tầng.

## Snapshot

| Thuộc tính           | Trạng thái                                                                            |
| -------------------- | ------------------------------------------------------------------------------------- |
| Ngày cập nhật        | 2026-08-13                                                                            |
| Repository           | `https://github.com/basangnguyen/TUTORHUB_WEB`                                        |
| Nhánh làm việc       | `main`                                                                                |
| Quy trình            | Một coding agent, commit trực tiếp vào `main`; GitHub dùng để lưu và sao lưu mã nguồn |
| Phase hoàn thành     | Phase 0, Phase 1, Phase 2                                                             |
| Phase hiện tại       | Phase 4 Classroom Media MVP; Phase 3 deferred carry-over vẫn hoạt động                |
| Task `DONE` gần nhất | P4-06 Participant roster, hand raise và reaction                                       |
| Mốc repository mới   | Exact candidate `d773641f` đã PASS CI/security và Live trên Render/Cloudflare          |
| Task hiện tại        | P4-07 Host/co-host/TA moderation (`VERIFY`)                                            |
| Task tiếp theo       | Review/push candidate, GitHub CI/security, rồi xin quyền forward shared P4-07          |

### Checkpoint P4-07 `VERIFY` — local/disposable PASS ngày 2026-08-13

P4-07 đã hoàn tất local candidate và disposable acceptance nhưng chưa migrate shared hoặc deploy. ADR-0030 và
OpenAPI chốt PostgreSQL/Core API là authority; browser chỉ gửi opaque participant key và chỉ render
server-projected operations. Forward migration `000033_media_moderation_commands` thêm assignment
co-host đúng tenant/space/RoomInstance, durable provider-effect receipt, PUBLIC zero privilege và
fail-closed down guard khi còn co-host động hoặc effect bắt buộc chưa hội tụ.

Core API đã có exact matrix cho host/co-host/TA/attendee/safety-admin, expected space/room/projection
version, stable idempotency và cùng effective-role resolver cho moderation/signal/lobby/credential.
Lock/unlock, promote/demote, remote mute, remove và end đều tái xác thực tenant/source/room/target;
không có remote unmute. Safety-admin chỉ được remove với bounded reason/audit, không được tự nhận
quyền tham gia. PostgreSQL actor/room/operation-family rate limit dùng database clock, trả exact
`429`/`Retry-After` và fail closed.

Provider call chạy sau transaction. Durable reconciler dùng `FOR UPDATE SKIP LOCKED`, lease/CAS,
bounded retry và giữ nguyên actor chỉ làm audit metadata; actor bị vô hiệu hóa không làm stranded
effect. End-room provider failure có typed `503` nói rõ business đã commit mà không tuyên bố provider
success; UI chỉ nhận trạng thái này khi exact status/space/payload khớp, còn generic `503` vẫn fail.
UI có confirmation, one-way mute language, focus return, keyboard/zoom/reduced-motion/forced-colors
và loading/forbidden/stale/provider-pending/reconcile/terminal states.

Full `pnpm verify` PASS: web `66/66` files, `424/424` tests; API client `7/7` files, `52/52` tests;
toàn bộ lint/typecheck/build/Storybook/security và Go test/vet đều xanh. Focused Go, integration-tag
compile/vet, OpenAPI generation, format và diff check PASS; deterministic P4-07 Playwright/Axe fixture
đã PASS `4/4`. Opt-in harness cho mute/remove/delete thật và NotFound replay đã PASS trên isolated
LiveKit test resource. Neon disposable owner preflight, forward-only
`32 false -> 33 false -> 33 false`, exact runtime/PUBLIC/maintenance/dependency ACL, P4-07
authority/concurrency và toàn bộ `9/9` media PostgreSQL regression programs đều PASS. Final read-only
snapshot giữ `33 false`, feature effective force-off và `unsafe_unresolved_effects=0`; retained audit
fixtures được báo cáo thay vì xóa. Shared-staging owner preflight/ACL/final-snapshot harness dùng bộ
xác nhận P4-07 riêng, fail closed với cờ stale và đã PASS static/compile/vet; harness này chưa được
thực thi. Các giá trị trong `.env.p4-07-disposable.local` chỉ được nạp trong đúng process test, không
hiển thị hoặc log; shared staging chưa được kết nối. Evidence:
[P4_07_STAGING_ACCEPTANCE.md](P4_07_STAGING_ACCEPTANCE.md).

Để chuyển `VERIFY -> DONE`, bước kế tiếp là review exact candidate, commit/push trực tiếp `main` và
chờ GitHub Verify/Security PASS. Chỉ sau đó mới xin quyền forward shared staging `32 -> 33`, provision
exact ACL, deploy exact candidate và chạy live acceptance. Không rollback; disposable branch tiếp tục
được giữ lại.

### Checkpoint P4-06 `DONE` ngày 2026-08-13

Local candidate đã thay roster fallback của P4-05 bằng projection có version do Core API/PostgreSQL
sở hữu. Forward migration `000032_media_participant_signals` bổ sung opaque participant key,
monotonic roster/signal sequence, hand state, reaction event TTL 10 giây và idempotency receipt giữ
24 giờ với composite same-tenant FK/PUBLIC zero privilege. Hai bounded maintenance function dùng
`SECURITY DEFINER`, static search path và `FOR UPDATE SKIP LOCKED`; Core API runtime không có direct
table `DELETE`. Neon disposable và shared staging đều đã chạy forward-only
`31 false -> 32 false -> 32 false`; không rollback.

Contract/API mới cung cấp bounded snapshot tối đa 50 participant và typed hand/reaction mutation.
Mọi mutation reauthorize current tenant/source/active ParticipantSession, dùng expected versions,
stable idempotency, shared-read/exclusive-write tenant lock, PostgreSQL-authoritative timestamp,
server FIFO, moderator lower-one/lower-all và PostgreSQL cross-instance rate limit. Projection không
trả email, user/session/join/provider ID; LiveKit chỉ nhận signed opaque
`tutorhub.participant_key`, giữ `CanPublishData=false` và cấm client tự sửa metadata.

Classroom shell đã dùng canonical roster sequence/key cho grid, roster drawer, hand queue, six-reaction
allowlist, tối đa 3 visual cluster nhưng vẫn giữ bounded summary, 2 giây polling/resync và các trạng
thái loading/error/retry/rate-limit. Reduced-motion, forced-colors, keyboard và polite live-region
đã có automated coverage. Exact full `pnpm verify` PASS trong `92.4 s`: web `64/64` files và `400/400` tests,
API client `50/50`, toàn bộ Go test/vet, lint/typecheck/build/Storybook/security đều xanh;
PostgreSQL integration-tag compile/vet cũng PASS. Vite-only Playwright/Axe production-shell fixture
đạt P4-06 `7/7`; combined P4-05/P4-06 đạt `14 passed`, `1` isolated provider test được skip đúng cấu
hình.

Neon disposable đã PASS read-only owner preflight với ba principal riêng biệt: direct owner và
maintenance, pooled runtime trên cùng endpoint/database, bắt đầu ở ledger `31 false`. Exact runtime,
PUBLIC, maintenance và dependency ACL đều PASS. PostgreSQL gates xác nhận tenant/source/participant
authorization, privacy/opaque key, roster/FIFO/idempotency/lower/terminal cleanup, receipt replay
24 giờ, reaction TTL/grouping/allowlist, cross-instance limits actor `3/5 s` + `20/60 s` và room
`100/5 s`, shared/exclusive locks, invalid purge batch, future preservation và two-transaction
`SKIP LOCKED`. Harness được harden bằng explicit timestamp cast, dependency-ordered cleanup,
deterministic rate windows và confirmation/pre-postflight riêng P4-06. Final disposable snapshot giữ
`32 false`, hai media feature force-off, P4-06 side-effect count `0`; disposable branch được giữ lại.

Exact candidate `d773641f796076b90f31a876ee840a427db43372` đã commit/push trực tiếp `main` và
PASS GitHub Verify/Security. Shared owner preflight sau đó PASS; migration forward-only giữ chuỗi
`31 false -> 32 false -> 32 false`, exact runtime/PUBLIC/maintenance ACL và final read-only snapshot
đều PASS. Hai media feature tiếp tục force-off, toàn bộ bounded P4-06 business count giữ `0` và không
có rollback.

Render deployment `dep-d9ul9q6417fc738gfa3g` đã `Live` đúng candidate; Cloudflare Pages cũng chạy
exact `d773641f`. Public health/readiness/status, anonymous privacy/concealment, authenticated
feature-off, automated browser accessibility/privacy/network và no-side-effect acceptance đều PASS;
không temporary-enable capability hoặc chạy positive signal canary trên shared. Post-live snapshot
giữ ledger/ACL/feature/count như trước live.

P4-06 chuyển `IN PROGRESS -> VERIFY -> DONE`; P4-07 là task `TODO` runnable tiếp theo. Physical/
manual browser-device, 25/50 provider load, outage và optional-effect gates tiếp tục
`UNVERIFIED — P4-11`, không được suy PASS từ P4-06. Evidence:
[P4_06_STAGING_ACCEPTANCE.md](P4_06_STAGING_ACCEPTANCE.md).

### Checkpoint P4-05 `DONE` ngày 2026-08-12

Canonical room đã được tách khỏi prejoin thành lazy route riêng và dùng custom LiveKit shell thay
cho prebuilt conference UI. Local slice hiện có stage/toolbar/participant rail và responsive drawer;
Grid/Active speaker/Presentation; grid cap `12/6/4`, rail tối đa `6`, local pin, hysteresis
`800/2500/1500 ms` và deterministic screen-share restore. Publish controls chiếu đúng credential
grants; `autoSubscribe=false`, remote audio/video được subscribe thủ công theo bounded projection,
hidden presenter camera/off-page video bị gỡ subscription và listen-only không gọi publish API.
Trong khi feature còn deployment-force-off, participant dùng opaque local-first session-local append-stable
fallback không đọc provider identity/join time; canonical server roster sequence/key thuộc P4-06
và bắt buộc trước feature enable.
Deterministic degradation đã có đủ chuỗi normal -> reduced video -> lower quality -> stage-only ->
audio-only với cửa sổ xuống cấp `5 s`, phục hồi `15 s`, ưu tiên presentation và giữ audio/control.

Device selector chỉ mount sau explicit mở panel và không enumerate input device khi grant publish
false. LiveKit/component logger bị tắt ở canonical room, provider identity không render vào DOM,
terminal leave/disconnect/error là first-wins và pending camera/mic/share/device promise bị invalidate
khi leave/unmount. TutorHub sở hữu trực tiếp `RoomContext`/connect/publish/disconnect; Room chỉ được tạo
sau StrictMode commit, synthetic cleanup không disconnect, terminal callback cũ không thể đổi scope mới
và wrapper/session cleanup dùng chung một idempotent disconnect lifecycle. Production baseline tiếp tục
chỉ có `effect=None`; không thêm processor/model/WASM, không migration/OpenAPI/schema/ACL delta và
không bật media capability.

Focused P4-05/P4-03 regression tests đạt `76/76`; full web đạt `62` files/`376` tests. Targeted/full
web lint, TypeScript web/E2E typecheck, production build, Vite-only Chromium regression P4-03/P4-04/
P4-05 `16/16`, Playwright P4-05 `7/7`, Prettier/diff check, client bundle security `20` files, exact
local security suite `24/24` và diff/secret-marker audit trên `29-file` pre-hotfix candidate cùng
`2-file` hotfix đều PASS. Rolldown tách LiveKit
thành vendor chunk chỉ được
static-import bởi room routes; app entry và canonical prejoin không static-import SDK. Application
room chunk hiện `38.02 kB` raw/`11.93 kB` gzip dưới budget `45/15 kB`; tổng room application +
LiveKit vendor + CSS là `642.90/173.66 kB`, dưới budget `700/190 kB`.

Neon disposable read-only đã PASS với endpoint boundary riêng, ledger `31 false`, `dirty=false`, hai
media feature effective false và exact runtime ACL; `53` historic enabled override rows được giữ
nguyên vì deployment guard vẫn force-off. Không có migration, rollback, ACL provision, seed hoặc
mutation. Isolated LiveKit Go two-participant grant matrix đạt `2/2`; Chromium actual-media gate đạt
`1/1` khi publisher và subscribe-only subscriber đều mount real `ClassroomLiveKitRoom`: subscriber
nhận remote camera/audio và chỉ nhận screen share sau explicit action; signed grant matrix chặn data,
privacy check PASS, mọi captured track `ended`, remote media detach khi Leave và exact room cleanup về `0`.

Exact runtime candidate `dcbdfef3c209a7c6d17197ccbcf737b58cd9e315` sửa fail-closed response
contract để auth failure chỉ ghi một Problem Details, sau khi live probe phát hiện response cũ nối
`401` và `403`. Focused MediaSpace regression, toàn bộ Go packages và `pnpm verify` PASS. GitHub
Verify `31598671906`, Security `31598671939` và Cloudflare deployment
`e2e72ca9-0173-4f2c-8221-f14203551c42` đều PASS; Render deployment
`dep-d9u6sdbm8hqs73em447g` Live đúng SHA. Public/anonymous matrix đạt `13/13`; authenticated
Organization Admin xác nhận hai media feature vẫn off, synthetic route fail closed và production
resource audit không tải LiveKit/effect/model. Shared before/after read-only snapshot giống nhau:
ledger `31 false`, exact ACL, mọi media aggregate/outbox/audit count không đổi.

P4-05 chuyển `IN PROGRESS -> VERIFY -> DONE`. P4-06 là task runnable tiếp theo. Physical device
indicator, browser/hardware/load, provider-outage và optional-effect matrix tiếp tục
`UNVERIFIED — P4-11`. Evidence ledger:
[P4_05_STAGING_ACCEPTANCE.md](P4_05_STAGING_ACCEPTANCE.md).

### Checkpoint P4-04 `DONE` ngày 2026-08-10

P4-04 local candidate đã thêm contract/API cho self join-attempt poll/cancel, moderator admission
queue cùng admit/deny/restore và explicit same-tenant StudyMeeting member list/invite/revoke/restore.
Core API reauthorize current tenant/source/membership, bind exact MediaSpace/RoomInstance/actor,
dùng expected version + idempotency receipt + CAS và giữ lock order cho race admit/deny/end/cancel/
timeout. Denied/removed member chỉ có thể rejoin sau explicit restore; waiting participant không có
provider credential/connection và device/effect state không đổi admission hoặc capacity.

Forward migration `000031_media_lobby_admission_restore` mở rộng exact receipt-operation allowlist
và thêm participant rejoin-restore marker cùng same-tenant FK/check/partial index. Migration không
revive terminal session hoặc mở public/anonymous authority. Raw email
chỉ là exact lookup input cho invite; projection, audit/outbox và log không lưu email, token,
provider/device identifier hoặc media content.

Web có waiting timeout/cancel/retry cùng denied/cancelled/meeting-ended/provider-unavailable states,
moderator lobby và invite panel fail-closed, bounded focus/error/empty/forbidden handling và không
enumerate tenant roster. Local evidence hiện có: OpenAPI generation/check PASS; API client `7` files/
`49` tests; web typecheck và `59` files/`305` tests PASS; focused P4-04 web `23/23`, targeted lint/build/
E2E typecheck/format/diff PASS; Go test/vet cùng integration-tag compile PASS. Isolated P4-04
Playwright đạt `3/3`; security/static sign-off cho deployment guard, audit, exact ACL columns,
lock/TTL/privacy/restore PASS. Full exact `pnpm verify` PASS với workspace-local Go cache;
integration compile không phải bằng chứng PostgreSQL runtime.

Neon disposable đã PASS owner preflight, forward-only `30 false -> 31 false -> 31 false`, exact
runtime/default/future-table ACL, lobby/admission/invite race/restore, lifecycle và RoomInstance
integration gates; final ledger `31 false`, không rollback. Full post-disposable `pnpm verify` cũng
PASS. Exact runtime candidate `735a5e5579d6e5efe7c4efca2b8a48c3de1b1f23` sau đó PASS GitHub
Verify `31356295542` và Security `31356295518`. Shared owner preflight, forward-only
`30 false -> 31 false -> 31 false`, exact ACL và read-only snapshot đều PASS; không chạy shared
mutation/provider fixture.

Cloudflare Pages check `93356654021`/deployment `7f180bf1-3736-4e40-a6f3-7ced6df2be27` và Render
deployment `dep-d9sli7740ujc73dfic70` chạy exact runtime SHA. Live đạt `6/6` health/readiness/status,
`22/22` anonymous privacy checks và authenticated Admin feature-off/prejoin fail-closed acceptance.
Shared snapshot trước/sau giống hệt ở ledger `31 false`; tất cả P4 media aggregate vẫn `0`, trừ
`livekit_webhook_events=16` có sẵn, và không có media feature override bật. Không rollback, không xóa
disposable branch; `classroom_media_rooms`/`instant_study_rooms` tiếp tục force-off, `effect=None`,
`CanPublishData=false`. P4-04 chuyển `VERIFY -> DONE`; P4-05 là task runnable tiếp theo.
Runbook/evidence: [P4_04_STAGING_ACCEPTANCE.md](P4_04_STAGING_ACCEPTANCE.md).

### Checkpoint P4-03 `DONE` ngày 2026-08-10

P4-03 đã thêm canonical join-attempt API trước credential, current-role/lobby reauthorization,
same-tenant concealment và exact runtime ACL không cho `joining_at` ở participant `INSERT`. Prejoin
không capture ban đầu, chỉ probe sau explicit action; có device/speaker/audio-profile/network states,
bounded errors, listen-only, memory-only credential handoff và purge khi route/principal/workspace/
disconnect thay đổi.

Fresh `pnpm verify` PASS trên exact candidate tree: web `57` files/`289` tests, API client `47` tests, format,
generated contract, lint/typecheck/build/Storybook/security và toàn bộ Go test/vet đều xanh. Isolated
Chromium Playwright đạt `6/6`; focused prejoin/room Vitest đạt `27` tests và stress test xác nhận 20
device-switch cycles để lại zero owned resource.

Neon disposable đạt exact ACL provision/ledger-role, runtime ACL, lifecycle/concurrency/quota/privacy
và join-attempt/credential/webhook gates. LiveKit test-provider smoke PASS; final read-only ledger giữ
`30 false`, không migration mới và không rollback. Lần fail đầu chỉ ra
fixture timestamp rơi đúng ranh giới Unix second; fixture được lệch `125ms`, unit + disposable rerun
đều PASS. Evidence: [P4_03_STAGING_ACCEPTANCE.md](P4_03_STAGING_ACCEPTANCE.md).

Exact candidate `e49a8cc38f464e3ec56655823bcbb1ee77cbc651` PASS GitHub Verify
`31330663644` và Security `31330663663`. Shared exact ACL reprovision + focused read-only probe
PASS, ledger giữ `30 false`; không migration, rollback hoặc shared fixture. Cloudflare Pages check
`93288377456` và Render deployment `dep-d9sd4ne7bikc739bf7l0` đều chạy exact SHA.

Live đạt 6/6 health/ready/status `200 + no-store`, 4/4 anonymous MediaSpace/join-attempt privacy
`401`, Organization Admin xác nhận hai media feature off và prejoin inaccessible route fail-closed
với Retry, zero video/audio element, URL sạch credential và console sạch. Transaction read-only
trước/sau giữ bảy media relation ở `0 -> 0`; không có join/provider/database side effect. Disposable
branch tiếp tục được giữ; `.env*.local` và `.tmp-gocache/` không thuộc candidate. P4-03 chuyển
`VERIFY -> DONE`; P4-04 sau đó đã triển khai local candidate và chuyển `VERIFY`.

### Checkpoint P4-MEDIA-UX-00 `DONE` ngày 2026-08-09

P4-MEDIA-UX-00 đã benchmark nguồn chính thức hiện hành của Google Meet, Zoom, LiveKit, W3C,
Mozilla/WebKit/Chrome và MediaPipe; audit V1 read-only; pin ba checkout LiveKit tham khảo; lưu
[báo cáo evidence](P4_MEDIA_UX_00_RESEARCH_REPORT.md) và chấp nhận
[ADR-0031](adr/0031-classroom-media-ux-devices-layout-effects-and-signals.md). Không sao chép UI/
asset đối thủ, không port V1 client-owned admission/DataChannel authority và không dùng bốn ảnh
nền V1 thiếu license/provenance đủ mạnh.

Prototype `apps/media-ux-spike` không nối production route, media device, Core API hoặc LiveKit.
Nó khóa fixture Grid/Active speaker/Presentation 2/5/25/50, cap 12/6/4, local pin, server-sequence
hand queue và reaction allowlist/TTL/grouping/rate-limit; unit/component 22/22, lint, typecheck,
build và Playwright/Axe browser/a11y 5/5 PASS. Processor harness Git-ignore chạy video tổng hợp
360p/540p/720p: Chrome/Edge đạt 30 frame/case và observable cleanup giới hạn nhưng có long task tới
172 ms;
Firefox fallback fail cả 9 case, Safari/macOS/máy yếu chưa test vật lý.

Repository gate tương đương `pnpm verify` đã PASS: format/API generation/local-infra/security,
8 workspace lint/typecheck/test/build, Storybook và client bundle đều xanh. Bước Go ban đầu chỉ bị
sandbox từ chối cache mặc định; exact `go test ./services/core-api/...` và
`go vet ./services/core-api/...` chạy lại với workspace `GOCACHE` đều PASS.

Vì vậy P4 production baseline là `effect=None`. `@livekit/track-processors@0.7.2` chỉ là optional
candidate force-off đến khi self-host immutable model/WASM/background, privacy/MediaPipe metrics,
CSP/network, exact Firefox/Safari/low-end performance và cleanup gates đạt ở P4-11. WebRTC
speech + explicit original-sound là audio baseline; Krisp/direct MediaPipe không vào MVP. Hand/
reaction vẫn qua Core API với `CanPublishData=false`. Không migration, shared staging write,
provider config, deploy hoặc feature activation trong research. P4-03, P4-04 và P4-05 sau đó đã đạt
`DONE` theo toàn bộ decision của ADR-0031; P4-06 là task runnable tiếp theo.

### Checkpoint P4-02 `DONE` ngày 2026-08-09

P4-02 đã có forward migration `000030_room_instance_livekit_binding`, exact runtime ACL/runbook,
official LiveKit server SDK adapter và canonical RoomInstance credential endpoint. Credential chỉ
nhận `join_attempt_id`, reauthorize active tenant/session/source, bind exact active RoomInstance,
tạo opaque ParticipantSession identity độc lập, áp dụng shared `30/10m` rate limit và mint JWT mặc
định 5 phút với camera/mic, screen-share và subscribe grant tách riêng; `CanPublishData=false`.
Response không trả provider room/participant identity và luôn `private, no-store`.

Provider lifecycle commit intent trước khi gọi LiveKit, create/reuse room idempotent rồi activation
CAS; provider outage trả typed/redacted `503`. Signed webhook dùng official verifier trước database,
lookup exact persisted room/SID và participant identity, receipt `(provider_kind,event_id)` chống
replay, xử lý mismatch/stale/out-of-order/terminal và không lưu raw payload/provider identifiers.
Legacy P1 class-wide route được gate mutual exclusion; runtime P4 có zero grant trên receipt legacy.
Review cuối còn siết activation phải reauthorize `session.start`, chuẩn hóa lỗi database tại webhook
boundary và map provider lifecycle outage thành `media_provider_unavailable`. `room_finished` giải
phóng toàn bộ active participant capacity; provisioning intent chuyển `failed` có thể được owner kết
thúc/cleanup an toàn; timestamp LiveKit độ chính xác một giây được clamp vào state PostgreSQL có
microsecond mà không hồi sinh hoặc đảo lifecycle.

Official verifier HTTP test chứng minh valid signature mới tới processor; unsigned, wrong-key và body
tamper đều dừng trước mutation. PostgreSQL harness có explicit disposable opt-in trước khi đọc URL,
chặn owner/runtime trùng role và khóa same-key credential, signed duplicate, connect/leave cùng
provider reconciliation concurrency; failed/unknown/composite-FK/retention/privacy branches cũng có
assertion thật khi chạy trên disposable. Raw driver cause không còn được render ra service error/test
failure.

Disposable owner/runtime preflight PASS; migration forward-only và idempotent đạt
`29 false -> 30 false -> 30 false`. Exact runtime ACL provision/probe PASS, không broad/PUBLIC/legacy
grant; gate thực tế phát hiện và sửa thiếu `SELECT attempt_number` cho truy vấn latest RoomInstance.
Focused RoomInstance/credential/webhook PostgreSQL gate PASS trong 223.7 giây; lifecycle exact ACL,
authority, quota, concurrency và privacy suite PASS trong 200.2 giây. Final ledger giữ `30 false` và
không rollback, không chạm shared staging.

LiveKit test-provider smoke tạo/reuse một opaque room, mint token 5 phút least-privilege,
connect/disconnect và exact cleanup PASS trong 29.8 giây. Official verifier valid/wrong-key/tamper
PASS với signed synthetic webhook. Full `pnpm verify` sau các sửa lock-order, tenant concealment, lobby/
Unix-second fixture và ACL PASS trong 182.5 giây; rerun sau khi bỏ log endpoint LiveKit tiếp tục
PASS trong 26.2 giây với cache. File `.env.p4-02-disposable.local` chỉ được nạp
trong cùng command, không in/log giá trị. Exact candidate
`f622e5f4b4c5efd6b877914e35aff16d765fba53` đã push `main`; GitHub Verify `31303424310` PASS
trong 3 phút 13 giây và Security `31303424335` PASS trong 2 phút 55 giây. Shared safety candidate
`d223daf0f2d504e6a0088071239aa9daeb36372c` PASS Verify `31304103932` trong 4 phút 28 giây và
Security `31304103916` trong 2 phút 40 giây. Shared owner/runtime preflight, forward-only
`29 false -> 30 false -> 30 false`, exact ACL provisioning và read-only ACL integration đều PASS;
final ledger giữ `30 false`, không rollback.

Render deployment `dep-d9s3vh0n74is73ftetr0` chạy exact safety candidate và đạt `live`.
Direct Render/Pages health, ready và status đạt 6/6 HTTP `200` + `no-store`; anonymous canonical
credential/unsigned webhook fail closed `401` qua cả hai đường; authenticated Organization Admin
UI xác nhận hai media feature tiếp tục off. LiveKit test-provider smoke rerun PASS và Render nhận,
xác minh rồi ignore đúng ba provider-emitted webhook của unknown test room; exact provider cleanup
PASS. Read-only shared probe giữ năm bảng MediaSpace/RoomInstance/session/mutation receipt/provider
receipt ở `0`; bảng receipt legacy có `16` dòng lịch sử nhưng không có dòng mới kể từ deployment.
Không log secret/URL/token, không xóa disposable branch. P4-02 đã chuyển
`IN PROGRESS -> VERIFY -> DONE`; task runnable tiếp theo là P4-03. Chi tiết nằm tại
[P4_02_STAGING_ACCEPTANCE.md](P4_02_STAGING_ACCEPTANCE.md).

### Checkpoint P4-01 `DONE` ngày 2026-08-09

P4-01 đã có implementation candidate local cho forward migration
`000029_classroom_media_spaces`, MediaSpace/RoomInstance lifecycle, source binding,
idempotency/concurrency barrier, shared policy actions, audit allowlist, REST
create/get/start/end/cancel, OpenAPI/generated client và feature/quota controls. Hai feature
`classroom_media_rooms`/`instant_study_rooms` giữ compiled/deployment default off; child phụ
thuộc parent và thiếu LiveKit prerequisite phải fail closed. Slice không mint token, gọi
LiveKit, xử lý webhook hoặc mở room end-user; provider binding thật thuộc P4-02.

Focused local feature-control/config/HTTP, API client, web, generated-contract và full
`pnpm verify` đã xanh; integration-tag PostgreSQL compile/skip sạch khi chủ động bỏ DB env.
Review còn sửa concealment mutation cùng actor-scoped receipt để loại hai existence oracle.
Typed `room.create.instant` đã nối đúng ADR-0021 cho mọi active member, nhưng instant command
vẫn reauthorize tenant, membership và StudyMeeting ownership; focused policy/media/HTTP cùng
full local verify sau policy đều PASS.

Neon disposable `p4-01-disposable-20260809` đã PASS owner/runtime preflight, forward-only
`28 false -> 29 false -> 29 false`, exact runtime ACL và full media PostgreSQL integration bằng
pooled runtime login; final ledger giữ `29 false`, không rollback. Integration suite xác nhận
authority/tenant concealment, same-key concurrency, source lifecycle barrier, feature/quota,
privacy/audit và không có provider side effect. Fresh `pnpm verify` sau hai sửa lỗi harness
(khởi chạy đủ hai concurrent operation và giữ append-only audit fixture) PASS trong 195.8 giây.
Exact candidate `183ca338557fafd6e8fe502d67763bb2a73d9aa0` đã PASS GitHub Verify
`31291917865`, Security `31291917871` và Cloudflare Pages check `93190579210`. Shared owner/
runtime preflight, forward-only `28 false -> 29 false -> 29 false`, exact ACL provisioning và
focused ACL integration đều PASS; final shared ledger giữ `29 false`, không rollback. Render
deployment `dep-d9rv61ajobas73e9pq90` chạy exact candidate và đạt `live`.

Live acceptance đạt 6/6 health/ready/status qua direct Render và Pages proxy; Organization Admin
UI xác nhận hai media feature off; anonymous media route đạt 4/4 `401` với privacy/cache headers.
Read-only shared probe sau live giữ bốn relation lifecycle ở `0`, nên không có MediaSpace,
RoomInstance, participant session, token/webhook/provider room hoặc provider side effect. P4-01
đã chuyển `IN PROGRESS -> VERIFY -> DONE`. Runbook và evidence đầy đủ nằm tại
[P4_01_STAGING_ACCEPTANCE.md](P4_01_STAGING_ACCEPTANCE.md). Task runnable tiếp theo là P4-02;
`P4-MEDIA-UX-00` sau đó đã `DONE`; quyết định áp dụng từ P4-03 theo ADR-0031.

### Checkpoint P4-00 `DONE` ngày 2026-08-08

P4-00 đã tạo `docs/PHASE_4_BACKLOG.md` với P4-00 đến P4-12, dependency graph, reuse
matrix, risk/test matrix và exit gate. ADR-0030 `Accepted` chốt ba cấp authority
`MediaSpace -> RoomInstance -> ParticipantSession`; official ClassSession/occurrence và
member-owned StudyMeeting giữ source/ownership riêng, còn instant command tạo/bind một
StudyMeeting thay vì tạo authority thứ ba. Không quyền nào được suy từ client, JWT hoặc
provider state.

P1-07 được kiểm kê và tái sử dụng token issuer, signed webhook verifier, bounded telemetry,
prejoin/LiveKit room UI, camera/mic/screen share và reconnect. Deterministic class-wide room,
participant identity chứa user/session và webhook parse tenant/class từ room name chỉ còn là
compatibility spike, không phải Phase 4 lifecycle authority. P4-01 là vertical slice kế tiếp:
schema/domain/OpenAPI/ACL/concurrency cùng feature `classroom_media_rooms` và
`instant_study_rooms` mặc định/deployment-force-off; chưa mint provider token trong slice này.

Baseline cũng chốt lobby/moderation server-authorized, không remote-unmute,
`CanPublishData=false`, persistent chat không dùng DataChannel, recording/egress off,
provider/participant identifiers opaque, identifiable diagnostics retention tối đa 30 ngày và
legacy/P4 authority mutual exclusion. P4-00 không thêm dependency, migration, credential,
provider config, shared staging write hoặc deploy. Full P3-14/Phase 3 carry-over vẫn giữ nguyên.

### Lịch sử khởi tạo P4-MEDIA-UX-00 ngày 2026-08-09

[P4-MEDIA-UX-00](P4_MEDIA_UX_00_RESEARCH_SPIKE.md) đã được thêm làm decision spike cho
prejoin/lobby, layout lớp học 2-50 người, hand raise/reaction và media effects. Task benchmark
current official Zoom/Google Meet/LiveKit, audit V1 read-only và so sánh native browser capability,
LiveKit layout primitives/track-processors, MediaPipe fallback, WebRTC audio processing cùng
Krisp go/no-go trên một ma trận performance/privacy/license/accessibility có thể tái lập.

Task đã chuyển `TODO -> DONE`; kết quả chốt grid/active-speaker/presentation 2/5/25/50, server
FIFO/resync/rate-limit với `CanPublishData=false` và ship baseline không effect. Physical browser/
device/load/manual accessibility còn lại là rollout gate P4-11, không phải PASS ngầm từ source;
các gate tự động thuộc P4-05 đã được nghiệm thu riêng.

Research setup ngày 2026-08-09 đã shallow-clone LiveKit Meet revision
`665e1cb7841ab872de0d8e5c310744009a763b08` vào `.tmp/research/livekit-meet`. Checkout Apache-2.0
được Git ignore và chỉ dùng audit read-only; chưa cài dependency, tạo `.env` hoặc kết nối provider.

Cùng research setup đã shallow-clone LiveKit Track Processors revision
`9ef5191da7fb6d82e55876fa04d0e6048d49859b` vào `.tmp/research/track-processors-js`. Revision
Apache-2.0/NOTICE khai báo `@livekit/track-processors@0.7.2` và
`@mediapipe/tasks-vision@0.10.14`; dependency/model chỉ được dùng trong checkout Git-ignore để
build và benchmark synthetic, không được thêm vào production app.

### Forward plan P5-COLLAB-00 đăng ký ngày 2026-08-09

[P5-COLLAB-00](P5_COLLAB_00_RESEARCH_SPIKE.md) đã được thêm làm decision spike đầu tiên cho
Phase 5. Task sẽ so sánh tldraw + official sync, Excalidraw + self-managed sync và
Yjs/provider cho shared notes/custom CRDT trên cùng ma trận license/cost, convergence,
snapshot/restore, tenant/role security, scale và accessibility. ADR phải chọn đúng một
document/history/undo authority trước khi thêm production dependency hoặc runtime mới.

Đây chỉ là forward plan ở trạng thái `TODO`: không đổi phase/task hiện tại, không chặn P4-01,
không cài dependency, tạo service, migration, shared-staging write hoặc deploy.

### Checkpoint P3-14-CORE `DONE` ngày 2026-08-08

Core Exit review xác nhận toàn bộ runnable lane P3-02A/B/C, P3-02D-A, P3-06/07A,
P3-08/09, P3-11A/12/13 vẫn ở `DONE`. Review tìm và sửa ba regression/harness gap:
Calendar toolbar wrap để không clip action sau khi thêm Messages/Poll; Classroom integration
giữ business mutation ở exact runtime pool nhưng đọc audit/outbox bằng owner assertion pool;
ClassSession create lấy shared owner-time advisory lock trước class row lock để loại deadlock
với StudyMeeting insert.

Full `pnpm verify` PASS với 55 web file/262 test, generated contract, lint/typecheck,
build/Storybook/security, toàn bộ Go test và vet. Calendar production-route Playwright PASS
15/15 gồm Axe, keyboard, 200% zoom, forced colors, performance và ba visual baseline. Neon
disposable chạy tuần tự Classroom/Calendar/Conversation/Content/Discovery/Feature Control/
Security đều PASS; focused owner-time race PASS 3/3 và ledger giữ `28 false`, không migration
hay rollback. Baseline live 6/6 public + 2/2 anonymous privacy PASS; Admin xác nhận file upload
và in-app notification đều effective-off. Carry-over register đã được tạo tại
`docs/P3_14_CORE_ACCEPTANCE.md`.

Exact candidate `f5f1eb32d1ed59dbf8f5848103bb6bc9f38fbafd` PASS Verify `31265719929`,
Security `31265719927`, Browser E2E và Cloudflare Pages. Render deployment
`dep-d9rl6bp42hec738p7ed0` Live đúng SHA; 6/6 public và 2/2 anonymous privacy probe PASS.
Authenticated Admin xác nhận file upload/in-app notification effective-off; live Calendar có
năm action đều nằm trong main region, wrap đúng và không horizontal overflow, console sạch.
P3-14-CORE chuyển `VERIFY -> DONE`, cho phép bắt đầu Phase 4. Full P3-14/Phase 3 vẫn `TODO`;
worker/provider/delivery/processing carry-over tiếp tục theo register và không side effect nào
được bật sớm.

### Checkpoint P3-13 `DONE` ngày 2026-08-08

ADR-0029 chốt vertical slice không migration: chỉ admin feature/quota draft không nhạy cảm được
lưu trong `sessionStorage` với TTL/size/scope actor-tenant; logout, 401 và workspace switch purge
đúng prefix. Mutation chỉ opt-in auto-retry một lần cho network/5xx khi request đã có stable
idempotency key; 4xx/quota/conflict và B2 transfer tiếp tục không auto-retry.

Web catalog bao phủ exact 9 feature/17 quota. Go quota-rejection metrics lấy label từ compiled
catalog và bỏ qua runtime label không xác định. Full `pnpm verify` PASS với 55 web file/262 test,
toàn bộ Go test/vet, build/Storybook/security. Neon disposable feature-control integration PASS
với exact runtime ACL và ledger giữ `28 false`; không migration, rollback hay shared staging write.
Exact candidate `25a323ad` đã PASS Verify `31260418987`, Security `31260418985`, Browser E2E và
Cloudflare Pages. Render deployment `dep-d9rjdnf10e5c7387mh60` Live đúng full SHA; 6/6 public
probe và 2/2 anonymous privacy probe PASS. Live Admin same-tab recovery, workspace-switch purge,
logout purge và console check PASS; server value giữ nguyên vì không bấm lưu hay tạo quota fixture.
P3-13 chuyển `VERIFY -> DONE`; bằng chứng chi tiết ở `docs/P3_13_STAGING_ACCEPTANCE.md`.

### Checkpoint P3-12 `DONE` ngày 2026-08-08

P3-12 đã có ADR-0028 và vertical slice không migration. Home tái dùng các API Calendar,
Notification và Conversation cho các card độc lập; discovery module mới cung cấp recent ready
files và PostgreSQL search metadata-only cho session, conversation và file mà actor còn quyền
truy cập. Query được bound theo tenant/user, có giới hạn cứng, xử lý `%`/`_` như ký tự thường và
không trả message content, file content, private snippet hoặc provider selector.

Web Home có loading/empty/error, retry từng card, partial degradation và search keyboard-first;
workspace/principal change purge cache cũ. OpenAPI/generated client, Go/API-client/web tests,
Playwright keyboard/Axe và full exact-tree `pnpm verify` đều PASS; web đạt 53 file/255 test.
Neon disposable discovery integration PASS cho Teacher/Student, direct/class authorization,
inactive membership, foreign-tenant concealment và ready-file visibility. Không thêm migration;
disposable giữ `28 false`, không rollback và không forward shared staging.

Exact candidate `1c73c52782ca8b4139af0802bbace8e82b0c288b` đạt Verify `31256747702`
và Security `31256747681`; Cloudflare Pages check PASS, Render deployment
`dep-d9rhu4ugekts739t6ssg` Live đúng SHA. Sáu health/readiness/status probe đạt HTTP 200
`no-store`; bốn discovery privacy probe đạt 401 với `no-store/no-referrer/nosniff`.

Live Teacher/Student authorized session và class-conversation search PASS; notification failure
chỉ degrade card riêng. Teacher switch P2-08 -> P2-12 loại ngay kết quả cũ; Student foreign
workspace term không xuất hiện. Live workspace không có ready file nên giữ empty-state evidence,
không tạo shared fixture; ready/pending/file isolation dùng Neon integration PASS. Viewport 375 px
không overflow, semantic accessibility tree/console sạch và exact-build keyboard/Axe PASS.
P3-12 chuyển `VERIFY -> DONE`; bằng chứng chi tiết ở `docs/P3_12_STAGING_ACCEPTANCE.md`.

### Checkpoint P3-11A `DONE` ngày 2026-08-08

P3-11A đã có vertical slice contract-first mà không bật upload cho end user. Core API bổ sung
class-scoped list có keyset cursor bind tenant/class, reauthorize từ PostgreSQL và projection
`can_upload`/`can_download`/`can_retry_upload`. Student chỉ thấy file `ready`; creator/upload
manager mới thấy transfer chưa sẵn sàng, foreign tenant và quyền bị thu hồi tiếp tục conceal.

Web class detail có Class Files loading/empty/error/forbidden/off state, attachment-only
download, background SHA-256, single PUT progress/retry và multipart progress/resume trong
phiên. Cache key chứa tenant/class và được purge/invalidate khi switch workspace, class archive/
restore hoặc roster/role thay đổi. Active content không được preview, signed URL/provider proof
không lưu vào query cache hay storage.

OpenAPI/generated client, API-client/web/Go unit gate và Neon disposable content integration
đã PASS; integration xác nhận exact runtime ACL, owner/student visibility, ready download,
foreign-tenant conceal và archived-class không còn retry capability. Không có migration mới,
không rollback, không forward shared staging. `file_uploads` vẫn fail-closed cho tới P3-10/P3-11B.
Full exact-tree `pnpm verify` đã PASS. Exact candidate CI/security và live Teacher/Student
feature-off acceptance được ghi tại `docs/P3_11A_STAGING_ACCEPTANCE.md`.

Candidate `73467ae665c5aa26a901585f59d41fa32eeff585` đã đạt Verify `31247921859` và
Security `31247921851`; Cloudflare Pages PASS và Render deployment
`dep-d9rechlbedkc73bj80v0` Live đúng SHA. Sáu public health/readiness/status probe trực tiếp
Render và qua Pages đều HTTP 200 với `no-store`. Live Teacher/Student feature-off, workspace
switch, role `student -> teaching_assistant -> student` và archived fixture restore/archive
đều PASS; fixture đã được trả về trạng thái ban đầu, không tạo intent/object, không lộ file
picker/provider URL. Owner đã phê duyệt rõ automated Axe/keyboard/accessibility-tree evidence
thay manual NVDA riêng cho P3-11A sau khi quick-navigation không phản hồi trong embedded
browser. Ngoại lệ không áp dụng cho P3-11B, activation, pilot/public release hoặc UI tương lai.
P3-11A chuyển `VERIFY -> DONE`; `file_uploads` tiếp tục fail-closed tới P3-10/P3-11B.

### Checkpoint P3-09 hoàn tất ngày 2026-08-08

P3-09 đã chuyển `VERIFY -> DONE`. Final candidate `d6365b5` đạt Verify `31245311233` và
Security `31245311235`; Browser E2E, local smoke, quality/integration, CodeQL, Trivy và secret
scan đều xanh. Cloudflare Pages deployed successfully; Render deploy
`dep-d9rdbnajobas73d6an40` Live đúng commit. Direct Render và same-origin Pages health/readiness
đều HTTP 200.

Shared Neon owner preflight xác nhận `25 false`, role migration/runtime tách biệt và runtime
không đặc quyền; forward-only tới `28 false`, exact ACL re-provision idempotent và ba content
PostgreSQL integration tests đều PASS. Final ledger giữ `28 false`, không rollback. B2
single/multipart smoke, CORS và lifecycle read-back/idempotency đã PASS trước khi mở shared gate.

Live acceptance phát hiện `file_uploads` từng effective-on vì thiếu deployment ceiling. Không có
intent/object nào được tạo. Render đã thêm `FEATURE_CONTROL_DISABLE_FILE_UPLOADS=true` và redeploy;
Teacher projection xác nhận “Tải tệp lên — Đang tắt”. Code config cùng `.env.example` cũng đổi sang
fail-closed mặc định. Capability không credential trả 401 với `no-store/no-referrer`, Problem Details
và không rò provider data. Feature phải giữ off tới P3-10 exact-version hash/scan authority.

### Checkpoint P3-09 forward design `000027` ngày 2026-08-08

ADR-0027 và `docs/P3_09_STAGING_ACCEPTANCE.md` đã khóa provider-first contract. Local
presigner unit gate PASS, nhưng disposable B2 Gate 0 đã chứng minh S3-compatible presigned
PUT không cung cấp SHA-256 authority đã yêu cầu bởi P3-08:

- exact PUT và Head size/MIME/ETag/version PASS, nhưng Head không trả SHA-256;
- B2 vẫn nhận same-length wrong bytes với signed `x-amz-checksum-sha256` và cả
  `x-amz-content-sha256` trong signed headers;
- actual SHA-256 làm presigned SigV4 payload hash bị B2 trả 403 cả payload đúng;
- test object được cleanup theo exact version; không log secret/URL/query/object key.

Owner đã duyệt forward path: migration `000027` cho phép finalize ghi exact-version
size/MIME/ETag nhưng không ghi checksum giả; P3-10 stream-hash + scan exact version trước
`ready`/download. Core API hiện có upload capability tối đa 5 phút, finalize nhận
`storage_version_id` làm selector và download capability tối đa 2 phút chỉ cho file `ready`.

Disposable Neon đã forward-only `26 false -> 27 false -> 27 false`. Lần chạy đầu bị dirty
marker do row P3-08 còn checksum chưa được provider chứng minh; exact-schema preflight xác nhận
transaction không commit, ledger được sửa về `26 false`, rồi migration forward xóa riêng proof
không đáng tin ở `uploaded/processing`. Exact runtime ACL thu hồi quyền update checksum và full
PostgreSQL content integration PASS. B2 smoke mới PASS exact PUT, HEAD exact version, versioned
GET byte-match và exact-version cleanup. Full local `pnpm verify` cũng PASS. Không rollback,
không shared staging, không deploy.

### Checkpoint P3-09 multipart `000028` ngày 2026-08-08

ADR-0027 đã chốt durable multipart ownership và migration `000028` đã triển khai session/part
tables, exact ACL cùng initiate/part/complete/abort API. Local `pnpm verify` PASS. Neon
disposable owner/runtime preflight PASS, forward-only `27 false -> 28 false`; full content
integration PASS cho ownership, tenant isolation, single-PUT barrier, part retry/length
conflict, complete/abort và expiry. Final disposable giữ `28 false`, không rollback.

B2 bucket-admin preflight xác nhận cấu hình cũ rỗng. Một CORS rule allow đúng origin staging,
`PUT`/`GET`/`HEAD`, expose `ETag`/`x-amz-version-id` và hai lifecycle rule abort incomplete sau
một ngày cho `tenants/`/`smoke/` đã được provision, read-back và kiểm tra idempotent. Runtime app
key không được nâng quyền. Single/multipart smoke PASS part PUT/CORS, complete immutable version,
exact-version GET, explicit abort và cleanup. P3-09 chuyển `IN PROGRESS -> VERIFY`; exact candidate
`04e30649` đã push và Security PASS. Verify phát hiện race thật ở feedback/focus sau gửi tin nhắn:
hook chờ cache refresh trước khi báo success và focus có thể chạy lúc composer còn disabled. Bản sửa
tách cache refresh thành background, chỉ focus sau khi mutation hết pending và có regression test với
refresh cố ý không hoàn tất. Full local `pnpm verify` PASS; host không có Docker nên exact Playwright
được giao cho CI. Shared staging vẫn `25 false`, chưa deploy.

### Checkpoint P3-08 hoàn tất ngày 2026-08-08

P3-08 đã chuyển `VERIFY -> DONE`: ADR-0026, vertical slice local, Neon disposable và exact
candidate CI/security đều đạt; shared staging chưa migrate và ứng dụng chưa deploy theo đúng
ranh giới task:

- forward migration `000026` tạo `content_files`, `tenant_file_usage`, feature
  `file_uploads` và bốn quota file; `PUBLIC` bị revoke và runtime provisioning dùng exact
  column-level `INSERT/UPDATE`;
- Go module `content` tạo/replay intent, reserve/reclaim quota, reauthorize class trong
  transaction, conceal foreign/inaccessible ID và finalize qua hai transaction tách khỏi
  lệnh `HeadObject` tới B2;
- finalize chỉ commit `pending -> uploaded` khi size, normalized MIME, SHA-256, ETag và
  immutable version đều đúng; object key/provider proof không xuất hiện trong public API,
  audit, outbox hoặc log;
- OpenAPI/generated client, capability projection, deployment guardrails và admin feature/
  quota controls đã cập nhật; chưa có presigned transfer, download/share, processing worker
  hoặc Class Files UI;
- `pnpm verify`, Go unit/vet, OpenAPI generated-contract, API client `37/37`, web `247/247`,
  lint/typecheck/build/Storybook và local security gates đều PASS;
- disposable owner preflight PASS với admin residual được ghi nhận; forward-only
  `25 false -> 26 false -> 26 false`, exact column ACL/runtime-role safety và `PUBLIC`
  zero-grant đều PASS, không rollback;
- PostgreSQL content integration package PASS, zero `SKIP`: concurrent intent/finalize,
  idempotency conflict, expiry/quota accounting, archived-class và tenant isolation đều đạt.

- CI đã phát hiện dependency ACL thật của `SELECT ... FOR SHARE`: runtime cần `UPDATE` trên ít
  nhất một cột của `classes` và `class_enrollments`. Isolated CI role hiện chỉ nhận
  `UPDATE(updated_at)`, exact-login probe fail closed nếu grant này bị mất; disposable rerun và
  stress concurrency `count=5` đều PASS.
- Exact candidate `6a50c3e4` đạt Verify #185: Quality/integration, local smoke và Browser E2E
  attempt 2 PASS. Browser attempt đầu chỉ flake focus ở conversation P3-07A; rerun riêng PASS.
  Security #183 PASS toàn bộ; final diff/secret review không phát hiện credential.

Acceptance và exact ACL được ghi tại `docs/P3_08_STAGING_ACCEPTANCE.md`. Real B2 proof cho
exact presigned PUT thuộc P3-09; nếu provider không trả checksum/version thì giữ feature off,
không nới điều kiện finalize.

### Checkpoint P3-07A hoàn tất ngày 2026-08-05

P3-07A đã chuyển `VERIFY -> DONE`: ADR-0025 được chấp nhận; migration forward
`000025`, REST API, OpenAPI/generated client và web message history/composer đã hoàn tất
local gate và disposable database acceptance.

- PostgreSQL giữ message/tombstone, server sequence, global author/client idempotency,
  monotonic self-read marker và O(1) `tenant_message_usage` counter dưới tenant advisory
  lock; runtime ACL chỉ cấp exact table/column allowlist qua provisioning riêng.
- Direct/class permission được reauthorize trong transaction; class archive và inactive
  direct peer giữ history read-only. Message content không đi vào audit, outbox, log,
  metric, cursor hay error detail; P3-07A không tạo notification/delivery side effect.
- API có keyset pagination, CAS edit/delete, tombstone projection, expected-tenant + CSRF,
  body `1..4000` Unicode code point/`16 KiB`, tenant storage/hourly quota, actor `60/phút`
  và typed `409/429` với `Retry-After` đã validate.
- Web `/app/messages` có persistent history, unread/read, memory-only retry ID, focus/keyboard,
  fail-closed khi quyền bị thu hồi và hàng đợi CSRF chung cho conversation mutations. Mark-read
  refetch message/detail/list để không bỏ sót message đến đồng thời.
- Local `go test -count=1 ./...`, `go vet ./...`, API client `34/34`, web `247/247`, lint,
  typecheck, build, format, generated-contract, security/bundle và Storybook gates đều PASS;
  integration-tag PostgreSQL code compile PASS bằng `-run '^$'`.
- Neon disposable owner preflight PASS ở `24 false`; direct/pool endpoint và runtime/
  migration role boundary đạt. Migration owner có admin residual được ghi tổng hợp nhưng
  runtime không nhận superuser/role/database/replication/bypass-RLS hoặc migration membership.
- Forward-only `24 false -> 25 false -> 25 false` PASS, không rollback. Exact runtime
  table/column ACL, `PUBLIC` zero-grant và role safety PASS; final ledger giữ `25 false`.
- Full PostgreSQL conversation suite 5/5, zero `SKIP`: P3-07A idempotency/lifecycle/read/
  quota/privacy, counter constraint/cascade/4.096-row query plan và P3-06 concurrency
  regression đều PASS. Riêng fixture query-plan 4.096 row được rollback và statistics
  được re-ANALYZE; các mutation fixture khác được giữ trên disposable để điều tra nếu cần.

- Exact candidate `a21ec385272d6f6da4200b1d76a6c438e45ba727` đã qua toàn bộ PR #33 và
  push-event CI/security; Cloudflare production xác nhận đúng candidate.
- Shared Neon owner preflight PASS, forward-only `24 false -> 25 false -> 25 false`, exact
  runtime ACL, `PUBLIC` zero-grant và role safety PASS. Không rollback và không chạy
  mutation/concurrency fixture suite trên shared.
- Render deployment `dep-d9pjeg8ae00c73f651t0` của exact candidate đã Live; direct Render
  và Pages proxy health/readiness/status `6/6` PASS, health/ready giữ `no-store`.
- Authenticated live acceptance PASS: Teacher gửi/reload không duplicate; Student nhận
  unread, đọc, trả lời và reload bền vững; archived class chỉ đọc; Admin ngoài direct bị
  conceal ở list và URL trực tiếp. Exact-candidate Browser E2E đóng retry/keyboard/focus/Axe;
  hai controlled canary zero-match trong Render logs.
- UI chưa cung cấp edit/delete nên CAS/stale `409`/tombstone/non-author được chứng minh bằng
  HTTP/PostgreSQL suite, không can thiệp SQL live. Unread query còn residual hiệu năng ở
  conversation cực dài; chỉ tối ưu sau load evidence bằng forward migration riêng.

Disposable branch tiếp tục được giữ theo quyết định của owner. P3-07B delivery vẫn
`DEFERRED/TODO`; runnable lane tiếp theo là P3-08 file metadata/upload intent/finalize.

### Checkpoint P3-06 ngày 2026-08-04

P3-06 đã khóa phạm vi REST-only cho conversation container; message persistence,
unread/read, realtime và notification vẫn thuộc P3-07A/P3-07B. ADR-0013 được amend:

- direct conversation đúng hai active member cùng tenant; request chỉ nhận exact target
  email, server tự resolve/canonicalize participant set và create lặp/concurrent phải trả
  cùng một conversation;
- mỗi class có tối đa một conversation, không copy roster; quyền đọc luôn lấy từ implicit
  owner, organization policy và enrollment authoritative;
- archived class giữ lịch sử read-only, chặn create/write mới; restore vẫn phải reauthorize;
- feature `conversations` có server-side emergency-off; quota tổng thể được giữ cho P3-13.

Vertical slice đã hoàn tất migration `000024`, Go module/API, OpenAPI/generated client và
web `/app/messages`; không bật worker, realtime hoặc delivery side effect. Toàn bộ exit gate
đã xanh:

- disposable rồi shared Neon được forward-only `23 false -> 24 false`; migrate lặp
  idempotent vẫn giữ `24 false`, exact runtime ACL cùng PostgreSQL canonical-create,
  authoritative-membership, archive, tenant/privacy/audit và concurrency gates đều PASS;
- candidate `756ca60aee579e60ae386c50020ecaf166d1a034` có Quality/integration,
  Browser E2E, Local environment smoke, Secret scan, CodeQL Go/JavaScript, repository và
  container scan cùng Cloudflare Pages đều PASS; dependency review skip đúng policy push;
- `pnpm verify` PASS với API-client `32/32`, web `237/237`, toàn bộ Go test/vet, lint,
  typecheck, build, Storybook, format và client-bundle/security checks;
- Render deployment `dep-d9ov689t0dsc73bu99a0` của exact candidate đã Live. Direct Render
  và Pages-proxied health/readiness đều trả `200 + Cache-Control: no-store`;
- authenticated live acceptance chứng minh Teacher/Student reverse-create trả cùng direct,
  projection đúng hai display name và không lộ email; Admin ngoài participant bị conceal
  `404`; class conversation bị conceal trước enrollment, trả cùng ID sau enrollment, bind
  sau archive trả `409` nhưng history cũ vẫn đọc `200` với trạng thái read-only;
- Browser E2E chạy UI thật với ba role đã PASS canonical direct/class, tenant concealment,
  privacy projection, Enter/Escape focus và Axe không violation trên direct/class page.
  Live acceptance cũng PASS heading focus, dialog autofocus, Escape trả focus, không
  overflow ngang; selector màu heading-action được thu hẹp sau khi Axe phát hiện contrast
  `1.55:1`, và exact candidate hiển thị chữ trắng trên nền primary đúng policy.

Acceptance class riêng được giữ ở trạng thái archived. Không có message persistence,
unread/read, realtime hoặc notification side effect được bật; các phần đó tiếp tục ở
P3-07A/P3-07B. P3-06 chuyển `VERIFY -> DONE` ngày 2026-08-04; runnable lane tiếp theo là
P3-07A.

### Cập nhật P3-02D-A ngày 2026-08-03

Automated database/deployment acceptance đã hoàn tất trên candidate
`8585864198ae0d9539c480e21e7b5efbbab0d389`:

- shared Neon forward-only từ `21 false` tới `23 false`; migrate lặp idempotent vẫn
  `23 false`, không rollback;
- owner preflight, exact runtime ACL, dedicated maintenance ACL/login, purge cascade/
  `SKIP LOCKED`, poll ownership/quota/tenant isolation/capability privacy/rate,
  StudyMeeting/ClassSession two-writer barrier và feature-control concurrency đều PASS;
- `corepack pnpm verify`, integration-tag compile và focused Playwright/axe desktop,
  public, mobile, keyboard, forced-colors `3/3` PASS;
- candidate Live trên Render deployment `dep-d9nvul5aeets73cpc330`; Cloudflare Pages,
  Browser E2E, Secret scan, Local environment smoke và CodeQL success. Direct Render và
  Pages-proxied health/readiness trả `200 + no-store`; public Pages/resolve giữ concealment
  và các header no-store/no-referrer/noindex/CSP đúng policy;
- notification/canary side-effect flags vẫn false; P3-02D-B auto-close/fan-out/delivery
  và LiveKit lifecycle chưa được bật. Disposable branch tiếp tục được giữ.

Authenticated live browser checkpoint đã chạy trên exact candidate:

- Teacher owner lifecycle, privacy projection, StudyMeeting và ClassSession business flow PASS;
- Student own-response/coarse projection, capability-negative finalize chỉ ra StudyMeeting và
  StudyMeeting cancellation PASS;
- non-owner class scheduler có authoritative `session.schedule` thấy individual projection/
  exact aggregate nhưng không có owner controls; ordinary class member không có capability chỉ
  thấy own/coarse projection;
- Admin negative response/owner-control matrix, cross-workspace concealment, inaccessible-class
  bind `404` không mutation, public fragment scrub/privacy và revoke cascade đều PASS;
- fixture poll đã hủy, ClassSession/StudyMeeting đã hủy và class fixture đã lưu trữ lại.

API-only safety-admin recovery checkpoint cũng PASS:

- exact Teacher/Admin role và P2-08 tenant preflight PASS;
- whitespace-only, 257-byte ASCII và 258-byte multibyte recovery reason đều bị từ chối mà
  không đổi version;
- Admin cross-owner capability revoke, poll cancel và StudyMeeting cancel PASS; revoked public
  token trở về uniform `404`, response không chứa raw token/share URL;
- audit actor/action/resource và exact metadata allowlist PASS; không có token/secret/email/
  title/description/answer/roster/session field;
- fixture poll và StudyMeeting đều terminal; cleanup PASS. Hai storage state app-origin-only và
  harness tạm nằm trong Git-ignored path đã bị xóa ngay sau test, không log credential.

Manual NVDA checkpoint trên exact organizer/public production route cũng PASS với NVDA
`2026.1.1.55980`:

- organizer heading/landmark, slot radio label/checked state, keyboard operation, save
  announcement và focus recovery PASS;
- stale two-tab `409` alert giữ draft và phục hồi focus PASS;
- public heading/deadline/timezone, slot label, low-cohort suppression, save announcement và
  focus recovery PASS;
- sau cancel/revoke, tab public đang giữ chỉ hiện trạng thái không khả dụng chung, không còn
  answer control và không lộ reason/identity/token; cleanup PASS.

Không lưu screenshot, trace, account identifier, capability token, cookie hoặc response payload.
Toàn bộ exit gate đã đạt và P3-02D-A chuyển `VERIFY -> DONE` ngày 2026-08-03. Hồ sơ chi tiết
nằm tại [`P3_02D_A_STAGING_ACCEPTANCE.md`](P3_02D_A_STAGING_ACCEPTANCE.md).

### Cập nhật P3-02D-A ngày 2026-08-01

P3-02D-A đã có vertical slice local ở trạng thái `VERIFY`: migration `000022`, normalized
poll/slot/participant/capability/response/answer/receipt và Study Meeting schema; Go API,
OpenAPI/generated TypeScript client; organizer/public React flow; manual lifecycle,
response/ranking/finalize; secure fragment exchange; owner-time conflict; audit/outbox;
feature kill switch, tenant quota và hard cap. Individual response dùng projection phân
trang riêng và chỉ mở cho owner, safety admin hoặc class member có authoritative
`session.schedule`; poll/public projection không nhúng dữ liệu này.

`VERIFY` không phải `DONE`: disposable đã xác nhận migration `000022` qua chuỗi up/down/up
`21 false -> 22 false -> 21 false -> 22 false`, sau đó forward `000023` và migrate lặp
idempotent đều ở `23 false`. Exact runtime/maintenance ACL, maintenance cascade/`SKIP LOCKED`,
poll ownership/quota/isolation/capability, StudyMeeting/ClassSession barrier, feature quota/rate
concurrency và full Calendar PostgreSQL package đã PASS. Shared staging, deploy Render/Cloudflare
và staging authorization/privacy/concurrency/accessibility smoke chưa có sign-off. Hồ sơ thực thi nằm tại
[`P3_02D_A_STAGING_ACCEPTANCE.md`](P3_02D_A_STAGING_ACCEPTANCE.md).

Security/architecture review ngày 2026-08-02 đã chấp nhận ADR-0024 và áp dụng migration
forward-only `000023`: purge function chuyển sang `SECURITY DEFINER`, fixed
`search_path = pg_catalog, pg_temp`, schema-qualified body, không dynamic SQL và revoke
`PUBLIC`. Maintenance contract sau provisioning chỉ còn schema `USAGE` + function
`EXECUTE`; không cấp direct table/column privilege. Disposable đã ở `23 false`; không có
rollback `000022` thêm.

P3-02D-B tiếp tục `DEFERRED/TODO`. Deadline worker auto-close, roster fan-out, email/ICS,
notification/reminder delivery và LiveKit room lifecycle đều chưa được bật; các feature/
worker/delivery/LiveKit consumer gate liên quan vẫn `false`.

Cross-writer code gate đã được harden local: Study Meeting và mọi writer tạo busy edge từ
one-time/recurring ClassSession, internal audience addition hoặc organizer transfer dùng
chung legacy-compatible advisory key theo tenant/user, khóa UUID theo thứ tự ổn định và
reverse-check scheduled StudyMeeting trong cùng transaction. HTTP trả conflict generic,
không lộ meeting riêng tư. Full `go test -count=1 ./...`, `go vet ./...`,
`corepack pnpm verify`, focused test và integration-tag compile đã đạt. Barrier hai writer
thật, exact runtime ACL probe, poll ownership/capability test, maintenance cascade và
feature-control concurrency đều PASS trên disposable; P3-02D-A tiếp tục giữ `VERIFY` cho
tới staging/browser/manual sign-off.

Gap kề cạnh được phát hiện khi harden recurring split: child series giờ giữ đúng organizer
đã transfer thay vì mặc định thành mutation actor, nhưng audience/participation settings
của parent vẫn chưa được copy/re-seal sang child. Owner-time reverse check chỉ dùng
occupancy thực sự được persist (organizer) để không false-positive. Audience continuity
cho following split phải được xử lý như regression riêng trước khi tuyên bố flow đó hoàn
chỉnh; task này không copy ciphertext invitation hoặc mở rộng delivery scope.

### Disposable database checkpoint — 2026-08-02

- Các URL database cần cho probe được nạp trong cùng command kiểm thử có credential; không
  ghi URL, password, role name, token hoặc fixture payload vào log/artifact.
- Owner preflight trước forward: `ENV_LOAD=PASS`, `OWNER_PREFLIGHT=PASS`; chỉ ghi nhận
  `OWNER_ADMIN_RESIDUAL=true` ở migration owner, không truyền sang runtime/maintenance.
- Disposable đã forward `22 false -> 23 false`; migrate lặp idempotent vẫn `23 false`. Không
  rollback thêm, không migrate/deploy shared staging và giữ nguyên disposable branch.
- Maintenance role đã đồng bộ password từ `DATABASE_POLL_MAINTENANCE_URL`; login và
  re-provision exact ACL đều PASS. Function metadata, direct-DML denial, cascade/detach và
  `SKIP LOCKED` đều PASS dưới maintenance credential.
- Exact Core API runtime ACL/role safety, poll ownership/quota/tenant isolation/capability
  lifecycle/privacy/rate, StudyMeeting/ClassSession barrier và feature-control quota/rate
  concurrency đều PASS trên PostgreSQL thật. Full Calendar integration package PASS.
- Full Classroom/feature-control package còn test debt ngoài gate: một số assertion dùng
  runtime login để `SELECT outbox_events` trái exact insert-only ACL và ba fixture/lifecycle
  Classroom không thuộc P3-02D-A còn đỏ. Không nới ACL; targeted barrier nêu trên đã PASS.
- Shared staging vẫn giữ nguyên `21 false`; Render/Cloudflare/browser/manual accessibility
  gates chưa chạy cho P3-02D-A.

### Mô hình thoát Phase 3 (re-baseline 2026-07-31)

Phase 3 được theo dõi theo hai lane, không còn coi toàn bộ phase là một chuỗi tuần tự:

- **Core Exit (runnable lane):** hoàn thiện các slice có thể chạy và nghiệm thu trên
  Render/Neon/B2 hiện tại: P3-02D-A poll/Study Meeting core, conversation/message core,
  file transfer core và các phần dashboard/quality không cần worker hoặc provider live.
  Khi checklist Core Exit xanh, owner có thể bắt đầu Phase 4.
- **Deferred carry-over:** P3-03B durable worker, P3-04 activation, P3-CAL-02 live
  SES/domain/interoperability, P3-05A/B delivery, P3-10/P3-11B processing phụ thuộc
  worker. Các lane này vẫn mở, tiếp tục được thực hiện sau khi Phase 4 bắt đầu và không
  được ghi `PASS` chỉ vì chưa có môi trường chạy.

Core Exit là một mốc chuyển tiếp, **không** phải `Phase 3 DONE` và không thay thế biên
bản P3-14. Full Phase 3 chỉ đóng khi carry-over gates, side-effect safety, staging
acceptance và `PHASE_3_COMPLETION.md` đều được sign-off. Trong thời gian chờ, các cờ
notification và Class Files activation vẫn `false`; không phát email/ICS/reminder hoặc
worker-driven file processing/sharing tới end user.

## Kiến trúc đang chạy

- Web: React + TypeScript + Vite trên `https://tutorhub-web.pages.dev`.
- Edge/BFF origin: Cloudflare Pages Function proxy `/api/*` tới Core API.
- Core API: Go modular monolith, OCI container trên `https://tutorhub-core-api.onrender.com`.
- Identity: ZITADEL OIDC Authorization Code + PKCE, session cookie phía BFF.
- Database: Neon PostgreSQL; staging tách branch và role runtime/migration.
- Object storage: Backblaze B2, application key tối thiểu quyền.
- Media: LiveKit Cloud; token do backend cấp, webhook được xác minh và lưu idempotent.
- Hugging Face không còn chạy Core API; chỉ là lựa chọn cho dịch vụ AI độc lập sau này.

## Đã hoàn thành

- P1-01 toolchain, monorepo, CI foundation.
- P1-02 React web shell, routing, TanStack Query, i18n và responsive states.
- P1-03 design tokens, UI primitives, Storybook và accessibility baseline.
- P1-04 Go API foundation: config fail-fast, graceful shutdown, structured log,
  request ID, Problem Details, metrics, health/live/ready.
- P1-05 OpenAPI source of truth, generated TypeScript client, PostgreSQL
  migrations, tenant context, outbox và integration tests.
- P1-06 OIDC/BFF authentication, session/CSRF/logout và workspace onboarding.
- P1-06B class vertical slice list/create/detail với authorization và tenant isolation.
- P1-07 LiveKit token service, prejoin, room UI, media controls, reconnect,
  telemetry và webhook receipt idempotent.
- P1-08A Verify/Security pipeline, Gitleaks, Dependency Review, CodeQL, Trivy,
  Dependabot, CODEOWNERS và bundle secret guard.
- P1-08B Cloudflare Pages production deployment, same-origin API proxy, Render
  Core API deployment, provider auto-deploy, health/readiness và rollback smoke.
- P1-09 PostgreSQL/Redis local bằng Compose, migration + seed idempotent,
  `local:setup` và `dev:local` một lệnh cho Windows/Linux.
- P1-10 staging resources: Neon, B2, Cloudflare Pages, Render, ZITADEL và LiveKit.
- Chấp nhận ADR-0011 để thay Hugging Face bằng Render cho Core API staging/private alpha.
- Rà exit gate Phase 1 trên commit `ee597af`: Verify/Security CI, HTTPS staging,
  OIDC, Neon, B2, LiveKit, telemetry và rollback đều đạt.
- Chấp nhận ADR-0012 cho direct-main có kiểm soát trong giai đoạn một người duy trì.
- P2-00: policy engine deny-by-default dùng chung cho identity/classroom/media;
  organization/class role matrix, effective permission, 403/404 concealment,
  OpenAPI enums/error conventions, policy test helpers và static boundary test.
- Chấp nhận ADR-0013 cho mô hình role tổ chức/lớp và authorization policy dùng chung.
- P2-01: profile GET/PATCH, identity list/link/unlink, recent-auth + state/nonce,
  collision protection, last-identity guard, audit/outbox, migration `000006`,
  OpenAPI/generated client và React settings UI có i18n vi/en.
- P2-02: tenant list/detail/create/update/archive, permission `tenant.view`/`tenant.manage`,
  optimistic version, session-context CAS, session/CSRF rotation và migration `000007`;
  success event `tenant.created/updated/archived/switched` được ghi durable qua outbox.
- Workspace UI áp dụng principal mới ngay sau create/switch/archive, hủy và xóa cache
  tenant-scoped để không flash dữ liệu workspace cũ; list/detail/update/archive có query,
  mutation và trạng thái lỗi phù hợp với contract typed.
- P2-03: permission `tenant.manage_members`, migration `000008`, invitation CSPRNG chỉ
  lưu purpose-bound HMAC, TTL/state machine, list/create/revoke và preview/accept bằng
  verified linked identity trong transaction idempotent; accept không tự đổi active tenant.
- Invitation URL giữ token trong fragment, web xóa fragment ngay và chỉ gửi token trong
  JSON POST body; admin UI có list/create/copy-once/revoke, public UI có đủ loading,
  offline, unavailable, mismatch, retry và success states bằng tiếng Việt/Anh.
- P2-04: migration `000009` bổ sung class `timezone`, optimistic `version` và
  `archived_at`; list có status filter cùng opaque keyset cursor, update/archive/restore/
  transfer ownership dùng `expected_version` CAS và transactional outbox.
- Class đi từ draft sang active; archive draft/active và restore đúng trạng thái trước.
  `owner_user_id` là owner implicit cho đến P2-05/P2-06, không tạo enrollment sớm.
  Chỉ `org_admin` hoặc owner có `class.archive`/`class.transfer_ownership`; target
  transfer phải là active member cùng tenant đủ điều kiện tạo lớp và actor phải có
  recent authentication trong 10 phút.
- Classroom UI đã có create/edit/activate/archive/restore, conflict recovery, status
  filter và pagination. LiveKit token/telemetry chỉ chấp nhận class active.
- P2-05: migration `000010` thêm enrollment lifecycle
  `invited -> active -> suspended/left/removed`, class invite code có TTL/usage limit
  và index/constraint tenant-scoped; owner tiếp tục là implicit ở `classes.owner_user_id`.
- Invite code dùng CSPRNG 256-bit và purpose-bound HMAC; raw token chỉ trả một lần
  trong create response, nằm trong URL fragment rồi được web xóa ngay. Join truyền
  token trong JSON body, khóa/cập nhật usage atomically và không tiêu thêm lượt khi
  active member, owner hoặc organization manager gọi lại.
- Direct enrollment, suspend, remove, revoke, join/rejoin và self-leave đều đi qua
  shared policy cùng repository tenant-scoped; outbox payload dùng allowlist và không
  chứa token, hash hoặc email.
- Class detail/list và LiveKit token/event dùng `viewer_access` do server resolve từ
  owner, organization manager hoặc enrollment active. Web có management panel,
  copy-once invite, public join và self-leave với đầy đủ loading/empty/error/
  forbidden/retry states bằng tiếng Việt/Anh.
- P2-06: manager-only roster API có owner implicit ghim riêng, normalized search theo
  display name/email, status filter và opaque keyset pagination bind đúng tenant/class/
  filter. Shared policy áp dụng hierarchy `org_admin > owner > teacher/co_teacher >
teaching_assistant > student/guest`, chặn self/peer/owner mutation và chỉ ownership
  transfer mới đổi owner.
- Single role update và bulk `update_role/suspend/remove` tối đa 50 user ID có ordered
  updated/unchanged/failed outcomes. Bulk commit từng item độc lập; web refetch cả khi
  mutation thành công hoặc lỗi hạ tầng. Role-changed outbox payload dùng allowlist và
  không chứa email/display name/token.
- Roster UI có search/status, infinite pagination, row/bulk confirmation, selection
  bằng keyboard, partial-failure summary và loading/empty/error/forbidden/archived
  states. Class management dùng class-scoped lifecycle capabilities từ server.
- LiveKit grant mới lấy class role authoritative và ghi effective/organization/class
  role attributes; token cấp sau mutation phản ánh role mới. JWT/participant đã tồn tại
  không được sửa hoặc thu hồi retroactively.
- P2-07 chấp nhận ADR-0014 và migration `000011`: `audit_events` tenant-owned,
  append-only bằng trigger `ALWAYS`, actor user/system, action/resource/outcome,
  request correlation, privacy-reduced source hints và flat metadata allowlist.
- Success audit của mutation P2-02 đến P2-06 được ghi cùng business transaction và
  outbox; authenticated no-op/denied/failed attempt dùng fallback transaction có
  server-generated request-instance ID để không ghi sai sau post-commit failure. Panic
  ở sensitive handler vẫn được audit rồi chuyển tiếp tới recovery middleware. Accept
  invitation bind tenant đích do server resolve; bulk roster dedupe từng target và ghi
  cả item lỗi hạ tầng/chưa được thực hiện.
- Permission `audit.view` chỉ cấp cho active `org_admin`. API
  `GET /api/v1/tenants/{tenant_id}/audit-events` reload membership authoritative,
  khóa path theo active tenant, hỗ trợ time/action/resource/outcome filter cùng opaque
  filter-bound keyset cursor và trả `no-store` projection không có IP/UA hash.
- Web có route `/app/workspace/audit`, permission gate, tenant-scoped infinite query,
  cache isolation khi switch/archive, filter validation, pagination và đầy đủ loading/
  empty/filtered-empty/error/forbidden/stale-refresh states bằng tiếng Việt/Anh.

## Kết quả acceptance staging ngày 2026-07-16

- Web và API HTTPS hoạt động; `/health` và `/ready` đạt trực tiếp và qua proxy.
- Readiness báo `database=ready` và `object_storage=ready`.
- OIDC login/callback, `/me`, reload giữ session, logout và đăng nhập lại đạt.
- Neon migrate -> rollback -> migrate đạt, version `5`, `dirty=false`; nhánh smoke
  tạm đã được kiểm tra không còn dữ liệu webhook thử nghiệm.
- B2 PUT/GET/checksum/DELETE đạt với key staging đã rotate.
- LiveKit 2-5 người đạt camera, micro, screen share và reconnect.
- LiveKit webhook HTTPS, xác minh chữ ký và idempotency đạt.
- Secret chỉ nằm trong file local bị Git-ignore hoặc secret store của provider;
  không xuất hiện trong repository, frontend bundle hoặc log kiểm thử.

## Kết luận Phase 1

Phase 1 hoàn thành ngày 2026-07-16. Biên bản và ma trận bằng chứng nằm tại
`docs/PHASE_1_COMPLETION.md`. Repository chưa có ruleset công khai; đây là ngoại lệ
được ghi nhận trong ADR-0012, không phải kiểm soát đã bật. Ngoại lệ phải hết hiệu lực
trước pilot/public beta hoặc khi có người duy trì thứ hai.

## Phase 2 đã hoàn thành ngày 2026-07-22

Backlog có thẩm quyền: `docs/PHASE_2_BACKLOG.md`.

1. P2-00 đến P2-12 đã hoàn thành; biên bản staging và đóng phase đã đạt exit gate,
   product/engineering owner sign-off ngày 2026-07-22.
2. P2-08 nối các contract workspace/invitation/class/roster/audit thành luồng UI
   org admin, teacher và student; capability guard, cache tenant/class, trạng thái
   forbidden/retry và navigation đã được chuẩn hóa.
3. [Verify #59](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888239)
   (`836ae7e`) xanh ngày 2026-07-20: Quality/integration, Browser E2E
   PostgreSQL 17 + Chromium và Local environment smoke đều đạt. Web 130/130, API
   client 15/15, UI 6/6 và E2E infrastructure 8/8 tiếp tục xanh.
4. Scenario Playwright ba role với fake OIDC loopback/PKCE đã chạy xuyên suốt
   workspace/invitation/class/roster/archive/audit trên CI. Visual QA thủ công đạt tại
   1440x900, 1024x768 và 390x844.
   [Security #54](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888233)
   cùng commit cũng xanh.
5. Acceptance UI staging P2-08 được chạy lại ngày 2026-07-20 trên fixture dùng một
   lần với ba identity ZITADEL đã xác minh riêng biệt cho org admin, teacher và
   student. Luồng tạo/chỉnh/chuyển workspace; tạo/thu hồi/chấp nhận invitation;
   teacher tạo/chỉnh/kích hoạt lớp và tạo join link; student join; teacher đổi role,
   suspend, remove, thu hồi link và archive lớp đều đạt.
6. Org admin xem được audit đúng actor, action, resource, outcome và request ID cho
   toàn bộ chuỗi thao tác. Lượt nghiệm thu không dùng SQL/manual API và không lưu
   storage state, token hay secret vào repository hoặc artifact. Deployment/contract
   drift ghi nhận ở lượt kiểm tra trước đã được đồng bộ; P2-08 chuyển `DONE`.
7. P2-09 chấp nhận ADR-0015, migration `000012`, typed catalog với global safety
   ceiling và tenant override có optimistic version. Quota member/active class/
   invitation được enforce transactionally ở server; capability API/UI fail-closed,
   thay đổi override có audit và quota rejection có metric.
8. Anonymous invitation preview/accept và class join dùng signed edge context cùng
   shared PostgreSQL limiter. Web 139/139, API client 16/16, root format/lint/
   typecheck/build/test/security bundle cùng full Go non-integration suite và `go vet`
   đều xanh cục bộ.
9. Acceptance P2-09 ngày 2026-07-21 đạt trên commit `096620a`: Render và Cloudflare
   cùng chạy head này; health/readiness/status trực tiếp và qua Pages đều trả 200.
   Neon staging ở migration `12 false`, runtime grants đúng ma trận tối thiểu và role
   không sở hữu bảng/không có quyền nguy hiểm. Signed edge/public limiter trả 404 cho
   token giả và ghi window active với `used_count=1`.
10. Hai integration test feature-control chạy bằng runtime role trên Neon staging đã
    đạt: feature disabled, tenant isolation, audit/outbox và concurrent member/class/
    invitation quota đều giữ invariant. HTTP regression xác nhận
    `403 feature_disabled`, `404 tenant_not_found`, `429 quota_exceeded`; metric quota
    rejection dùng label bounded. Bounded cleanup xóa `0` rate-limit window và `0`
    tenant-quota window; P2-09 chuyển `DONE`.
11. P2-10 ngày 2026-07-22 đã bổ sung actor/resource matrix, PostgreSQL
    security fixture có rollback, exact foreign class/user/invite ID invariants, stale
    membership và workspace-switch token rotation. HTTP mutation dùng strict JSON object,
    từ chối unknown/duplicate/trailing/oversized payload; resource UUID ở path/query chỉ
    nhận dạng canonical. Class cursor v2 bind tenant/filter và class/roster cursor dùng
    strict decoder. Chín fuzz function cho JSON, UUID, invitation token, cursor, roster
    search và media identifier đều đạt. Commit `c4205b9` đã xanh trên
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539891), gồm
    PostgreSQL 17 matrix, và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539912),
    gồm CodeQL, Trivy repository/container cùng secret scan. Không có finding
    High/Critical chưa xử lý; P2-10 chuyển `DONE`.
12. P2-11 đã chấp nhận ADR-0016 và bổ sung migration `000013`, fixture JSON ẩn danh,
    CLI `v1-fixture-import`, external-ID mapping, per-record checkpoint/resume và
    reconciliation report. Importer chặn production, từ chối payload không strict hoặc
    email ngoài `.invalid`, không đọc dữ liệu/configuration V1 thật. PostgreSQL 17 local
    tạm xác nhận full integration; commit `f07d05d` đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333712) và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333728).
    Migration `13 -> 12 -> 13`, dry-run, apply/rerun, checkpoint/resume, reconciliation
    và cleanup/reset database tạm đều đạt; P2-11 chuyển `DONE`.
13. P2-12 đã mở rộng scenario Playwright ba role để kiểm tra class invite link đi từ
    `0/2` sang `1/2` lượt dùng, roster vẫn được giữ sau archive, invite link còn active
    không thể dùng để join lớp đã archive, và audit create-class có actor, resource ID
    cùng request ID. Implementation nằm ở commit `bf30605`; các commit `7563ed1` và
    `6fb4f84` thu hẹp locator audit để tránh match nhầm action có tiền tố giống nhau.
    Candidate `6fb4f84` đã đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433), gồm
    Browser E2E PostgreSQL 17 + Chromium, và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424).
    Checkpoint `3c48964e3900b2a262c4026abf0174b3c39c5d93` tiếp tục đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093175)
    và [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093166);
    Cloudflare Pages check suite cùng full SHA cũng success.
14. Neon P2-12 ngày 2026-07-22 đã chạy trên branch dùng một lần từ staging:
    `12 false -> 13 false -> 12 false -> 13 false`. Importer dry-run lập kế hoạch 12
    record; apply nhập 10, skip 2, fail 0; rerun giữ 10 unchanged, không duplicate;
    reconciliation có 2 run, 10 mapping và 24 item, sau đó branch được xóa. Neon staging
    thật được forward `12 false -> 13 false`.
15. Lượt provider audit phát hiện default ACL cũ của Neon owner tự cấp CRUD cho runtime
    trên bảng mới. Provisioning đã thu hồi default table ACL global/schema-scoped và
    grant materialized trên ba ledger. Kết quả cuối: default ACL leak `0`,
    effective/direct ledger privilege `0`, runtime không owner/superuser/bypass RLS,
    schema `USAGE=true`, `CREATE=false`, audit chỉ `SELECT/INSERT`, future-table probe
    không có quyền. Không sửa migration lịch sử `000013` hoặc re-grant khi rollback.
16. Sau staging migration, Render đã live release candidate full SHA
    `3c48964e3900b2a262c4026abf0174b3c39c5d93` qua deploy
    `dep-d9gaiturnols73c75qp0`. Public health/readiness/status trực tiếp Render và qua
    Pages proxy đều HTTP 200 (6/6), readiness báo database/object storage ready.
17. Lượt UI staging S01-S07 chạy khoảng 19:06-19:36 ngày 2026-07-22 trên workspace
    `P2-12 Acceptance 202607221900`, class
    `f61e3344-251f-42eb-b3bc-90fd9f9cff5d`, đã đạt. Admin tạo workspace/mời hai role;
    teacher/student accept, login và switch; teacher tạo class active cùng link 1 ngày,
    2 lượt (`0/2 -> 1/2`); student join; roster đổi Trợ giảng, suspend, remove và refresh
    vẫn giữ `Đã xóa`. Cross-tenant UI conceal exact class và exact audit filter ở `KMA`
    trả 0; exact foreign roster/media-token tiếp tục dùng P2-10 automated baseline,
    không có direct staging POST room-token trong lượt này. Archive chặn identity chưa
    enroll join qua link còn `1/2` và vẫn giữ roster history. Audit workspace có 22 event,
    exact class có 5 event create/update/roster/archive, actor/resource/request ID đầy đủ
    và denied join cũng được audit.
18. S09 provider closure đạt khoảng 19:39-19:43 ngày 2026-07-22. Render live latest
    `0be98bb`, application rollback bằng `Deploy a specific commit` về `3c48964`, rồi
    forward lại `0be98bb`; mỗi bước đều đạt 6/6 probe direct Render và qua Pages. Native
    Rollback được cancel an toàn vì cảnh báo không tải được cấu hình live; không có thay
    đổi cấu hình được tuyên bố. Phiên Admin/audit vẫn hoạt động sau forward deploy.
19. Owner đã sign-off P2-12. Closure-record docs-only phải được hậu kiểm Verify/Security
    sau push; nếu một workflow thất bại thì mở lại P2-12 và khắc phục regression.

## Phase 3 bắt đầu ngày 2026-07-22

Backlog có thẩm quyền: `docs/PHASE_3_BACKLOG.md`.

1. P3-00 đã tạo task ID/dependency/acceptance/exit gate cho daily learning workspace.
2. ADR-0017 chốt P3-01 session một lần: instant UTC + IANA timezone, DST gap/overlap,
   optimistic version, tenant/class policy và boundary với recurrence/Phase 4 media.
3. ADR-0018 chốt worker process riêng trong cùng Go modular monolith: PostgreSQL lease
   có fencing token, at-least-once, retry/backoff, idempotency và dead-letter; chưa thêm
   Redis/NATS/Kafka hoặc provider mới.
4. P3-01 đã hoàn tất vertical slice local: migration `000014_class_sessions`,
   permission/feature control, OpenAPI/generated client, Go repository/service/HTTP,
   timezone/DST validation và UI tối thiểu trên class detail. Scope không gồm
   recurrence, reminder, calendar tổng hợp, worker runtime hoặc media lifecycle.
5. P3-CAL-00 đã audit Google Calendar, Microsoft Teams, Zoom, ClassIn, các lựa chọn
   mã nguồn mở và TutorHub V1; kết quả nằm tại
   `docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md`.
6. Đề xuất calendar-first dùng FullCalendar Standard chỉ làm renderer sau spike; domain,
   quyền, recurrence, conflict, reminder và LiveKit vẫn do TutorHub sở hữu. Chưa thêm
   dependency hoặc runtime code trong P3-CAL-00.
7. V1 chỉ được giữ làm nguồn nghiệp vụ: event/task/availability poll, quick create và
   panel agenda. Không port CalendarFX/DAO/model vì V1 hard-code user, JDBC trực tiếp,
   thiếu tenant/timezone/DST/version/audit và nhiều control chỉ là vỏ UI.
8. P3-CAL-01 đã chốt ADR-0019 về series/exception/occurrence, recurrence DST,
   conflict policy và exact dependency pin sau performance, accessibility automated,
   license và security spike. Manual NVDA được giữ làm explicit production-route gate.
9. P3-CAL-00B đã nghiên cứu lại Teams/Google và CSS live Vauliys; chốt Teams-inspired
   IA/editor, Warm Academic cream palette và professional everyday parity. Không sao
   chép asset/font/trade dress và chưa đổi runtime token.
10. Invitation/update/cancellation/reminder email, ICS và RSVP đã được đưa vào Phase 3
    exit gate. Owner đã chọn AWS SES làm provider target; P3-CAL-02/ADR-0020 vẫn phải
    xác minh account/region/sandbox/quota, adapter, provider-event ingress, iTIP/iMIP và
    deliverability.
    Trước khi có domain chỉ thử bằng owner-controlled verified identities trong SES
    sandbox; production vẫn cần domain/DNS cùng SPF/DKIM/DMARC. Mọi effect runtime chỉ
    chạy sau commit qua P3-03 worker. Đây là re-baseline tài liệu, chưa phải chức năng
    đã chạy.
11. ADR-0021 đã `Accepted` và chốt P3-02D Native Availability Poll: mọi active
    authenticated tenant member, gồm student, được tạo/quản lý poll và Study Meeting của
    mình theo feature/quota. Poll có class-only, invited-only và explicit anyone-link;
    public capability chỉ lưu hash, có expiry/revoke/scope/rate limit và không lộ roster/
    email/individual availability. Chỉ actor có `session.schedule` mới finalize thành
    ClassSession; actor khác chỉ tạo Study Meeting. When2meet chỉ là comparator, không
    phải runtime/API/iframe/fork/code dependency.
12. P3-CAL-00C đã rà soát readiness lần cuối bằng nguồn chính thức và upstream: tách
    P3-02A/B/C cùng P3-05A/B, kéo P3-03 lên trước consumer side effect, bổ sung
    WorkingSchedule/suggested-time contract, audience diff, reminder lifecycle,
    split-exception preview, direct StudyMeeting API, poll close/reopen và hardening
    capability link. SES không có caller idempotency token; timeout mơ hồ dùng app effect
    ledger + trạng thái `outcome_unknown`; canonical delivery state không gọi
    mail-server acceptance là inbox. Kế hoạch đã khóa required VCALENDAR/MIME một calendar
    part, full durable provider-event path tới inbox/consumer, iterator recurrence có
    cancellation/cap, DST suggested-time total order và giới hạn đúng mức của public-poll
    cohort/dedupe. Vòng hậu kiểm đã tách `CalendarDisplayPreference` về P3-02A,
    `WorkingSchedule` về P3-02C; tách P3-02D thành P3-02D-A core (có thể triển khai
    không cần durable worker) và P3-02D-B lifecycle delivery/auto-close/fan-out
    (`DEFERRED/TODO`, chờ P3-03B/P3-04 activation). P3-05B là delivery adapter downstream
    ở carry-over. P3-02D-A đã `DONE` sau exact staging/browser/API/NVDA acceptance; mọi
    delivery thuộc P3-02D-B vẫn chưa được triển khai hoặc bật.
13. P3-CAL-01 đã `DONE` ở cấp decision spike. FullCalendar Standard `7.0.1`,
    Temporal `1.0.1`, Warm Academic theme và adapter boundary nằm trong
    `apps/calendar-spike`; comparator v6.1.21 nằm riêng ở `apps/calendar-spike-v6`.
    Typecheck/lint, 8 unit/DOM test, build, dependency/license guard 3/3 và grouped
    Playwright interaction/a11y/performance đều đạt. Full rerun v7 hậu fix đạt
    `9 passed (23.6s)`, exit 0; Axe critical/serious bằng 0, waiver upstream
    `empty-table-header` bị khóa exact một node/target/HTML/scope.
    V7 render p95 `152/164/201 ms`, navigation p95 `204/327/548 ms`, long-task max
    `79/198/315 ms` ở 500/1.000/2.000 item, heap delta 2.000 item `26,34 MiB`,
    JS/CSS gzip `155.15/5.37 KiB`. Comparator parity-config v6 full run đạt
    `4 passed (17.5s)` và heap nhỏ hơn `7,44 MiB`, nhưng fail render 500
    `1.492 > 500 ms` cùng long-task 2.000 `404 > 400 ms`, nên bị loại. Agenda
    progressive `24 -> 48 -> 51` đạt keyboard/mobile automated evidence. ADR-0019 được
    `Accepted; manual NVDA gate PASS`; FullCalendar vẫn chưa nối route production vì
    còn route-level authorization/range/bundle/a11y gate.
14. P3-01 local verification ngày 2026-07-24 đạt web typecheck/test/build, API-client
    typecheck/test và các Go package liên quan. Contract chỉ hỗ trợ session một lần,
    lifecycle public `scheduled -> cancelled`, bounded list/range/duration, optimistic
    version, audit/outbox và feature kill switch.
15. Neon staging đã migrate `13 -> 14` và trả `14 false`; runtime role có
    `SELECT/INSERT/UPDATE` nhưng không có `DELETE/TRUNCATE`. Feature commit `b58666c`
    cùng security patch `a5741a1` đã được deploy; Render direct và Cloudflare same-origin
    health/readiness/status public probes đều xanh.
16. Web acceptance fix `e7dc161` sửa regression dialog edit không hydrate dữ liệu và thêm
    regression test. Browser staging xác nhận Teacher create/update/cancel, Student xem
    session nhưng không có create/edit/cancel, và exact foreign class ID trả trạng thái
    `404` không lộ tên lớp/session. P3-01 chuyển `VERIFY -> DONE`; biên bản tại
    `docs/P3_01_STAGING_ACCEPTANCE.md`. Lượt browser này không được mô tả là Playwright.
17. Go recurrence spike đã khóa query window `366 ngày`, series horizon `730 ngày`,
    `512 occurrence/series`, `2.000 occurrence/request` và deadline `250 ms`.
    COUNT compile validate occurrence cuối trong horizon, boundary test bao phủ
    DAILY/WEEKLY/MONTHLY/YEARLY và YEARLY golden đã đạt. Lượt post-fix unit PASS; hai
    fuzz target 10 giây đạt `238.755`/`199.088` executions. Benchmark 366 occurrence
    count=5 đạt `706.580 ns/op` UTC, `674.220 ns/op` Ho Chi Minh và `984.380 ns/op`
    New York. P3-02B đã đưa adapter typed vào production boundary, nhưng vẫn phải đạt
    integration/load/staging gate khi nối persistence và occurrence API.
18. P3-03A repository/runtime foundation đã đạt `VERIFY`: migration `000015` bổ sung lease,
    fencing token và retained dead-letter; `cmd/worker` dùng credential riêng, exact
    typed registry, bounded claim concurrency, retry/backoff, graceful shutdown và
    structured heartbeat/metric. Startup ACL probe fail-closed nếu worker role không có
    đúng direct-LOGIN exact ACL, có membership/ownership/DDL/quyền bảng nghiệp vụ khác,
    hoặc có INSERT/DELETE/TRUNCATE/REFERENCES/TRIGGER dư thừa.
    Unit test, integration compile gate, duplicate/poison/shutdown PostgreSQL fixtures,
    CI PostgreSQL step, OCI image chung và `docs/P3_03_OUTBOX_WORKER_RUNBOOK.md` đã có.
    Registry production gate-off rỗng có chủ ý và vẫn phát heartbeat định kỳ. Máy local
    không có Docker/psql hoặc migration URL nên PostgreSQL runtime suite phải chạy ở CI;
    worker role/worker grants, durable host và staging crash/reclaim vẫn là gate bên
    ngoài trước `DONE`.
19. P3-04 implementation đạt local `VERIFY` theo ADR-0022. Migration `000016` tạo
    notification projection và preference tenant/user-scoped. Worker chỉ đăng ký exact
    `notification.in_app_canary.requested.v1` khi
    `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY=true`; gate mặc định false, sink idempotent
    và `system.worker_canary` luôn bị API feed loại. API có list keyset, unread bounded,
    mark one/all read và preference GET/PUT CAS; scope authoritative lấy từ session,
    `X-TutorHub-Expected-Tenant-ID` chỉ là assertion chống workspace/cache race.
    Web có bell, center, preference và bounded polling 30 giây, không polling nền.
    `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS=false` ép visibility tắt mặc định.
    Tests/module/HTTP/config/worker/API-client/web liên quan đạt local; không gọi email
    provider. Neon staging đã ở `17 false` sau khi áp `000015 -> 000016 -> 000017`;
    API runtime exact grants đã được probe xanh, nhưng `tutorhub_worker` chưa provision nên
    durable host/canary/crash-reclaim acceptance chưa có. Vì vậy P3-03B vẫn
    `DEFERRED/VERIFY`, P3-04 vẫn `VERIFY` và chưa có end-user activation.
20. P3-02A read foundation đạt local `VERIFY` ngày 2026-07-26. Migration `000017` thêm
    bounded-read index cho ClassSession và `calendar_display_preferences` theo
    tenant/user. Go API có projection ổn định, overlap half-open, range tối đa 366 ngày,
    filter/search/keyset cursor bind tenant/actor/range/filter/timezone, source-policy
    capability và preference full-replacement CAS. HTTP yêu cầu expected-tenant assertion,
    PUT yêu cầu CSRF; OpenAPI/generated client đã đồng bộ.
21. Web có top-level `/app/calendar`, năm URL view Day/Work week/Week/Month/Agenda,
    mobile mặc định Agenda, mini month, filter/search, preference drawer, secondary
    timezone badge và semantic Agenda progressive. Cache key gồm tenant/user/timezone/
    range/filter; workspace switch/logout hủy/xóa calendar cache. Loading, empty,
    filtered-empty, error, forbidden, offline/degraded, retry và pagination đã có.
    Mutation preference dùng principal generation để chặn response cũ ghi lại cache sau
    switch/logout; drawer remount theo tenant/user và Agenda giữ đúng local date ở UTC+14.
    Warm Academic calendar tokens cùng production guard fail-closed chỉ cho exact
    FullCalendar Standard v7.0.1 đã duyệt; Premium, telemetry và CSS/assets ngoài allowlist
    vẫn bị chặn.
22. Local verification P3-02A đạt toàn bộ Go test + vet và integration-tag compile;
    API client 22/22; web 176/176 + lint/typecheck/build; format, security 19/19,
    production Calendar guard và 20 cặp contrast đều xanh. PostgreSQL integration thật
    không chạy vì process không có hai biến database và agent không đọc `.env*.local`.
    Quick create/full editor timed one-time ClassSession, production renderer và
    drag/resize/undo đã nối; manual NVDA đã PASS.
23. P3-02A staging rollout ngày 2026-07-26: tạo backup branch Neon
    `p3-calendar-pre-migration-20260726` (auto-delete 7 ngày, parent `staging`), rồi migrate
    direct staging `14 false -> 17 false`. Exact runtime ACL probe đạt: schema
    `USAGE=true/CREATE=false`; outbox chỉ column INSERT; notifications chỉ SELECT +
    UPDATE(read_at); notification preferences SELECT + allowlist INSERT/UPDATE; calendar
    display preferences SELECT/INSERT/UPDATE; mọi quyền DELETE/TRUNCATE ngoài allowlist đều
    `false`. `tutorhub_worker` chưa tồn tại nên worker ACL và durable-worker gate vẫn để
    P3-03B; public health/ready Render và Cloudflare đều HTTP 200.

24. Manual NVDA review P3-CAL-01 đã PASS ngày 2026-07-26 với NVDA 2026.1.1 trên môi
    trường cài đặt. Người dùng xác nhận PASS cho heading/landmark, event label/time/
    category, view switch/Agenda, keyboard alternative, focus restore sau 409 và live
    announcement. Đây là bằng chứng đóng NVDA gate; không thay thế route-level
    authorization/range/a11y/bundle, visual hoặc browser acceptance của P3-02A.
25. P3-02A renderer/editor integration ngày 2026-07-26: exact pin
    `@fullcalendar/react@7.0.1` đã được nối vào `/app/calendar` cho Day/Work week/Week/Month;
    Agenda semantic được giữ làm mobile view và keyboard alternative. Drag/resize one-time
    gọi lại command ClassSession với expected version, optimistic revert, undo và thông báo
    live-region khi server trả `409`. Route unit integration, web lint/typecheck/test/build,
    production Calendar guard và rerun spike 9/9 đều đạt. Calendar lazy chunk khoảng
    298,64 kB (82,29 kB gzip).
26. P3-02A chuyển `VERIFY -> DONE` ngày 2026-07-26. Production-route Playwright đạt 8/8
    với authorization/range/pagination, Axe, semantic Agenda, zoom/forced-colors/
    reduced-motion và visual snapshots desktop/tablet/mobile. Render/navigation/long-task
    p95 cho 500/1.000/2.000 item là `177/266/102`, `310/481/180`,
    `570/716/326 ms`, đều trong budget ADR-0019. Commit `0606813` đã manual deploy lên
    Render vì Auto-Deploy đang tắt; bốn public health/ready probe đều HTTP 200. Browser
    staging Admin xác nhận empty-state, Agenda URL state, Preference drawer và Quick Create
    no-class state, không tạo mutation. Biên bản:
    `docs/P3_02A_STAGING_ACCEPTANCE.md`.
27. P3-CAL-02 đạt local `VERIFY` ngày 2026-07-26. ADR-0020 khóa audience/organizer/RSVP,
    stable UID + monotonic sequence, RFC 5545/5546/6047 outbound subset, CTA-only
    `RSVP=FALSE`, one-recipient privacy, deterministic canonical payload, SES ambiguous
    outcome và đường provider event
    `Configuration Set -> EventBridge -> SQS/DLQ -> worker -> PostgreSQL inbox`.
    Package cô lập `internal/spikes/calendarinvitation` có audience diff, bounded
    iCalendar/VTIMEZONE/MIME renderer, trusted CTA origin/tzdata policy, opaque effect
    identity, deterministic sink và SES v2 `Content.Raw` adapter pin sender/configuration
    set với SDK retry tắt. Bảy golden lineage create/update/reschedule/role/override/
    split/cancel, focused coverage `85,4%/81,0%`, fuzz 10 giây hơn 1,28 triệu lượt,
    focused/full Core API test và vet đều đạt. Spike không được import vào runtime,
    không consume outbox và không gửi business email. ADR giữ `Proposed` vì SES sandbox,
    EventBridge/SQS/DLQ/inbox, bounce/complaint/suppression, sending domain/DNS và
    Gmail/Outlook/Apple interoperability chưa có live evidence.
28. P3-02B chuyển `IN PROGRESS -> VERIFY` ngày 2026-07-27: typed recurrence boundary,
    migration `000018/000019`, series/exception repository/service/HTTP, scope preview,
    split/cancel/idempotency/audit/outbox, override capability, recurring UI và
    recurrence metrics đã có. Local package tests, HTTP/classroom tests, `go vet`,
    migration-fragment checks, integration-tag compile, API client 22/22, web
    typecheck/lint/176 tests và format check đạt. Neon migration/grants, tenant canary
    và focused Teacher staging smoke đã đạt ngày 2026-07-28; query-plan/concurrency,
    authorization và cross-tenant acceptance vẫn mở nên chưa mô tả P3-02B là `DONE`.
    Runbook nghiệm thu nằm tại
    `docs/P3_02B_STAGING_ACCEPTANCE.md`.
29. Feature key `class_session_recurrence` đã nhất quán từ migration/catalog/config/
    guardrail tới tenant capability OpenAPI/generated client và UI.
    `FEATURE_CONTROL_ENABLE_CLASS_SESSION_RECURRENCE=false` mặc định fail-closed;
    Go package/config/HTTP tests, API check, web typecheck và 176 web tests đều xanh.
    Domain read-overlay đã có bounded expansion + cancel/override và DST/civil-time
    test; series repository/mutation/HTTP nối vào SQL Calendar projection. Deployment
    guardrail toàn cục vẫn fail-closed; staging chỉ mở bằng tenant override cho canary.
30. P3-02B staging checkpoint ngày 2026-07-28 đã đạt migration `19 false`, exact runtime
    ACL và Teacher UI mutation. Receipt append-only giữ `SELECT/INSERT`; commit `c622244`
    bỏ `FOR UPDATE` thừa khỏi receipt lookup thay vì cấp thêm `UPDATE`, toàn bộ Go test
    đạt và Render health `200`. Drag occurrence 2026-07-30 sang `11:30–12:30` theo
    `this_occurrence` giữ nguyên sau reload; hai tuần sau vẫn `13:00–14:00`. Bốn
    recurrence metrics hiện diện và có số liệu. P3-02B tiếp tục `VERIFY` cho concurrent/
    idempotency, split/exception retention, Student/Admin authorization, cross-tenant và
    bounded query-plan evidence; biên bản tại `docs/P3_02B_STAGING_ACCEPTANCE.md`.
31. P3-02B chuyển `VERIFY -> DONE` ngày 2026-07-28 tại acceptance commit `734d2b6`.
    Neon disposable branch `p3-calendar-pre-migration-20260726`
    (`br-silent-math-aozfo2ci`) đạt literal `19 false` và automated PostgreSQL test
    trong `25,75s`: concurrent identical replay 1/1, competing edit success/conflict
    1/1, Student read-only, Teacher override bị từ chối, Organization Admin override
    có reason, cross-tenant concealment, split giữ exception trước boundary và carry
    đủ exception tương lai. Query class-scoped bị chặn `LIMIT 129` và dùng được index
    `class_session_series_class_start_idx`. Full Core API test và vet đều xanh; không
    tạo/xóa Neon branch mới. P3-02C là bước nghiệp vụ Calendar tiếp theo.
32. P3-02C chuyển `TODO -> IN PROGRESS` ngày 2026-07-28 với lát cắt working schedule
    và privacy-safe availability đầu tiên. Migration `000020` thêm lịch làm việc theo
    tenant/user (nhiều interval/ngày, holiday/OOO/special hours, IANA timezone, CAS),
    attendee/audience persistence foundation và RSVP-capability foundation. Core API có
    `GET/PUT /api/v1/calendar/working-schedule` cùng
    `POST /api/v1/calendar/availability/query`; PostgreSQL repository fail-closed theo
    feature `class_session_scheduling`, tenant/class authorization và participant scope
    server-side (class owner/active roster hoặc external audience đã gắn class); ID không
    đủ scope bị conceal 404 và không project title/description/email/roster. Availability
    bị chặn ở 31 ngày lịch theo scheduling timezone/50 người/2.000 start/250 ms;
    external/no-sync là `unknown`; grid civil-time xử lý DST gap/overlap. Web
    có drawer Working hours với CAS/conflict recovery và accessibility cơ bản. Chưa đánh
    dấu task `DONE`: audience command, RSVP domain/API/UI, Scheduling Assistant gắn vào
    session editor và PostgreSQL staging acceptance vẫn còn lại.
33. P3-02C đạt internal one-time participation checkpoint ngày 2026-07-29. Core
    API/OpenAPI/client có privacy-filtered audience replacement và authenticated self RSVP
    với CAS/idempotency; PostgreSQL giữ current RSVP authority cùng encrypted neutral
    invitation/recipient snapshot, audit và outbox domain fact. Web có attendee/organizer
    summary, self RSVP và Scheduling Assistant manager-only với working interval, canonical
    free/busy status, conflict reason, dual timezone và semantic table; 192 web tests cùng
    lint/typecheck/build đều xanh. Task vẫn `IN PROGRESS` vì còn manager roster editor,
    external guest/capability, typed series/occurrence, organizer/archive lifecycle,
    concurrency/IDOR/E2E và Neon migration `000020/000021` + ACL/staging smoke.
34. P3-02C đạt recurring participation checkpoint ngày 2026-07-29. Manager roster editor
    có active-roster search/cursor, required/optional role, cap 128, 403 conceal/retry và
    409 focus recovery. Typed OpenAPI/client/HTTP hỗ trợ session/series/occurrence; occurrence
    đọc inherited audience revision `0` và copy-on-write thành revision `1` khi thay đổi hoặc
    RSVP đầu tiên, không làm đổi series hay occurrence khác. `go test ./... -count=1`,
    integration-tag compile, 10 web tests, 25 API-client tests, lint/typecheck và Vite build
    đều xanh. P3-02C vẫn `IN PROGRESS` vì external guest/capability, diff/lifecycle policy,
    full concurrency/IDOR/E2E và Neon migration `000020/000021` + ACL/staging acceptance còn lại.
35. P3-02C đạt local implementation gate và chuyển `IN PROGRESS -> VERIFY` ngày
    2026-07-29. OpenAPI/generated client, Core API và web đã nối external audience,
    purpose-bound RSVP capability hash/expiry/revoke/rate limit, public RSVP chỉ nhận
    capability trong POST body và privacy-safe projection. Audience replacement có
    deterministic added/removed/unchanged/role-change cùng RSVP retain/reset; organizer
    transfer giữ source authority; cancel session/series/occurrence đóng response,
    revoke capability và giữ immutable cancellation invitation snapshot. Focused HTTP,
    domain, PostgreSQL integration-tag compile và web/API-client coverage đã có ở local.
    Trạng thái chỉ là `VERIFY`: migration `000020/000021` đã đạt `21 false`, exact runtime
    ACL và role isolation đã đạt; browser staging mới xác nhận audience PUT và Student
    self-RSVP sau reload. Concurrent/IDOR, public capability và lifecycle live vẫn mở.
36. P3-02C đạt staging participation checkpoint ngày 2026-07-30 trên commit `32a770ac`.
    Teacher audience PUT và Student self-RSVP đều trả HTTP 200, reload giữ trạng thái và
    ordinary participant không thấy external roster/email. Public capability fixture sau
    đó được chạy trên disposable Codespace; task tiếp tục ở `VERIFY` cho đến khi full
    concurrency/IDOR, organizer/cancel/archive và accessibility live được nghiệm thu.
37. P3-02C regression local được chạy lại ngày 2026-07-30: focused Go
    calendar/classroom/http API suites, API client `27/27` và Calendar E2E `11/11` đều
    đạt. Evidence này không thay thế live fixture public RSVP, Teacher lifecycle,
    cross-tenant/IDOR và NVDA/observability acceptance; task vẫn `VERIFY`.
38. P3-02C Teacher lifecycle staging checkpoint ngày 2026-07-30: reload giữ working
    schedule gồm hai interval, timezone IANA và exception; Scheduling Assistant trả 10
    gợi ý bounded, privacy-safe. Fixture staging có audience nội bộ/yêu cầu RSVP được
    tạo rồi hủy, reload giữ `Đã hủy` và lịch sử. Public capability, organizer
    transfer/archive/capability revocation, concurrency/IDOR, NVDA và observability
    vẫn mở; P3-02C giữ `VERIFY`.
39. P3-02C public RSVP staging checkpoint ngày 2026-07-30: server-issued fixture trên
    disposable Codespace đạt fragment scrub, minimal resolve, respond/replay,
    collision/stale `409`, origin/malformed guard, secure headers và bounded resolve
    rate. Teacher remove/re-add external fixture qua business flow làm link cũ generic
    unavailable. Focused Go regression cho fixture, Calendar repository/service và API
    đều đạt. Full rate/expiry/hash-at-rest, organizer transfer/archive, PostgreSQL
    concurrency/IDOR vẫn mở; task giữ `VERIFY`.
40. P3-02C manual accessibility/privacy checkpoint ngày 2026-07-30: operator xác nhận
    PASS cho Teacher working-hours/audience, Student privacy/self-RSVP, keyboard flow,
    focus recovery sau `409`, NVDA trên Calendar/public RSVP và cancel/revocation
    fixture. Không lưu credential, capability URL/token hoặc dữ liệu người học. Manual
    gate đã đóng; full rate/expiry/hash-at-rest, organizer transfer/archive,
    PostgreSQL concurrency/IDOR và observability live vẫn mở nên task giữ `VERIFY`.
41. P3-02C chuyển `VERIFY -> DONE` ngày 2026-07-30. Neon staging ở `21 false`, exact
    ACL/runtime-role isolation đạt; `corepack pnpm verify`, focused Go
    Calendar/Classroom/HTTP/API/fixture và Calendar E2E `11/11` đều PASS. Operator xác
    nhận toàn bộ ma trận staging/disposable PostgreSQL và manual accessibility/privacy
    còn lại PASS, gồm concurrency/IDOR, capability digest/expiry/rate/revocation,
    organizer transfer/cancel/archive và log privacy. Exact runtime acceptance commit
    `7859c233` đã Live trên Render và Cloudflare; Render health trả HTTP `200`.
42. Quyết định vận hành được tái xác nhận ngày 2026-07-31: giữ **Render Web Service**
    cho Core API staging/private alpha; không chuyển sang OCI/Cloud Run/Oracle hay
    provider khác trong cập nhật này. Render Free có cold start/spin-down và không phải
    durable worker. Vì vậy các kiểm tra chạy được ở local/CI/disposable staging vẫn là
    bắt buộc; các gate phụ thuộc host không spin-down hoặc provider production được ghi
    `DEFERRED/VERIFY`, không được suy diễn thành PASS hay bỏ qua. P3-03B (worker role/
    grants, crash/reclaim, duplicate canary) và các gate live SES/domain/event
    topology tiếp tục mở; hai feature gate notification và Class Files activation vẫn
    `false`, không có asynchronous email/notification/reminder hoặc worker-driven file
    processing/sharing tới end user.

## Rủi ro đã biết

- P3-01 đã `DONE` cho phạm vi one-time ClassSession trên staging/private alpha. Kết quả
  này không mở rộng phạm vi sang recurrence, reminder, calendar tổng hợp, email/ICS,
  durable worker hoặc Phase 4 media lifecycle.
- ADR-0019 đã chấp nhận decision dùng FullCalendar v7 và recurrence bounded; manual NVDA
  đã PASS. P3-02A shell/read projection và P3-02B recurrence/class conflict đã `DONE`.
  P3-02C cũng đã `DONE` cho working-hours/free-busy, internal/external audience, typed
  series/occurrence copy-on-write, organizer transfer, cancellation lifecycle và
  internal/public RSVP. Email/ICS delivery vẫn thuộc P3-05A/P3-CAL-02; Availability
  Poll/Study Meeting vẫn thuộc P3-02D/P3-05B.
- AWS SES đã được chọn làm provider target; local SES v2 Raw adapter, deterministic
  renderer/sink và error semantics đã đạt trong spike cô lập, nhưng provider account/
  region/sandbox/quota chưa được live-verify và sending domain chưa có. Pre-domain chỉ
  cho phép owner-controlled verified identities trong SES sandbox; không được coi là
  production readiness. SPF/DKIM/DMARC, provider-event topology ADR-0020,
  bounce/complaint/suppression và cross-client ICS chưa được nghiệm thu. Không gửi
  business email tới end user trước khi các live gate của ADR-0020, P3-03B và P3-05A đạt.
- Warm Academic calendar shell, semantic Agenda, FullCalendar renderer và visual
  desktop/tablet/mobile đã được nghiệm thu trong P3-02A; recurrence/conflict và
  participant/RSVP đã được nghiệm thu trong P3-02B/P3-02C. Email/ICS và Availability
  Poll vẫn thuộc P3-CAL-02, P3-02D và P3-05A/B; Calendar chưa phải toàn bộ sản phẩm cuối
  cùng cho đến khi các phạm vi đó đạt gate.
- P3-02D-A đã `DONE` sau migration `000023`, exact ACL, PostgreSQL concurrency/cascade,
  schema/API/UI/capability exchange, automated authorization/privacy, authenticated staging
  browser/API matrix và manual NVDA organizer/public production-route acceptance đều PASS.
- External poll link có rủi ro token/PII leak và abuse. Local implementation dùng token
  entropy cao, hash-at-rest, fragment exchange, expiry/revoke/rate limit, log redaction
  và privacy-safe aggregate theo ADR-0021; exact staging đã xác nhận các boundary này.
  Minimum cohort/coarse bucket chỉ giảm rủi ro differencing/Sybil; anonymous link không
  thể hứa one-human-one-response.
- Quyền tạo instant study room đã được chốt làm authorization target, nhưng LiveKit
  token, lobby, moderation và media lifecycle vẫn thuộc Phase 4.
- P3-03A/P3-04 mới ở `VERIFY`: lease/fencing/dead-letter, worker binary, notification
  projection/API/UI, ACL probe và test đã
  có trong repository nhưng chưa chạy như durable service trên staging. Render Free web
  service không được xem là durable worker. Quyết định hiện tại là giữ Render Web Service
  cho Core API staging/private alpha; Render Background Worker trả phí chỉ là candidate
  tương lai, chưa được provision/phê duyệt và không có provider migration trong task này.
  Không bật notification delivery, email/ICS, reminder hoặc worker-driven file processing/
  sharing tới end user trước khi worker role/grants, non-spin-down host và crash/reclaim
  acceptance đạt. Migration `000015/000016` và exact API runtime grants đã áp dụng/probe
  xanh. Hai P3-04 gate phải giữ false; grant worker notification và worker gate
  phải chuyển cùng nhau khi process đã dừng, nếu không startup probe sẽ fail closed.
  P3-CAL-02 chỉ
  được chạy renderer/provider sandbox cô lập trong lúc gate này còn mở.

### Phân loại gate vận hành hiện tại (2026-07-31)

| Nhóm | Quy tắc |
| --- | --- |
| Bắt buộc chạy ngay | Unit/integration/CI, static ACL/config checks, local/disposable PostgreSQL, API/browser/accessibility và sandbox/sink tests trong phạm vi hiện có. |
| `DEFERRED/VERIFY` | Durable worker host không spin-down, live worker role/grants, crash/reclaim/duplicate canary, SES production access/quota/event ingress, domain/DNS và Gmail/Outlook/Apple interoperability. |
| Không được bật | `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY`, `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS`, Class Files sharing/processing và mọi asynchronous email/ICS/reminder side effect trước khi gate phụ thuộc đạt. |

- Render Free spin down khi không hoạt động và có thể cold start trên 50 giây;
  chỉ chấp nhận cho staging/private alpha.
- Direct-main chưa có pre-merge protection; `pnpm verify` và CI hậu kiểm là kiểm soát
  bù tạm thời theo ADR-0012.
- Chưa chọn managed Redis và observability provider cho quy mô lớn hơn.
- P2-09 thay limiter theo process bằng fixed-window PostgreSQL và chỉ tin client prefix
  do Cloudflare Pages ký. Staging đã đồng bộ `EDGE_CONTEXT_SECRET` ở edge/Core API và
  public limiter smoke đã đạt; assertion không hợp lệ vẫn fallback về direct peer
  prefix. Redis tiếp tục hoãn cho tới khi có số liệu tải.
- Migration `000012` không hardcode runtime role theo môi trường. Staging đã cấp grants
  tối thiểu và chạy bounded cleanup cho `tenant_quota_windows` cùng
  `rate_limit_windows`; maintenance định kỳ vẫn phải theo `docs/DATABASE.md`.
- Class projection chưa lộ `archived_from_status`, nên web chỉ dùng feature gate khi
  hiển thị restore; quota active-class vẫn được server enforce transactionally. Khi
  quota đã hết, restore lớp từng active có thể nhận 409 sau submit; bổ sung projection
  class-specific nếu UX này trở thành vấn đề thực tế.
- Verify #59 đã xác nhận PostgreSQL runtime, migration/integration và Browser E2E
  trên CI. Acceptance UI staging P2-08 ngày 2026-07-20 đã xanh sau khi web/Core API
  được đồng bộ; các lần nghiệm thu sau vẫn phải đối chiếu commit/image, migration và
  configuration trước khi kết luận lỗi contract.
- Host hiện tại thiếu Docker/PostgreSQL nên không thể lặp lại full browser scenario
  ngoài CI; nếu CI không sẵn có thì đây vẫn là hạn chế chẩn đoán cục bộ.
- P2-12 đã đóng sau khi CI/Cloudflare/Render/Neon/importer/public probe, UI staging
  S01-S07 và S09 application rollback/redeploy đều đạt. Native Rollback không được dùng
  vì Render cảnh báo không tải được cấu hình live; application rollback giữ config hiện
  tại là bằng chứng phục hồi đã chấp nhận. Closure-record CI sau push là hậu kiểm bắt buộc.
- Class/roster/audit cursor vẫn là payload client đọc được; scope hash ngăn replay sai
  tenant/filter nhưng không phải chữ ký bí mật. SQL luôn giữ tenant/class predicate nên
  finding hiện được xếp Low; quyết định HMAC toàn bộ cursor được hoãn sang backlog/ADR
  riêng sau P2-10 nếu threat model yêu cầu cursor chống giả mạo.
- Production retention/export, privacy erasure, partitioning và dedicated maintenance
  role cho audit được hoãn tới Phase 8. Audit của tenant archived được giữ nhưng chưa có
  recovery/export UI ngoài active-tenant API.
- Roster role update hiện dùng last-write-wins, chưa có enrollment version/CAS. Client
  refetch sau mutation; nếu concurrent editing trở thành rủi ro thực tế thì cần ADR và
  migration riêng.
- Ownership transfer tái dùng `auth_time` của session theo semantics P2-01; chưa ép
  OIDC `max_age`/`prompt`, nên recent-auth 10 phút chưa phải step-up tuyệt đối.
- Archive hoặc roster role mutation ngăn/đổi credential LiveKit cấp mới nhưng không
  thu hồi JWT đã phát hoặc kick/cập nhật participant đang ở trong room.
- Dữ liệu V1 chưa được migrate.
- LiveKit chunk phía web còn lớn và cần performance budget ở phase sau.

## Quy tắc Git hiện tại

- Làm việc và commit trực tiếp trên `main` để ưu tiên tốc độ phát triển.
- Không bắt buộc Issue, branch hoặc Pull Request cho mỗi task.
- Trước khi push: xem `git diff`, chạy kiểm tra phù hợp và quét secret.
- Không force-push `main`; branch tạm chỉ dùng cho thử nghiệm rủi ro cao.

## Tài liệu liên quan

- `docs/MASTER_PLAN.md`
- `docs/PHASE_1_BACKLOG.md`
- `docs/PHASE_1_COMPLETION.md`
- `docs/PHASE_2_BACKLOG.md`
- `docs/P2_12_STAGING_ACCEPTANCE.md`
- `docs/PHASE_2_COMPLETION.md`
- `docs/PHASE_3_BACKLOG.md`
- `docs/P3_03_OUTBOX_WORKER_RUNBOOK.md`
- `docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md`
- `docs/calendar/P3_CAL_01_SPIKE_EVIDENCE.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/DATABASE.md`
- `docs/AUTHENTICATION.md`
- `docs/E2E_TESTING.md`
- `docs/LIVEKIT_SPIKE_RUNBOOK.md`
- `docs/CI_SECURITY.md`
- `docs/adr/0011-render-core-api-staging.md`
- `docs/adr/0012-single-maintainer-direct-main-governance.md`
- `docs/adr/0013-shared-organization-class-authorization-policy.md`
- `docs/adr/0014-append-only-tenant-audit-log.md`
- `docs/adr/0015-server-evaluated-feature-controls-and-quotas.md`
- `docs/adr/0016-idempotent-v1-fixture-import.md`
- `docs/adr/0017-class-session-scheduling-and-civil-time.md`
- `docs/adr/0018-postgresql-leased-outbox-worker.md`
- `docs/adr/0019-calendar-renderer-recurrence-and-conflict.md`
- `docs/adr/0020-calendar-invitation-rsvp-icalendar-and-ses.md`
- `docs/adr/0021-native-availability-polls-and-member-owned-study-meetings.md`
- `docs/adr/0022-tenant-scoped-in-app-notification-projection.md`
- `docs/adr/0023-calendar-working-schedule-free-busy-and-rsvp-authority.md`
- `docs/adr/0024-forward-maintenance-purge-security.md`
