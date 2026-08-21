# P5-COLLAB-05 - Collaboration data plane/provider adapter acceptance

> Trạng thái: `DONE` cho local candidate ngày 2026-08-22. Tên tài liệu giữ convention staging,
> nhưng task này không migrate shared staging, không deploy Render và không bật whiteboard production.

## 1. Kết quả

Runtime Hocuspocus/Yjs riêng đã được nối với one-time grant/authority lease của P5-COLLAB-04 và giữ
đúng một document authority. Candidate cung cấp authenticated WebSocket, server-enforced read-only,
awareness ephemeral đã sanitize, bounded ingress, quota theo tenant, health/readiness/metrics và
graceful drain. Profile `FREE_PRIVATE_ALPHA` được khóa một instance bằng PostgreSQL session advisory
lock; duplicate runtime không thể trở thành authority thứ hai.

Production whiteboard tiếp tục force-off tới P5-COLLAB-17.

## 2. Authority và transport boundary

- Document update đi trực tiếp qua Hocuspocus WebSocket; Core API REST chỉ exchange/revalidate opaque
  grant và không nhận scene/Yjs update.
- LiveKit DataChannel không nằm trong dependency graph hoặc document path của runtime.
- Mỗi connection bind exact Origin, provider document name, tenant, actor, capability, generation,
  authority lease và writer fence do control plane trả về.
- `view` bị đặt `readOnly` trước khi sync; write khi runtime read-only/off, authority mất hoặc drain
  đều bị deny/đóng fail closed.
- Awareness không đi vào `Y.Doc` checkpoint. Client không được tự khai actor/capability/generation;
  runtime thay các trường này bằng scope đã xác thực và chỉ chấp nhận một state mỗi connection frame.

## 3. Bounded data-plane policy

| Boundary                         |                                        Default | Hard bound/behavior                        |
| -------------------------------- | ---------------------------------------------: | ------------------------------------------ |
| Connection global                |                                          `100` | tối đa `100`                               |
| Connection tenant/document/actor |                                 `100 / 50 / 3` | quota độc lập, release idempotent          |
| Reconnect actor                  |                                  `8 / 10 giây` | sliding window theo tenant + actor         |
| Frame                            |                                        `1 MiB` | WebSocket `maxPayload` và pre-handle guard |
| Yjs update                       |                                      `768 KiB` | bounded trước khi apply                     |
| Full document                    |                                       `10 MiB` | conservative encoded-state budget          |
| Ingress socket                   |                      `60 message/s`, `4 MiB/s` | rolling message/byte budget                |
| Awareness                        | `1 state`, `16 KiB`, depth `8`, `30 message/s` | JSON-only, sanitized, ephemeral            |

Mọi policy denial chỉ xuất bounded reason code/metric. Tenant, actor, document, grant, state content và
raw payload không trở thành metric label hoặc application log field.

Hocuspocus còn giữ unauthenticated queue `16 KiB` như lớp defense-in-depth bên trong raw WebSocket
gate `1 MiB`. Patch local xóa awareness scratch metadata sau khi re-encode và thay log lỗi nội bộ bằng
thông điệp cố định, nên inner rejection không làm lộ socket/document/token hay raw exception.

## 4. Split-brain và lifecycle

`PostgresProviderAuthorityGuard` giữ advisory lock hai khóa trên một dedicated direct PostgreSQL
session. Startup thứ hai nhận `provider_authority_duplicate` trước khi listen. Probe kiểm tra cùng
backend PID và exact granted lock; mất session/lock làm readiness đỏ và đóng active authority. Guard
được release idempotent khi startup lỗi hoặc drain hoàn tất.

Drain thực hiện theo thứ tự: chuyển `draining`, dừng admission/timer, flush pending stores, destroy
server trong deadline, xác nhận không còn dirty document, đóng checkpoint store rồi release singleton
lock. Authentication hoàn tất sau khi drain bắt đầu vẫn bị deny.

## 5. Health, telemetry và operations

- `/livez`: process liveness, không đại diện dependency readiness.
- `/readyz`: `200` chỉ khi singleton guard, control plane và checkpoint persistence đều sẵn sàng,
  runtime mode không `off`, không có latched checkpoint failure; ngược lại trả `503` bounded reason.
- `/metrics`: bearer riêng, fixed-cardinality build/dependency/connection/checkpoint/policy/drain metrics.
- Scale: giữ đúng `COLLAB_INSTANCE_COUNT=1`. Không tăng replica trước shared atomic coordination ADR;
  nếu vô tình khởi động replica thứ hai, advisory guard phải chặn nó.
- Secret rotation: thay secret trong provider secret store, redeploy đúng một instance và kiểm tra
  `/livez`, `/readyz`, authenticated exchange cùng `/metrics`; private alpha chấp nhận gián đoạn ngắn.
- Outage: control/Neon/authority-guard outage giữ `/livez=200`, `/readyz=503` và fail closed; owner chỉ
  mở lại admission sau probe xanh. B2 snapshot outage thuộc P5-COLLAB-07, không được biến document
  transport thành REST fallback.
- Runbook chính: [P5_COLLAB_01_RUNTIME_OPERATIONS.md](P5_COLLAB_01_RUNTIME_OPERATIONS.md).
- Owner: primary on-call `Bá Sáng`; backup `Duy Mạnh`; security incident và cost owner `Bá Sáng`.

## 6. Verification

Các gate local của exact candidate:

```powershell
pnpm --filter @tutorhub/whiteboard-runtime typecheck
pnpm --filter @tutorhub/whiteboard-runtime lint
pnpm --filter @tutorhub/whiteboard-runtime test -- --maxWorkers=1
pnpm --filter @tutorhub/whiteboard-runtime build
node scripts/check-whiteboard-runtime-oci.mjs
pnpm verify
git diff --check -- . ':(exclude)patches/*.patch'
```

Runtime package PASS `100/100` test trên 12 file. Coverage bao gồm connection/document recovery,
readiness/off/revoke, cleanup-zero, authenticated ephemeral awareness, duplicate-awareness-client
ownership, per-tenant quota/release, authorization-to-load/drain race, immediate stale-guard
readiness, policy sliding windows/nesting/size và singleton acquire/duplicate/session-loss/release.
TypeScript, ESLint, build, OCI dependency boundary và full repository verify đều PASS trên candidate
cuối trước commit. Source diff sạch whitespace; artifact do `pnpm patch-commit` sinh được loại khỏi
whitespace-only check và được kiểm integrity qua frozen install cùng OCI static gate.

## 7. Deferred, không suy rộng

- Không có migration mới. `public.collaboration_document_checkpoints` hiện chỉ là disposable Gate F
  fixture, không phải production relation; durable snapshot/import/export/restore worker thuộc P5-COLLAB-07.
- Chưa nối classroom UI/editor; P5-COLLAB-06 sở hữu lazy shell và browser integration.
- Chưa tuyên bố reconnect/compaction recovery end-to-end (P5-COLLAB-08), distributed quota/HA
  (P5-COLLAB-09), full abuse closure (P5-COLLAB-11) hoặc production rollout (P5-COLLAB-17).
- Advisory-lock gate của task này dùng hai PostgreSQL client/session giả lập độc lập để kiểm chứng
  acquire/duplicate/session-loss/release. Không có live Neon disposable drill mới trong P5-COLLAB-05;
  provider/disposable outage và recovery evidence thuộc các rollout/failure gate sau.
- Không chạy shared Neon, không thay Render service, không rotate external secret và không deploy
  trong task này.
