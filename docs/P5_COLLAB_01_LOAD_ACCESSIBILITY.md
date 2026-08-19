# P5-COLLAB-01 Gate E — Excalidraw load và accessibility

> **Ngày chạy:** 2026-08-19
> **Trạng thái:** `GATE E 4/4 PASS; OWNER NVDA SPEECH CONFIRMED`
> **Phạm vi:** exact isolated Excalidraw/Y.Doc/Hocuspocus candidate trong
> `apps/whiteboard-spike`; không nối `apps/web`, database, shared staging hoặc deploy.

## 1. Exact profile và budget

Profile dùng full Excalidraw rectangle records, canonical `Y.Doc` `13.6.27`, Hocuspocus `4.6.0`
và Gate C one-time grant/capability/connection quota. Mỗi client có Y.Doc/provider riêng; 50 client
không phải 50 canvas chạy chung một browser. Bootstrap 2.000 phần tử được chia transaction tăng dần
để từng inbound update vẫn dưới cap 768 KiB; final state không bị rút gọn.

Budget local pre-production được khóa trong code và fail test nếu vượt:

| Profile    |  Join p95 | Convergence p95 | Aggregate CPU | Heap delta | Client receive |  Cleanup |
| ---------- | --------: | --------------: | ------------: | ---------: | -------------: | -------: |
| 2 × 500    |  3.000 ms |        1.500 ms |      3.000 ms |    128 MiB |          4 MiB | 2.000 ms |
| 10 × 500   |  7.500 ms |        2.500 ms |      8.000 ms |    320 MiB |         16 MiB | 3.000 ms |
| 50 × 2.000 | 20.000 ms |        5.000 ms |     40.000 ms |  1.024 MiB |        128 MiB | 5.000 ms |

Kết quả lần chạy được lưu làm baseline local, không tự suy thành production SLO:

| Profile    |   Join p95 | Convergence p95 | Aggregate CPU |    Heap delta | Client receive |       State |                Cleanup |
| ---------- | ---------: | --------------: | ------------: | ------------: | -------------: | ----------: | ---------------------: |
| 2 × 500    |    29,6 ms |         26,9 ms |        516 ms |    0 B[^heap] |      607.947 B |   303.488 B | 22,3 ms / 0 connection |
| 10 × 500   |    86,3 ms |         61,2 ms |      1.124 ms |  16.295.432 B |    3.038.921 B |   303.488 B | 25,3 ms / 0 connection |
| 50 × 2.000 | 1.100,9 ms |        595,6 ms |     28.454 ms | 227.828.512 B |   61.989.867 B | 1.575.530 B | 31,7 ms / 0 connection |

[^heap]:
    `heapDeltaBytes` là delta không âm tại thời điểm sample. Giá trị 0 nghĩa heap sau profile
    thấp hơn baseline do garbage collection của full suite; đây không phải tuyên bố profile không dùng
    bộ nhớ.

Tất cả client đạt cùng deterministic semantic hash sau mutation. Profile 50 người chạm đúng
document connection ceiling `50`, sau destroy provider/authority thì `activeConnections=0`.
Aggregate CPU ở profile lớn là tổng CPU của 50 client chạy trong một Node process; Gate F vẫn phải
đo exact multi-node runtime, backpressure, persistence và chi phí trước production approval.

## 2. Semantic canvas companion/fallback

Candidate hiện luôn cung cấp một semantic companion lấy từ cùng canonical scene/fixture:

- page name, total count, deterministic hash và danh sách z-order;
- mô tả type, text/label, vị trí/kích thước, arrow binding, link và cảnh báo image thiếu alt;
- phân trang 50 phần tử để fixture 2.000 không tạo 2.000 DOM node cùng lúc;
- nút keyboard chuyển focus từ shell sang semantic heading, từ semantic view về canvas và trả focus
  về nút mở sau khi đóng engine;
- companion vẫn hoạt động khi engine chưa mở hoặc đã đóng và không ghi ngược vào document, nên không
  tạo document/history authority thứ hai.

Đây là reading fallback cho giới hạn canvas/screen reader, chưa phải bằng chứng rằng toàn bộ object
authoring của Excalidraw dùng được bằng NVDA. Production phải công bố giới hạn này và giữ feature
force-off cho tới physical gate.

## 3. Automated accessibility

Exact production-style candidate bundle PASS `6/6` Playwright tổng: 3 Gate A/B/C và 3 Gate E.
Gate E bao phủ:

