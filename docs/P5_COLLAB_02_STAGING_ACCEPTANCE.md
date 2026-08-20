# P5-COLLAB-02 staging acceptance — whiteboard control-plane schema

## 1. Trạng thái

`VERIFY` — migration `000037`, exact ACL harness, PostgreSQL concurrency/tenant/authority gates và
CI wiring đã có trong source. Local/full verify và Neon disposable forward/idempotency/exact ACL/
concurrency/tenant/authority gates PASS tại final ledger `37 dirty=false`. Exact candidate GitHub
Verify/Security và shared staging acceptance còn mở; production chưa được migrate hoặc deploy.

## 2. Safety boundary

- Không đọc, in, echo hoặc log giá trị trong `.env*.local`.
- Chỉ dùng Neon disposable branch tách từ `staging`; không dùng `production` hoặc shared `staging`.
- Chỉ forward `36 false -> 37 false`; không rollback `000037` trên shared staging/production.
- Không xóa disposable branch trước khi toàn bộ gate database PASS.
- Không forward shared staging, deploy hoặc bật whiteboard production trong P5-COLLAB-02.
- Ba URL phải trỏ cùng một disposable database nhưng dùng ba principal riêng: migration owner,
  Core API runtime và maintenance.

## 3. Schema/authority contract

Migration `000037_whiteboard_control_plane` tạo năm relation tenant-scoped:

- `whiteboard_documents`: source binding một-một với `media_spaces`, lifecycle, optimistic version,
  current generation và revoke generation;
- `whiteboard_document_generations`: immutable Yjs/Hocuspocus generation descriptor;
- `whiteboard_capability_policies`: projection `view/edit/present` theo audience hữu hạn;
- `whiteboard_snapshots`: immutable B2 catalog, hash/watermark, provenance và retention 14 ngày;
- `whiteboard_document_mutation_receipts`: bounded business idempotency receipt.

PostgreSQL không lưu Excalidraw scene, Yjs update, awareness, undo/redo hoặc operation history.
Y.Doc/Hocuspocus vẫn là canonical live document/history authority theo ADR-0034.

## 4. Exact ACL matrix

Core API runtime:

- schema `tutorhub`: `USAGE`, không `CREATE`;
- năm relation mới: table-level `SELECT`, không table-level `INSERT/UPDATE/DELETE`;
- chỉ `INSERT` đúng các cột command metadata cần thiết;
- chỉ `UPDATE` lifecycle/version/generation/actor/timestamp của `whiteboard_documents` và
  capability/version/updater/timestamp của `whiteboard_capability_policies`;
- generation, snapshot và mutation receipt không có đường `UPDATE/DELETE`.

Maintenance:

- schema `USAGE`, không `CREATE`;
- không có DML trên năm relation P5-COLLAB-02.

Purge retention sẽ được thiết kế cùng worker/function bounded tại P5-COLLAB-07; P5-COLLAB-02 không
pre-grant quyền maintenance chưa dùng. `PUBLIC` không có DML trên năm relation.

## 5. Local/CI gates

- [x] Static schema guard: tenant composite FK, source uniqueness, exact version pins, snapshot
  immutability/retention và no-second-authority boundary.
- [x] Unit test package collaboration PASS.
- [x] Integration-tag compile cho collaboration và retained media regression PASS.
- [x] GitHub CI có PostgreSQL 17 functional + exact ACL gate P5-COLLAB-02.
- [x] Full repository `pnpm verify` PASS trên candidate cuối.
- [ ] Exact GitHub Verify/Security PASS sau commit/push candidate.

## 6. Neon disposable gates

- [x] Owner preflight: ledger `36 dirty=false`, ba URL cùng branch/database và ba role tách biệt.
- [x] Forward-only/idempotent `36 false -> 37 false -> 37 false`.
- [x] Exact runtime/maintenance/PUBLIC table, column và schema ACL.
- [x] Lifecycle CAS: hai writer cùng expected version chỉ có một winner và một receipt.
- [x] Restore generation swap: row lock + deferred current-generation FK, chỉ một generation mới.
- [x] Foreign-tenant predicate/constrained write denial.
- [x] Information-schema proof PostgreSQL không chứa live operation/history authority.
- [x] Final ledger `37 dirty=false`; disposable branch được giữ lại.

Evidence 2026-08-21: `test:integration:collaboration:p502` PASS cả control-plane PostgreSQL gate và
exact ACL provisioning gate; forward/idempotent giữ `37 dirty=false`. Không rollback, không xóa
disposable branch và không chạm shared staging.

### File local cần tạo

Tạo `.env.p5-collab-02-disposable.local` ở repository root và chỉ thêm:

```dotenv
DATABASE_MIGRATION_URL=<direct, role owner của disposable branch>
DATABASE_POOL_URL=<pooled, role tutorhub_runtime trên cùng disposable branch>
DATABASE_POLL_MAINTENANCE_URL=<direct hoặc pooled, role maintenance trên cùng branch>
```

File phải tiếp tục bị Git ignore. Không gửi ba giá trị vào chat.

### Lệnh disposable gate

Chạy từ `D:\TutorHub_V2` trong một PowerShell process. Đoạn nạp dưới đây chỉ đặt process environment,
không in giá trị:

```powershell
$envFile = '.env.p5-collab-02-disposable.local'
$allowed = @(
  'DATABASE_MIGRATION_URL',
  'DATABASE_POOL_URL',
  'DATABASE_POLL_MAINTENANCE_URL'
)

Get-Content -LiteralPath $envFile | ForEach-Object {
  if ($_ -match '^\s*([A-Z0-9_]+)\s*=\s*(.+?)\s*$' -and $allowed -contains $matches[1]) {
    $name = $matches[1]
    $value = $matches[2].Trim()
    if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
        ($value.StartsWith("'") -and $value.EndsWith("'"))) {
      $value = $value.Substring(1, $value.Length - 2)
    }
    [Environment]::SetEnvironmentVariable($name, $value, 'Process')
  }
}

$env:P5_COLLAB_02_DISPOSABLE_CONFIRM = 'I_UNDERSTAND_P5_COLLAB_02_DISPOSABLE_ONLY'
$env:P5_COLLAB_02_ACL_PROVISION_CONFIRM = 'I_UNDERSTAND_P5_COLLAB_02_ACL_PROVISION_DISPOSABLE_ONLY'
corepack pnpm test:integration:collaboration:p502
```

Không chạy rollback. Harness không log URL/role name và fail closed nếu ba principal không tách biệt.

## 7. Điều kiện chuyển `VERIFY -> DONE`

1. Local full verify và exact candidate CI/security PASS.
2. Toàn bộ Neon disposable gate mục 6 PASS ở `37 false`.
3. Review migration/ACL evidence và xác nhận không có live-operation/history authority.
4. Chỉ sau báo cáo disposable mới xin quyền forward shared staging `36 -> 37`, provision ACL và chạy
   read-only final snapshot. P5-COLLAB-02 không yêu cầu deploy UI/runtime mới.
