# Biên bản nghiệm thu staging P2-12

## 1. Trạng thái

| Thuộc tính | Giá trị |
| --- | --- |
| Ngày lập biên bản | 2026-07-22 |
| Phạm vi | P2-12 Staging acceptance và đóng Phase 2 |
| Trạng thái | **DONE - Phase 2 đã được nghiệm thu và đóng** |
| Release candidate SHA | `3c48964e3900b2a262c4026abf0174b3c39c5d93` - cây application/E2E dùng cho provider acceptance |
| Closure-record SHA | Commit docs-only chứa biên bản này; SHA được giải quyết bằng Git history sau commit, không tự tham chiếu trong nội dung |
| Owner sign-off | **ĐẠT** - product/engineering owner xác nhận hoàn tất P2-12 ngày 2026-07-22 |
| Môi trường | Cloudflare Pages, Render Core API, Neon PostgreSQL và ZITADEL staging |

Biên bản này ghi quyết định nghiệm thu P2-12 và đóng Phase 2. Bằng chứng tự động của
P2-00 đến P2-11 được tái sử dụng khi còn đúng với candidate hiện tại; mọi kết quả phụ
thuộc provider đã được chạy trên chuỗi release candidate/rollback/forward nêu bên dưới.
Closure-record là chính commit docs-only chứa evidence cuối. Commit này phải đạt CI và
operator sẽ xác nhận ngay sau khi push; nếu hậu kiểm thất bại, P2-12 phải được mở lại.
Provider không phải redeploy khi diff chỉ chứa tài liệu và không đổi runtime artifact.

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
| P2-12-S01 | Admin tạo tenant và mời teacher/student | Scenario `P2-12 closes admin, instructor, and learner workflows through the real UI` trong `e2e/p2-08-core-workflows.spec.ts` tạo/chỉnh/switch workspace, tạo hai invitation và revoke invitation thứ ba. Luồng nền tương ứng đã xanh ở P2-08 Verify #59. | Admin tạo workspace `P2-12 Acceptance 202607221900` và tạo hai invitation riêng cho teacher/student qua UI staging; không ghi invitation token vào biên bản. | **ĐẠT** |
| P2-12-S02 | Teacher/student accept invitation, login và switch đúng workspace | Cùng scenario Playwright dùng ba browser context độc lập, preview/accept invitation và xác nhận active workspace; PostgreSQL invitation integration kiểm tra accept lặp lại idempotent. | Cả teacher và student đều login ZITADEL, accept invitation và switch/reload đúng workspace nghiệm thu trên deployment release candidate. | **ĐẠT** |
| P2-12-S03 | Teacher tạo class và invite code có TTL/usage limit | Playwright tạo class active, link lifetime `1 day`, limit `2`, kiểm tra `0/2` rồi `1/2`. `TestPostgresEnrollmentInviteUsageIsAtomicAndIdempotent` cùng enrollment service tests bao phủ expiry, revoke, exhaustion và concurrent usage. | Teacher tạo class active `f61e3344-251f-42eb-b3bc-90fd9f9cff5d`, tạo link TTL `1 day`, giới hạn `2`; UI xác nhận usage chuyển đúng `0/2 -> 1/2` sau một lần join. | **ĐẠT** |
| P2-12-S04 | Student join class; teacher thấy roster và đổi role hợp lệ | Playwright xác nhận class xuất hiện ngay cho learner, teacher thấy learner, đổi thành teaching assistant, suspend rồi remove. Roster repository/service tests kiểm tra scope, hierarchy, pagination và mutation. | Student join thành công; teacher thấy roster, đổi role sang Trợ giảng, suspend rồi remove. Sau refresh, hàng lịch sử vẫn còn với trạng thái `Đã xóa`. | **ĐẠT** |
| P2-12-S05 | User tenant khác không đọc/ghi class, roster, audit hoặc room token | P2-10 `TestSecurityActorResourceMatrix` và `TestSecurityForeignResourceIDsDoNotMutate` kiểm tra exact foreign class/user/invite IDs, concealment và snapshot không đổi; audit integration kiểm tra cross-tenant query. Media authorization có unit/service boundary tests. | Khi active tenant là `KMA`, truy cập exact class của workspace nghiệm thu bị conceal; audit filter theo exact class trả `0`. Exact foreign roster/media-token tiếp tục dựa trên automated baseline ở cột trước; lượt này **không** thực hiện hoặc tuyên bố direct staging POST room-token. | **ĐẠT (STAGING UI + AUTO SECURITY BASELINE)** |
| P2-12-S06 | Archive class chặn join mới nhưng giữ audit/roster lịch sử | Playwright giữ link còn active ở `1/2`, archive class, xác nhận roster `Removed`, rồi dùng admin chưa enroll thử join và nhận lỗi class không còn active. `TestPostgresInviteScopeAndArchivedLifecycle` kiểm tra archive làm link unavailable; roster/audit integration kiểm tra dữ liệu lịch sử và append-only. | Teacher archive class khi link vẫn ở `1/2`; student chưa enroll tại thời điểm thử join bị từ chối. Sau refresh, roster history vẫn giữ nguyên và có thể đối chiếu. | **ĐẠT** |
| P2-12-S07 | Audit query trả đúng actor/request/resource | Playwright tìm đúng hàng `Create class`, actor UUID không phải `Unavailable user`, resource class UUID và request ID không rỗng; đồng thời thấy suspend/remove. `TestPostgresAuditEventsAreTenantScopedAndAppendOnly` kiểm tra projection, tenant scope, redaction và chống update/delete/truncate. | Audit workspace nghiệm thu trả `22` event; filter exact class trả `5` event bao phủ create/update/roster/archive. Actor UUID, resource UUID và request ID đều có giá trị; denied join cũng được audit, không thấy token/session trong projection UI. | **ĐẠT** |
| P2-12-S08 | V1 fixture import dry-run + apply + rerun đạt idempotency | Source baseline `f07d05d`: `TestFixtureImportIsIdempotentAndResumable`, fixture parser/plan tests và PostgreSQL 17 CI xác nhận dry-run, apply/rerun, checkpoint/resume, reconciliation, mapping/ownership và cleanup/reset. Closure RC là `3c48964`. | Operator: coding agent dưới ủy quyền của owner; ngày 2026-07-22; Neon disposable `p2-12-closure-20260722` (`br-still-base-aobr6mrt`). Dry-run lập kế hoạch 12 record; apply nhập 10, skip 2, fail 0; rerun giữ 10 record unchanged, không duplicate. Reconciliation có 2 run, 10 mapping, 24 item; toàn bộ target link tồn tại. Branch đã bị xóa sau kiểm tra, trước yêu cầu sau đó của owner về việc giữ nguyên các branch còn lại; không lưu credential/source payload. | **ĐẠT** |
| P2-12-S09 | Deploy, migration up/down/up và rollback smoke đạt trên staging | P2-11 đã có PostgreSQL 17 `13 -> 12 -> 13`; health/readiness/status direct Render và qua Pages proxy đã trả HTTP 200 trong lượt kiểm tra P2-12 ban đầu. | Neon disposable đạt `12 false -> 13 false -> 12 false -> 13 false`; Neon staging thật đã forward `12 false -> 13 false`. Render deploy `0be98bb` qua `dep-d9gbjq7aqgkc73ffftm0` live khoảng 19:39; 6/6 probe đạt. Native Rollback về deploy `3c48964` tiếp tục cảnh báo không tải được config nên được cancel an toàn. Application rollback dùng `Deploy a specific commit`, giữ config hiện tại: `dep-d9gbl47lk1mc739t86rg` chạy `3c48964`, live khoảng 19:42 và 6/6 probe đạt. Forward deploy lại `0be98bb` qua `dep-d9gbllupbkes73cda040`, live khoảng 19:43; 6/6 probe đạt và phiên Admin/audit vẫn hoạt động. Auto-Deploy được quan sát là `Off`; không tuyên bố đã đổi hoặc restore vì baseline trước smoke không được ghi chắc chắn. | **ĐẠT - APPLICATION ROLLBACK/REDEPLOY** |

