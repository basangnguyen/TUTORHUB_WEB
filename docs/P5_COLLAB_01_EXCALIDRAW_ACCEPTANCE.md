# P5-COLLAB-01 — Excalidraw decision và automated acceptance

> **Ngày mở forward gate:** 2026-08-18; Gate A checkpoint 2026-08-19; closed 2026-08-20
> **Trạng thái:** `DONE`
> **Owner decision:** Excalidraw là whiteboard engine chính thức của TutorHub; collaboration data
> plane phải self-managed và có đúng một document/history/undo authority.
> **Phạm vi:** isolated editor tại `apps/whiteboard-spike`, OCI source candidate tại
> `services/whiteboard-runtime` và disposable Render/Neon/B2 acceptance; không nối `apps/web`, Core API
> production, migration hoặc shared staging.

## 1. Quyết định và ranh giới

- Dùng `@excalidraw/excalidraw` làm editor/view projection mang thương hiệu TutorHub Whiteboard.
- MIT là license target của engine; exact production artifact vẫn phải có dependency/asset notice
  audit và giữ copyright/license notice.
- Không dùng Excalidraw demo Firebase/collaboration relay làm production backend.
- Yjs `13.6.27` là canonical document/history authority và Hocuspocus `4.6.0` là transport/provider
  được chấp nhận cho development/private alpha sau khi scene adapter, actor-local undo, durable
  persistence và operations đạt gate.
- Không đưa tldraw SDK, `@tldraw/sync` hoặc `@tldraw/sync-core` vào production candidate. Prototype
  tldraw đã PASS được giữ nguyên làm comparison/provider-exit evidence.
- Không chạy hai writer hoặc dual-write. Nếu Y.Doc được chọn, nó là canonical document/history;
  Excalidraw scene chỉ là projection và local history không được cạnh tranh với provider history.

## 2. Chuỗi evidence

- Excalidraw `0.18.1` đã PASS isolated render/snapshot-envelope smoke, 500/2.000-object fixture,
  lazy build và automated shell accessibility trong P5-COLLAB-00.
- Exact Excalidraw scene <-> `Y.Doc` `13.6.27` canonical mapping với Hocuspocus `4.6.0` transport đã
  PASS Gate B: two-browser convergence, offline/reconnect và actor-local undo/redo.
- Exact candidate authorization đã PASS Gate C cho one-time grant, capability/view-only, generation/
  revoke và abuse boundary. Gate D đã PASS durable filesystem restart, immutable snapshot/quarantine,
  generation swap và provider-neutral portable round-trip. Gate E automated profile 2/10/50,
  semantic fallback, accessibility automation, installed Chrome/Edge matrix và owner-confirmed NVDA
  speech đã PASS. Gate F isolated runtime/operations contract đã PASS; owner đã chọn Render Free
  Singapore một instance, không Redis, cho development/private alpha. Gate F.1 đã biến harness thành
  OCI source candidate thật với Neon/B2 adapter và lifecycle fail-closed. Actual image/SBOM scan,
  disposable Render Free/Neon/B2 drill và operational owner acceptance vẫn chưa đạt.
- Research bundle từng bị guard chặn vì upstream public Google API/Firebase/collaboration config và
  Radix Tabs 1.0.2 chưa công bố React 19 compatibility. Gate A đã đóng các finding này trong exact
  candidate; các gate authority/runtime bên dưới vẫn chưa được suy diễn là PASS.
- Tldraw automated acceptance không được dùng để đánh dấu các mục Excalidraw bên dưới là PASS.

### Gate A checkpoint — 2026-08-19

Đã tạo exact candidate riêng tại `apps/whiteboard-spike/excalidraw.html` với output
`dist-excalidraw`; artifact không nối `apps/web`, Core API, database, shared staging hoặc deploy.

**PASS có bằng chứng:**

