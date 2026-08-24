# P5-COLLAB-18 — Internal canary acceptance

- Trạng thái: `DONE` — internal canary gates, recovery và cleanup PASS
- Ngày kickoff: 2026-08-24
- Quyết định kiến trúc áp dụng: [ADR-0037](adr/0037-whiteboard-feature-quota-and-operations.md)
- Trạng thái triển khai khi closure: exact-one internal tenant enabled; tenant khác vẫn force-off
- Task hạ nguồn: P5-COLLAB-19 đã được mở sau closure `DONE`

## 1. Authorization và fixture được phê duyệt

Authorization ngày 2026-08-24 chỉ cho phép bắt đầu chuẩn bị và kiểm chứng internal canary theo
phạm vi dưới đây. Authorization này không tự động đánh dấu bất kỳ exit gate nào là PASS và không
được dùng thay cho candidate, CI, disposable, shared staging, dashboard hoặc physical evidence.

| Thuộc tính                | Giá trị được phê duyệt                                                                           |
| ------------------------- | ------------------------------------------------------------------------------------------------ |
| Workspace                 | `P5-COLLAB-18 Internal Canary`                                                                   |
| Tenant                    | Đúng một tenant nội bộ được allowlist server-side; identifier không ghi vào tài liệu hoặc log    |
| Tài khoản                 | Các tài khoản Organization Admin, Teacher và Student hiện có; không tạo hoặc ghi credential mới  |
| Dữ liệu                   | Board synthetic, non-sensitive; không dùng dữ liệu học sinh, lớp học thật hoặc nội dung riêng tư |
| Phạm vi rollout           | Internal canary có quota thấp; không phải private alpha hoặc production ramp                     |
| Feature state lúc kickoff | Force-off cho tới khi exact canary candidate và các gate trước triển khai đạt                    |

Local candidate đã pin và kiểm chứng exact quota profile: 2 documents, 10 connections, 64 MiB
durable bytes và 600 operations/phút. Giá trị đúng giới hạn được chấp nhận; mọi trường hợp `limit + 1`
bị từ chối. Profile này đã được áp dụng cho exact internal canary tenant; server/data plane vẫn enforce cùng giới hạn.

## 2. Owner và escalation

Giữ nguyên owner đã phê duyệt cho private-alpha operations:

| Trách nhiệm             | Owner    |
| ----------------------- | -------- |
| Primary on-call         | Bá Sáng  |
| Backup on-call          | Duy Mạnh |
| Security incident owner | Bá Sáng  |
| Cost owner              | Bá Sáng  |

Owner phải có quyền force-off canary và thực hiện runbook. Không ghi token, URL có credential, raw
board content, email hoặc provider error chi tiết vào log/evidence.

## 3. Safety boundary

- Chỉ exact internal tenant allowlist mới được nhận canary; tenant khác phải tiếp tục force-off.
- Browser không tự quyết định tenant, capability, quota hoặc provider document.
- Quota phải được server/data plane enforce; UI disable không được coi là enforcement.
- SLO/error/cost/privacy dashboard chỉ dùng bounded metadata, không có raw operation/snapshot body.
- Kill switch phải đưa canary về `off` hoặc trạng thái an toàn đã công bố mà không mở rộng blast radius.
- Canary off/rollback phải giữ export và last-good snapshot khả dụng.
- Không forward shared staging hoặc deploy trước disposable report và exact candidate evidence theo
  quy trình dự án.
- Không mở P5-COLLAB-19 nếu bất kỳ gate P5-COLLAB-18 nào còn thiếu bằng chứng.

## 4. Evidence plan

### Disposable runner contract

