# TutorHub whiteboard collaboration runtime

OCI candidate cho P5-COLLAB-01 Gate F. Service chạy Hocuspocus/Yjs trên một process HTTP + WebSocket,
đọc/ghi current-generation checkpoint bằng Neon và ghi portable immutable snapshot vào Backblaze B2.
Core API vẫn là control authority duy nhất cho tenant, lifecycle, generation và one-time grant.

## Ranh giới hiện tại

- Profile duy nhất: `FREE_PRIVATE_ALPHA`, đúng một instance, không Redis và không HA.
- `/livez` chỉ chứng minh process còn sống; `/readyz` chỉ xanh khi control plane và Neon sẵn sàng.
- `/metrics` cần bearer token riêng và chỉ xuất label có cardinality hữu hạn.
- Không dùng `DATABASE_POOL_URL`, `DATABASE_MIGRATION_URL`, B2 admin key hoặc local filesystem làm
  durable authority.
- Runtime không chứa React, Excalidraw hoặc tldraw. Browser editor được build/deploy riêng.
- Production whiteboard vẫn force-off. Đây chưa phải quyền deploy shared staging hoặc production.

## Điều kiện tích hợp còn thiếu

Candidate cố ý fail closed cho tới khi các slice sau cung cấp contract thật:

1. P5-COLLAB-02 tạo migration và least-privilege ACL cho
   `public.collaboration_document_checkpoints`.
2. P5-COLLAB-04 cung cấp exact server-to-server grant exchange và runtime-state endpoints.
3. P5-COLLAB-07 nối lịch snapshot/restore với `B2SnapshotStore`; B2 đã có adapter và readiness probe
   nhưng không tự chạy background snapshot trong Gate F.1.

## Kiểm tra cục bộ

```powershell
pnpm --filter @tutorhub/whiteboard-runtime typecheck
pnpm --filter @tutorhub/whiteboard-runtime lint
pnpm --filter @tutorhub/whiteboard-runtime test
pnpm --filter @tutorhub/whiteboard-runtime build
node scripts/check-whiteboard-runtime-oci.mjs
```

Tên biến cấu hình và placeholder nằm trong `.env.example`. Không nạp toàn bộ environment của Core API
vào runtime vì profile này từ chối `REDIS_URL`; chỉ truyền allowlist biến collaboration, B2 và
`DATABASE_COLLABORATION_URL`. Giá trị secret chỉ đặt trong secret store của môi trường chạy.

## OCI và Render disposable

- Docker context: repository root.
- Dockerfile: `services/whiteboard-runtime/Dockerfile`.
- Region/plan/replica: Singapore / Free / `1`.
- Health check: `/readyz`; port do Render truyền qua `PORT`.
- Auto-deploy: tắt cho tới khi exact image/SBOM/vulnerability gate và quyền disposable được duyệt.
- Image chạy user `node`, chỉ chứa `dist`, package manifest và bốn dependency production exact-pin.

Không deploy nếu `/readyz` đỏ. Không dùng `/livez` làm readiness hoặc bỏ qua control-plane/Neon
preflight để ép service lên trạng thái ready.
