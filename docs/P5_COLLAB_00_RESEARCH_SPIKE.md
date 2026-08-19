# P5-COLLAB-00 — Whiteboard engine và collaboration topology research spike

> Task chuẩn bị cho **Phase 5 - Classroom Collaboration**. Task này không chặn Phase 4,
> không cài production dependency, không tạo service, migration hoặc deploy. Mọi lựa chọn
> engine/provider chỉ có hiệu lực sau khi evidence đạt và ADR được chấp nhận.

| Thuộc tính         | Giá trị                                                                  |
| ------------------ | ------------------------------------------------------------------------ |
| Trạng thái         | `DONE`                                                                   |
| Phase              | Phase 5 - Classroom Collaboration                                        |
| Thời điểm          | Có thể chạy cuối Phase 4; phải `DONE` trước whiteboard implementation    |
| Quyết định bị chặn | Whiteboard engine, document authority, sync provider và runtime topology |
| Không bị chặn      | P4-01 đến P4-12 và Phase 3 deferred carry-over                           |
| Cập nhật           | 2026-08-18                                                               |

## 1. Mục tiêu

Thu hẹp lựa chọn và chuẩn bị một kiến trúc whiteboard/collaboration decision-ready cho lớp học
2-50 người, thay vì mặc định chọn thư viện theo tên hoặc mang nguyên topology của V1 sang V2.
Spike phải trả lời:

1. engine nào phù hợp sản phẩm, accessibility, hiệu năng và ngân sách;
2. một authority duy nhất nào sở hữu document, history và undo/redo;
3. realtime provider/gateway nào đồng bộ, xác thực, revoke và scale trạng thái;
4. snapshot/export/restore được lưu an toàn trong PostgreSQL/B2 như thế nào;
5. topology vận hành có cần runtime mới ngoài Go modular monolith hay không.

## 2. Candidate bắt buộc so sánh

| Candidate                              | Cần chứng minh                                                                         | Ranh giới                                                                                                    |
| -------------------------------------- | -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| tldraw SDK + official sync             | Product fit, engine-native store/sync, license key và production cost                  | Không mô tả tldraw là permissive open source; không ghép thêm Yjs nếu chưa chứng minh một authority duy nhất |
| Excalidraw package + self-managed sync | MIT license, khả năng embed và chi phí tự xây collaboration adapter                    | Collaboration của ứng dụng Excalidraw không được giả định là drop-in cho package nhúng                       |
| Yjs + provider phù hợp                 | Shared notes hoặc custom CRDT; awareness, reconnect, persistence và horizontal scaling | Là building block, không phải mặc định cho mọi whiteboard engine                                             |
| Hocuspocus/y-websocket/@y/hub          | Auth hook, persistence, vận hành và scale khi chọn Yjs                                 | Node/collaboration service mới cần ADR; không thêm chỉ để chạy demo                                          |

Nguồn nghiên cứu ưu tiên tài liệu/license chính thức:

- [tldraw licensing](https://tldraw.dev/community/license),
  [tldraw sync](https://tldraw.dev/docs/sync) và
  [tldraw repository license](https://github.com/tldraw/tldraw/blob/main/LICENSE.md).
- [Excalidraw repository](https://github.com/excalidraw/excalidraw).
- [Yjs repository](https://github.com/yjs/yjs),
  [Yjs awareness](https://docs.yjs.dev/getting-started/adding-awareness) và
  [y-websocket](https://github.com/yjs/y-websocket).
- [Hocuspocus overview](https://tiptap.dev/hocuspocus/) và
  [persistence guidance](https://tiptap.dev/docs/hocuspocus/guides/persistence).

## 3. Evidence matrix bắt buộc

### 3.1 License, chi phí và khả năng thay thế

- Xác minh license của SDK, sync server, assets/fonts và dependency chuyển tiếp.
- Ghi rõ production key, commercial/hobby/trial terms, giới hạn self-host và tổng chi phí dự kiến.
- Chứng minh export dữ liệu không khóa người dùng vào provider; ghi exit trigger và fallback.
- Paid license hoặc dịch vụ/runtime mới phải được owner chấp thuận trước khi tích hợp production.

### 3.2 Product và integration fit

- Embed trong React classroom tool/side panel hoặc full-canvas mà không làm rời media room.
- Teacher có thể open, present, suspend, close; reconnect không tạo document authority thứ hai.
- Hỗ trợ pen/mouse/touch, selection, text, image/file reference và export tối thiểu cần cho pilot.
- Xác định ranh giới với toolbar, classroom layout, persistent file metadata và media state.

### 3.3 Convergence, history và recovery

- Hai browser cùng document hội tụ sau edit đồng thời, mất mạng và reconnect.
- Concurrent undo/redo không hoàn tác thay đổi của actor khác ngoài semantics đã công bố.
- Snapshot định kỳ, restore, export/import và corrupted/incompatible snapshot đều có test.
- Chỉ một document/history/undo authority. Không xếp Yjs lên engine có sync/store riêng nếu
  prototype và ADR chưa chứng minh được boundary không tạo dual authority.

### 3.4 Authorization, tenant isolation và privacy

- Backend cấp capability `view/edit/present`; teacher edit và student read-only được enforce
  tại collaboration boundary, không chỉ disable nút ở client.
- Cross-tenant document/connection, forged role, stale membership, revoked session và IDOR bị deny.
- WebSocket handshake có short-lived server-issued credential, origin validation, expiry/revoke,
  payload cap, rate limit và connection quota.
- Không log board content, snapshot body, credential, email học sinh hoặc raw provider error.
- Snapshot/object key không chứa identifier nhạy cảm có thể đoán; audit chỉ lưu metadata allowlist.

### 3.5 Hiệu năng và vận hành

- Đo bundle/lazy-load, memory, interaction latency và reconnect với 500 và 2.000 shapes.
- Chạy convergence tối thiểu 2 browser; mô phỏng profile 10/50 participant hoặc công bố giới hạn
  thấp hơn kèm số liệu và provider quota.
- Xác định backpressure, compaction, snapshot cadence, recovery point, retention và cleanup.
- Nếu cần Node/collaboration service, ADR phải nêu owner vận hành, health/readiness, deploy,
  scale, observability, secret rotation, outage mode và chi phí; không nhập chung với media plane.

### 3.6 Accessibility

- Keyboard-only cho open/close tool, toolbar, canvas/object navigation và focus handoff.
- NVDA trên Windows cho tên công cụ, trạng thái, selection/action quan trọng và error recovery.
- 200% zoom, forced colors, reduced motion và visible focus không làm mất chức năng cốt lõi.
- Ghi rõ limitation của engine và mitigation/product fallback; automated Axe không thay thế toàn bộ
  kiểm tra canvas/screen-reader thủ công.

## 4. Cách thực hiện spike

1. Tạo prototype cô lập, không nối route production và không dùng credential production.
2. Dùng cùng một fixture/benchmark để so sánh candidate; không tối ưu riêng một candidate rồi kết luận.
3. Lưu command, phiên bản, cấu hình, kết quả số và limitation có thể tái lập.
4. Thực hiện threat model cho connection token, document ID, tenant boundary, snapshot và export.
5. Viết decision matrix có trọng số cho product fit, accessibility, security, performance, license,
   implementation cost và operational ownership.
6. Tạo ADR `Proposed` decision-ready và ghi rõ finalist/blocker. Việc chạy hard gate engine-native,
   lấy owner decision và chuyển ADR sang `Accepted` thuộc P5-COLLAB-01.

## 5. Checkpoint thực thi ngày 2026-08-18

- Prototype cô lập tại `apps/whiteboard-spike` đã chạy cùng fixture 500/2.000 object cho tldraw
  `5.3.1` và Excalidraw `0.18.1`; snapshot envelope/restore-callback smoke, JSON-corruption denial,
  keyboard shell, Axe, CSS 200%, forced-colors và reduced-motion có automated evidence. Scene đã
  thay đổi chưa được đối chiếu sau restore, nên semantic object-count/hash recovery vẫn chưa được
  chứng minh.
- Yjs `13.6.27` + Hocuspocus `4.6.0` generic network spike đã PASS hai-client convergence,
  offline/reconnect, binary restore, viewer receive-only, wrong/cross-tenant denial và raw frame caps.
  Raw cap có thể chặn full-state resync lớn và chưa phải semantic complexity budget. Kết quả này chưa
  chứng minh scene adapter Excalidraw, durable persistence hoặc multi-node/50 client.
- Cold-context Chromium có ba observation load/heap cho 500/2.000 object; tldraw 2.000 object biến
  thiên 2.937–7.806 ms nên chưa thể đặt p50/p95 budget.
- Evidence chạy từ baseline repository `0d2e098` cộng working tree cục bộ chưa commit, bằng
  Playwright `1.61.1`/Chromium `149.0.7827.55`; exact command và limitation đã được ghi để tái lập.
- Kết quả và limitation nằm tại
  [P5_COLLAB_00_RESEARCH_RESULTS.md](P5_COLLAB_00_RESEARCH_RESULTS.md).
- [ADR-0034](adr/0034-whiteboard-engine-document-authority-and-collaboration-topology.md) đã tạo ở
  trạng thái `Proposed`; tại thời điểm đóng research chưa chọn production engine/provider.
- [PHASE_5_BACKLOG.md](PHASE_5_BACKLOG.md) đã tách P5-COLLAB-01 đến P5-COLLAB-20.

Các hard gate được bàn giao cho P5-COLLAB-01: engine-native two-browser convergence/actor-local undo,
persistence/restart, Core API-issued grant/revoke/origin/rate/quota, profile 10/50, manual
NVDA/object navigation và owner approval cho license/cost/runtime. Bundle guard còn chặn upstream
Excalidraw vì nhúng public Google API key/Firebase-collaboration config; React 19.2.7 cũng chưa nằm
trong peer range mà các Radix dependency được Excalidraw pin công bố. ADR chỉ được chuyển `Accepted`
trong P5-COLLAB-01 sau khi các blocker có evidence/decision.

## 6. Definition of Done

- [x] Audit bốn candidate có source pin, license/docs chính thức và các bất định cost/owner được ghi.
- [x] tldraw và Excalidraw chạy cùng fixture 500/2.000, build/bundle/cold-load evidence và snapshot
      limitation được báo cáo, không nối production route.
- [x] Generic Yjs/Hocuspocus prototype chứng minh hai-client convergence, reconnect, binary restore,
      read-only và raw frame cap; giới hạn semantic budget/engine adapter/persistence/multi-node
      được ghi rõ.
- [x] One-authority architecture, threat model, matrix, finalist và production blocker ledger có
      evidence; không tự chọn winner từ weighted score.
- [x] ADR-0034 giữ `Proposed` decision-ready; P5-COLLAB-01 sở hữu hard gate, owner decision và việc
      chuyển `Accepted`.
- [x] Exact baseline, package/browser version, lệnh tái lập, test result và limitation được ghi.
- [x] Phase 5 backlog được tách task decision/implementation/test/rollout; P5-COLLAB-02 trở đi bị
      chặn cho tới khi ADR `Accepted`.

P5-COLLAB-00 kết thúc với kết quả hợp lệ là **hai finalist, chưa có production winner**. Cho tới khi
P5-COLLAB-01 chấp nhận ADR, Master Plan phải dùng cụm “engine/sync được ADR chọn”; không được coi
tldraw, Excalidraw, Yjs hoặc Hocuspocus là production decision đã chốt.

Initial owner checkpoint sau research ngày 2026-08-18 từng chọn **tldraw SDK + official self-hosted
sync** làm target của P5-COLLAB-01. Checkpoint đó đã bị superseded trước production; trial,
commercial license và production runtime tldraw chưa được kích hoạt hoặc phê duyệt.

Final owner checkpoint cùng ngày chốt **Excalidraw + self-managed collaboration** làm target chính
thức. ADR-0034 vẫn `Proposed` vì canonical authority, exact provider/runtime và toàn bộ hard gate
Excalidraw chưa đạt; P5-COLLAB-01 đang `IN PROGRESS` và production whiteboard tiếp tục force-off.