Runner `scripts/run-p518-disposable.mjs` chỉ đọc file local bị Git ignore, không hiển thị giá trị
credential và không tự chạy migration hoặc rollback. Contract mặc định là
`.env.p5-collab-18-disposable.local` với đúng các biến:

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
P5_COLLAB_18_DISPOSABLE_CONFIRM=I_UNDERSTAND_P5_COLLAB_18_DISPOSABLE_ONLY
```

Bốn PostgreSQL URL phải trỏ tới cùng một disposable branch/database và lần lượt dùng bốn role
khác nhau: owner direct, runtime pooled, collaboration worker direct và maintenance direct. B2
phải là private disposable bucket với scoped key. Runner chỉ chấp nhận migration ledger sạch
`41 false`; nếu khác, runner dừng mà không thay đổi database. Các gate được chạy độc lập bằng:

```powershell
pnpm test:integration:collaboration:p518 -- .env.p5-collab-18-disposable.local preflight
pnpm test:integration:collaboration:p518 -- .env.p5-collab-18-disposable.local database
pnpm test:integration:collaboration:p518 -- .env.p5-collab-18-disposable.local provider
pnpm test:integration:collaboration:p518 -- .env.p5-collab-18-disposable.local all
```

Các lệnh trên chỉ được dùng với disposable provider. Shared staging/deploy chỉ được thực hiện sau
khi disposable report PASS và có authorization riêng; điều kiện này đã được đáp ứng trong closure.

### A. Candidate và pre-staging

- [x] Changed-file inventory 21 file, loại `.lnk`, không có `.env*.local`; secret scan và
      `git diff --check` PASS. Exact candidate `8d65898` đã push lên `origin/main`.
- [x] Focused P5-COLLAB-18 test và full `pnpm verify` PASS. GitHub Verify `32692298247` và
      Security `32692298253` đều PASS trên exact candidate.
- [x] Runtime packaging fix `ccc1f13` PASS GitHub Verify `32712385493` và Security
      `32712385386`; Render Core API, control plane và collaboration runtime đều healthy/ready.
- [x] Disposable runner/contract và validator tests đã được thêm; unit `4/4`, exact same-branch/B2
      preflight và secret-safe argument handling PASS.
- [x] Disposable Neon/B2 gates PASS trước mọi shared-staging mutation; ba PostgreSQL gate và B2
      artifact lifecycle/recovery đều xanh tại final ledger `41 false`.

### B. Allowlist, quota và tenant isolation

- [x] Exact-one internal workspace/tenant được server-side allowlist; default allowlist rỗng và tenant
      khác fail closed/concealed.
- [x] Low-quota profile 2 documents, 10 connections, 64 MiB và 600 operations/phút được enforce;
      at-limit PASS, `limit + 1` bị từ chối.
- [x] Organization Admin, Teacher và Student active membership/capability projection PASS bằng exact
      shared canary audit và shared PostgreSQL authorization integration gates.
- [x] Synthetic fixture PASS; cross-tenant gate conceal/fail-closed và xác nhận zero PostgreSQL,
      B2 hoặc runtime side effect. Không dùng dữ liệu thật.

### C. Operations, dashboards và kill switch

- [x] Live authenticated metrics PASS với bốn dependency signal và các giá trị connections,
      documents, dirty documents, drain đều bằng zero; không có board body hoặc credential.
- [x] Owner/on-call route giữ đúng phê duyệt. Profile free/private-alpha giữ hard cap 0 USD và
      force-off khi vượt quota theo owner sign-off P5-COLLAB-01/P5-COLLAB-16.
- [x] Kill-switch evidence kế thừa actual force-off staging P5-COLLAB-17; P5-COLLAB-18 chạy lại
      P5-16/P5-17 regression PASS. Không lặp lại off/on mutation trên live canary sau khi gates xanh.

### D. Rollback, recovery và cleanup

- [x] Export/provider-exit round-trip và canary-off contract kế thừa P5-COLLAB-16/P5-COLLAB-17;
      synthetic artifact vẫn portable khi provider/runtime không khả dụng.
- [x] Last-good snapshot restore PASS trên disposable với generation/checksum contract;
      `RPO=last_verified_artifact`, `RTO_MS=2042`.
- [x] Final shared canary audit xác nhận không còn whiteboard document/relation/binding; live metrics
      xác nhận connections/documents/dirty/drain đều bằng zero. Disposable B2 lifecycle cleanup PASS.

## 5. Evidence log

| Gate                                    | Trạng thái | Bằng chứng                                               |
| --------------------------------------- | ---------- | -------------------------------------------------------- |
| Local inventory/secret scan/full verify | `PASS`     | Focused P5-18 aggregate và full verify xanh              |
| Candidate/packaging GitHub CI           | `PASS`     | `8d65898`; packaging `ccc1f13`; Verify/Security xanh     |
| Disposable runner/contract              | `PASS`     | Unit `4/4`; secret-safe; no migration                    |
| Disposable Neon/B2                      | `PASS`     | 3 PostgreSQL + B2 recovery; final `41 false`             |
| Internal tenant allowlist và low quota  | `PASS`     | Exact-one shared canary audit; four quota limits         |
| Role/cross-tenant isolation             | `PASS`     | Shared P5-09/P5-10 PostgreSQL integration gates          |
| Shared staging/deploy                   | `PASS`     | Core/control/runtime healthy; runtime exact `ccc1f13`    |
| SLO/error/cost/privacy signals          | `PASS`     | Authenticated live metrics; four deps; cleanup all zero  |
| Kill-switch/off/export/restore          | `PASS`     | Retained P5-16/P5-17 + P5-18 regression; RTO 2042 ms     |
| Physical Chrome/Edge + NVDA             | `PASS`     | Retained production component matrix từ P5-COLLAB-15     |
| Final cleanup snapshot                  | `PASS`     | Shared residue zero; runtime active/dirty/drain all zero |

## 6. Exact exit gate

Các gate dưới đây là điều kiện bắt buộc từ Phase 5 backlog:

- [x] Allowlist tenant nội bộ, quota thấp, synthetic/non-sensitive board và on-call owner rõ.
- [x] SLO/error/cost/privacy signals cùng kill-switch evidence PASS; không cross-tenant leakage.
- [x] Canary off/rollback giữ export và last-good snapshot khả dụng.

## 7. Quyết định hiện tại

P5-COLLAB-18 đạt `DONE`. Exact candidate `8d65898` và packaging fix `ccc1f13` đã PASS local/full
verify cùng GitHub Verify/Security. Disposable Neon/B2 PASS final `41 false`; shared database vẫn
`41 false`, exact role/tenant/quota/fixture và authorization integration gates đều PASS. Render
Core API, control plane và runtime healthy/ready; authenticated metrics xác nhận dependency xanh và
connections/documents/dirty/drain đều zero. Recovery giữ `RPO=last_verified_artifact`,
`RTO_MS=2042`; retained Chrome/Edge + NVDA component evidence P5-COLLAB-15 và force-off/outage/
provider-exit evidence P5-COLLAB-16/P5-COLLAB-17 tiếp tục hợp lệ vì P5-COLLAB-18 không đổi browser
semantics, provider topology hoặc snapshot format. Exact-one tenant canary được bật; mọi tenant khác
vẫn fail closed/force-off. Final cleanup zero-residue PASS. P5-COLLAB-19 đã được mở.