### 3.1 Lượt UI staging S01-S07

- Thời gian: khoảng 19:06-19:36 ngày 2026-07-22 (Asia/Saigon).
- Fixture: workspace `P2-12 Acceptance 202607221900`; class
  `f61e3344-251f-42eb-b3bc-90fd9f9cff5d`.
- Cách chạy: thao tác UI staging với ba phiên ZITADEL admin/teacher/student riêng biệt;
  người dùng tự nhập mật khẩu tại điểm handoff. Không đọc/lưu password, cookie, token
  mời hoặc browser storage.
- Kết quả: S01-S04 và S06-S07 đạt trực tiếp qua UI. S05 đạt bằng staging UI exact-class/
  exact-audit concealment kết hợp P2-10 automated baseline cho exact foreign roster và
  media-token; không có direct staging POST room-token trong lượt này.
- Audit: workspace có 22 event; filter exact class có 5 event và denied join được ghi;
  các hàng kiểm tra có actor UUID, resource UUID và request ID.

## 4. Release candidate CI và hậu kiểm closure-record

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

P2-12 được đóng dựa trên toàn bộ CI, provider, database, UI, rollback và owner sign-off,
không dựa riêng vào Security hay baseline P2-10/P2-11. Closure-record docs-only phải đạt
Verify/Security sau push nhưng không tạo yêu cầu provider redeploy giả tạo; operator xác
nhận hai workflow ngay khi có kết quả và mở lại P2-12 nếu một gate thất bại.

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
- [x] Render deploy latest `0be98bb` qua `dep-d9gbjq7aqgkc73ffftm0` live khoảng 19:39;
      direct Render + Pages đạt 6/6 probe HTTP 200.
