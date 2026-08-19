# P5-COLLAB-01 — tldraw automated candidate evidence

> **Ngày chạy:** 2026-08-18
> **Trạng thái:** `RETAINED EVIDENCE — SUPERSEDED TARGET`. Toàn bộ gate tự động của isolated tldraw
> prototype đã PASS, nhưng owner đã chọn Excalidraw làm engine chính thức sau đó. Kết quả này không
> làm P5-COLLAB-01 `VERIFY` và không được suy thành Excalidraw/production evidence.
> **Phạm vi:** chỉ `apps/whiteboard-spike`; không nối `apps/web`, Core API production, migration,
> shared staging hoặc deploy.

## Candidate đã kiểm tra

- `tldraw@5.3.1`, `@tldraw/sync@5.3.1`, `@tldraw/sync-core@5.3.1`.
- React/ReactDOM `19.2.7`, nằm trong peer range công bố `^18.2.0 || ^19.2.1` của tldraw `5.3.1`.
- tldraw store + đúng một `TLSocketRoom` cho mỗi tenant/document/generation là document, history và
  undo authority duy nhất; SQLite chỉ là persistence của cùng official sync model.
- Node fixture mô phỏng TutorHub control-plane boundary để kiểm tra grant/capability/revoke. Đây chưa
  phải Core API production và không thay cho P5-COLLAB-02..11.

## Kết quả tự động

| Gate                        | Kết quả | Evidence chính                                                                                                        |
| --------------------------- | ------- | --------------------------------------------------------------------------------------------------------------------- |
| Lint + TypeScript strict    | PASS    | ESLint sạch; app/server/E2E typecheck sạch                                                                            |
| Unit                        | PASS    | 3 files, 13 tests                                                                                                     |
| Full Playwright             | PASS    | 15/15 tests trong khoảng 1,1 phút                                                                                     |
| Convergence/history         | PASS    | Hai browser concurrent edit; actor-local undo/redo không xóa remote edit                                              |
| Offline/recovery            | PASS    | Hai phía chỉnh sửa khi offline rồi hội tụ; SQLite sống qua room restart                                               |
| Auth/isolation              | PASS    | Exact Origin, tenant-qualified room, capability escalation/reader write denial, grant không nằm trong URL/DOM/storage |
| Credential lifecycle        | PASS    | One-time grant 30 giây, replay/binding mismatch denial, actor revoke đóng socket và chặn grant mới                    |
| Abuse boundary              | PASS    | 4 connection/actor ceiling; connection thứ 5 bị từ chối; frame 600 KiB và message burst bị đóng fail-closed           |
| Snapshot/restore            | PASS    | Checksum, corrupt denial, generation CAS/swap, stale client và stale restore bị chặn                                  |
| Automated accessibility     | PASS    | Axe shell, keyboard focus, 200% zoom, forced colors, reduced motion                                                   |
| Load/convergence            | PASS    | 2/10/50 official-sync client với fixture 500/500/2.000 shape; cleanup active session về 0                             |
| Production dependency audit | PASS    | `pnpm audit --prod --audit-level high`: không có known vulnerability                                                  |
| Candidate bundle guard      | PASS    | 6 file; không có secret pattern và không kéo Excalidraw/Yjs/Hocuspocus vào tldraw-only build                          |

Quan sát tải mới nhất, chỉ dùng làm baseline local chứ chưa phải SLO production:

| Client | Shape |     Join | Convergence | Chromium heap observation |
| -----: | ----: | -------: | ----------: | ------------------------: |
|      2 |   500 | 1.069 ms |    1.063 ms |           64.000.000 byte |
|     10 |   500 |   557 ms |    1.423 ms |           64.000.000 byte |
|     50 | 2.000 | 6.627 ms |    3.638 ms |           64.000.000 byte |

Tldraw-only candidate bundle hiện có JavaScript chính 1.908,80 KiB raw / 570,76 KiB gzip và CSS
77,42 KiB raw / 14,49 KiB gzip. Đây là bằng chứng cần lazy-load ở tool boundary; chưa phải budget
được chấp nhận cho production.

## Lệnh tái kiểm tra

```powershell
pnpm.cmd --filter @tutorhub/whiteboard-spike lint
pnpm.cmd --filter @tutorhub/whiteboard-spike typecheck
pnpm.cmd --filter @tutorhub/whiteboard-spike test
pnpm.cmd --filter @tutorhub/whiteboard-spike build
pnpm.cmd --filter @tutorhub/whiteboard-spike e2e
pnpm.cmd --filter @tutorhub/whiteboard-spike build:collab
pnpm.cmd --filter @tutorhub/whiteboard-spike security:collab-bundle
pnpm.cmd audit --prod --audit-level high
```

## Gate đã còn mở tại thời điểm tldraw là target

1. Owner nhận và chấp thuận commercial license/quote/terms, telemetry behavior, exact production key/
   allowed-domain model và TCO.
2. Chọn exact self-hosted runtime/region/storage/HA/backup/on-call/cost; chạy credential rotation,
   sustained outage, kill switch và provider-exit drill trên runtime đó.
3. Installed Windows Chrome/Edge automation sau đó đã PASS role/name/focus, reflow, forced colors và
   Axe trên Excalidraw target; owner NVDA speech cho toolbar, object action,
   reconnect/error/focus recovery vẫn bắt buộc vì automation không chứng minh nội dung đã nghe.
4. Portable export/import round-trip ngoài provider đang chọn phải giữ semantic/final hash; snapshot
   native hiện chỉ chứng minh same-engine recovery.

Target tldraw đã bị thay thế trước production approval; trial/commercial key chưa được kích hoạt.
Whiteboard tiếp tục force-off và P5-COLLAB-02 trở đi chưa được mở. Forward gate của target hiện tại
nằm tại `docs/P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md`.
