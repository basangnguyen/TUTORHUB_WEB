# P5-COLLAB-08 - Reconnect, compaction và recovery acceptance

> Trạng thái: `VERIFY` ngày 2026-08-22. ADR-0036, implementation, local/full verify và Neon/B2
> disposable recovery gate đã PASS. Còn commit/push exact candidate cùng GitHub Verify/Security trước
> `DONE`. Không có migration mới, không ghi shared staging, không deploy và production whiteboard tiếp
> tục force-off.

## 1. Candidate đã triển khai

- Yjs state vector là resume watermark; reconnect dùng differential sync và giữ cùng in-memory `Y.Doc`
  trong mất mạng tạm thời.
- Duplicate, out-of-order và offline concurrent update hội tụ; delete set/tombstone được giữ để stale
  update không làm dữ liệu đã xóa sống lại.
- Durable checkpoint được compact copy-on-write trong hai `Y.Doc` cô lập. Runtime chỉ persist khi cả
  state vector và full encoded state của probe khớp exact candidate; live document/undo manager không bị
  thay thế.
- Corrupt, empty, oversized hoặc divergent checkpoint fail closed. Durable checkpoint cũ không bị thay
  nếu validation thất bại.
- Browser phân loại terminal reason một lần: authority changed, grant rejected, reconnect exhausted,
  recovery required hoặc runtime unavailable. Chỉ authority change mới refetch Core API projection;
  cached grant không được dùng lại qua generation/revoke fence.
- Last-good restore tiếp tục dùng P5-COLLAB-07 verified immutable B2 artifact, stage checkpoint generation
  mới rồi atomic swap `current_generation` và tăng `revoke_generation`.

ADR: [ADR-0036](adr/0036-whiteboard-reconnect-compaction-and-generation-recovery.md).

## 2. Local gate đã PASS

```powershell
node --test scripts/run-p508-disposable.test.mjs
pnpm --filter @tutorhub/collaboration-client lint
pnpm --filter @tutorhub/collaboration-client typecheck
pnpm --filter @tutorhub/collaboration-client test
pnpm --filter @tutorhub/whiteboard-runtime lint
pnpm --filter @tutorhub/whiteboard-runtime typecheck
pnpm --filter @tutorhub/whiteboard-runtime test
pnpm --filter @tutorhub/web typecheck
pnpm --filter @tutorhub/web test
go test ./services/core-api/internal/modules/collaboration
git diff --check
```

Kết quả tại checkpoint `VERIFY`: runtime `120` PASS và `2` integration test skip có chủ đích; client
`9/9` PASS; web `456/456` PASS; Go collaboration PASS; runner preflight `2/2` PASS; full `pnpm verify`
PASS khi chạy tuần tự bằng `TURBO_CONCURRENCY=1`.

## 3. Disposable recovery gate — PASS

Gate chạy ngày 2026-08-22 trên tài nguyên Neon/B2 disposable P5-COLLAB-07 được tái sử dụng có kiểm soát:

- preflight PASS tại migration ledger `40 false`;
- corrupt/incompatible artifact quarantine PASS, không publish target checkpoint và không đổi generation;
- last-good verified artifact restore PASS với semantic scene hash giữ nguyên;
- generation swap và stale generation/revoke fence PASS;
- `RPO=last_verified_artifact`, measured `RTO_MS=3096`, thấp hơn objective `300000 ms`;
- cleanup fixture/B2 object PASS; postflight ledger vẫn `40 false`.

Không có credential nào được ghi vào evidence hoặc command output.

Tạo hoặc cập nhật file ignored `.env.p5-collab-08-disposable.local`. Có thể dùng lại branch/bucket
disposable P5-COLLAB-07 nếu vẫn còn và vẫn là tài nguyên cô lập; không dùng shared staging hoặc bucket
production.

```dotenv
DATABASE_MIGRATION_URL=
DATABASE_POOL_URL=
DATABASE_COLLABORATION_URL=
DATABASE_POLL_MAINTENANCE_URL=
B2_ENDPOINT=
B2_REGION=
B2_BUCKET=
B2_KEY_ID=
B2_APPLICATION_KEY=
P5_COLLAB_08_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_08_DISPOSABLE_ONLY
```

Runner chỉ allowlist các giá trị cần thiết trong child process và không in credential:

```powershell
node scripts/run-p508-disposable.mjs .env.p5-collab-08-disposable.local preflight
node scripts/run-p508-disposable.mjs .env.p5-collab-08-disposable.local recovery
```

Preflight bắt buộc bốn role khác nhau, cùng exact Neon branch/database, owner/worker/maintenance dùng
direct endpoint, Core API dùng pooled endpoint, B2 HTTPS credential-free endpoint và migration ledger
`40 false`. Runner không migrate và postflight phải vẫn là `40 false`.

Recovery gate phải chứng minh:

1. corrupt/incompatible artifact bị quarantine, generation hiện tại không đổi và target checkpoint
   không được publish;
2. last-good verified artifact stage thành checkpoint generation mới với cùng semantic scene hash;
3. authority swap đúng một generation, stale generation/revoke fence không thể tiếp tục ghi;
4. RPO là last verified durable artifact và measured RTO nhỏ hơn private-alpha objective `300.000 ms`;
5. mọi fixture row và exact B2 object version do gate tạo được cleanup.

## 4. Điều kiện chuyển DONE

- [x] Disposable recovery gate PASS và lưu kết quả RPO/RTO không chứa secret.
- [x] `pnpm verify`, review diff/no-secret và `git diff --check` PASS.
- Commit/push exact candidate lên `main`; GitHub Verify và Security PASS.
- Cập nhật backlog/project state sang `DONE`.

Không xóa disposable branch/bucket/key trước khi lưu đủ evidence. Không shared-staging write hoặc deploy
trước báo cáo disposable PASS và xin quyền riêng.
