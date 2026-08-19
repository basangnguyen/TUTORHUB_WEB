# P5-COLLAB-01 — Gate C authorization và abuse boundary

> **Checkpoint:** 2026-08-19
> **Kết quả:** Gate C `4/4 DONE` trên Excalidraw candidate cô lập.
> **Phạm vi:** `apps/whiteboard-spike`; đây là contract/prototype để khóa hành vi trước
> P5-COLLAB-02..11, chưa phải Core API/PostgreSQL hay collaboration runtime production.

## 1. Ranh giới đã triển khai

- Control plane fixture lấy membership/capability từ server, không tin role/capability do browser tự
  khai. Grant là opaque random value, chỉ lưu bản hash trong memory, dùng đúng một lần và mặc định hết
  hạn sau 30 giây; TTL tuyệt đối không vượt 60 giây.
- Grant bind exact `Origin + session + tenant + document + generation + actor + capability`. Provider
  document name là opaque value do server chọn; browser không tự ghép room name hay nhận service key.
- Response grant dùng `Cache-Control: no-store`, `Referrer-Policy: no-referrer`; browser chỉ giữ grant
  trong biến cục bộ khi khởi tạo provider. Grant không nằm trong URL, DOM, cookie, localStorage hoặc
  sessionStorage.
- Data plane xác thực lại toàn bộ binding khi WebSocket handshake. Capability `view` là receive-only;
  direct protocol mutation vẫn bị từ chối. `edit` và `present` không làm thay đổi tenant/document scope.
- `revoke`, `close` và `restore` luôn tăng generation, chặn grant/refresh cũ và đóng socket của exact
  document generation trong budget 1.000 ms.
- Authorization/rate authority lỗi sẽ fail closed; error chỉ dùng taxonomy bounded, không đưa grant,
  raw tenant/document ID hay board content vào log/evidence.

## 2. Abuse budgets của candidate

| Boundary | Giới hạn |
| --- | ---: |
| HTTP grant body | 8 KiB |
| WebSocket frame | 1 MiB |
| Canonical Yjs update | 768 KiB |
| Awareness payload | 16 KiB |
| Awareness depth / states | 8 / 64 |
| Update rate | 120 message/giây |
| Connection/actor | 4 |
| Connection/document | 50 |
| Connection/tenant | 100 |
| Reconnect attempts | 8 trong 10 giây |
| Grant issues | 8 trong 10 giây |

Payload quá cỡ, scene sâu/corrupt, reader mutation, quota overflow, reconnect storm hoặc rate authority
không sẵn sàng đều bị từ chối trước khi mutation được replicate.

## 3. Bằng chứng

- 7 authorization integration tests PASS: TTL/replay/binding, forged capability và tenant/session/
  generation denial, editor-to-viewer receive-only, direct reader protocol write denial, revoke/close/
  restore socket lifecycle, payload/scene boundary và actor/document/tenant/reconnect/rate quota.
- Full Vitest suite PASS `28/28` trên 6 file; lint và TypeScript strict/e2e typecheck PASS.
- Full Playwright Excalidraw suite PASS `3/3`. Gate C dùng hai browser thật: editor nhận one-time grant,
  viewer nhận `view`, editor mutation hội tụ sang viewer, viewer control bị khóa, response privacy header
  đúng và không có grant trong URL/DOM/storage.
- Gate B concurrency regression được phát hiện trong full suite và đã sửa bằng actor-delta merge cùng
  programmatic projection barrier. Stress two-browser convergence/undo/redo PASS `5/5` liên tiếp.
- `build:excalidraw`, exact dependency/license gate và Excalidraw-only bundle security guard đều PASS;
  production route vẫn force-off.

## 4. Phần chưa được suy diễn là production-ready

- Fixture membership/document lifecycle đang in-memory; P5-COLLAB-02..04 vẫn phải tạo PostgreSQL
  schema, OpenAPI/Core API command và durable grant/revoke authority thật.
- Hocuspocus hiện là exact candidate để kiểm chứng protocol, chưa có durable persistence, HA/drain,
  backup, secret rotation hay approved hosting/TCO.
- Gate D-F vẫn bắt buộc: durable snapshot/recovery/provider exit, load/accessibility và runtime/outage
  acceptance. Vì vậy ADR-0034 giữ `Proposed`, P5-COLLAB-01 giữ `IN PROGRESS` và không deploy.

## 5. Lệnh tái kiểm tra

```powershell
pnpm.cmd --filter @tutorhub/whiteboard-spike lint
pnpm.cmd --filter @tutorhub/whiteboard-spike typecheck
pnpm.cmd --filter @tutorhub/whiteboard-spike test
pnpm.cmd --filter @tutorhub/whiteboard-spike build:excalidraw
pnpm.cmd --filter @tutorhub/whiteboard-spike e2e:excalidraw
pnpm.cmd --filter @tutorhub/whiteboard-spike gate:excalidraw-dependencies
pnpm.cmd --filter @tutorhub/whiteboard-spike security:excalidraw-bundle
```
