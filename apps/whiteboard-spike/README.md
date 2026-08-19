# P5 whiteboard decision spike

Prototype cô lập này phục vụ P5-COLLAB-00 research và P5-COLLAB-01 Excalidraw forward acceptance.
Tldraw automated acceptance được giữ làm historical comparison/provider-exit evidence. Đây không
phải route production, không gọi Core API/LiveKit và không được deploy.

## Chạy

```powershell
pnpm --filter @tutorhub/whiteboard-spike dev
pnpm --filter @tutorhub/whiteboard-spike test
pnpm --filter @tutorhub/whiteboard-spike e2e
pnpm --filter @tutorhub/whiteboard-spike build:collab
pnpm --filter @tutorhub/whiteboard-spike security:collab-bundle
pnpm --filter @tutorhub/whiteboard-spike build:excalidraw
pnpm --filter @tutorhub/whiteboard-spike structure:excalidraw-bundle
pnpm --filter @tutorhub/whiteboard-spike security:excalidraw-bundle
pnpm --filter @tutorhub/whiteboard-spike gate:excalidraw-dependencies
pnpm --filter @tutorhub/whiteboard-spike e2e:excalidraw
```

Mở `http://127.0.0.1:4178` khi chạy dev.

## P5-COLLAB-01 Excalidraw Gate A candidate

- `build:excalidraw` tạo artifact riêng tại `dist-excalidraw`; output này được ignore và không deploy.
- `structure:excalidraw-bundle` kiểm tra entry/manifest, cấm tldraw code và bắt buộc Excalidraw nằm sau
  dynamic import.
- `security:excalidraw-bundle` quét secret/credential cùng demo Firebase/collaboration config; chỉ log
  loại finding và tên file, không log giá trị.
- `gate:excalidraw-dependencies` khóa exact pin, kiểm tra peer/license metadata, inventory dependency,
  packaged font và notice ship cùng artifact.
- `e2e:excalidraw` chạy exact candidate bundle và chứng minh engine chưa tải ở initial shell, chỉ được
  yêu cầu sau hành động người dùng, rồi render fixture 500.

Checkpoint 2026-08-19: Gate A PASS. Exact upstream `v0.18.1`/commit
`a2ec2889babf7d2295469c6d90ebe77fae57df84`; candidate alias sang Radix Tabs `1.1.21` có React 19 peer,
ship `THIRD_PARTY_NOTICES.txt`, và dùng sanitizer fail-closed cho release demo config. Final structure/
security scan, Playwright Radix Sidebar/Tablist và `pnpm audit --prod --audit-level high` đều PASS.
Không dùng peer override hoặc bundle allowlist.

## Retained P5-COLLAB-01 tldraw official-sync evidence

- `?mode=collab` mở tldraw editor dùng official sync với one-time grant qua WebSocket subprotocol.
- `?mode=load` chạy cùng-page 2/10/50 official-sync client profile.
- Node fixture dùng `TLSocketRoom` + SQLite để kiểm tra convergence, actor-local undo, restart,
  tenant/capability, Origin/replay/revoke, quota/rate/frame và snapshot generation swap.
- `build:collab` tạo retained bundle chỉ có tldraw; `security:collab-bundle` scan secret/config và
  chứng minh candidate lịch sử không bị trộn authority.
- Fixture mô phỏng control plane để kiểm tra boundary, không phải Core API production.

Forward gate hiện hành nằm tại
[`docs/P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md`](../../docs/P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md).
Không tái sử dụng PASS của tldraw cho Excalidraw.

## P5-COLLAB-01 Excalidraw Gate D snapshot/recovery

- `CanonicalExcalidrawAuthority` xuất exact provider state và causal state-vector từ cùng một `Y.Doc`.
- `DurableSnapshotStore` ghi immutable artifact qua temporary file/read-back/atomic rename; catalog chỉ
  publish sau checksum verification. Object key ngẫu nhiên và scope binding dùng HMAC key được inject.
- Restart giữ last-good snapshot; interrupted unpublished artifact bị bỏ qua. Corrupt artifact bị
  quarantine với bounded reason và không đổi current generation.
- `SnapshotRestoreCoordinator` dựng authority mới trước, serialize restore owner rồi atomic generation
  swap qua Gate C control plane; stale grant bị deny.
- Portable scene JSON không chứa provider-native state nhưng round-trip giữ semantic hash; active
  content, external fetch, traversal, tamper và unsupported version fail closed.

Automated Gate D `4/4`, portable `2/2`, full suite `34/34` PASS. Đây vẫn là filesystem/control-plane
fixture cô lập; PostgreSQL/B2/durable worker/multi-node thuộc các implementation slice sau. Chi tiết:
[`docs/P5_COLLAB_01_SNAPSHOT_RECOVERY.md`](../../docs/P5_COLLAB_01_SNAPSHOT_RECOVERY.md).

## Phạm vi evidence hiện tại

- tldraw `5.3.1` và `@excalidraw/excalidraw` `0.18.1` được lazy-load độc lập.
- Cùng fixture trung lập 500/2.000 rectangle có label.
- Cùng control shell cho capability `view/edit/present`.
- Snapshot envelope có schema/engine, giới hạn byte khi serialize/parse và `shapeCount` khai báo;
  JSON corruption fail-closed. Restore E2E hiện chỉ là callback smoke với payload chưa thay đổi;
  harness chưa đối chiếu scene/hash hay semantic object count bên trong payload engine, nên chưa
  được coi đây là object-cap hoặc persisted-recovery evidence.
- Automated gate cho keyboard shell, Axe, 200% zoom, forced colors và reduced motion.
- Isolated Yjs `13.6.27` + Hocuspocus `4.6.0` network gate cho hai client convergence,
  offline/reconnect, binary restore, viewer receive-only, tenant/token denial và raw frame cap.
  Giới hạn này có thể chặn full-state resync lớn và chưa phải semantic mutation/complexity budget.
- Ba cold-context Chromium observation cho 500/2.000 object; đây chưa phải p50/p95 production budget.

## Không được suy diễn

- Default research route không phải collaboration evidence; official-sync evidence nằm trong các
  test `tldraw-sync-*` riêng.
- Hocuspocus test chỉ đồng bộ generic `Y.Map`; nó chưa chứng minh scene adapter Excalidraw,
  persistence/restart, multi-node/50 client hoặc TutorHub-issued credential.
- `view/edit/present` trong harness chỉ kiểm tra integration surface; production enforcement
  phải nằm tại collaboration boundary với credential ngắn hạn do backend cấp.
- Không đưa tldraw vào production candidate. Không đưa Excalidraw vào `apps/web` trước khi ADR được
  `Accepted` và Excalidraw-specific gates đạt.

Có thể mở thẳng một engine/fixture độc lập để đo cold context:

```text
http://127.0.0.1:4178/?engine=tldraw&fixture=2000
http://127.0.0.1:4178/?engine=excalidraw&fixture=2000
```
