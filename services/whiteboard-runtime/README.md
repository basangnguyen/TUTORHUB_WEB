# TutorHub whiteboard collaboration runtime

Local data-plane candidate của P5-COLLAB-05. Service chạy Hocuspocus `4.6.0` và Yjs `13.6.27` trên
một process HTTP + WebSocket riêng; Core API chỉ giữ control authority cho tenant, lifecycle,
generation và one-time grant, không vận chuyển document update.

## Ranh giới và policy

- Profile duy nhất: `FREE_PRIVATE_ALPHA`, đúng một instance, không Redis và không HA.
- WebSocket phải đổi one-time grant qua control plane và bind exact Origin, document, actor,
  capability, generation cùng writer fence. `view` bị ép read-only ở server.
- Awareness chỉ là dữ liệu ephemeral: tối đa một state mỗi connection, được giới hạn size/rate/depth,
  gỡ trường authority do client gửi và thay actor bằng scope đã xác thực; không ghi checkpoint.
- Frame/update/document, message/byte rate, reconnect và quota connection global/tenant/document/actor
  đều fail closed. Postgres session advisory lock bảo đảm profile một instance không tạo authority thứ hai.
- `/livez` chỉ chứng minh process sống; `/readyz` chỉ xanh khi singleton guard, control plane và Neon
  sẵn sàng; `/metrics` cần bearer token riêng và chỉ có label hữu hạn; SIGTERM chạy bounded drain/checkpoint.
- Không dùng `DATABASE_POOL_URL`, `DATABASE_MIGRATION_URL`, B2 admin key, Core API REST hoặc LiveKit
  DataChannel làm document authority/transport. Runtime không chứa React, Excalidraw hoặc tldraw.
- Production whiteboard vẫn force-off tới P5-COLLAB-17. Candidate này chưa cấp quyền shared-staging
  migration, provider deploy hoặc production enable.

## Giới hạn mặc định

| Policy                                        |                                       Mặc định |
| --------------------------------------------- | ---------------------------------------------: |
| Connection global / tenant / document / actor |                           `100 / 100 / 50 / 3` |
| Reconnect mỗi actor trong 10 giây             |                                            `8` |
| Frame / Yjs update / full document            |                     `1 MiB / 768 KiB / 10 MiB` |
| Ingress mỗi socket                            |                      `60 message/s`, `4 MiB/s` |
| Awareness                                     | `1 state`, `16 KiB`, depth `8`, `30 message/s` |
| Drain timeout                                 |                                      `45 giây` |

Mọi override phải nằm trong hard bound của `src/config.ts`; cấu hình mâu thuẫn làm process từ chối
khởi động.

Hocuspocus giữ unauthenticated queue `16 KiB` như defense-in-depth bên trong raw frame gate `1 MiB`.
Patch workspace xóa awareness scratch metadata sau khi re-encode và dùng log lỗi cố định, không đưa
socket/document/token hoặc raw exception vào application log.

## Boundary còn lại

P5-COLLAB-02 đã tạo control-plane schema/catalog và P5-COLLAB-04 đã cung cấp exact grant exchange/
authority lease. Các slice sau vẫn tách biệt:

1. P5-COLLAB-06 nối lazy classroom tool shell/editor với data plane.
2. P5-COLLAB-07 xây durable snapshot/import/export/restore worker và B2 schedule. Checkpoint relation
   hiện tại chỉ là fixture của disposable Gate F, không phải production migration hay second authority.
3. P5-COLLAB-08 đóng reconnect/compaction/recovery end-to-end.
4. P5-COLLAB-09/11 đóng shared quota/operations và abuse/security trước bất kỳ topology nhiều instance.

## Kiểm tra cục bộ

```powershell
pnpm --filter @tutorhub/whiteboard-runtime typecheck
pnpm --filter @tutorhub/whiteboard-runtime lint
pnpm --filter @tutorhub/whiteboard-runtime test
pnpm --filter @tutorhub/whiteboard-runtime build
node scripts/check-whiteboard-runtime-oci.mjs
```

Tên biến cấu hình và placeholder nằm trong `.env.example`. Không nạp toàn bộ environment Core API
vào runtime vì profile này từ chối `REDIS_URL`; chỉ truyền allowlist biến collaboration, B2 và
`DATABASE_COLLABORATION_URL` direct/non-pooler. Secret chỉ đặt trong secret store của môi trường chạy.

## OCI và Render disposable

- Docker context: repository root.
- Dockerfile: `services/whiteboard-runtime/Dockerfile`.
- Region/plan/replica: Singapore / Free / `1`.
- Container/Render liveness health check: `/livez`; port do Render truyền qua `PORT`.
- Dependency readiness vẫn ở `/readyz` và phải trả `503` khi control plane hoặc Neon unavailable.
- Auto-deploy: tắt cho tới khi exact image/SBOM/vulnerability gate và quyền disposable được duyệt.
- Image dùng official Node `24.15.0-alpine3.23` pin digest, yêu cầu Alpine OpenSSL tối thiểu
  `3.5.7-r0`, chạy user `node`, loại package manager khỏi final layer và chỉ chứa `dist`, package
  manifest cùng bốn dependency production exact-pin.

Không deploy nếu preflight `/readyz` đỏ. `/livez` chỉ ngăn Render restart-loop trong dependency
outage; không được coi là readiness hoặc bỏ qua control-plane/Neon/authority-guard preflight. Runbook
và owner escalation: [`../../docs/P5_COLLAB_01_RUNTIME_OPERATIONS.md`](../../docs/P5_COLLAB_01_RUNTIME_OPERATIONS.md).
