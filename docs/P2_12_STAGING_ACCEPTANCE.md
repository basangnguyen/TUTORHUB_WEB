# Biên bản nghiệm thu staging P2-12

## 1. Trạng thái

| Thuộc tính | Giá trị |
| --- | --- |
| Ngày lập biên bản | 2026-07-22 |
| Phạm vi | P2-12 Staging acceptance và đóng Phase 2 |
| Trạng thái | **IN PROGRESS - chưa đủ điều kiện đóng Phase 2** |
| Commit closure chuẩn | **PENDING** - chỉ chốt sau khi Verify, Security và staging parity cùng đạt |
| Môi trường | Cloudflare Pages, Render Core API, Neon PostgreSQL và ZITADEL staging |

Biên bản này là worksheet nghiệm thu có kiểm soát, không phải tuyên bố Phase 2 đã hoàn
thành. Bằng chứng tự động của P2-00 đến P2-11 được tái sử dụng khi còn đúng với candidate
hiện tại; mọi kết quả phụ thuộc provider phải được chạy lại trên cùng commit closure.

Không ghi token, cookie, connection string, storage state, password hoặc nội dung file
`.env*.local` vào tài liệu, command output, log CI hay artifact. Các thao tác phá hủy dữ
liệu chỉ được chạy trên branch/database staging dùng một lần.

## 2. Quy ước trạng thái

- **AUTO BASELINE ĐẠT**: đã có automated test và một CI baseline xanh trước P2-12.
- **AUTO CANDIDATE ĐẠT**: test mới đã xanh trên candidate P2-12, nhưng chưa thay thế
  staging acceptance và CI của commit tài liệu đóng phase cuối.
- **STAGING PENDING**: chưa có bằng chứng cuối trên cùng commit/deployment closure.
- **ĐẠT**: chỉ dùng sau khi cả automated gate và staging gate tương ứng đều xanh.

## 3. Ma trận 9 acceptance scenario

| ID | Scenario bắt buộc | Bằng chứng tự động hiện có | Bằng chứng staging còn phải thu | Trạng thái |
| --- | --- | --- | --- | --- |
| P2-12-S01 | Admin tạo tenant và mời teacher/student | Scenario `P2-12 closes admin, instructor, and learner workflows through the real UI` trong `e2e/p2-08-core-workflows.spec.ts` tạo/chỉnh/switch workspace, tạo hai invitation và revoke invitation thứ ba. Luồng nền tương ứng đã xanh ở P2-08 Verify #59. | Chạy lại qua UI staging với ba identity ZITADEL riêng biệt trên fixture dùng một lần; ghi commit web/API và thời điểm chạy, không ghi invitation token. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S02 | Teacher/student accept invitation, login và switch đúng workspace | Cùng scenario Playwright dùng ba browser context độc lập, preview/accept invitation và xác nhận active workspace; PostgreSQL invitation integration kiểm tra accept lặp lại idempotent. | Xác nhận login/callback, accept, switch, reload session và quyền hiển thị đúng trên deployment closure. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S03 | Teacher tạo class và invite code có TTL/usage limit | Playwright tạo class active, link lifetime `1 day`, limit `2`, kiểm tra `0/2` rồi `1/2`. `TestPostgresEnrollmentInviteUsageIsAtomicAndIdempotent` cùng enrollment service tests bao phủ expiry, revoke, exhaustion và concurrent usage. | Tạo link staging với TTL/limit hữu hạn; xác nhận usage tăng đúng sau một lần join và link hết hạn/hết lượt bị từ chối mà không tăng sai counter. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S04 | Student join class; teacher thấy roster và đổi role hợp lệ | Playwright xác nhận class xuất hiện ngay cho learner, teacher thấy learner, đổi thành teaching assistant, suspend rồi remove. Roster repository/service tests kiểm tra scope, hierarchy, pagination và mutation. | Chạy lại toàn bộ join/role/suspend/remove qua UI staging và xác nhận roster sau refresh. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S05 | User tenant khác không đọc/ghi class, roster, audit hoặc room token | P2-10 `TestSecurityActorResourceMatrix` và `TestSecurityForeignResourceIDsDoNotMutate` kiểm tra exact foreign class/user/invite IDs, concealment và snapshot không đổi; audit integration kiểm tra cross-tenant query. Media authorization có unit/service boundary tests. | Chạy negative smoke bằng identity tenant B đối với exact class/roster/audit của tenant A và request room token. Phải nhận deny/conceal phù hợp, không có business row/audit ngoài dự kiến bị thay đổi. | **AUTO BASELINE ĐẠT / STAGING PENDING** |
| P2-12-S06 | Archive class chặn join mới nhưng giữ audit/roster lịch sử | Playwright giữ link còn active ở `1/2`, archive class, xác nhận roster `Removed`, rồi dùng admin chưa enroll thử join và nhận lỗi class không còn active. `TestPostgresInviteScopeAndArchivedLifecycle` kiểm tra archive làm link unavailable; roster/audit integration kiểm tra dữ liệu lịch sử và append-only. | Trên staging, archive khi link vẫn còn TTL/lượt; thử join bằng identity chưa enroll; kiểm tra roster/audit vẫn truy vấn được sau refresh. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S07 | Audit query trả đúng actor/request/resource | Playwright tìm đúng hàng `Create class`, actor UUID không phải `Unavailable user`, resource class UUID và request ID không rỗng; đồng thời thấy suspend/remove. `TestPostgresAuditEventsAreTenantScopedAndAppendOnly` kiểm tra projection, tenant scope, redaction và chống update/delete/truncate. | Org admin đối chiếu actor, action, outcome, resource và request ID của chính chuỗi staging; kiểm tra không lộ token/session/email thừa. | **AUTO CANDIDATE ĐẠT / STAGING PENDING** |
| P2-12-S08 | V1 fixture import dry-run + apply + rerun đạt idempotency | P2-11 commit `f07d05d`: `TestFixtureImportIsIdempotentAndResumable`, fixture parser/plan tests và PostgreSQL 17 CI xác nhận dry-run, apply/rerun, checkpoint/resume, reconciliation, mapping/ownership và cleanup/reset. | Chạy trên Neon branch dùng một lần bằng migration role; lưu reconciliation chỉ gồm số đếm/reason code an toàn; rerun không tạo duplicate; reset branch sau nghiệm thu. | **AUTO BASELINE ĐẠT / STAGING PENDING** |
| P2-12-S09 | Deploy, migration up/down/up và rollback smoke đạt trên staging | P2-11 đã có PostgreSQL 17 `13 -> 12 -> 13`; health/readiness/status direct Render và qua Pages proxy đã trả HTTP 200 trong lượt kiểm tra P2-12 ban đầu. Đây chưa chứng minh provider parity của commit closure. | Chứng minh Pages và Render cùng full SHA; Neon `version=13`, `dirty=false`; migration/runtime role tách đúng; chạy `13 -> 12 -> 13` trên branch dùng một lần; rollback/redeploy candidate trước đó rồi forward lại; health/readiness/status đều đạt. | **PARTIAL / STAGING PENDING** |

