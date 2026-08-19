# P5-COLLAB-01 Gate D — Snapshot, recovery và provider exit

> **Ngày kiểm chứng:** 2026-08-19
> **Kết quả:** `4/4 DONE` trong prototype cô lập `apps/whiteboard-spike`
> **Ranh giới:** không nối `apps/web`, không tạo migration, không ghi shared staging và không deploy.

## 1. Topology đã chứng minh

Mỗi document generation vẫn có đúng một `Y.Doc` canonical authority. Snapshot worker chỉ đọc một
điểm causal đã xác định và tạo hai representation trong cùng immutable artifact:

- provider state Yjs để khởi động lại đúng provider/generation;
- portable canonical scene JSON để khôi phục sang generation mới hoặc rời provider mà không cần đọc
  provider-native binary.

Catalog chỉ được publish sau khi worker ghi artifact qua temporary file, kiểm tra lại byte length và
SHA-256, rồi atomic rename. PostgreSQL/B2 production chưa được provision trong gate này; filesystem
fixture mô phỏng đúng separation: catalog là metadata authority, object path là immutable blob và
không bên nào trở thành live document writer.

## 2. Snapshot contract

Snapshot format `tutorhub.excalidraw.immutable-snapshot` v2 khóa:

- engine `excalidraw@0.18.1`, canonical schema v1 và provider `yjs@13.6.27`;
- semantic hash, causal state-vector watermark, element/file count, created-at và creator allowlist;
- SHA-256 và exact byte length trong catalog;
- artifact tối đa 32 MiB, provider state tối đa 20 MiB, canonical scene tối đa 16 MiB, 2.000 element
  và 256 file theo canonical authority;
- object key ngẫu nhiên 192-bit; key và artifact không chứa raw tenant/document ID;
- tenant/document/generation binding bằng HMAC-SHA-256 với key được inject, không hardcode; v2 thêm
  bounded `scopeBindingKeyId` để giữ verification key cũ suốt retention khi rotate.

Interrupted write sau atomic artifact rename nhưng trước catalog publication để lại orphan không thể
được restore qua API. Sau restart, store chỉ nhìn thấy last-good entry đã publish. Test còn thay đổi live
authority sau điểm snapshot để xác nhận durable recovery không lấy nhầm update mới hơn.

## 3. Corrupt quarantine và generation swap

Mỗi restore đọc lại artifact, đối chiếu checksum/size/version/HMAC/metadata/object count/semantic hash.
Artifact thiếu, sai checksum, sai binding, không tương thích hoặc provider state hỏng bị chuyển sang
quarantine và catalog chỉ giữ bounded reason; lỗi không chứa board content hay raw provider error.
Last-good generation không thay đổi khi validation thất bại.

Restore hợp lệ dựng authority generation mới từ portable scene trước, sau đó mới gọi control-plane
`restore` để tăng generation và atomic current-generation swap. Restore lock theo tenant/document chỉ
cho một worker sở hữu next generation. Grant của generation cũ bị Gate C control plane từ chối với
`stale_generation`.

## 4. Portable provider-exit

Portable format `tutorhub.excalidraw.portable-scene` v1 chỉ chứa canonical scene, engine/schema version,
export timestamp và semantic hash; không chứa `providerState`, Yjs update hoặc provider credential.
Round-trip sang một authority mới giữ nguyên semantic hash. Import fail closed với unsupported version,
hash tamper, oversized/deep JSON, external fetch URL, path traversal, active HTML/iframe/script/SVG,
unsafe data URL và prototype-pollution key.

## 5. Automated evidence

- Gate D snapshot/recovery integration: `4/4` PASS.
- Portable provider-exit unit: `2/2` PASS.
- Gate F bổ sung current/historical binding-key recovery và premature-retirement fail-closed PASS;
  artifact/catalog format được bump rõ từ prototype v1 sang v2, không reinterpret dữ liệu cũ.
- Full whiteboard unit/integration: `34/34`, 8 test files PASS.
- ESLint và TypeScript project/E2E typecheck PASS.
- Excalidraw-only production build PASS; expected large-chunk warning vẫn thuộc Gate E performance.
- Exact dependency/license gate PASS; structure guard PASS 184 assets, security scan PASS 182 text
  assets và không có tldraw code trong candidate bundle.

Các test chính nằm tại:

- `apps/whiteboard-spike/server/excalidrawSnapshotHarness.test.ts`;
- `apps/whiteboard-spike/src/excalidraw/portableScene.test.ts`.

## 6. Giới hạn còn lại

Gate D xác minh contract và failure semantics, không phải production persistence approval. P5-COLLAB-02,
P5-COLLAB-07 và P5-COLLAB-08 vẫn phải triển khai PostgreSQL CAS/catalog, B2 upload/retention, durable
worker, multi-node fencing và operational backup/restore. Gate E đã PASS; Gate F isolated contract
đã PASS nhưng exact disposable runtime/HA/drain/outage/TCO và owner approval vẫn còn. Vì vậy ADR-0034 vẫn `Proposed`,
P5-COLLAB-01 vẫn `IN PROGRESS` và production whiteboard vẫn force-off.
