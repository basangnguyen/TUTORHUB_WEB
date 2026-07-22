# Biên bản nghiệm thu staging P2-12

## 1. Trạng thái

| Thuộc tính | Giá trị |
| --- | --- |
| Ngày lập biên bản | 2026-07-22 |
| Phạm vi | P2-12 Staging acceptance và đóng Phase 2 |
| Trạng thái | **VERIFY / IN PROGRESS - chưa đủ điều kiện đóng Phase 2** |
| Release candidate SHA | `3c48964e3900b2a262c4026abf0174b3c39c5d93` - cây application/E2E dùng cho provider acceptance |
| Closure-record SHA | **PENDING** - commit docs-only ghi bằng chứng sau cùng, không tự tham chiếu vào chính nội dung biên bản |
| Môi trường | Cloudflare Pages, Render Core API, Neon PostgreSQL và ZITADEL staging |

Biên bản này là worksheet nghiệm thu có kiểm soát, không phải tuyên bố Phase 2 đã hoàn
thành. Bằng chứng tự động của P2-00 đến P2-11 được tái sử dụng khi còn đúng với candidate
hiện tại; mọi kết quả phụ thuộc provider phải được chạy trên release candidate ở trên.
Closure-record là commit tài liệu tạo sau khi thu đủ evidence: commit này phải đạt CI,
nhưng provider không phải redeploy nếu diff chỉ chứa tài liệu và không đổi runtime artifact.

Không ghi token, cookie, connection string, storage state, password hoặc nội dung file
`.env*.local` vào tài liệu, command output, log CI hay artifact. Các thao tác phá hủy dữ
liệu chỉ được chạy trên branch/database staging dùng một lần.

## 2. Quy ước trạng thái

- **AUTO BASELINE ĐẠT**: đã có automated test và một CI baseline xanh trước P2-12.
- **AUTO CANDIDATE ĐẠT**: test mới đã xanh trên candidate P2-12, nhưng chưa thay thế
  staging acceptance và CI của commit tài liệu đóng phase cuối.
- **STAGING PENDING**: chưa có bằng chứng cuối trên cùng release candidate/deployment.
- **PROVIDER ĐẠT**: kiểm tra trực tiếp trên provider đã đạt nhưng không thay thế UI
  acceptance hoặc deployment parity còn thiếu.
- **ĐẠT**: chỉ dùng sau khi cả automated gate và staging gate tương ứng đều xanh.

## 3. Ma trận acceptance: 7 UI scenarios S01-S07 + S08 importer + S09 provider closure