## 4. Bằng chứng CI hiện có và closure CI còn thiếu

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

Không được dùng Security xanh riêng lẻ hoặc các baseline P2-10/P2-11 để đánh dấu P2-12
`DONE`. Closure cần Verify và Security cùng xanh trên đúng commit được deploy.

## 5. Provider parity và database gate

### 5.1 Cloudflare Pages và Render

- [ ] Production deployment của Pages hiển thị đúng full SHA closure.
- [ ] Render deployment/image hiển thị cùng full SHA closure.
- [x] `GET /health`, `/ready`, `/api/v1/status` trực tiếp trên Render trả HTTP 200
      trong probe ngày 2026-07-22.
- [x] Ba endpoint tương ứng qua same-origin `/api/*` trên Pages trả HTTP 200 trong
      cùng lượt probe.
- [x] Readiness báo `database=ready` và `object_storage=ready`.
- [ ] Rollback/redeploy smoke không làm sai contract, session hoặc migration state.

### 5.2 Neon PostgreSQL

- [ ] Branch nghiệm thu được xác nhận là staging/disposable, không phải production.
- [ ] Migration version là `13`, `dirty=false` trước và sau lượt kiểm tra.
- [ ] Migration role và runtime role là hai identity khác nhau.
- [ ] Runtime role không phải superuser, không tạo role/database, không sở hữu bảng.
- [ ] Runtime role không có quyền trực tiếp trên các bảng `legacy_import_*`.
- [ ] Quyền `audit_events` giữ đúng append-only runtime contract; không có
      `UPDATE`, `DELETE` hoặc `TRUNCATE`.
- [ ] Migration role chạy được up/down/up; runtime role chỉ chạy workload ứng dụng.
- [ ] Import fixture chỉ dùng migration role và reconciliation không chứa secret/PII thật.

Khoảng trống quan trọng tại thời điểm lập biên bản: CI hiện dùng cùng một database URL
cho migration và workload test, nên CI không tự chứng minh role split của Neon. Gate này
phải được xác nhận bằng hai credential thực trên staging nhưng không ghi giá trị credential
vào bất kỳ bằng chứng nào.

## 6. Phiếu ghi bằng chứng staging cuối

| Bằng chứng | Giá trị phải ghi | Trạng thái |
| --- | --- | --- |
| Full commit SHA closure | SHA của commit đã có Verify + Security xanh | **PENDING** |
| Candidate Verify workflow | `29910962433` xanh tại `6fb4f84`; chạy lại trên closure SHA | **CANDIDATE ĐẠT / CLOSURE PENDING** |
| Candidate Security workflow | `29910962424` xanh tại `6fb4f84`; chạy lại trên closure SHA | **CANDIDATE ĐẠT / CLOSURE PENDING** |
| Cloudflare deployment | Full SHA + thời điểm deploy | **PENDING** |
| Render deployment | Full SHA/image + thời điểm deploy | **PENDING** |
| Public health/readiness/status | 6/6 endpoint HTTP 200; database/object storage ready ngày 2026-07-22 | **ĐẠT PROBE / PARITY PENDING** |
| Neon migration | `13`, `dirty=false` trước/sau | **PENDING** |
| Neon role parity | Tên role an toàn và ma trận boolean/quyền; không ghi URL/password | **PENDING** |
| 9 scenario | Người chạy, thời điểm, kết quả từng ID, fixture đã cleanup | **PENDING** |
| Rollback smoke | Candidate rollback, kết quả probe, forward deploy khôi phục | **PENDING** |

## 7. Điều kiện chuyển trạng thái

P2-12 chỉ được chuyển `DONE` khi:

1. Cả 9 dòng P2-12-S01 đến P2-12-S09 có staging evidence trên cùng closure SHA.
2. Verify và Security đều xanh trên SHA đó.
3. Cloudflare Pages, Render và Neon parity được xác nhận; migration `13 false`.
4. Runtime/migration role split đạt, đặc biệt các bảng import và audit giữ least privilege.
5. Rollback smoke và cleanup fixture/branch hoàn tất.
6. `docs/PHASE_2_COMPLETION.md` được cập nhật từ `PENDING` sang quyết định đóng phase.

**Kết luận hiện tại: PENDING - không được bắt đầu Phase 3 dựa trên biên bản này.**
