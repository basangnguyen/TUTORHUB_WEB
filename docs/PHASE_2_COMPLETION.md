# Biên bản đóng Phase 2 - Identity, tenant và class core

> **DONE - PHASE 2 HOÀN THÀNH NGÀY 2026-07-22**

## 1. Kết luận hiện tại

| Thuộc tính | Kết quả |
| --- | --- |
| Ngày rà soát | 2026-07-22 |
| Release candidate SHA | `3c48964e3900b2a262c4026abf0174b3c39c5d93` - cây application/E2E dùng cho provider acceptance |
| Closure-record SHA | Commit docs-only chứa biên bản này; SHA được xác định bởi lịch sử Git sau commit |
| Kết luận | **ĐẠT - Phase 2 hoàn thành** |
| Task vừa hoàn thành | P2-12 Staging acceptance và đóng phase |
| Phase kế tiếp | Phase 3 - Workspace, schedule, chat, notification và Drive |

P2-00 đến P2-11 đã hoàn thành theo backlog và có bằng chứng tự động tương ứng. P2-12
đã bổ sung coverage closure cho TTL/usage của class invitation, archive chặn join mới,
giữ roster lịch sử và audit actor/request/resource. Release candidate `3c48964` đã đạt
Verify, Security, Cloudflare/Render deployment parity, Neon migration/role separation,
importer staging, 6/6 public probe và 7 UI scenarios S01-S07. S09 application
rollback/redeploy đạt trên Render, toàn bộ probe sau rollback và forward deploy đều xanh;
product/engineering owner đã sign-off ngày 2026-07-22.

Closure-record là commit tài liệu được tạo sau khi thu đủ evidence. Commit đó phải đạt
CI, nhưng provider không phải redeploy nếu diff chỉ là docs và không đổi runtime artifact.

Biên bản nghiệm thu chi tiết nằm tại `docs/P2_12_STAGING_ACCEPTANCE.md`.

## 2. Phạm vi Phase 2 đã triển khai

- Policy/contract deny-by-default và role matrix dùng chung.
- Profile, identity linking và session/recent-auth boundary.
- Tenant lifecycle, workspace switch và tenant-scoped cache invalidation.
- Membership invitation create/preview/accept/revoke idempotent.
- Class lifecycle, ownership, archive/restore và optimistic concurrency.
- Enrollment, class invite code có TTL/usage limit và roster role hierarchy.
- Audit tenant-scoped, privacy-reduced và append-only.
- Admin/teacher/student UI end-to-end với loading, empty, error, forbidden và retry.
- Feature flag/quota server-authoritative, edge context và shared limiter.
- Tenant isolation/IDOR matrix, strict input/resource boundary và fuzz coverage.
- V1 fixture importer ẩn danh, dry-run/apply/rerun, checkpoint/resume và reconciliation.

Danh sách trên mô tả implementation đã có, không thay thế acceptance staging P2-12.

## 3. Trạng thái task

| Task | Kết quả |
| --- | --- |
| P2-00 Policy/contract baseline | DONE |
| P2-01 Profile/identity | DONE |
| P2-02 Tenant lifecycle | DONE |
| P2-03 Membership invitation | DONE |
| P2-04 Class lifecycle | DONE |
| P2-05 Enrollment/invite code | DONE |
| P2-06 Roster/class roles | DONE |
| P2-07 Audit log | DONE |
| P2-08 Admin/teacher/student E2E UI | DONE |
| P2-09 Feature flag/quota | DONE |
| P2-10 Tenant isolation/IDOR | DONE |
| P2-11 V1 fixture import | DONE |
| P2-12 Staging acceptance và đóng phase | **DONE** |

## 4. Ma trận exit gate Phase 2

