# P5-COLLAB-06 - Lazy classroom tool shell acceptance

> Trạng thái: `DONE` cho local candidate ngày 2026-08-22. Task này không migrate shared staging,
> không deploy Render và không bật whiteboard production.

## 1. Kết quả

Classroom media shell đã có typed tool registry và whiteboard drawer lazy-load. Teacher có thể mở
tool, chuẩn bị document, bắt đầu trình bày, tạm dừng, tiếp tục và đóng theo capability do server trả
về. Student nhận exact `view` projection và Excalidraw chạy `viewModeEnabled`; UI không suy quyền từ
role, route hoặc tenant state phía client.

Production whiteboard tiếp tục force-off tới P5-COLLAB-17.

## 2. Lifecycle và authority boundary

- `GET /api/v1/whiteboards?media_space_id=...` resolve document theo tenant + media space và trả
  nullable document cùng `can_create`; record không thuộc tenant vẫn bị che giấu.
- Empty state chỉ hiện prepare khi `can_create=true`. Các action open/suspend/resume/close và grant
  exchange chỉ hiện theo `viewer_capabilities` do Core API chiếu.
- Mutation dùng CSRF rotation, idempotency key và expected version/generation hiện có. Data plane
  tiếp tục xác thực capability/generation/revoke generation; client không tự cấp grant hoặc biến
  `view` thành writer.
- Browser session derive exact provider document name, dùng one-time credential qua Hocuspocus/Yjs,
  không đưa token vào URL/DOM/storage và destroy provider/Y.Doc khi tool unmount.
- Canonical authority giữ scene/undo/redo. Excalidraw local history bị clear khi nhận canonical
  projection; view-only không khởi tạo document và không phát scene update.

## 3. Lazy shell, focus và media continuity

- Tool content chỉ mount sau khi người dùng mở whiteboard drawer; đóng drawer unmount engine và trả
  focus về đúng toolbar trigger.
- Toolbar giữ roving keyboard focus. Loading, empty, retryable error, forbidden, read-only,
  reconnect và feature-off đều có bounded text/status; semantic canvas fallback liệt kê element cho
  assistive technology.
- Whiteboard drawer nằm dưới `ClassroomMediaShell`, không thay key hoặc lifecycle của
  `LiveKitRoom`. Automated test xác nhận media tile DOM node được giữ nguyên qua open/close tool và
  page StrictMode chỉ dựng một room instance.

## 4. Bundle guard

Vite production build PASS với `4,465` modules. Evidence trên exact local build:

| Artifact                            |      Raw size | Kết quả                              |
| ----------------------------------- | ------------: | ------------------------------------ |
| `index-_5n_2ds0.js`                 | 484,736 bytes | không chứa `Excalidraw`/`excalidraw` |
| `LazyWhiteboardEngine-D56MdFR-.js`  | 673,009 bytes | lazy engine riêng, chứa Excalidraw   |
| `LazyWhiteboardEngine-DtRVLazd.css` | 142,020 bytes | editor CSS nằm ở lazy boundary       |

Shell/API projection nhỏ có thể nằm trong route chunk, nhưng Excalidraw engine và CSS không nằm
trong initial application/classroom entry. Các chunk Mermaid/language lớn là dependency chỉ tải sau
lazy engine; tối ưu sâu hơn được theo dõi ở performance gate P5-COLLAB-14.

## 5. Verification

Các gate local của candidate:

```powershell
go test ./internal/modules/collaboration ./internal/httpapi
vitest run ClassroomWhiteboardTool.test.tsx LazyWhiteboardEngine.test.tsx `
  ClassroomMediaShell.test.tsx MediaSpacePages.test.tsx
vitest run packages/collaboration-client/src/browserSession.test.ts
tsc -b apps/web/tsconfig.json --pretty false
vite build
pnpm verify
git diff --check
```

Targeted UI/page suite PASS `59/59`; collaboration-client PASS `2/2`; Core API collaboration và
HTTP API PASS. TypeScript build, Vite production build, Storybook và client bundle-security PASS.
Full repository frontend verify PASS; Go test/vet PASS với `GOCACHE` đặt trong workspace để tránh
giới hạn ghi cache mặc định của Windows. Final no-secret/diff checks PASS trên candidate cuối.

## 6. Deferred, không suy rộng

- Durable snapshot/import/export/restore worker và B2 binding thuộc P5-COLLAB-07.
- Reconnect/compaction recovery end-to-end thuộc P5-COLLAB-08; performance 500/2.000 shapes và
  2/10/50 người thuộc P5-COLLAB-14; physical browser/NVDA matrix thuộc P5-COLLAB-15.
- Không chạy migration mới, live Neon disposable, shared-staging write, Render deploy hoặc external
  secret rotation trong task này.