| ID | Scenario bắt buộc | Bằng chứng tự động hiện có | Bằng chứng/kết quả staging | Trạng thái |
| --- | --- | --- | --- | --- |
| P2-12-S01 | Admin tạo tenant và mời teacher/student | Scenario `P2-12 closes admin, instructor, and learner workflows through the real UI` trong `e2e/p2-08-core-workflows.spec.ts` tạo/chỉnh/switch workspace, tạo hai invitation và revoke invitation thứ ba. Luồng nền tương ứng đã xanh ở P2-08 Verify #59. | Chạy lại qua UI staging với ba identity ZITADEL riêng biệt trên fixture dùng một lần; ghi commit web/API và thời điểm chạy, không ghi invitation token. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S02 | Teacher/student accept invitation, login và switch đúng workspace | Cùng scenario Playwright dùng ba browser context độc lập, preview/accept invitation và xác nhận active workspace; PostgreSQL invitation integration kiểm tra accept lặp lại idempotent. | Xác nhận login/callback, accept, switch, reload session và quyền hiển thị đúng trên release candidate deployment. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S03 | Teacher tạo class và invite code có TTL/usage limit | Playwright tạo class active, link lifetime `1 day`, limit `2`, kiểm tra `0/2` rồi `1/2`. `TestPostgresEnrollmentInviteUsageIsAtomicAndIdempotent` cùng enrollment service tests bao phủ expiry, revoke, exhaustion và concurrent usage. | Tạo link staging với TTL/limit hữu hạn; xác nhận usage tăng đúng sau một lần join và link hết hạn/hết lượt bị từ chối mà không tăng sai counter. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S04 | Student join class; teacher thấy roster và đổi role hợp lệ | Playwright xác nhận class xuất hiện ngay cho learner, teacher thấy learner, đổi thành teaching assistant, suspend rồi remove. Roster repository/service tests kiểm tra scope, hierarchy, pagination và mutation. | Chạy lại toàn bộ join/role/suspend/remove qua UI staging và xác nhận roster sau refresh. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S05 | User tenant khác không đọc/ghi class, roster, audit hoặc room token | P2-10 `TestSecurityActorResourceMatrix` và `TestSecurityForeignResourceIDsDoNotMutate` kiểm tra exact foreign class/user/invite IDs, concealment và snapshot không đổi; audit integration kiểm tra cross-tenant query. Media authorization có unit/service boundary tests. | Chạy negative smoke bằng identity tenant B đối với exact class/roster/audit của tenant A và request room token. Phải nhận deny/conceal phù hợp, không có business row/audit ngoài dự kiến bị thay đổi. | **AUTO BASELINE ĐẠT / STAGING PENDING** |
| P2-12-S06 | Archive class chặn join mới nhưng giữ audit/roster lịch sử | Playwright giữ link còn active ở `1/2`, archive class, xác nhận roster `Removed`, rồi dùng admin chưa enroll thử join và nhận lỗi class không còn active. `TestPostgresInviteScopeAndArchivedLifecycle` kiểm tra archive làm link unavailable; roster/audit integration kiểm tra dữ liệu lịch sử và append-only. | Trên staging, archive khi link vẫn còn TTL/lượt; thử join bằng identity chưa enroll; kiểm tra roster/audit vẫn truy vấn được sau refresh. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S07 | Audit query trả đúng actor/request/resource | Playwright tìm đúng hàng `Create class`, actor UUID không phải `Unavailable user`, resource class UUID và request ID không rỗng; đồng thời thấy suspend/remove. `TestPostgresAuditEventsAreTenantScopedAndAppendOnly` kiểm tra projection, tenant scope, redaction và chống update/delete/truncate. | Org admin đối chiếu actor, action, outcome, resource và request ID của chính chuỗi staging; kiểm tra không lộ token/session/email thừa. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S08 | V1 fixture import dry-run + apply + rerun đạt idempotency | Source baseline `f07d05d`: `TestFixtureImportIsIdempotentAndResumable`, fixture parser/plan tests và PostgreSQL 17 CI xác nhận dry-run, apply/rerun, checkpoint/resume, reconciliation, mapping/ownership và cleanup/reset. Closure RC là `3c48964`. | Operator: coding agent dưới ủy quyền của owner; ngày 2026-07-22; Neon disposable `p2-12-closure-20260722` (`br-still-base-aobr6mrt`). Dry-run lập kế hoạch 12 record; apply nhập 10, skip 2, fail 0; rerun giữ 10 record unchanged, không duplicate. Reconciliation có 2 run, 10 mapping, 24 item; toàn bộ target link tồn tại. Branch đã bị xóa sau kiểm tra, trước yêu cầu sau đó của owner về việc giữ nguyên các branch còn lại; không lưu credential/source payload. | **ĐẠT** |
| P2-12-S09 | Deploy, migration up/down/up và rollback smoke đạt trên staging | P2-11 đã có PostgreSQL 17 `13 -> 12 -> 13`; health/readiness/status direct Render và qua Pages proxy đã trả HTTP 200 trong lượt kiểm tra P2-12 ban đầu. | Neon disposable đạt `12 false -> 13 false -> 12 false -> 13 false`; Neon staging thật đã forward `12 false -> 13 false`. Render đã live full SHA `3c48964e3900b2a262c4026abf0174b3c39c5d93` qua deploy `dep-d9gaiturnols73c75qp0` lúc 18:30 ngày 2026-07-22 (Asia/Saigon); 6/6 public probe sau deploy đều HTTP 200. Provider rollback/redeploy còn pending vì Render cảnh báo không tải được cấu hình của live deploy hiện tại và rollback có thể gây thay đổi cấu hình ngoài dự kiến; thao tác đã được hủy an toàn. | **MIGRATION/DEPLOY/PROBE ĐẠT / ROLLBACK PENDING** |

