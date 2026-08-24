# P5-COLLAB-18 — Internal canary acceptance

- Trạng thái: `VERIFY` — local pre-staging
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

### A. Candidate và pre-staging

- [x] Changed-file inventory 21 file, loại `.lnk`, không có `.env*.local`; secret scan và
      `git diff --check` PASS. Exact candidate SHA vẫn `PENDING` cho tới khi commit.
- [x] Focused P5-COLLAB-18 test và full `pnpm verify` PASS. GitHub Verify/Security vẫn `PENDING`.
- [ ] Disposable Neon/B2 gates PASS trước mọi shared-staging mutation.

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

| Gate                                       | Trạng thái | Bằng chứng                     |
| ------------------------------------------ | ---------- | ------------------------------ |
| Local inventory/secret scan/full verify    | `PASS`     | 21 file; `.lnk` loại; focused/full verify xanh |
| Exact candidate SHA                        | `PENDING`  | Chờ commit candidate           |
| GitHub Verify/Security                     | `PENDING`  | Chưa chạy                      |
| Disposable Neon/B2                         | `PENDING`  | Chưa chạy                      |
| Internal tenant allowlist và low quota     | `PASS`     | Local exact-one/quota boundary |
| Denied-tenant zero side effect             | `PASS`     | Local PostgreSQL/B2/runtime    |
| Shared staging/deploy                      | `PENDING`  | Chưa thực hiện                 |
| SLO/error/cost/privacy dashboards          | `PENDING`  | Chưa thu thập                  |
| Kill-switch và cross-tenant isolation      | `PENDING`  | Chưa chạy                      |
| Off/rollback, export và last-good snapshot | `PENDING`  | Chưa chạy                      |
| Physical Chrome/Edge + NVDA                | `PENDING`  | Chưa chạy cho canary           |
| Final cleanup snapshot                     | `PENDING`  | Chưa chạy                      |

## 6. Exact exit gate

Các gate dưới đây là điều kiện bắt buộc từ Phase 5 backlog. Local implementation evidence đã xanh,
nhưng các gate tổng hợp chỉ hoàn tất sau external/live acceptance:

- [ ] Allowlist tenant nội bộ, quota thấp, synthetic/non-sensitive board và on-call owner rõ.
- [ ] SLO/error/cost/privacy dashboards cùng kill-switch drill PASS; không cross-tenant leakage.
- [ ] Canary off/rollback giữ export và last-good snapshot khả dụng.

## 7. Quyết định hiện tại

P5-COLLAB-18 ở `VERIFY` — local pre-staging. Local candidate implementation và test đã xanh;
repository milestone gần nhất vẫn `637e8b5`, exact candidate SHA cùng GitHub/disposable/shared/live
evidence vẫn `PENDING`. Classroom whiteboard tiếp tục deployment force-off. Chỉ chuyển `DONE` khi
exact canary, rollback/recovery, physical và cleanup evidence được lưu. P5-COLLAB-19 vẫn bị khóa.