| Exit gate | Bằng chứng hiện có | Kết quả hiện tại |
| --- | --- | --- |
| Permission matrix được phê duyệt và có automated tests | ADR-0013, policy engine tests, P2-10 actor/resource matrix | **ĐẠT BASELINE** |
| IDOR/cross-tenant suite xanh | `securitysuite/security_integration_test.go`; P2-10 Verify `29884539891` | **ĐẠT BASELINE** |
| Audit query tenant-scoped, append-only và không chứa secret | ADR-0014; audit PostgreSQL integration; UI audit E2E; Neon runtime audit ACL `SELECT/INSERT=true`, mutation/truncate=false; staging UI trả 22 event và 5 exact-class event có actor/resource/request ID | **ĐẠT** |
| Import fixture idempotent và có reconciliation | P2-11 commit `f07d05d`; Verify `29891333712`; Neon disposable apply/rerun 10 imported/10 unchanged, 2 bounded skip, 0 failure | **ĐẠT** |
| UI có loading/empty/error/forbidden cho luồng bắt buộc | Web component/unit tests, P2-08 browser scenario, visual QA và staging S01-S07 ngày 2026-07-22 | **ĐẠT** |
| Không còn role check nghiệp vụ rải rác ngoài policy layer | Static boundary/policy tests và review P2-00/P2-10 | **ĐẠT BASELINE** |
| `pnpm verify` xanh trên release candidate | RC `3c48964` đạt Verify `29912093175`, gồm Browser E2E PostgreSQL 17 + Chromium; closure-record docs-only phải được hậu kiểm sau push | **ĐẠT** |
| Security workflow xanh trên release candidate | RC `3c48964` đạt Security `29912093166`; closure-record docs-only phải được hậu kiểm sau push | **ĐẠT** |
| 7 UI scenarios S01-S07; S09 provider closure đều xanh | S01-S07 đạt khoảng 19:06-19:36 ngày 2026-07-22 trên workspace/class fixture P2-12; S08 importer đạt; S09 application rollback/redeploy đạt khoảng 19:39-19:43 | **ĐẠT** |
| Deploy/migration/rollback cùng release candidate | Cloudflare/Render parity đạt; Neon staging `13 false`, role split/ACL probes và disposable up/down/up đạt. Render application rollback `0be98bb -> 3c48964 -> 0be98bb` giữ config hiện tại; mỗi bước có 6/6 probe đạt | **ĐẠT** |
| Biên bản đóng phase phản ánh đúng bằng chứng | Biên bản staging, backlog, project state và master plan đồng bộ; owner sign-off ngày 2026-07-22 | **ĐẠT** |

## 5. Bằng chứng CI baseline

| Phạm vi | Commit | Verify | Security | Kết quả |
| --- | --- | --- | --- | --- |
| P2-08 UI/E2E | `836ae7e` | [29716888239](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888239) | [29716888233](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888233) | ĐẠT baseline |
| P2-09 feature/quota | `096620a` | Bằng chứng ghi trong backlog/state | Bằng chứng ghi trong backlog/state | ĐẠT baseline |
| P2-10 security | `c4205b9` | [29884539891](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539891) | [29884539912](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539912) | ĐẠT baseline |
| P2-11 importer | `f07d05d` | [29891333712](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333712) | [29891333728](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333728) | ĐẠT baseline |
| P2-12 automation candidate | `6fb4f84` | [29910962433](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433) đạt, gồm Browser E2E | [29910962424](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424) đạt | **ĐẠT CANDIDATE; CHƯA PHẢI CLOSURE** |
| P2-12 provider checkpoint | `3c48964` | [29912093175](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093175) đạt | [29912093166](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093166) đạt | **CI + CLOUDFLARE + RENDER + NEON ĐẠT CHECKPOINT** |

Baseline cũ chứng minh các work package riêng lẻ, nhưng không chứng minh deployment hiện
tại cùng chạy một SHA. Provider acceptance bám release candidate; closure-record docs-only
phải có Verify/Security riêng sau khi evidence được ghi, không buộc provider redeploy nếu
runtime artifact không đổi.

### Bằng chứng UI staging P2-12

- Khoảng 19:06-19:36 ngày 2026-07-22 (Asia/Saigon), ba identity admin/teacher/student
  hoàn tất S01-S07 trên workspace `P2-12 Acceptance 202607221900` và class
  `f61e3344-251f-42eb-b3bc-90fd9f9cff5d`.
- Link class có TTL 1 ngày, limit 2 và usage đúng `0/2 -> 1/2`; roster hiển thị student,
  đổi Trợ giảng, suspend, remove và refresh vẫn giữ trạng thái `Đã xóa`.