- Manifest và installed artifact đều khóa `@excalidraw/excalidraw` `0.18.1`; package engine khai báo
  MIT và peer chính thức bao phủ React/ReactDOM 19. Exact release có security fix Mermaid XSS và
  official ESM integration tại [release v0.18.1](https://github.com/excalidraw/excalidraw/releases/tag/v0.18.1),
  [MIT license](https://github.com/excalidraw/excalidraw/blob/v0.18.1/LICENSE) và
  [0.18.x changelog](https://github.com/excalidraw/excalidraw/blob/master/packages/excalidraw/CHANGELOG.md).
- Exact tag `v0.18.1` được shallow-clone và xác minh tại commit
  `a2ec2889babf7d2295469c6d90ebe77fae57df84`; thư mục kiểm chứng nằm dưới `tmp/` đã ignore.
- `build:excalidraw` PASS với 2.313 module; structural guard kiểm tra 180 asset, đúng một dynamic
  Excalidraw adapter entry, không có tldraw code/identifier và initial entry không static-import engine.
- Exact candidate Playwright PASS `1/1`: initial shell không request engine; sau hành động “Mở bảng
  Excalidraw” mới request chunk, render fixture 500 và mở Radix-backed sidebar/tablist không có
  browser/runtime error.
- Lint, TypeScript project/e2e typecheck và unit `14/14` PASS. Không secret value nào được log trong
  các guard.
- Production dependency audit `pnpm audit --prod --audit-level high` PASS, không có known
  vulnerability tại thời điểm checkpoint.

**Ba blocker Gate A đã đóng, không peer override/bundle allowlist:**

- Candidate alias trực tiếp sang exact `@radix-ui/react-tabs` `1.1.21`; manifest bản này khai báo peer
  React/ReactDOM 19 và E2E thực thi Sidebar/Tablist PASS. Không dùng pnpm peer override/package extension.
- Dependency gate audit 31 manifest, chấp nhận `fuzzy@0.1.3` bằng packaged `LICENSE-MIT`, xác minh 9
  font family và buộc ship `THIRD_PARTY_NOTICES.txt` chứa exact provenance/license/font attribution.
- Build-only sanitizer chỉ tác động Excalidraw release chunk, khóa số finding đã kiểm chứng và fail
  closed khi upstream drift. Strict final scanner PASS 178 text asset: không còn Google API-key pattern,
  demo Firebase host/config hoặc `excalidraw-room`; không có allowlist.
- Candidate graph hiện sinh 180 asset và có lazy chunk lớn; size/performance budget sẽ được xử lý ở
  Gate E, không được suy diễn từ structural PASS của Gate A.

## 3. Exit gate để ADR-0034 có thể `Accepted`

### A. License, dependency và candidate bundle

- [x] Owner chọn Excalidraw làm engine chính thức và chấp thuận self-managed collaboration direction.
- [x] Exact production pin được khóa từ release/source chính thức; MIT, font, icon, shape library và
      transitive dependency notices được audit.
- [x] React/ReactDOM 19.2.7 peer compatibility PASS mà không dùng override để che incompatibility.
- [x] Excalidraw-only production bundle guard PASS: không secret, credential hoặc demo Firebase/
      collaboration endpoint/config ngoài explicit reviewed allowlist có lý do.
- [x] Engine/tool được lazy-load và không nằm trong initial classroom bundle/candidate shell; exact
      artifact và browser request gate PASS, production route vẫn force-off.

### B. Một document/history/undo authority

- [x] Exact canonical model/provider được chốt bằng ADR: một authority cho mỗi tenant/document/
      generation; không dual Excalidraw-local/provider history.
- [x] Scene <-> canonical model mapping round-trip lossless cho shape/text/binding/image/page subset
      được hỗ trợ, có schema version và deterministic semantic hash.
- [x] Hai browser concurrent edit hội tụ; actor-local undo/redo không xóa remote actor change.
- [x] Offline hai chiều, duplicate/out-of-order update, reconnect/resync và compaction hội tụ.
- [x] Unsupported/corrupt/oversized scene hoặc update fail closed với bounded error taxonomy.

### Gate B checkpoint — 2026-08-19

`CanonicalExcalidrawAuthority` khóa `Y.Doc` `13.6.27` làm một canonical document/history authority
theo exact `{tenantId, documentId, generation}`; Hocuspocus `4.6.0` là transport/provider candidate,
không phải history thứ hai. Scene v1 dùng actor/element revision register + CRDT z-order + page/file
maps, actor-scoped transaction origin và `Y.UndoManager`. Unit/provider suite PASS `20/20`; lint và
TypeScript PASS.

Round-trip shape/text/binding/image/page, semantic hash, concurrent same-element conflict,
actor-local undo/redo, two-way offline, duplicate/out-of-order, compact restore và fail-closed error
taxonomy đều có evidence. Browser E2E PASS `2/2` với hai Excalidraw instance thật: concurrent edit,
equal canonical hash/render count, Teacher undo giữ Student change và redo hội tụ. Projection
feedback-loop bị suppress, internal history bị clear, Ctrl/Cmd+Z đi qua canonical undo và upstream
fonts được self-host. Structure/security guard tiếp tục PASS với 182 asset/180 text asset. Gate B
`5/5` `DONE`. Contract/evidence chi tiết:
[P5_COLLAB_01_CANONICAL_AUTHORITY.md](P5_COLLAB_01_CANONICAL_AUTHORITY.md).

### C. Authorization và abuse boundary

- [x] One-time grant TTL <=60 giây, exact Origin/tenant/document/generation/actor/capability binding,
      replay denial và memory-only/no-store PASS.
- [x] Data plane enforce `view` receive-only; forged role, cross-tenant/document và stale generation
      bị deny dù client gọi protocol trực tiếp.
- [x] Revoke/close/restore tăng generation, chặn refresh và đóng socket trong failure budget.
- [x] Frame/update/object/depth/rate/connection/tenant quota và reconnect storm fail closed.

### Gate C checkpoint — 2026-08-19

Excalidraw candidate có control/data-plane authorization fixture riêng: opaque one-time grant mặc định
30 giây và tối đa 60 giây, exact Origin/session/tenant/document/generation/actor/capability binding,
hashed memory-only store, no-store/no-referrer response và server-selected opaque provider document.
Hocuspocus data plane enforce `view` receive-only kể cả direct protocol write; forged capability,
cross-tenant/document, replay, stale generation và authorization/rate authority outage đều fail closed.

`revoke`, `close`, `restore` tăng generation và đóng socket trong budget 1.000 ms. Candidate khóa HTTP/
frame/update/awareness/depth/rate cùng connection actor/document/tenant, grant issue và reconnect-storm
budget. Authorization integration tests PASS `7/7`; full unit/integration PASS `28/28`; Playwright
Excalidraw A/B/C PASS `3/3`; Gate B concurrency/undo stress sau race fix PASS `5/5`. Lint, TypeScript,
build, exact dependency/license và bundle security guard đều PASS. Gate C `4/4 DONE`; đây vẫn là fixture
cô lập, không thay Core API/PostgreSQL production. Chi tiết:
[P5_COLLAB_01_AUTHORIZATION.md](P5_COLLAB_01_AUTHORIZATION.md).

### D. Persistence, snapshot và provider exit

- [x] Durable restart recovery, crash giữa update/snapshot và single-authority ownership PASS.
- [x] Immutable snapshot envelope có engine/schema/provider version, checksum, causal watermark,
      byte/object cap và opaque B2 binding.
- [x] Corrupt restore bị quarantine; restore tạo generation mới và stale writer bị deny.
- [x] Portable export/import/provider-exit round-trip giữ supported semantic hash; không phụ thuộc
      duy nhất vào provider-native binary.

Gate D `4/4 DONE`: filesystem fixture chứng minh catalog chỉ publish sau atomic write + checksum,
last-good recovery sau restart/crash, HMAC scope binding với opaque object key, corrupt quarantine,
single restore owner/generation swap và stale-grant denial. Portable canonical JSON không chứa Yjs
provider state nhưng round-trip sang authority mới vẫn giữ semantic hash. Full unit/integration
`34/34`, lint, typecheck, build, dependency/license và bundle structure/security đều PASS. Đây là
contract fixture cô lập, chưa thay PostgreSQL/B2/durable worker production. Chi tiết:
[P5_COLLAB_01_SNAPSHOT_RECOVERY.md](P5_COLLAB_01_SNAPSHOT_RECOVERY.md).

### E. Performance và accessibility

- [x] Excalidraw collaboration profile 2/10/50 với 500/500/2.000 object đạt convergence, latency,
      memory và cleanup-zero budget được công bố.
- [x] Automated Axe, keyboard, focus handoff, 200% zoom, forced colors và reduced motion PASS trên
      exact candidate bundle.
- [x] Physical Windows Chrome/Edge + NVDA PASS cho toolbar, mode, selection/action, reconnect/error
      và focus recovery.
- [x] Semantic companion/fallback cho nội dung canvas được công bố và kiểm thử.

Gate E automated `3/3 PASS`: exact authorization/provider profile đạt 2 × 500, 10 × 500 và
50 × 2.000 với deterministic hash, measured join/convergence/CPU/heap/network và cleanup về 0.
Candidate thêm semantic companion phân trang 50 phần tử từ cùng canonical authority, keyboard
open/close và deterministic focus handoff. Playwright tổng A–E PASS `6/6`; Axe không allowlist,
200%-equivalent reflow, forced colors và reduced motion đều xanh. Installed Chrome
`151.0.7922.140` và Edge `151.0.4129.86` đều PASS 11 tool role/name, Axe `0/0`, semantic page 2/10
và cleanup focus. Hai lỗi upstream về mobile main-menu accessible name và nested footer landmark đã
được sửa bằng bounded adapter patch và regression test.

Full run cũng phát hiện projection programmatic có thể bị browser thứ hai echo như local mutation,
làm actor-local undo không lùi đúng semantic contribution. Adapter nay chặn projection echo cho tới
trusted pointer/keyboard interaction và canonical undo/redo bỏ qua projection-only revision; exact
Gate B regression cùng Playwright A–E vẫn PASS. Owner xác nhận NVDA `2026.1.1.55980` đọc đúng
toolbar/mode/semantic fallback và reconnect/error/focus recovery trên Chrome/Edge. Gate E đạt
`4/4 PASS`. Full unit/integration `39/39`, lint, typecheck, build, dependency/license và bundle
structure/security đều PASS. Chi tiết:
[P5_COLLAB_01_LOAD_ACCESSIBILITY.md](P5_COLLAB_01_LOAD_ACCESSIBILITY.md).

### F. Runtime và vận hành

- [x] Exact self-hosted runtime, region, declared single-instance/no-HA profile, persistence, drain/
      recovery, backup, secret rotation, on-call và hard cap `0 USD` được owner chấp thuận.
- [x] Isolated sustained outage, kill switch, credential rotation, backup/restore và provider-exit
      contract drill cùng disposable provider drill PASS.
- [x] Bounded-cardinality metrics/log privacy, global quota, cost/quota alerts và runbook PASS.
- [x] ADR-0034 chuyển `Accepted`; production vẫn force-off cho tới P5-COLLAB-17 exact staging.

### Gate F automated checkpoint — 2026-08-19

Candidate khóa Node `24.15.0`, Hocuspocus `4.6.0`, Yjs `13.6.27`, Excalidraw `0.18.1`. Owner quyết
định profile `FREE_PRIVATE_ALPHA`: một Render Free instance tại Singapore, không Redis, Neon giữ Yjs
binary checkpoint và B2 giữ immutable portable snapshot trong free allowance. Render Standard x2 +
Redis Cloud paid Multi-AZ chỉ là production HA upgrade path đã hoãn. Exact topology, drain/RPO/RTO,
outage/rotation/restore, privacy/metrics/alert và TCO worksheet nằm tại
[P5_COLLAB_01_RUNTIME_OPERATIONS.md](P5_COLLAB_01_RUNTIME_OPERATIONS.md).

`gate:excalidraw-runtime` PASS `18/18`: atomic one-time grant/global quota qua hai node, opaque keyed
document name, one-writer fencing/takeover, checkpoint watermark và drain; sustained 10 phút
control/coordination/persistence outage fail closed; B2/snapshot outage không làm live writer fail;
server-owned read-only/export-only/off; rotation 4 credential kind, snapshot verification key giữ qua
retention rồi mới old-key negative probe; provider-exit/restore; fixed-label telemetry cùng cost 70/90/100 và quota
70/85/100. Full unit/integration `53/53`, lint, typecheck, build, dependency/license và bundle
structure/security cùng Gate A–E regression tiếp tục PASS.

Đây là isolated executable contract, không phải bằng chứng Neon/B2/Render thật. Gate F hiện
`2/4 PASS`, giữ `VERIFY`. Hai blocker còn lại là: (1) exact OCI/SBOM cùng disposable Render Free cold-start,
restart/drain, Neon/B2 outage/rotation/restore drill; (2) owner chấp thuận no-HA/cold-start/free-quota,
điền on-call/retention/RPO/RTO và xác nhận hard cap `0 USD` để ADR-0034 chuyển `Accepted`. Paid
two-node/Redis drill được hoãn tới production HA gate; không phải blocker của free private alpha.
Không provision, không shared staging và không deploy trong checkpoint này.

### Gate F.1 OCI source candidate — 2026-08-19

Đã thêm service độc lập `services/whiteboard-runtime` chạy Hocuspocus/Yjs thật, exact grant/control
exchange, Neon binary checkpoint, B2 immutable portable snapshot, readiness/liveness/metrics và
bounded SIGTERM drain. Dockerfile multi-stage pin Node base digest, chạy non-root và chỉ deploy bốn
dependency production; Security workflow build image, enforce Trivy HIGH/CRITICAL và sinh/kiểm tra
CycloneDX SBOM. Runtime test `9/9`, lint, typecheck, build, OCI static guard và isolated production
package cùng production dependency audit đều PASS, không có known vulnerability. Không đọc
`.env*.local`, không kết nối provider và không deploy.

Gate F vẫn `2/4`: máy hiện tại chưa có Docker/Trivy/Syft nên actual image digest/SBOM/vulnerability
evidence còn mở; P5-COLLAB-02 schema/ACL, P5-COLLAB-04 control endpoints và disposable provider drill
chưa được suy PASS.

### Gate F.3 baseline provider checkpoint — 2026-08-20

Exact candidate `7febbce` đã PASS GitHub Verify `32335427818` và Security `32335427799`, được deploy
`live` trên Render Free Singapore và chạy với Neon/B2 disposable thật. Baseline PASS
`hocuspocus_sync`, Neon `checkpoint_recovery`, B2 `read_after_write`, `cleanup_zero` và cold-start
bucket `lt_5s`; preflight cùng Neon schema/exact ACL cũng PASS mà không log secret. Gate F hiện
`3/4 VERIFY`.

F.3 vẫn còn real SIGTERM/drain + post-redeploy recovery, sustained control/Neon/B2 outage,
credential rotation/restore và owner/provider checklist. Không suy PASS các mục này từ baseline;
ADR-0034 giữ `Proposed`, P5-COLLAB-01 giữ `IN PROGRESS`, shared staging/production vẫn force-off.

### Gate F.3 SIGTERM/drain + post-redeploy recovery — 2026-08-20

Manual deploy exact candidate `7febbce` tạo deploy `dep-da39i0ibkg8c7381o8i0`. Instance cũ ghi
`drain_started`, rồi `drain_complete` với `outcome=ok`, duration bucket `lt_100ms`; instance mới lên
`live` và `/readyz=200`. Provider drill sau redeploy PASS `hocuspocus_sync`, Neon
`checkpoint_recovery`, B2 `read_after_write`, `cleanup_zero` và cold-start bucket `lt_5s`.

Sub-gate SIGTERM/drain + post-redeploy recovery đã đóng. Gate F vẫn `3/4 VERIFY`; còn sustained
control/Neon/B2 outage, credential rotation/restore và owner/provider checklist. ADR-0034 giữ
`Proposed`, P5-COLLAB-01 giữ `IN PROGRESS`, shared staging/production tiếp tục force-off.

### Gate F.3 full provider + owner closure — 2026-08-20

Sustained Control/Neon/B2 outage `600s`, Control/B2 credential rotation, portable semantic restore,
cleanup-zero và post-restore probes đều PASS. Render/Neon/B2 quota evidence được giữ dạng aggregate
redacted và current/projected cost `0 USD`.

Owner chấp thuận Render Free single-instance/no-HA, spin-down/cold-start, hard cap `0 USD`, RPO/RTO
candidate, incident severity và B2 Object Lock disabled cho private alpha. Primary on-call `Bá Sáng`,
backup `Duy Mạnh`, security incident owner `Bá Sáng`, cost owner `Bá Sáng`.

## 4. Kết quả và bước tiếp theo

Gate A-F đều đạt; ADR-0034 chuyển `Accepted` và P5-COLLAB-01 chuyển `DONE`. P5-COLLAB-02 control-plane
schema là task runnable tiếp theo. P5-COLLAB-03..20 vẫn theo dependency graph; shared staging chưa
migrate, collaboration plane production chưa deploy và whiteboard production tiếp tục force-off tới
P5-COLLAB-17 exact staging.
