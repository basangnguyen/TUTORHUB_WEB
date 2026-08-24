# P5-COLLAB-18 — Internal canary acceptance

- Trạng thái: `VERIFY` — disposable PASS, shared/live PENDING
- Ngày kickoff: 2026-08-24
- Quyết định kiến trúc áp dụng: [ADR-0037](adr/0037-whiteboard-feature-quota-and-operations.md)
- Trạng thái triển khai tại kickoff: classroom whiteboard vẫn deployment force-off
- Task hạ nguồn: P5-COLLAB-19 bị khóa cho tới khi P5-COLLAB-18 đạt `DONE`

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
bị từ chối. Profile này vẫn chỉ là local evidence và chưa cho phép shared-staging rollout.

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

Các lệnh trên chỉ được dùng với disposable provider. Shared staging/deploy vẫn ngoài phạm vi
authorization hiện tại.

### A. Candidate và pre-staging

- [x] Changed-file inventory 21 file, loại `.lnk`, không có `.env*.local`; secret scan và
      `git diff --check` PASS. Exact candidate `8d65898` đã push lên `origin/main`.
- [x] Focused P5-COLLAB-18 test và full `pnpm verify` PASS. GitHub Verify `32692298247` và
      Security `32692298253` đều PASS trên exact candidate.
- [x] Disposable runner/contract và validator tests đã được thêm; unit `4/4`, exact same-branch/B2
      preflight và secret-safe argument handling PASS.
- [x] Disposable Neon/B2 gates PASS trước mọi shared-staging mutation; ba PostgreSQL gate và B2
      artifact lifecycle/recovery đều xanh tại final ledger `41 false`.

### B. Allowlist, quota và tenant isolation

- [x] Exact-one internal workspace/tenant được server-side allowlist; default allowlist rỗng và tenant
      khác fail closed/concealed.
- [x] Low-quota profile 2 documents, 10 connections, 64 MiB và 600 operations/phút được enforce;
      at-limit PASS, `limit + 1` bị từ chối.
- [ ] Organization Admin, Teacher và Student hiện có chỉ thấy capability đúng vai trò.
- [x] Local synthetic/non-sensitive fixture PASS; denied tenant không tạo PostgreSQL, B2 hoặc runtime
      side effect. Live cross-tenant acceptance vẫn `PENDING`.

### C. Operations, dashboards và kill switch

- [ ] SLO, error, cost và privacy dashboards hiển thị bounded signals cần thiết.
- [ ] Alert/owner route và hard-cost guard được xác nhận.
- [ ] Kill-switch drill PASS và đưa canary về force-off an toàn.

### D. Rollback, recovery và cleanup

- [ ] Canary off/rollback vẫn export được board synthetic.
- [ ] Last-good snapshot restore PASS với đúng generation/checksum contract.
- [ ] Post-test cleanup xác nhận không còn canary document, relation, binding hoặc B2 object ngoài
      evidence được phép giữ.

## 5. Evidence log

| Gate                                       | Trạng thái | Bằng chứng                                     |
| ------------------------------------------ | ---------- | ---------------------------------------------- |
| Local inventory/secret scan/full verify    | `PASS`     | 21 file; `.lnk` loại; focused/full verify xanh |
| Exact candidate SHA                        | `PASS`     | `8d65898`                                      |
| GitHub Verify/Security                     | `PASS`     | `32692298247` / `32692298253`                  |
| Disposable runner/contract                 | `PASS`     | Unit `4/4`; secret-safe; no migration          |
| Disposable Neon/B2                         | `PASS`     | 3 PostgreSQL + B2 recovery; final `41 false`   |
| Internal tenant allowlist và low quota     | `PASS`     | Local exact-one/quota boundary                 |
| Denied-tenant zero side effect             | `PASS`     | Local PostgreSQL/B2/runtime                    |
| Shared staging/deploy                      | `PENDING`  | Chưa thực hiện                                 |
| SLO/error/cost/privacy dashboards          | `PENDING`  | Chưa thu thập                                  |
| Kill-switch và cross-tenant isolation      | `PENDING`  | Chưa chạy                                      |
| Off/rollback, export và last-good snapshot | `PENDING`  | Chưa chạy                                      |
| Physical Chrome/Edge + NVDA                | `PENDING`  | Chưa chạy cho canary                           |
| Final cleanup snapshot                     | `PENDING`  | Chưa chạy                                      |

## 6. Exact exit gate

Các gate dưới đây là điều kiện bắt buộc từ Phase 5 backlog. Local implementation evidence đã xanh,
nhưng các gate tổng hợp chỉ hoàn tất sau external/live acceptance:

- [ ] Allowlist tenant nội bộ, quota thấp, synthetic/non-sensitive board và on-call owner rõ.
- [ ] SLO/error/cost/privacy dashboards cùng kill-switch drill PASS; không cross-tenant leakage.
- [ ] Canary off/rollback giữ export và last-good snapshot khả dụng.

## 7. Quyết định hiện tại

P5-COLLAB-18 ở `VERIFY` — disposable PASS, shared/live PENDING. Exact candidate `8d65898` đã PASS
local/full verify cùng GitHub Verify `32692298247` và Security `32692298253`. Disposable runner đã
tự nạp file local secret-safe và PASS exact preflight, ba PostgreSQL gate cùng B2 artifact
lifecycle/recovery tại final ledger `41 false`; observed `RPO=last_verified_artifact`, `RTO_MS=2042`.
Bốn synthetic pending command của gate đã được đóng `failed` theo fixture cleanup. Không migration,
rollback, shared-staging mutation hoặc deploy. Classroom whiteboard tiếp tục deployment force-off.
Chỉ chuyển `DONE` khi exact canary, rollback/recovery, physical và cleanup evidence được lưu.
P5-COLLAB-19 vẫn bị khóa.
