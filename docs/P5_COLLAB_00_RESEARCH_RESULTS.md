# P5-COLLAB-00 — Whiteboard engine và collaboration topology: kết quả nghiên cứu tạm thời

> **Trạng thái:** P5-COLLAB-00 research package `DONE`, **chưa phải quyết định production**.
> **Ngày chốt evidence trong tài liệu này:** 2026-08-18.
> **Phạm vi:** prototype cô lập tại `apps/whiteboard-spike`; không nối route production, không dùng
> credential, không thêm migration/service và không deploy.

## 1. Kết luận hiện tại

Chưa có candidate nào đủ bằng chứng để được chọn làm whiteboard production. Hai hướng cần tiếp tục
prototype là:

1. **tldraw SDK + tldraw sync chính thức**, trong đó tldraw store/`TLSocketRoom` là authority duy nhất;
2. **Excalidraw package + một sync topology tự quản**, trong đó CRDT/provider được chọn sau phải là
   authority duy nhất và Excalidraw chỉ là projection/editor.

tldraw đang dẫn nhẹ trong ma trận tài liệu tĩnh, nhưng khoảng cách với Excalidraw nhỏ và bị chi phối
bởi hai bất định lớn: license thương mại của tldraw và chi phí tự xây/duy trì sync + accessibility
mitigation của Excalidraw. Vì vậy:

- không coi điểm số bên dưới là quyết định engine;
- không thêm tldraw, Excalidraw, Yjs, Hocuspocus, Fastboard hoặc BlockSuite vào production app;
- không viết ADR `Accepted` trước khi có convergence, authorization, concurrent undo, 10/50-user
  profile và manual NVDA evidence;
- mọi lựa chọn tạo runtime JavaScript/Node/Cloudflare mới hoặc phát sinh phí license/provider cần
  owner phê duyệt rõ ràng.

### Owner direction sau khi đóng research

Initial checkpoint ngày 2026-08-18 từng chọn **tldraw SDK + official self-hosted sync** để chạy hard
gate tiếp theo. Checkpoint đó đã bị superseded trước production; các số đo/PASS tldraw vẫn là
historical comparison/provider-exit evidence và không được chuyển sang engine khác.

Final owner checkpoint ngày 2026-08-18 chốt **Excalidraw + self-managed collaboration** làm target
chính thức của P5-COLLAB-01. Đây là quyết định chọn engine, không làm các PASS cục bộ thành production
evidence và không tự động chuyển ADR-0034 sang `Accepted`. Exact canonical authority/provider,
production pin/runtime và toàn bộ Excalidraw hard gate vẫn còn mở; không chạy tldraw như
implementation/writer lane song song.

## 2. Source pin và phạm vi audit

Các repository được clone nông vào `.tmp/research/p5-whiteboards/` để đọc source. `.tmp/` là dữ liệu
nghiên cứu cục bộ, không phải source production và không được commit.

