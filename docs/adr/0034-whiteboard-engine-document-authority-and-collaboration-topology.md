# ADR 0034: Whiteboard engine, document authority và collaboration topology

- Status: Accepted
- Engine decision: Final — Excalidraw
- Runtime profile decision: Final for development/private alpha — Render Free Singapore, one instance,
  no Redis
- Date: 2026-08-18
- Accepted: 2026-08-20
- Scope: P5-COLLAB-00 và P5-COLLAB-01 đến P5-COLLAB-20
- Depends on: ADR-0002, ADR-0003, ADR-0013, ADR-0015, ADR-0026, ADR-0027, ADR-0030
- Evidence: `docs/P5_COLLAB_00_RESEARCH_SPIKE.md`, `docs/P5_COLLAB_00_RESEARCH_RESULTS.md`,
  `docs/P5_COLLAB_01_AUTOMATED_ACCEPTANCE.md`, `docs/P5_COLLAB_01_CANONICAL_AUTHORITY.md`,
  `docs/P5_COLLAB_01_AUTHORIZATION.md`, `docs/P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md`,
  `docs/P5_COLLAB_01_RUNTIME_OPERATIONS.md` và prototype cô lập `apps/whiteboard-spike`

## Context

Phase 5 cần whiteboard cộng tác cho classroom pilot 2-50 người. V1 có kinh nghiệm tldraw/Yjs nhưng
JCEF bridge, client-owned authority và topology V1 không được port. Phase 4 đã khóa PostgreSQL/shared
policy + Core API là business/control authority và LiveKit chỉ là media transport; whiteboard không
được dùng LiveKit DataChannel hoặc Core API REST làm document operation transport.

P5-COLLAB-00 giữ hai finalist:

1. **tldraw SDK + official sync**: engine/store/sync cùng một mô hình record native;
2. **Excalidraw package + Yjs/Hocuspocus**: Excalidraw là renderer/editor React, còn Y.Doc cùng
   provider tự quản lý là candidate document/sync authority.

Hai lựa chọn đều có điểm mạnh nhưng chưa đủ bằng chứng để chấp nhận production. tldraw còn blocker
owner cho production license/key/cost, telemetry và runtime/managed sync ownership. Excalidraw có
license MIT cho package nhưng package nhúng không cung cấp collaboration drop-in, còn bundle hiện
nhúng public Google API key cùng Firebase/collaboration config upstream và bị client bundle guard
chặn. Các Radix dependency mà Excalidraw pin cũng chỉ công bố React/ReactDOM peer range đến 18,
chưa bao phủ React 19.2.7 của TutorHub; build/E2E xanh không thay compatibility approval. Adapter
Yjs, actor-local undo, persistence, scaling và Node/Hocuspocus operations là trách nhiệm TutorHub.

Nguồn chính thức cần được dùng khi đóng quyết định:

- [tldraw licensing](https://tldraw.dev/community/license),
  [tldraw sync](https://tldraw.dev/docs/sync) và
  [tldraw accessibility](https://tldraw.dev/sdk-features/accessibility);
- [Excalidraw integration](https://docs.excalidraw.com/docs/@excalidraw/excalidraw/integration),
  [Excalidraw API](https://docs.excalidraw.com/docs/@excalidraw/excalidraw/api) và
  [Excalidraw repository](https://github.com/excalidraw/excalidraw);
- [Yjs repository](https://github.com/yjs/yjs),
  [Yjs awareness](https://docs.yjs.dev/getting-started/adding-awareness) và
  [Hocuspocus](https://tiptap.dev/docs/hocuspocus/introduction).

## Decision status

Sau khi xem lại ràng buộc license thương mại và quyền tự chủ dài hạn, owner direction cuối ngày
2026-08-18 chọn **Excalidraw làm whiteboard engine chính thức** và một collaboration topology tự
quản làm target duy nhất của P5-COLLAB-01. Gate B ngày 2026-08-19 khóa `Y.Doc` `13.6.27` làm canonical
document/history authority và Hocuspocus `4.6.0` làm exact transport/provider candidate cho chuỗi
acceptance. Gate F đã khóa exact runtime contract; ngày 2026-08-19 owner chọn profile
`FREE_PRIVATE_ALPHA` dùng một Render Free instance Singapore, không Redis, Neon checkpoint và B2
portable snapshot trong free allowance. Provider-backed drill, quota evidence và owner risk/operations
approval đều PASS ngày 2026-08-20. Quyết định này thay thế target tldraw trước đó và cho phép bắt đầu
P5-COLLAB-02, nhưng chưa phải production deployment approval.

Acceptance evidence đã đạt gồm:

- cùng test matrix chứng minh convergence, concurrent actor-local undo, snapshot recovery,
  500/2.000 shapes, 2/10/50 profile và accessibility;
- owner chấp thuận exact production pin, license/dependency notices, runtime và TCO của lựa chọn;
- prototype chứng minh chỉ có một document/history/undo authority;
- rollback/provider-exit bằng portable artifact được drill.

Những constraint dưới đây tiếp tục bắt buộc. P5-COLLAB-01 đã khóa exact version, một document authority,
runtime/cost cùng evidence. Tldraw prototype được giữ nguyên làm historical comparison/provider-exit
evidence, không phải writer/provider song song và không được đưa vào production bundle. Không được
coi ADR `Accepted` là production enablement.

Owner closure 2026-08-20:

- disposable Render Free Singapore, Neon role riêng và B2 private bucket PASS baseline,
  SIGTERM/drain + recovery, sustained Control/Neon/B2 outage, credential rotation, portable semantic
  restore round-trip và cleanup-zero;
- quota evidence redacted PASS: Render `35.12/750` free instance hours, Neon `9.55/100` CU-hours và
  `8/10` branches, B2 current cost `0.00` với `709` transactions;
- owner chấp thuận single-instance/no-HA, spin-down/cold-start, hard cap `0 USD`, RPO/RTO candidate,
  `SEV-1/SEV-2` taxonomy và B2 Object Lock disabled cho private alpha;
- Primary on-call `Bá Sáng`, backup `Duy Mạnh`, security incident owner `Bá Sáng`, cost owner `Bá Sáng`.

Các bằng chứng này đủ để ADR chuyển `Accepted` cho development/private alpha topology; production
whiteboard vẫn force-off cho tới P5-COLLAB-17 exact staging và rollout approval sau đó.

### Target topology đã chấp nhận

- `@excalidraw/excalidraw` là engine/editor projection; không tự trở thành collaboration/history
  authority khi chạy nhiều người;
- self-managed collaboration layer phải có đúng một canonical document/history/undo authority cho
  mỗi document generation. Gate B chọn `Y.Doc` `13.6.27`; Hocuspocus `4.6.0` chỉ vận chuyển/replicate
  authority đó, không sở hữu một document/history thứ hai. Scene mapping, actor-local undo và
  reconnect và durable recovery đã PASS ở isolated adapter/provider test;
- Gate F khóa Node `24.15.0` và owner chọn một Render Free instance Singapore, không Redis, Neon Yjs
  binary checkpoint và B2 immutable portable snapshot cho development/private alpha với hard cap
  `0 USD`. Profile này chấp nhận spin-down/cold-start, restart gap, single point of failure và không có
  multi-region DR; disposable provider drill và operational owner approval đã PASS ngày 2026-08-20;
- Render Standard x2 + Redis Cloud paid Multi-AZ chỉ là deferred production HA upgrade path. Chưa mua,
  chưa provision và không được dùng isolated two-node tests để tuyên bố free profile là HA;
- Core API/PostgreSQL sở hữu tenant, lifecycle, capability, one-time grant và revoke generation;
- PostgreSQL/B2 chỉ giữ metadata cùng immutable snapshot/export artifact, không làm live writer;
- không dùng Excalidraw local history và CRDT/provider history như hai writer cạnh tranh; không ghép
  tldraw store/sync hoặc TutorHub operation log vào cùng document generation;
- exact candidate pin `@excalidraw/excalidraw@0.18.1` đã PASS Gate A về upstream bundle config,
  React peer compatibility và dependency/asset notices; production route/runtime vẫn force-off.

## Accepted invariants chung

### 1. Hai loại authority, không trộn vai trò

**Control plane - Core API/PostgreSQL** sở hữu:

- tenant/source/document binding, lifecycle và current restore generation;
- active membership, capability `view/edit/present`, feature/quota và revoke generation;
- one-time connection grant, snapshot catalog, retention và audit metadata allowlist;
- export/import/restore command authorization và idempotency.

**Collaboration data plane - provider/gateway được chọn** sở hữu:

- authenticated connection, operation ordering/convergence và transient awareness;
- live document state/history theo document model đã chọn;
- payload/rate/connection enforcement và resync protocol.

PostgreSQL không lưu song song live operation log để tái tạo một history cạnh tranh. B2 chỉ giữ
immutable snapshot/export blob, không quyết định document hiện hành. Awareness/cursor/presence không
phải attendance, grade, membership hoặc durable audit authority.

### 2. Một document/history/undo authority

Mỗi document generation có đúng một writer topology và một causal history:

- **Selected engine — Excalidraw:** `Y.Doc` `13.6.27` là canonical document/history cho exact
  `{tenant, document, generation}`; Excalidraw chỉ là view/editor projection. Hocuspocus `4.6.0` là
  exact Gate B provider candidate và không tạo authority thứ hai. Excalidraw internal history không
  được nối thành writer/history cạnh tranh; undo/redo chỉ đi qua actor-scoped transaction origin và
  `Y.UndoManager`. Element register giữ revision/tombstone riêng theo actor để local undo không xóa
  remote value của cùng element. Free private-alpha hosting đã được chọn nhưng vẫn chờ provider-backed
  persistence/recovery/operations gate; production HA vẫn deferred.
- **Held fallback — tldraw:** official-sync prototype chỉ là comparison/provider-exit evidence,
  không nằm trong active implementation hoặc production bundle. Nếu một owner/ADR mới kích hoạt lại,
  tldraw store + official sync phải là canonical document/history và không được ghép Yjs,
  Hocuspocus hoặc TutorHub operation log vào cùng generation.

Không dual-write giữa hai engine/provider để migration hoặc fallback. Provider migration dùng
immutable export/snapshot, tạo generation mới và atomic control-plane swap; generation cũ chuyển
read-only/terminal.

### 3. Grant, capability và revoke

Browser chỉ nhận one-time exchange grant:

- TTL tối đa 60 giây; exact opaque document ID, actor/session binding, capability allowlist,
  permitted Origin, nonce/JTI và current revoke generation;
- response `Cache-Control: no-store`, `Referrer-Policy: no-referrer`; giữ memory-only, không URL,
  DOM, storage, analytics hoặc support export;
- provider credential tối thiểu quyền; browser không nhận service secret hay tự chọn provider room;
- data plane enforce `view` không được gửi document mutation, kể cả client gọi API trực tiếp;
- membership/source/document terminal hoặc revoke tăng generation, chặn exchange/refresh và đóng
  connection trong failure budget đã công bố.

Gate C ngày 2026-08-19 đã chứng minh contract này trên exact Excalidraw/Y.Doc/Hocuspocus candidate
cô lập: grant mặc định 30 giây/tối đa 60 giây, hashed one-time store, exact binding và replay denial;
reader direct-protocol mutation bị chặn; revoke/close/restore tăng generation và đóng socket trong
budget 1.000 ms. Actor/document/tenant connection quota, payload/rate/reconnect storm và authority
outage đều fail closed. Đây là prototype boundary, không thay Core API/PostgreSQL production của
P5-COLLAB-02..11.

Handshake phải kiểm tra Origin allowlist, exact document/generation, expiry/replay và server-resolved
capability. Frame/update/awareness có byte/object/depth/rate cap; connection quota theo actor/document/
tenant chặn reconnect storm, CRDT amplification và noisy tenant. Storage/rate authority lỗi phải fail
closed, không nâng reader thành writer.

### 4. Snapshot, import/export và restore

Snapshot là immutable envelope gồm tối thiểu:

- format/engine/schema version;
- opaque tenant/document/generation binding trong metadata server-side;
- causal sequence/watermark thích hợp với engine;
- byte length, content checksum, created-at và creator/service provenance allowlist;
- opaque B2 object key; không chứa email, display name hoặc ID có thể đoán trong key/log.

Snapshot chỉ được publish vào catalog sau upload + checksum verification. Restore không sửa live
generation tại chỗ: worker validate/quarantine artifact, tạo generation mới và Core API atomic swap
current generation. Stale client/provider connection không được ghi vào generation mới.

Import/export có size/object/depth/time cap; archive path traversal/zip bomb, active HTML/SVG/script,
external fetch/SSRF và unsupported schema fail closed. Export cần current tenant/document authority,
`no-store`, short-lived B2 capability và audit metadata không chứa content. Corrupt/incompatible
snapshot giữ last-good generation và trả error taxonomy bounded.

### 5. Undo/history boundary

Undo/redo là actor-local theo transaction origin đã xác thực:

- local undo không xóa remote actor change ngoài semantics được công bố và test;
- reconnect/resync/compaction không biến remote transaction thành local undo item;
- present/pan/selection/cursor/awareness không đi vào durable document history trừ khi ADR Accepted
  nêu rõ object nào là document state;
- restore là lifecycle command tạo generation mới, không phải một undo item;
- concurrent undo/redo, duplicate/out-of-order update và offline reconnect phải hội tụ cùng final hash.

Candidate không chứng minh được boundary trên là `NO-GO`, dù demo hai browser có vẻ đồng bộ.

### 6. Accessibility và product fallback

Engine phải lazy-load trong TutorHub tool shell và không remount classroom media. Gate tối thiểu:

- keyboard-only open/close/tool selection/object action và deterministic focus handoff;
- Windows Chrome/Edge + NVDA cho tên công cụ, mode, selection/action quan trọng, error/reconnect;
- 200% zoom/reflow, forced colors, reduced motion và visible focus;
- semantic alternative/fallback cho nội dung canvas không thể điều hướng đầy đủ bằng screen reader;
- automated Axe chỉ bổ sung, không thay physical NVDA/canvas test.

Nếu engine không cung cấp object-level accessibility đủ cho use case, pilot phải giới hạn authoring,
cung cấp accessible companion representation/export hoặc giữ feature force-off. Accessibility
limitation không được che bằng tài liệu marketing của engine.

### 7. Privacy, observability và operations

- Không log board content, operation, snapshot/import/export body, grant, provider credential, email,
  raw document/provider ID hoặc raw provider error.
- Metrics dùng bounded labels: outcome/error taxonomy, engine/provider version, coarse size/profile;
  không tenant/user/document high-cardinality label.
- Collaboration runtime có health/readiness/drain, secret rotation, backup/restore, compaction,
  on-call owner, cost/quota alert và outage runbook riêng media plane.
- Feature catalog default false và deployment guardrail force-off tới exact staging acceptance.
- Kill switch chặn connection/edit mới và đưa room hiện hữu về bounded read-only/export path; không
  xóa snapshot hoặc tự động chuyển sang writer provider khác.

Gate F isolated executable model ngày 2026-08-19 PASS `18/18`: opaque room name thay đổi theo
generation, atomic one-time grant/global quota, writer lease/fencing/takeover, checkpoint/drain,
10-minute dependency outage fail-closed, server kill switch, 4-kind credential rotation với snapshot
verification key giữ theo retention, snapshot/restore/provider-exit, fixed-label telemetry và
cost/quota thresholds. Model này chỉ chứng minh invariant; Map/boolean fixture không thay Neon, B2
hoặc Render. Provider drill, OCI/SBOM, free-quota evidence và operational owner vẫn là blocker.
Two-node/Redis evidence chỉ chuẩn bị cho deferred production HA path.

## Finalist comparison

| Tiêu chí              | tldraw SDK + official sync                                              | Excalidraw + Yjs/Hocuspocus                                                     |
| --------------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Document authority    | Native tldraw record store/sync; boundary tự nhiên hơn                  | Y.Doc phải canonical; scene là projection                                       |
| Undo/history          | Native engine semantics nhưng vẫn phải test concurrent actor-local      | Cần transaction-origin mapping và loại dual internal/Yjs history                |
| React/product fit     | SDK/tool/store APIs mạnh, phù hợp custom classroom shell                | Package embed/API scene/export rõ, editor quen thuộc                            |
| Collaboration         | Official sync cùng engine model                                         | Package không có drop-in collab; TutorHub tự xây adapter/provider               |
| License/owner blocker | Production license/key/cost/telemetry cần owner duyệt                   | Engine MIT; dependency/asset notices và runtime cost vẫn cần review             |
| Runtime blocker       | Exact official sync deploy/managed topology và owner vận hành chưa chốt | Free private-alpha profile đã chọn; disposable provider/risk/on-call chưa duyệt |
| Accessibility         | Có official accessibility surface; vẫn cần NVDA/object evidence         | Canvas/object semantics cần mitigation và physical evidence                     |
| Portability/exit      | Cần versioned neutral export/adapter ngoài provider                     | Yjs/scene export có tiềm năng nhưng mapping phải round-trip lossless            |
| Main production risk  | Commercial/runtime commitment và provider coupling                      | Adapter complexity, dual authority, actor-local undo và operational burden      |

Không dùng chênh lệch nhỏ trong prototype/bundle score để tự động chọn. Security, one-authority,
accessibility hoặc owner approval fail là hard blocker, không phải mục có thể bù bằng weighted score.

## Rejected/held alternatives trong spike

- **Excalidraw demo collaboration relay/Firebase:** reference-only; không phải backend nhúng có
  TutorHub tenant/capability/revoke/rate/persistence contract.
- **Yjs đặt trên tldraw official sync:** tạo dual store/history risk; chỉ được xem lại bằng ADR khác
  nếu upstream topology thay đổi và prototype chứng minh một authority.
- **BlockSuite:** reference cho Y.Doc/actor-local undo; maturity, accessibility và production
  transport/authorization chưa đủ làm finalist.
- **Fastboard/Agora:** managed-provider reference; provider lock-in, editable export và accessibility
  chưa đạt current gate.
- **Custom operation log qua Core API/LiveKit DataChannel:** phá separation, backpressure và one-
  authority invariant; không được triển khai.

## Rollback và provider-exit

Rollback không dual-run hai writer:

1. force-off open/edit/grant mới và revoke current grant generation;
2. giữ exact current generation read-only trong bounded window;
3. tạo/verify immutable last-good snapshot + portable export;
4. nếu phục hồi cùng provider, tạo provider instance/generation mới rồi atomic swap;
5. nếu exit provider/engine, offline migrate verified artifact sang generation mới, chạy round-trip/
   convergence/accessibility gate, sau đó mới đổi current generation;
6. giữ old generation immutable tới retention/incident review; purge chỉ qua maintenance policy.

Exit trigger gồm license/terms không chấp nhận, security/revoke failure, convergence/divergence,
accessibility blocker, không đạt RPO/RTO/performance/cost, provider outage kéo dài hoặc không còn
portable export. Owner, thời gian migrate và supported read-only window phải được điền khi ADR Accepted.

## Acceptance để đổi status

- [x] Owner chọn đúng một engine target: Excalidraw + self-managed collaboration topology.
- [x] Exact Excalidraw production pin, MIT/dependency/asset notices, bundle config và React peer
      compatibility PASS mà không allowlist che upstream config.
- [x] Exact scene <-> canonical authority mapping cùng one-authority proof được ghi; không dual
      Excalidraw/provider history hoặc undo.
- [x] Two-browser Excalidraw convergence, concurrent actor-local undo và offline/reconnect PASS.
- [x] Immutable Excalidraw/provider snapshot, durable restart, corrupt quarantine và restore generation
      swap PASS trong isolated Gate D fixture; production PostgreSQL/B2/worker vẫn chưa được chấp nhận.
- [x] Portable export/import round-trip không cần provider-native state giữ semantic/final hash.
- [x] Tenant/role/reader/IDOR, one-time <=60s grant, Origin/replay/revoke và abuse caps PASS với
      Excalidraw adapter/provider; generic Yjs và tldraw fixture không thay evidence này.
- [x] 500/2.000 shapes cùng 2/10/50 Excalidraw collaboration profile có số liệu và cleanup-zero.
- [x] Automated Axe/keyboard/200%/forced-colors/reduced-motion PASS trên candidate Excalidraw sạch.
- [x] Semantic canvas companion/fallback từ cùng canonical authority có automated evidence.
- [x] Physical Chrome/Edge + NVDA có owner evidence; Axe/semantic fallback không thay gate này.
- [x] Isolated outage, kill switch, credential/snapshot-key rotation và provider-exit contract PASS.
- [x] Exact disposable Render Free/Neon/B2 cold-start/outage/drain/rotation/backup drill, image/SBOM
      và owner no-HA/free-quota/RPO/RTO/on-call approval PASS.
- [x] ADR cập nhật lựa chọn, consequences, owner, exact cost/runtime và chuyển `Accepted`.

## Consequences

- P5-COLLAB-02 được phép bắt đầu control-plane schema theo topology đã chấp nhận; các task sau vẫn phải
  qua dependency/exit gate riêng.
- `FREE_PRIVATE_ALPHA` chỉ cho development/private alpha: một instance, cold-start/restart gap và hard
  cap `0 USD`; vượt quota/RPO/RTO phải force-off thay vì tự nâng gói.
- Trước public beta/production phải mở lại HA gate cho Render Standard x2 + Redis Cloud Multi-AZ hoặc
  ADR thay thế có evidence tương đương.
- Không migrate shared staging, deploy collaboration plane production hoặc bật whiteboard feature
  trước exact task gate tương ứng; P5-COLLAB-17 tiếp tục là force-off staging checkpoint.
- Nếu không candidate nào đạt hard gate, quyết định đúng là giữ feature off và ghi `BLOCKED`, không
  chọn candidate có weighted score cao hơn để giữ lịch.
