# P5-COLLAB-03 — OpenAPI lifecycle/grant/snapshot/export/restore acceptance

## 1. Mục tiêu

P5-COLLAB-03 đưa control-plane schema của P5-COLLAB-02 thành contract và Core API thực thi được cho:

- tạo/đọc/mở/tạm dừng/tiếp tục/đóng whiteboard theo exact `tenant_id` và media-space authority;
- chiếu capability hiện tại mà browser không được tự khai;
- ranh giới exchange grant ngắn hạn;
- danh sách/tạo snapshot, export, validate portable import và restore generation swap;
- strict JSON, bounded body, Problem Details, generated TypeScript client và privacy headers.

PostgreSQL chỉ giữ control-plane metadata/idempotency receipt/snapshot catalog. Scene, Yjs operation,
awareness, undo/history và provider credential không được lưu hoặc trả về từ API này.

## 2. Boundary với task sau

- Endpoint grant đã có contract/authorization nhưng **fail closed `503`** cho tới khi
  P5-COLLAB-04 inject one-time grant broker thật.
- Endpoint tạo snapshot/export đã có contract/authorization nhưng **fail closed `503`** cho tới khi
  P5-COLLAB-07 inject durable artifact workflow/worker thật.
- Import validation chỉ kiểm manifest bounded; không tự tải object hoặc mutate document.
- Không nối `apps/web`, không deploy collaboration production và không bật whiteboard trước
  P5-COLLAB-17.
- `COLLABORATION_CONTROL_PLANE_ENABLED` là deployment guard độc lập, mặc định `false`; tenant override
  không thể tự mở các route này. Disposable/local chỉ bật rõ ràng khi kiểm tra tiến tới HTTP runtime.

## 3. Gate tự động local

- [x] OpenAPI paths/components strict, generated client đồng bộ và `api:check` PASS.
- [x] Go service/repository/HTTP compile và unit tests PASS.
- [x] Create dùng canonical UUID + opaque provider name; provider/storage coordinate không lộ.
- [x] Capability projection map exact media role host/co-host/TA/attendee; dependency fail-closed chỉ trả
      sau tenant/resource/capability authorization.
- [x] Session, CSRF mutation, expected-tenant assertion và membership fail closed.
- [x] Missing/foreign/inaccessible resource cùng `404 whiteboard_not_found`.
- [x] Response whiteboard có `no-store`, `Pragma: no-cache`, `Referrer-Policy: no-referrer` và
      `X-Content-Type-Options: nosniff`.
- [x] Unknown field/oversized body bị chặn trước service.
- [x] Lifecycle/restore dùng optimistic version, expected generation và bounded idempotency key.
- [x] Idempotent replay trả đúng receipt projection ban đầu dù document đã chuyển tiếp.
- [x] Import pin Excalidraw `0.18.1`, Yjs `13.6.27`, format `1`, SHA-256 và tối đa `64 MiB`.
- [x] Deployment guard mặc định `false`; Core API không khởi tạo control plane khi guard tắt.
- [x] Runner chỉ nạp ba biến allowlist từ file ignored, xác minh owner direct/runtime pooled cùng branch
      và không in credential.

## 4. Neon disposable gate

Tạo branch mới từ `staging`, ví dụ `p5-collab-03-disposable-YYYYMMDD`, giữ cả data và schema. Tạo file
ignored `D:\TutorHub_V2\.env.p5-collab-03-disposable.local`:

```dotenv
DATABASE_MIGRATION_URL=<neondb_owner direct/unpooled URL của đúng disposable branch>
DATABASE_POOL_URL=<tutorhub_runtime pooled URL của cùng disposable branch>
P5_COLLAB_03_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_03_DISPOSABLE_ONLY
```

Không đưa giá trị URL vào chat/log/commit. Gate chỉ seed fixture tenant cô lập, chạy runtime repository
rồi cleanup; không rollback migration và không đụng shared staging:

```powershell
pnpm.cmd run test:integration:collaboration:p503
```

Lệnh trên tự nạp file trong **cùng process** trước khi chạy Go test; không cần phụ thuộc biến PowerShell
đã export ở process khác. Có thể truyền đường dẫn khác làm đối số duy nhất nếu cần, nhưng không đưa URL
trực tiếp vào command line.

Điều kiện PASS:

- [x] owner và runtime URL đều kết nối đúng cùng disposable branch ở ledger `37 false`;
- [x] runtime create/read/capability projection PASS exact column ACL;
- [x] open/suspend CAS, stale conflict và changed-payload idempotency conflict PASS;
- [x] replay lifecycle trả đúng historical receipt projection;
- [x] snapshot metadata không lộ B2 coordinates;
- [x] restore tăng generation/revoke/version nguyên tử và replay ổn định;
- [x] foreign tenant trả `ErrNotFound` và fixture cleanup hoàn tất.

Evidence 2026-08-21: runner preflight xác nhận owner direct/runtime pooled cùng disposable database,
ledger `37 dirty=false`; PostgreSQL integration gate PASS sau `15.167s`. Runtime create dùng database
timestamp/default đúng exact ACL; lifecycle, receipt replay, snapshot projection, restore generation swap,
tenant concealment và cleanup đều PASS. Không rollback, không chạm shared staging và không log secret.

## 5. Candidate/CI closure

- [x] Full `pnpm verify` PASS ngày 2026-08-21: format, generated OpenAPI client, security, lint,
      typecheck, web/package tests/build, Storybook, bundle guard, toàn bộ Go test và `go vet`.
- [x] Review diff/no-secret PASS.
- [x] Commit candidate `647ffe4` trực tiếp `main` và push sau khi owner cho phép exact SHA.
- [x] GitHub Verify `32461523646` và Security `32461523627` PASS exact SHA `647ffe4`.
- [x] P5-COLLAB-03 chuyển `VERIFY -> DONE`; shared staging/database migration mới không cần thiết vì
      task này không thay đổi schema `000037`.

Evidence 2026-08-21: Verify PASS Quality/integration, Browser E2E và local-environment smoke.
Security PASS secret scan, repository/container/whiteboard OCI vulnerability scan, SBOM và CodeQL;
không phát hiện leak. Dependency review được workflow bỏ qua đúng điều kiện push. Production
whiteboard vẫn force-off, không deploy và không thay đổi shared staging.

## 6. Rollback và vận hành

- Rollback ứng dụng: redeploy commit trước; schema vẫn giữ `37 false`.
- Không rollback migration `000037`.
- Grant/artifact dependency thiếu phải giữ fail-closed, không dùng mock credential hoặc local-only
  artifact trong staging/production.
- Production whiteboard tiếp tục force-off tới P5-COLLAB-17 exact staging acceptance.