| Candidate  | Source pin đã audit                                                                                                                              | Phạm vi hiện tại                                                                             |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| tldraw     | [`1e3bdba8b45fa74e160d687a40af4d9de1411736`](https://github.com/tldraw/tldraw/commit/1e3bdba8b45fa74e160d687a40af4d9de1411736), `main`           | Prototype thật với `tldraw@5.3.1`; đọc license, store/sync, history và accessibility         |
| Excalidraw | [`e160ff7ba0641fba729c528482de5277ffb19c58`](https://github.com/excalidraw/excalidraw/commit/e160ff7ba0641fba729c528482de5277ffb19c58), `master` | Prototype thật với `@excalidraw/excalidraw@0.18.1`; đọc embed/API và ranh giới collaboration |
| BlockSuite | [`5cb5cb68471ca692f3c162258f0087cb22fcb82d`](https://github.com/toeverything/blocksuite/commit/5cb5cb68471ca692f3c162258f0087cb22fcb82d), `main` | Source/static audit; chưa cài vào prototype                                                  |
| Fastboard  | [`c9ccce0e59fe76731d9e4c40f6ac22c6c98b35e4`](https://github.com/netless-io/fastboard/commit/c9ccce0e59fe76731d9e4c40f6ac22c6c98b35e4), `main`    | Source/static audit; chưa cài vào prototype và chưa gọi Agora                                |

Source pin chỉ làm kết quả tái lập được; nó không khẳng định đây là release mới nhất tại thời điểm
đọc lại tài liệu.

## 3. Nguyên tắc kiến trúc bắt buộc: chỉ một authority

### 3.1 Phân quyền trách nhiệm

| Thành phần                | Được sở hữu                                                                                                                     | Không được sở hữu                                            |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| Core API                  | identity, `tenant_id`, membership, capability `view/edit/present`, board metadata, lifecycle và audit metadata allowlist        | live document state, CRDT merge hoặc client undo stack       |
| Collaboration boundary    | WebSocket authorization, room/document mapping, rate/payload/connection cap, presence sanitation và chuyển update đến authority | một bản document thứ hai có thể ghi độc lập                  |
| Engine document authority | document state, merge/convergence, schema migration và history/undo semantics đã công bố                                        | tenant role do client tự khai hoặc asset secret              |
| PostgreSQL/B2             | versioned checkpoint/snapshot, export artifact và recovery metadata                                                             | live writer hoặc nguồn merge song song với authority         |
| Awareness/presence        | cursor, selection và trạng thái tạm thời có TTL                                                                                 | nội dung bền vững, quyền, identity tự khai hoặc audit record |

### 3.2 Hai topology hợp lệ để tiếp tục chứng minh

**Nếu chọn tldraw:** tldraw store và đúng một `TLSocketRoom` toàn cục trên mỗi document là document
authority. Snapshot sang PostgreSQL/B2 chỉ là checkpoint. Không đặt Yjs bên dưới hoặc bên cạnh
tldraw store. Tài liệu tldraw cũng cảnh báo nhiều `TLSocketRoom` cho cùng room sẽ làm người dùng
không thấy nhau và có thể ghi đè thay đổi.

**Nếu chọn Excalidraw:** cần thiết kế adapter để một `Y.Doc` hoặc một CRDT được ADR chọn là authority;
Excalidraw scene là projection có version/schema rõ. Undo phải dùng transaction origin/actor scope đã
kiểm thử. Không được để Excalidraw local history và CRDT history cùng trở thành hai nguồn quyết định
trạng thái.

Snapshot restore luôn tạo một revision/checkpoint được kiểm soát. Client không được tự đưa snapshot
cũ vào live room và ghi đè authority.

## 4. Evidence prototype đã có

Prototype dùng cùng một model trung lập gồm rectangle có label, cùng fixture 500/2.000 shape, cùng
control shell `view/edit/present` và cùng snapshot envelope. Capability trong harness chỉ chứng minh
integration surface; nó **không** phải server-side authorization evidence.

Metadata tái lập của checkpoint này:

- repository baseline `0d2e098` cộng working tree cục bộ chưa commit;
- `tldraw@5.3.1`, `@excalidraw/excalidraw@0.18.1`, `yjs@13.6.27`,
  `@hocuspocus/provider@4.6.0`, `@hocuspocus/server@4.6.0` và React `19.2.7`;
- Playwright `1.61.1`, bundled Chromium `149.0.7827.55`, fixture `500` và `2.000` object logic;
- không nạp `.env*.local`, không dùng credential/provider và không ghi vào shared staging.

### 4.1 Check ledger

| Gate                                                  | Kết quả                   | Ý nghĩa và giới hạn                                                                                                                                                                  |
| ----------------------------------------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| TypeScript typecheck                                  | PASS                      | Prototype và E2E config compile ở strict toolchain hiện tại                                                                                                                          |
| ESLint                                                | PASS                      | Không có lint error trong source/E2E/config của spike                                                                                                                                |
| Vitest                                                | PASS — 2 files, 10 tests  | 7 model/snapshot tests và 3 network tests Yjs/Hocuspocus                                                                                                                             |
| Vite production build                                 | PASS                      | Cả hai engine build độc lập và lazy-load; chưa phải production budget approval                                                                                                       |
| Production dependency audit                           | PASS                      | `pnpm audit --prod --audit-level high` không còn known vulnerability sau khi nâng Excalidraw/ws và dùng bounded override cho Nanoid/lodash-es                                        |
| Full workspace audit kể cả dev tooling                | FAIL — baseline blocker   | Còn 8 High + 5 Moderate ở các path dev/tooling ngoài spike (`js-yaml`, `brace-expansion`, `undici`, `postcss`); cần xử lý/CI xác nhận riêng trước candidate push                     |
| Client bundle secret/config guard                     | FAIL — finalist blocker   | Excalidraw package nhúng public Google API key và Firebase/collaboration endpoints upstream; research dist không deploy, production phải loại/strip hợp lệ rồi scan lại              |
| `pnpm peers check`                                    | FAIL — production blocker | Radix packages Excalidraw pin chỉ khai React/ReactDOM đến 18, còn TutorHub dùng 19.2.7; build/E2E chạy được nhưng không được che warning hoặc coi là compatibility approval          |
| Playwright targeted: same fixture, restore/corruption | PARTIAL                   | Cả hai engine gọi restore callback với payload chưa đổi và từ chối JSON corruption; no-op restore vẫn có thể PASS, chưa đối chiếu scene/hash hoặc persisted multi-client recovery    |
| Playwright targeted: 2.000 shapes                     | PASS hiện tại             | Cả hai canvas tải được 2.000 object logic; Excalidraw giữ thêm bound-text record nội bộ                                                                                              |
| Yjs 13.6.27 + Hocuspocus 4.6.0 network                | PASS — 3 tests            | Hai client hội tụ, offline/reconnect hai chiều, binary restore, viewer receive-only, wrong/cross-tenant deny và raw frame ceilings; chỉ là generic `Y.Map`, chưa phải engine adapter |
| Keyboard shell + Axe                                  | PASS hiện tại             | Chỉ bao phủ control/status shell; automated Axe không chứng minh canvas screen-reader usability                                                                                      |
| CSS 200% + forced colors + reduced motion             | PASS hiện tại             | Chỉ là shell smoke bằng CSS zoom/emulation; không thay physical browser zoom, canvas navigation hoặc manual NVDA                                                                     |

Không chuyển một PASS cục bộ thành production gate. Đặc biệt, test snapshot hiện tại chỉ chứng minh
serialize/parse, restore callback với payload chưa đổi và JSON corruption denial; no-op restore vẫn
có thể PASS, nên chưa chứng minh semantic round-trip, object cap, document hash hay persisted
multi-client recovery.

### 4.2 Bundle evidence

Số dưới đây lấy từ output Vite production build, đơn vị kB, làm tròn đúng như build report. Đây là
chunk evidence, không phải tổng network transfer của một user flow đã instrument.

| Engine/chunk                | Minified |     Gzip | Nhận xét                                                            |
| --------------------------- | -------: | -------: | ------------------------------------------------------------------- |
| tldraw JavaScript chính     | 1.836,66 |   546,45 | Lớn; bắt buộc lazy-load theo lúc mở tool                            |
| tldraw CSS                  |    77,42 |    14,49 | Chưa tính font/asset tải ngoài nếu có                               |
| Excalidraw JavaScript chính | 4.756,79 | 1.588,14 | Lớn hơn đáng kể; không được vào initial classroom bundle            |
| Excalidraw CSS              |   142,02 |    22,37 | Cần kiểm tra theme/forced-colors trên shell thật                    |
| Excalidraw graph lazy chunk |   662,68 |   143,23 | Chỉ tải khi feature liên quan được dùng; cần xác minh split thực tế |
| Excalidraw Cytoscape lazy   |   435,43 |   137,93 | Cần loại/hoãn feature ngoài pilot nếu có thể                        |
| Excalidraw KaTeX lazy chunk |   258,87 |    77,46 | Cần kiểm tra có cần cho pilot hay không                             |

Kết luận bundle hiện tại: cả hai hướng chỉ khả thi với route/tool-level lazy loading; Excalidraw cần
budget và feature-trimming chặt hơn.

### 4.3 Cold-context load/heap observation

Playwright Chromium chạy mỗi trường hợp trong một browser context mới, query trực tiếp engine/fixture
để không nạp candidate còn lại. Đây là **ba observation trên máy hiện tại**, không phải p50/p95 hay
budget production. `heapBytes` là V8 heap tại thời điểm status ready, không bao gồm toàn bộ native/GPU
memory và không được dùng để so sánh chi phí server.

| Engine            | Object logic | Ready quan sát (3 lượt) |    V8 heap quan sát (3 lượt) |
| ----------------- | -----------: | ----------------------: | ---------------------------: |
| tldraw 5.3.1      |          500 |          2.177–2.274 ms |   48.037.620–48.048.804 byte |
| tldraw 5.3.1      |        2.000 |          2.937–7.806 ms | 103.092.240–115.122.180 byte |
| Excalidraw 0.18.1 |          500 |              825–894 ms |   14.428.912–14.446.532 byte |
| Excalidraw 0.18.1 |        2.000 |              824–894 ms |   25.626.904–25.650.196 byte |

Ba lượt chỉ xác nhận fixture có thể ready, đồng thời lộ biến thiên lớn ở tldraw 2.000 object; đây
không phải p50/p95 và không đủ đặt budget. Cần chạy lặp, đo input/long task, GPU/native memory và
thiết bị profile đã công bố trước khi dùng cho quyết định.

## 5. Evidence chưa có — không được suy diễn

Các gate sau vẫn **chưa chứng minh**:

1. Hai physical browser chạy engine adapter thật cùng provider hội tụ sau edit đồng thời, mất mạng,
   reconnect và stale client; generic Y.Map hai client đã PASS nhưng không thay gate này.
2. Concurrent undo/redo theo actor không hoàn tác thay đổi của actor khác.
3. Persisted snapshot/compaction/restart recovery trên collaboration service thật.
4. Core API-issued short-lived credential, origin/audience/expiry/revoke và stale membership denial.
5. Full Core API-issued cross-tenant document/connection, forged role, IDOR, read-only write và
   present escalation denial; local Hocuspocus fixture đã deny wrong/cross-tenant token và viewer write.
6. Update complexity cap, rate limit, connection/document quota và backpressure; local spike mới
   chứng minh raw Hocuspocus frame trên 32 KiB và WebSocket payload trên 64 KiB bị chặn. Raw cap có
   thể chặn legitimate full-state resync lớn và chưa chứng minh semantic mutation/complexity budget.
7. Profile 10/50 participant có số reconnect, convergence lag, CPU, memory và provider quota.
8. Manual keyboard traversal toàn canvas, object navigation và **NVDA trên Windows**.
9. 500/2.000-shape interaction p50/p95, heap growth qua thời gian, long task và snapshot/restore
   duration; hiện chỉ có ba cold-context ready/heap observation trên cùng máy.
10. Asset upload/download, image/file reference, export/import portability và malicious asset path.

Các mục này là hard gate của P5-COLLAB-01. Chúng không phủ định việc research package P5-COLLAB-00
đã hoàn tất, nhưng ADR không thể `Accepted` và production implementation không thể bắt đầu chỉ bằng
evidence hiện tại.

## 6. Ma trận quyết định tạm thời

### 6.1 Cách tính

Đây là **source/static evidence matrix**, tách riêng khỏi measured prototype ledger ở mục 4. Mỗi
candidate được chấm 0–10 theo từng tiêu chí, rồi nhân trọng số:

`tổng = Σ(điểm / 10 × trọng số)`

| Tiêu chí                             | Trọng số |
| ------------------------------------ | -------: |
| Product/integration fit              |       20 |
| Accessibility                        |       20 |
| Security/tenant boundary fit         |       20 |
| Performance/scale confidence         |       15 |
| License/cost/exit                    |       15 |
| Implementation/operational ownership |       10 |

### 6.2 Điểm tạm thời

| Candidate                      | Product | A11y | Security | Perf | License/cost | Impl/ops | Tổng / 100 |
| ------------------------------ | ------: | ---: | -------: | ---: | -----------: | -------: | ---------: |
| tldraw + official sync         |       8 |    6 |        5 |    6 |            3 |        5 |   **56,5** |
| Excalidraw + self-managed sync |       8 |    3 |        3 |    5 |            9 |        3 |   **52,0** |
| Fastboard/Agora                |       7 |    2 |        5 |    5 |            5 |        6 |   **49,0** |
| BlockSuite                     |       6 |    2 |        4 |    5 |            6 |        3 |   **43,5** |

Khoảng cách 4,5 điểm giữa tldraw và Excalidraw nhỏ hơn độ bất định của những gate chưa đo. Thứ tự
này chỉ giúp ưu tiên prototype tiếp theo, không phải winner ranking.

## 7. Nhận định theo candidate

### 7.1 tldraw SDK + official sync

**Điểm mạnh**

- React embed và engine-native store/sync tạo ranh giới authority rõ hơn.
- Official sync định nghĩa một authoritative room, WebSocket client, persistence hook và schema
  validation/migration cùng hệ sinh thái engine.
- Tài liệu chính thức có accessibility, focus và history surface rõ hơn các candidate còn lại.
- Prototype 500/2.000 shape, snapshot local và production build đã chạy.

**Rủi ro/chưa chấp nhận**

- SDK là source-available, **không phải permissive open source**. Mặc định chỉ dùng development;
  production cần trial, commercial hoặc hobby license key. TutorHub là sản phẩm thương mại nên cần
  commercial quote/terms và owner phê duyệt; chưa có cost number để đưa vào TCO.
- Trial có telemetry license-hash; commercial/hobby theo tài liệu không gửi thông tin đến tldraw.
  Dù không phải board content, trial telemetry vẫn cần privacy review trước thử nghiệm có người dùng.
- Hosted sync demo chỉ dành cho prototype. Production phải self-host; hướng được khuyến nghị dùng
  Cloudflare Durable Objects + SQLite/R2, hoặc JavaScript WebSocket backend. Cả hai đều tạo ownership
  ngoài Go modular monolith hiện tại.
- Template chính thức không tự giải quyết TutorHub authentication/authorization, rate limit, asset
  size cap, long-term snapshot hay room listing; TutorHub phải thiết kế và kiểm thử các lớp này.
- Chưa có actual two-browser sync, concurrent actor undo, 50-user và manual NVDA evidence.

**Exit trigger:** dừng hướng tldraw nếu commercial terms/TCO không được chấp nhận, production
telemetry/contract không phù hợp, hoặc topology không đạt tenant/revoke/50-user/a11y gate.

Nguồn: [tldraw license](https://tldraw.dev/community/license),
[tldraw sync](https://tldraw.dev/docs/sync),
[tldraw accessibility](https://tldraw.dev/sdk-features/accessibility),
[tldraw history](https://tldraw.dev/sdk-features/history).

### 7.2 Excalidraw package + self-managed sync

**Điểm mạnh**

- Repository/package dùng MIT, thuận lợi hơn về license và exit.
- React component, imperative API, scene data và export surface phù hợp để tạo adapter riêng.
- Prototype cùng fixture đã build, snapshot local và tải 2.000 shape.

**Rủi ro/chưa chấp nhận**

- `@excalidraw/excalidraw` là editor package; collaboration của ứng dụng Excalidraw không được coi
  là một production backend drop-in cho TutorHub.
- Self-managed collaboration đòi hỏi tự định nghĩa scene-to-CRDT mapping, transaction origin,
  presence, undo semantics, snapshot/compaction và schema migration. Đây là implementation/ops cost
  lớn nhất của hướng này.
- Không được dùng Firebase/demo relay hoặc `excalidraw-room` như production mặc định nếu chưa chứng
  minh auth, tenant isolation, revoke, rate/payload cap, persistence và horizontal scale.
- Canvas/object screen-reader navigation và concurrent undo chưa có evidence; điểm accessibility và
  security hiện thấp do thiếu bằng chứng, không phải kết luận rằng không thể cải thiện.
- Bundle chính lớn hơn tldraw trong build hiện tại và cần feature-level split/budget.
- `pnpm peers check` hiện fail vì Radix dependencies được Excalidraw pin chỉ khai React/ReactDOM
  đến 18 trong khi TutorHub dùng React 19.2.7. Runtime tests không thay cho upstream compatibility;
  phải có upgrade/fix chính thức hoặc evidence + owner chấp nhận trước production.

**Exit trigger:** dừng hướng Excalidraw nếu adapter không đạt actor-scoped undo/convergence, manual
NVDA không có mitigation chấp nhận được, hoặc runtime/TCO tự vận hành vượt lợi ích license MIT.

Nguồn: [Excalidraw repository và license](https://github.com/excalidraw/excalidraw),
[integration guide](https://docs.excalidraw.com/docs/%40excalidraw/excalidraw/integration),
[API](https://docs.excalidraw.com/docs/%40excalidraw/excalidraw/api),
[Excalidraw room reference](https://github.com/excalidraw/excalidraw-room).

### 7.3 BlockSuite

BlockSuite chỉ là reference candidate tại checkpoint này. Source cho thấy hướng Y.Doc/CRDT và
actor-local undo đáng nghiên cứu cho shared notes/custom collaboration, nhưng chưa có TutorHub
transport/auth prototype, accessibility evidence hoặc 50-user measurement.

Root repository dùng MPL-2.0 trong khi nhiều package khai báo MIT; trước khi nhúng cần khóa đúng
package graph và review license theo artifact thực tế, không được gọi toàn bộ repository là MIT.
Không nên chọn chỉ vì BlockSuite đã dùng Yjs: editor surface, sync authority và product maturity vẫn
phải đạt cùng gate.

**Exit trigger:** loại khỏi shortlist nếu package graph/license không rõ, accessibility không có
mitigation hoặc bắt buộc mang quá nhiều AFFiNE-specific architecture vào TutorHub.

Nguồn: [BlockSuite repository](https://github.com/toeverything/blocksuite),
[root MPL-2.0 license](https://github.com/toeverything/blocksuite/blob/master/LICENSE).

### 7.4 Fastboard/Agora

Fastboard là managed-provider reference: wrapper có license MIT và có thể giảm phần tự vận hành room,
nhưng collaboration authority, quota, token và availability gắn với Agora/Netless. FAQ upstream tại
source pin hướng dẫn cấp role `writer` cho mọi user khi tạo room rồi hạn chế UI phía client; cách đó
không đáp ứng capability `view/edit` phía server của TutorHub và là blocker nếu provider không có
boundary chặt hơn. Source audit cũng chưa chứng minh portable editable export, tenant/revoke boundary,
manual a11y hoặc load profile riêng. Provider cost, residency, quota và exit plan phải được xác minh
trực tiếp trước pilot.

**Exit trigger:** loại nếu không có editable export/provider exit, contract/data residency không phù
hợp, cost/quota không đạt 50-user classroom hoặc token boundary không đáp ứng capability model.

Nguồn: [Fastboard repository](https://github.com/netless-io/fastboard),
[MIT license](https://github.com/netless-io/fastboard/blob/main/LICENSE.txt),
[FAQ role `writer` tại source pin](https://github.com/netless-io/fastboard/blob/c9ccce0e59fe76731d9e4c40f6ac22c6c98b35e4/docs/en/faq.md#windowmanger-room-must-be-switched-to-be-writable),
[Agora Interactive Whiteboard documentation](https://docs.agora.io/en/interactive-whiteboard/overview/product-overview).

## 8. Yjs/provider topology nếu tiếp tục hướng Excalidraw

Yjs là CRDT building block, không phải whiteboard engine hoặc production boundary hoàn chỉnh. Nếu
được chọn, Y.Doc binary phải là authority và awareness phải là dữ liệu tạm thời, không persist như
document content.

| Provider/reference          | Kết luận tạm thời                                                                                                                                                                                                                                                                                                    |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hocuspocus                  | Isolated generic Y.Map prototype PASS với `onAuthenticate`, server-side read-only, concurrent convergence, offline/reconnect, binary restore và raw frame caps. Vẫn chưa chứng minh Excalidraw mapping, persistence/restart, Origin/revoke/rate/quota, horizontal scale, health/secret rotation hoặc outage runbook. |
| y-websocket                 | Reference đơn giản để hiểu protocol/convergence; không được mặc định coi demo server là TutorHub production boundary.                                                                                                                                                                                                |
| `@y/hub`/managed Yjs option | Chưa đủ source pin, contract/cost và TutorHub auth evidence trong checkpoint này; không đưa vào quyết định.                                                                                                                                                                                                          |

Nếu prototype Hocuspocus, persist **Yjs binary**, không round-trip Y.Doc qua JSON rồi tạo lại. Chính
tài liệu persistence cảnh báo cách JSON này làm mất merge history và có thể nhân đôi content.

Spike hiện pin `yjs@13.6.27`, `@hocuspocus/server@4.6.0` và
`@hocuspocus/provider@4.6.0`. Server ephemeral trên localhost đã chứng minh:

- hai editor tạo key khác nhau đồng thời rồi hội tụ;
- một client offline vẫn tạo draft, client online tiếp tục sửa, reconnect hội tụ hai chiều;
- `Y.encodeStateAsUpdate`/`Y.applyUpdate` round-trip binary snapshot;
- viewer được server cấp `readonly`: forged local mutation không replicate, editor mutation vẫn tới viewer;
- invalid credential và credential tenant B mở document tenant A bị deny;
- raw Hocuspocus frame trên 32 KiB bị `beforeHandleMessage` từ chối, WebSocket payload trên 64 KiB
  bị đóng; 32 KiB chưa phải semantic update budget và có thể chặn full-state resync hợp lệ;
- unauthenticated queue bị giới hạn 16 KiB/32 message/2 pending document.

Fixture token trong test không phải TutorHub credential. Viewer vẫn có thể thấy forged edit tạm thời
trong Y.Doc cục bộ của chính nó; adapter production phải khóa local mutation và reconcile authoritative
state. Test cũng chưa có rate theo thời gian, semantic object budget, durable persistence hoặc multi-node.

Nguồn: [Yjs repository](https://github.com/yjs/yjs),
[Yjs awareness](https://docs.yjs.dev/getting-started/adding-awareness),
[Yjs UndoManager](https://docs.yjs.dev/api/undo-manager),
[y-websocket](https://github.com/yjs/y-websocket),
[Hocuspocus hooks/auth](https://tiptap.dev/docs/hocuspocus/server/hooks),
[Hocuspocus configuration và resource limits](https://tiptap.dev/docs/hocuspocus/server/configuration),
[Hocuspocus persistence](https://tiptap.dev/docs/hocuspocus/guides/persistence).

## 9. Threat model tối thiểu cho provider prototype tiếp theo

| Boundary                | Threat                                                          | Gate cần chứng minh                                                                                                          |
| ----------------------- | --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Document/room ID        | IDOR, đoán ID, cross-tenant join                                | Opaque server ID; Core API map `tenant_id + board_id`; deny mismatch trước khi load document                                 |
| Connection token        | forged role, replay, stale membership, leaked URL               | Short TTL, audience/document/tenant/capability binding, origin validation, expiry/revoke; không log hoặc lưu URL/DOM/storage |
| Edit/present capability | read-only client gửi raw update; student tự nâng role           | Provider/server enforce `view/edit/present`; client UI chỉ là presentation, không phải enforcement                           |
| Update message          | oversized/complex update, rate flood, fan-out memory exhaustion | Per-message bytes, objects/transaction, messages/sec, connections/user/tenant, pending-doc cap và backpressure               |
| Awareness               | giả identity, oversized cursor/name, presence spam              | Server stamp identity, allowlist field/length, TTL, rate cap; không persist vào snapshot                                     |
| Snapshot/restore        | corrupt/incompatible/rollback overwrite                         | Schema+engine version, hash, bytes/object cap, fail closed, privileged restore tạo revision mới và audit metadata            |
| Asset/export            | malicious MIME/URL, cross-tenant asset, provider lock-in        | Core API-issued B2 intent, content/size scan policy, tenant-scoped object, portable export test và retention cleanup         |
| Observability           | board content/PII/token lọt log                                 | Metadata allowlist: request/tenant pseudonymous ID, room hash, counts, bytes, latency, result code; redact payload/raw error |

## 10. Gate và lệnh tái lập

Chạy từ repository root, không nạp `.env*.local` và không cần credential:

```powershell
pnpm install --no-frozen-lockfile
pnpm install --frozen-lockfile
pnpm --filter @tutorhub/whiteboard-spike typecheck
pnpm --filter @tutorhub/whiteboard-spike lint
pnpm --filter @tutorhub/whiteboard-spike test
pnpm --filter @tutorhub/whiteboard-spike build
pnpm --filter @tutorhub/whiteboard-spike e2e
pnpm audit --prod --audit-level high
pnpm peers check
```

Mở harness thủ công:

```powershell
pnpm --filter @tutorhub/whiteboard-spike dev
```

Sau đó truy cập `http://127.0.0.1:4178`. Mỗi report mới phải ghi commit, engine/provider version,
browser version, fixture, command, elapsed time, bundle/heap/latency và limitation; không chỉ ghi
`PASS` chung chung.

## 11. Bước evidence tiếp theo trước khi ADR

1. Dựng **Excalidraw-only candidate bundle**: khóa exact production pin, audit MIT/transitive
   notices, React peer compatibility và loại demo Firebase/collaboration config khỏi bundle.
2. Chọn một canonical self-managed model qua prototype; nếu dùng Yjs/Hocuspocus, triển khai
   scene ↔ canonical mapping, schema/version và transaction-origin/actor-local undo contract.
3. Chạy hai browser: concurrent create/move/delete/text, actor A/B undo, offline 120 giây, reconnect,
   server restart, corrupt snapshot restore và semantic export/import round-trip.
4. Thêm malicious client: cross-tenant room, forged capability, stale/revoked token, raw write ở
   read-only, oversized update và message flood.
5. Đo 500/2.000 shape và 2/10/50 participant: load, input p50/p95, convergence p95, heap, CPU,
   reconnect success, snapshot bytes/time và provider quota.
6. Chạy physical Chrome/Edge + keyboard + NVDA ở 100%/200%, forced colors và reduced motion; ghi rõ
   object/canvas limitation và semantic companion/fallback.
7. Chỉ sau khi các gate đạt mới chuyển ADR sang `Accepted` với **một** document/history/undo
   authority, exact provider/runtime topology, snapshot/asset boundary, failure mode và exit plan.