- Cross-tenant staging UI conceal exact class và exact audit filter ở `KMA` trả 0.
  Exact foreign roster/media-token dựa trên P2-10 automated security baseline; không có
  direct staging POST room-token trong lượt này.
- Archive khi link còn `1/2` chặn identity chưa enroll join mới nhưng giữ roster history.
  Audit workspace có 22 event, exact class có 5 event create/update/roster/archive;
  actor UUID, resource UUID, request ID đầy đủ và denied join cũng được audit.

## 6. Các gate đã đạt khi sign-off

1. Release candidate `3c48964` đã đạt Verify toàn bộ, bao gồm Browser E2E PostgreSQL 17
   + Chromium; Verify trên closure-record docs-only là hậu kiểm bắt buộc sau push.
2. Release candidate đã đạt Security; Security trên closure-record docs-only là hậu kiểm
   bắt buộc sau push.
3. Render deploy `dep-d9gaiturnols73c75qp0` đã live cùng full SHA với Cloudflare.
4. Neon gate đã đạt: staging `13`, `dirty=false`; up/down/up trên branch dùng một lần;
   branch đã cleanup.
5. Neon role gate đã đạt: runtime khác migration owner, không phải owner/superuser,
   default/effective/direct quyền ledger đều bằng 0 và audit mutation/truncate bị chặn.
6. Bảy UI scenarios P2-12-S01 đến S07 đã đạt trên staging. Với S05, exact class/audit
   được kiểm tra trực tiếp qua UI; exact foreign roster/media-token dùng automated
   baseline, không tuyên bố direct staging POST. S09 application rollback/redeploy và
   S08 importer rerun idempotent đều đã đạt.
7. Provider rollback/redeploy smoke đạt; fixture đã cleanup. Không xóa thêm Neon branch
   theo quyết định hiện tại của owner.
8. Cập nhật backlog, project state, master plan và biên bản này bằng evidence cuối.

## 7. Rủi ro và follow-up không bị che giấu

- Direct-main là ngoại lệ quản trị có thời hạn theo ADR-0012; phải thay trước pilot/public
  beta hoặc khi có người duy trì thứ hai.
- Render free/private-alpha có cold start; kết quả acceptance không phải cam kết SLO
  production.
- P2-10-F02: cursor class/roster/audit bind scope bằng hash nhưng chưa ký HMAC. Low risk
  đã được chấp nhận/defer sang Phase 3 security hardening với owner Backend/Security;
  bắt buộc mở lại trước public beta hoặc trước khi cursor được lưu/chia sẻ ngoài phiên
  phân trang ngắn hạn, tùy mốc nào đến trước.
- CI dùng cùng database URL nên không tự chứng minh role separation; P2-12 đã bù bằng
  provider assertions trực tiếp trên Neon staging mà không ghi credential. Neon default
  ACL leak phát hiện trong lượt này đã được thu hồi ở lớp provisioning; migration lịch
  sử `000013` không bị sửa.
- Media authorization có automated exact-foreign-class boundary tests. Lượt staging UI
  S05 chỉ kiểm tra exact class/audit concealment và không thực hiện direct POST room-token;
  giới hạn bằng chứng này phải tiếp tục được nêu rõ, không được nâng thành provider smoke.

## 8. Quyết định chuyển phase

- [x] Verify xanh trên release candidate SHA; closure-record docs-only được hậu kiểm sau push.
- [x] Security xanh trên release candidate SHA; closure-record docs-only được hậu kiểm sau push.
- [x] 7 UI scenarios S01-S07 đạt.
- [x] S09 provider closure đạt.
- [x] Pages/Render/Neon parity đạt.
- [x] Provider rollback/redeploy smoke đạt.
- [x] Runtime/migration role split đạt.
- [x] Fixture/disposable branch nghiệm thu đã cleanup.
- [x] Product/engineering owner xác nhận sign-off.

**Quyết định: ĐẠT - Phase 2 hoàn thành ngày 2026-07-22; Phase 3 được phép bắt đầu.**

Nếu Verify hoặc Security của closure-record docs-only thất bại sau push, phải mở lại P2-12,
khắc phục regression và cập nhật biên bản; không được che giấu bằng chứng hậu kiểm thất bại.