- [x] Native Rollback về deploy `3c48964` cảnh báo không tải được configuration hiện tại
      nên được cancel an toàn, không override configuration.
- [x] Application rollback bằng `Deploy a specific commit` giữ config hiện tại:
      `dep-d9gbl47lk1mc739t86rg` chạy `3c48964`, live khoảng 19:42; 6/6 probe đạt.
- [x] Forward deploy lại `0be98bb` qua `dep-d9gbllupbkes73cda040`, live khoảng 19:43;
      6/6 probe đạt và phiên Admin/audit tiếp tục hoạt động.
- [x] Rollback/redeploy smoke không làm sai contract, session hoặc migration state.

Auto-Deploy được quan sát là `Off` sau smoke. Baseline trước rollback không được ghi chắc
chắn, vì vậy biên bản không tuyên bố coding agent đã đổi hoặc restore setting này.

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
| Release candidate SHA | `3c48964e3900b2a262c4026abf0174b3c39c5d93` | **CI/PROVIDER PARITY/UI S01-S07/S09 ĐẠT** |
| Closure-record SHA | Commit docs-only chứa biên bản này; tra bằng Git history, không ghi hash tự tham chiếu | **RESOLVED BY GIT HISTORY; XÁC NHẬN CI SAU PUSH** |
| Verify workflow | `29912093175` xanh tại `3c48964` | **ĐẠT CHECKPOINT** |
| Security workflow | `29912093166` xanh tại `3c48964` | **ĐẠT CHECKPOINT** |
| Cloudflare deployment | Check suite success tại full SHA `3c48964e3900b2a262c4026abf0174b3c39c5d93` | **ĐẠT CHECKPOINT** |
| Render deployment | Latest `0be98bb` live qua `dep-d9gbjq7aqgkc73ffftm0`; application rollback `3c48964` qua `dep-d9gbl47lk1mc739t86rg`; forward `0be98bb` qua `dep-d9gbllupbkes73cda040` | **ĐẠT** |
| Public health/readiness/status | Mỗi bước latest/rollback/forward đều đạt 6/6 endpoint HTTP 200 direct Render + Pages; database/object storage ready ngày 2026-07-22 | **ĐẠT** |
| Neon migration | Disposable `12→13→12→13`, staging `12→13`; mọi checkpoint `dirty=false` | **ĐẠT** |
| Neon role parity | Runtime safe flags; ledger privilege/default ACL leak `0`; audit contract đạt; future-table probe đạt | **ĐẠT** |
| Importer staging | 12 planned; apply 10/2/0; rerun 10 unchanged; branch cleanup | **ĐẠT** |
| 7 UI scenarios S01-S07; S09 provider closure | S01-S07 đạt khoảng 19:06-19:36 ngày 2026-07-22 trên workspace/class fixture nêu ở mục 3.1; S08 importer đạt; S09 application rollback/redeploy đạt khoảng 19:39-19:43 | **ĐẠT** |
| Rollback smoke | Database up/down/up đạt; Render application rollback/forward deploy đạt. Native config rollback không dùng vì warning và đã cancel an toàn | **ĐẠT** |
| Product/engineering owner sign-off | Owner xác nhận hoàn tất P2-12 ngày 2026-07-22 | **ĐẠT** |

## 7. Quyết định chuyển trạng thái

Các điều kiện chuyển P2-12 sang `DONE` đã được đáp ứng:

1. Cả 7 UI scenarios P2-12-S01 đến S07 có staging evidence trên release candidate;
   S09 provider closure cũng đạt và S08 importer tiếp tục giữ bằng chứng ở trên.
2. Verify và Security xanh trên release candidate; closure-record docs-only được nhận
   diện bằng Git history và phải được operator xác nhận CI ngay sau push.
3. Cloudflare Pages, Render và Neon parity được xác nhận; migration `13 false`.
4. Runtime/migration role split đạt, đặc biệt các bảng import và audit giữ least privilege.
5. Rollback smoke và cleanup fixture hoàn tất; không xóa thêm Neon branch theo quyết định
   hiện tại của owner.
6. `docs/PHASE_2_COMPLETION.md` đã ghi quyết định đóng phase và owner sign-off ngày
   2026-07-22.

**Kết luận: DONE - P2-12 và Phase 2 hoàn thành ngày 2026-07-22. Phase 3 có thể bắt đầu.
CI của closure-record docs-only là hậu kiểm bắt buộc ngay sau push; kết quả thất bại sẽ
mở lại P2-12 thay vì bị che giấu.**