- Axe toàn shell trước/sau lazy-load, không allowlist violation;
- keyboard open/close, semantic pagination và focus handoff hai chiều;
- fixture 2.000 ở viewport 640 CSS px, tương đương desktop 1.280 px tại browser zoom 200%, không
  horizontal overflow;
- forced colors, visible border/focus và reduced-motion duration tối đa 0,01 ms;
- semantic DOM chỉ có 50 list item mỗi trang.

Axe đã phát hiện hai lỗi upstream Excalidraw `0.18.1` trên exact candidate: nút main-menu thu gọn
thiếu accessible name tại viewport 200% và `footer` lồng trong shell tạo landmark `contentinfo`
không ở top-level. Adapter thêm bounded DOM compatibility repair: gắn `aria-label` đúng nút và đổi
footer lồng thành `role="group"` có tên "Điều khiển bảng vẽ". Cả hai được đánh dấu
`data-accessibility-patch`, không sửa `node_modules` và không che violation. Sau remediation, toàn bộ
Axe scan PASS và E2E có regression assertion cho cả hai repair.

Unit/integration Gate E PASS profile `3/3` và semantic companion `1/1`; full suite PASS `39/39` trong
10 file. Lint, typecheck, build, exact dependency/license, 184-asset structure và 182-text-asset
security guard đều PASS.

## 4. Installed-browser physical automation

Harness headed chạy trực tiếp hai browser cài trên Windows, không dùng bundled Chromium:

| Browser        | Version          | Tool role/name | Axe default | Axe forced colors | Semantic  | Cleanup focus      |
| -------------- | ---------------- | -------------: | ----------: | ----------------: | --------- | ------------------ |
| Google Chrome  | `151.0.7922.140` |             11 |           0 |                 0 | page 2/10 | Mở bảng Excalidraw |
| Microsoft Edge | `151.0.4129.86`  |             11 |           0 |                 0 | page 2/10 | Mở bảng Excalidraw |

Hai browser đều PASS accessible role/name/focus của toolbar, keyboard semantic pagination,
640-CSS-pixel reflow tương đương desktop 1.280 px ở 200%, không horizontal overflow, forced colors,
reduced motion và deterministic cleanup focus. Bằng chứng bounded nằm trong ignored local path
`test-results/p5-collab-01-gate-e-physical/evidence.json`; file không chứa secret hoặc nội dung bảng
riêng tư.

Computer UI automation cũng xác nhận accessibility tree thật của Chrome công bố tên menu, 11 công
cụ, zoom, undo/redo, trợ giúp và semantic companion. Đây là bằng chứng browser/UIA bổ sung, không
thể thay việc một người thực sự nghe nội dung NVDA phát ra.

## 5. Physical NVDA speech — owner confirmed

Installed-browser automation và Axe không thay physical NVDA speech. Ngày 2026-08-19, owner xác
nhận đã nghe NVDA đọc đúng trên exact candidate:

- [x] Chrome + NVDA phát đúng toolbar, tool/mode name, selection/action và semantic fallback.
- [x] Edge + NVDA phát đúng cùng matrix, không suy PASS từ Chrome.
- [x] Reconnect/error announcement và deterministic focus recovery.
- [x] 200% equivalent, forced colors, reduced motion và keyboard-only trên hai browser cài thật.
- [x] Kết quả speech PASS với NVDA `2026.1.1.55980`; không ghi nội dung bảng
      riêng tư.

Gate E đạt `4/4 PASS`. P5-COLLAB-01 vẫn `IN PROGRESS` vì Gate F runtime/operations chưa đạt;
ADR-0034 vẫn `Proposed` và production whiteboard tiếp tục force-off.

## 6. Lệnh tái kiểm tra

```powershell
pnpm.cmd --filter @tutorhub/whiteboard-spike test
pnpm.cmd --filter @tutorhub/whiteboard-spike lint
pnpm.cmd --filter @tutorhub/whiteboard-spike typecheck
pnpm.cmd --filter @tutorhub/whiteboard-spike build:excalidraw
pnpm.cmd --filter @tutorhub/whiteboard-spike e2e:excalidraw
pnpm.cmd --filter @tutorhub/whiteboard-spike e2e:excalidraw:physical
pnpm.cmd --filter @tutorhub/whiteboard-spike gate:excalidraw-dependencies
pnpm.cmd --filter @tutorhub/whiteboard-spike structure:excalidraw-bundle
pnpm.cmd --filter @tutorhub/whiteboard-spike security:excalidraw-bundle
```