## 4. Release candidate CI hiện có và closure-record CI còn thiếu

### Baseline đã xanh

- P2-10: [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539891)
  và [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539912)
  tại commit `c4205b9`.
- P2-11: [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333712)
  và [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333728)
  tại commit `f07d05d`.

### Candidate P2-12

- Commit `bf30605` đưa coverage P2-12 vào scenario; `7563ed1` và `6fb4f84` thu hẹp
  locator audit về đúng action `Create class`.
- Candidate `6fb4f84aa0f1cd20f1253472b22bfe9100f2adaa` đã đạt
  [Verify #29910962433](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433),
  gồm Browser E2E PostgreSQL 17 + Chromium, và
  [Security #29910962424](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424).
- Release candidate `3c48964e3900b2a262c4026abf0174b3c39c5d93` đã đạt
  [Verify #29912093175](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093175)
  và
  [Security #29912093166](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093166).
  Cloudflare Pages check suite cùng full SHA hoàn tất với kết quả success.

Không được dùng Security xanh riêng lẻ hoặc các baseline P2-10/P2-11 để đánh dấu P2-12
`DONE`. Provider parity phải bám release candidate. Closure-record docs-only phải đạt
Verify/Security sau khi ghi evidence nhưng không tạo yêu cầu provider redeploy giả tạo.

## 5. Provider parity và database gate

### 5.1 Cloudflare Pages và Render

- [x] Cloudflare Pages check suite xác nhận deployment success tại full SHA
      `3c48964e3900b2a262c4026abf0174b3c39c5d93`.
- [x] Render deployment hiển thị live cùng full SHA
      `3c48964e3900b2a262c4026abf0174b3c39c5d93`, deploy
      `dep-d9gaiturnols73c75qp0`, lúc 18:30 ngày 2026-07-22 (Asia/Saigon).
- [x] `GET /health`, `/ready`, `/api/v1/status` trực tiếp trên Render trả HTTP 200
      trong probe ngày 2026-07-22.
- [x] Ba endpoint tương ứng qua same-origin `/api/*` trên Pages trả HTTP 200 trong
      cùng lượt probe.
- [x] Readiness báo `database=ready` và `object_storage=ready`.
- [ ] Rollback/redeploy smoke không làm sai contract, session hoặc migration state.

### 5.2 Neon PostgreSQL

- [x] Branch nghiệm thu được tạo từ staging, không phải production, rồi đã xóa sau test.
- [x] Disposable đạt `12 false -> 13 false -> 12 false -> 13 false`; staging thật đạt
      `12 false -> 13 false`.
- [x] Migration owner và `tutorhub_runtime` là hai identity khác nhau.
- [x] Runtime role không phải superuser, không tạo role/database, không bypass RLS,
      không là member của owner và không sở hữu bảng.
- [x] Runtime role không có effective/direct privilege trên ba bảng `legacy_import_*`.
- [x] Quyền `audit_events` giữ đúng append-only runtime contract; không có
      `UPDATE`, `DELETE` hoặc `TRUNCATE`.
- [x] Migration identity chạy được up/down/up; runtime role chỉ giữ workload grants.
- [x] Import fixture chỉ dùng migration identity và reconciliation không chứa secret/PII thật.

Trong lần kiểm tra này phát hiện Neon default ACL cũ của owner tự cấp CRUD cho runtime
trên bảng mới. Trước khi migrate staging, default table ACL global và schema-scoped đã
được thu hồi, sau đó thu hồi các grant đã materialize trên ba ledger. Assertions cuối:
default ACL leak `0`, effective/direct ledger privilege `0`, schema `USAGE=true`,
`CREATE=false`; transaction probe trên bảng tương lai cho tất cả table privilege bằng
`false` và rollback không để lại bảng. Không sửa migration lịch sử `000013` và không
re-grant quyền trong down migration.

CI hiện dùng cùng một database URL cho migration và workload test nên CI không tự chứng
minh role split của Neon. P2-12 đã bù khoảng trống đó bằng provider assertions trực tiếp:
hai identity khác nhau, runtime safe flags, ledger/default ACL, audit contract và probe
bảng tương lai đều đạt trên staging. Không ghi giá trị credential vào bất kỳ bằng chứng nào.

## 6. Phiếu ghi bằng chứng staging cuối

| Bằng chứng | Giá trị phải ghi | Trạng thái |
| --- | --- | --- |
| Release candidate SHA | `3c48964e3900b2a262c4026abf0174b3c39c5d93` | **CI/PROVIDER PARITY ĐẠT; UI/ROLLBACK PENDING** |
| Closure-record SHA | Docs-only commit tạo sau khi hoàn tất evidence; chưa tồn tại tại thời điểm ghi | **PENDING** |
| Verify workflow | `29912093175` xanh tại `3c48964` | **ĐẠT CHECKPOINT** |
| Security workflow | `29912093166` xanh tại `3c48964` | **ĐẠT CHECKPOINT** |
| Cloudflare deployment | Check suite success tại full SHA `3c48964e3900b2a262c4026abf0174b3c39c5d93` | **ĐẠT CHECKPOINT** |
| Render deployment | `dep-d9gaiturnols73c75qp0` live full SHA `3c48964e3900b2a262c4026abf0174b3c39c5d93` lúc 18:30 ngày 2026-07-22 (Asia/Saigon) | **ĐẠT** |
| Public health/readiness/status | 6/6 endpoint HTTP 200 sau Render deploy; database/object storage ready ngày 2026-07-22 | **ĐẠT** |
| Neon migration | Disposable `12→13→12→13`, staging `12→13`; mọi checkpoint `dirty=false` | **ĐẠT** |
| Neon role parity | Runtime safe flags; ledger privilege/default ACL leak `0`; audit contract đạt; future-table probe đạt | **ĐẠT** |
| Importer staging | 12 planned; apply 10/2/0; rerun 10 unchanged; branch cleanup | **ĐẠT** |
| 7 UI scenarios S01-S07; S09 provider closure | Người chạy, thời điểm, kết quả từng ID; S08 importer đã đạt riêng | **PENDING** |
| Rollback smoke | Database up/down/up đạt; provider rollback/redeploy chưa chạy | **PARTIAL / PENDING** |

## 7. Điều kiện chuyển trạng thái

P2-12 chỉ được chuyển `DONE` khi:

1. Cả 7 UI scenarios P2-12-S01 đến S07 có staging evidence trên release candidate;
   S09 provider closure cũng đạt và S08 importer tiếp tục giữ bằng chứng ở trên.
2. Verify và Security xanh trên release candidate; closure-record docs-only cũng xanh
   sau khi ghi đủ bằng chứng.
3. Cloudflare Pages, Render và Neon parity được xác nhận; migration `13 false`.
4. Runtime/migration role split đạt, đặc biệt các bảng import và audit giữ least privilege.
5. Rollback smoke và cleanup fixture hoàn tất; không xóa thêm Neon branch theo quyết định
   hiện tại của owner.
6. `docs/PHASE_2_COMPLETION.md` được cập nhật từ `PENDING` sang quyết định đóng phase.

**Kết luận hiện tại: VERIFY - CI, Cloudflare/Render parity, Neon
migration/least-privilege, importer và public probe đã đạt; 7 UI scenarios S01-S07;
S09 provider rollback/redeploy và owner sign-off còn pending. Không bắt đầu Phase 3 dựa
trên biên bản này.**
