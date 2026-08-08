# P5-COLLAB-00 — Whiteboard engine và collaboration topology research spike

> Task chuẩn bị cho **Phase 5 - Classroom Collaboration**. Task này không chặn Phase 4,
> không cài production dependency, không tạo service, migration hoặc deploy. Mọi lựa chọn
> engine/provider chỉ có hiệu lực sau khi evidence đạt và ADR được chấp nhận.

| Thuộc tính         | Giá trị                                                                  |
| ------------------ | ------------------------------------------------------------------------ |
| Trạng thái         | `TODO`                                                                   |
| Phase              | Phase 5 - Classroom Collaboration                                        |
| Thời điểm          | Có thể chạy cuối Phase 4; phải `DONE` trước whiteboard implementation    |
| Quyết định bị chặn | Whiteboard engine, document authority, sync provider và runtime topology |
| Không bị chặn      | P4-01 đến P4-12 và Phase 3 deferred carry-over                           |
| Cập nhật           | 2026-08-09                                                               |

## 1. Mục tiêu

Chọn một kiến trúc whiteboard/collaboration có bằng chứng cho lớp học 2-50 người, thay vì
mặc định chọn thư viện theo tên hoặc mang nguyên topology của V1 sang V2. Spike phải trả lời:

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
6. Tạo ADR chọn engine/document authority/realtime topology hoặc ghi `BLOCKED` nếu license,
   accessibility hay vận hành chưa được chấp nhận.

## 5. Definition of Done

- [ ] Prototype/evidence của ít nhất tldraw và Excalidraw chạy trên cùng test matrix.
- [ ] Nếu Yjs được đề xuất, provider/topology có prototype và reconnect/persistence evidence.
- [ ] License/cost được xác minh từ nguồn chính thức; mọi paid/new-runtime choice được owner chấp thuận.
- [ ] Convergence, concurrent undo, snapshot restore, tenant/role denial và rate/payload cap đạt.
- [ ] Bundle/memory/500-2.000-shape metrics cùng 2/10/50 participant profile được báo cáo.
- [ ] Keyboard, NVDA, 200% zoom và forced-colors gate có evidence cùng limitation/mitigation.
- [ ] ADR `Accepted` chọn đúng một document/history/undo authority, provider topology và fallback.
- [ ] Phase 5 backlog được tách task implementation/test/rollout theo quyết định ADR.

Cho tới khi toàn bộ checklist trên đạt, Master Plan phải dùng cụm “engine/sync được ADR chọn”;
không được coi tldraw, Excalidraw, Yjs hoặc Hocuspocus là production decision đã chốt.
