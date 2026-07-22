# Biên bản đóng Phase 2 - Identity, tenant và class core

> **DRAFT - CHƯA CÓ HIỆU LỰC ĐÓNG PHASE**

## 1. Kết luận hiện tại

| Thuộc tính | Kết quả |
| --- | --- |
| Ngày rà soát | 2026-07-22 |
| Commit làm chuẩn | **PENDING** - chưa có closure SHA đạt toàn bộ gate |
| Kết luận | **CHƯA ĐẠT - Phase 2 vẫn đang thực hiện** |
| Task hiện tại | P2-12 Staging acceptance và đóng phase |
| Phase kế tiếp | Phase 3 chỉ được bắt đầu sau khi biên bản này chuyển sang **ĐẠT** |

P2-00 đến P2-11 đã hoàn thành theo backlog và có bằng chứng tự động tương ứng. P2-12
đã bổ sung coverage closure cho TTL/usage của class invitation, archive chặn join mới,
giữ roster lịch sử và audit actor/request/resource. Tuy nhiên, Phase 2 chưa được đóng vì
chưa có một commit duy nhất đồng thời đạt Verify, Security, provider parity, Neon role
separation và đầy đủ chín scenario staging.

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
| P2-12 Staging acceptance và đóng phase | **IN PROGRESS** |

## 4. Ma trận exit gate Phase 2

| Exit gate | Bằng chứng hiện có | Kết quả hiện tại |
| --- | --- | --- |
| Permission matrix được phê duyệt và có automated tests | ADR-0013, policy engine tests, P2-10 actor/resource matrix | **ĐẠT BASELINE** |
| IDOR/cross-tenant suite xanh | `securitysuite/security_integration_test.go`; P2-10 Verify `29884539891` | **ĐẠT BASELINE** |
| Audit query tenant-scoped, append-only và không chứa secret | ADR-0014; audit PostgreSQL integration; UI audit E2E | **ĐẠT BASELINE, STAGING CLOSURE PENDING** |
| Import fixture idempotent và có reconciliation | P2-11 commit `f07d05d`; Verify `29891333712` | **ĐẠT BASELINE, NEON RUN PENDING** |
| UI có loading/empty/error/forbidden cho luồng bắt buộc | Web component/unit tests, P2-08 browser scenario và visual QA | **ĐẠT BASELINE, STAGING CLOSURE PENDING** |
| Không còn role check nghiệp vụ rải rác ngoài policy layer | Static boundary/policy tests và review P2-00/P2-10 | **ĐẠT BASELINE** |
| `pnpm verify` xanh trên closure SHA | Candidate `6fb4f84` đã đạt Verify `29910962433`, gồm Browser E2E PostgreSQL 17 + Chromium; phải chạy lại trên final closure SHA | **CANDIDATE ĐẠT / CLOSURE PENDING** |
| Security workflow xanh trên closure SHA | Candidate `6fb4f84` đã đạt Security `29910962424`; phải chạy lại trên final closure SHA | **CANDIDATE ĐẠT / CLOSURE PENDING** |
| Chín staging acceptance scenario đều xanh | Worksheet P2-12 đã được tạo, bằng chứng cuối chưa đủ | **PENDING** |
| Deploy/migration/rollback cùng closure SHA | Direct Render và Pages proxy health/readiness/status đều HTTP 200; Render/Pages SHA, Neon `13 false`, role split và rollback cuối chưa xác nhận đủ | **PARTIAL / PENDING** |
| Biên bản đóng phase phản ánh đúng bằng chứng | File này tồn tại ở trạng thái draft và không tuyên bố hoàn tất sớm | **PENDING SIGN-OFF** |

## 5. Bằng chứng CI baseline

| Phạm vi | Commit | Verify | Security | Kết quả |
| --- | --- | --- | --- | --- |
| P2-08 UI/E2E | `836ae7e` | [29716888239](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888239) | [29716888233](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888233) | ĐẠT baseline |
| P2-09 feature/quota | `096620a` | Bằng chứng ghi trong backlog/state | Bằng chứng ghi trong backlog/state | ĐẠT baseline |
| P2-10 security | `c4205b9` | [29884539891](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539891) | [29884539912](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539912) | ĐẠT baseline |
| P2-11 importer | `f07d05d` | [29891333712](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333712) | [29891333728](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333728) | ĐẠT baseline |
| P2-12 automation candidate | `6fb4f84` | [29910962433](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433) đạt, gồm Browser E2E | [29910962424](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424) đạt | **ĐẠT CANDIDATE; CHƯA PHẢI CLOSURE** |

Baseline cũ chứng minh các work package riêng lẻ, nhưng không chứng minh deployment hiện
tại cùng chạy một SHA. Lượt closure phải ghi URL Verify/Security mới trên commit chuẩn.

## 6. Blocker bắt buộc trước sign-off

1. Chạy lại Verify xanh toàn bộ, bao gồm Browser E2E PostgreSQL 17 + Chromium, trên
   final closure SHA; candidate `6fb4f84` đã đạt gate này.
2. Chạy lại Security xanh trên cùng final closure SHA; candidate `6fb4f84` đã đạt.
3. Cloudflare Pages và Render cùng deploy full SHA đó; không chỉ khớp short SHA hoặc
   commit message.
4. Neon staging ở migration `13`, `dirty=false`; up/down/up chạy trên branch dùng một lần.
5. Chứng minh migration role khác runtime role. Runtime role không phải owner/superuser,
   không có quyền trên `legacy_import_*` và không thể sửa/xóa/truncate audit history.
6. Chạy đủ P2-12-S01 đến P2-12-S09 trên staging, bao gồm foreign-tenant room-token deny,
   class archive với link còn hiệu lực và importer rerun idempotent.
7. Rollback/redeploy smoke đạt và cleanup fixture/Neon branch hoàn tất.
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
- CI hiện chưa chứng minh Neon runtime/migration credential separation vì workload test
  dùng cùng database URL. Đây là provider gate bắt buộc, không phải mục tùy chọn.
- Media authorization có automated boundary tests, nhưng exact foreign-class room-token
  negative smoke trên deployment closure vẫn phải chạy.

## 8. Quyết định chuyển phase

- [ ] Verify xanh trên closure SHA.
- [ ] Security xanh trên closure SHA.
- [ ] Chín scenario staging đều đạt.
- [ ] Pages/Render/Neon parity và rollback smoke đạt.
- [ ] Runtime/migration role split đạt.
- [ ] Fixture/branch nghiệm thu đã cleanup.
- [ ] Product/engineering owner xác nhận sign-off.

**Quyết định hiện tại: KHÔNG CHUYỂN PHASE.**

Khi toàn bộ checkbox được đánh dấu, cập nhật commit chuẩn, link CI, ma trận staging và đổi
kết luận thành `ĐẠT - Phase 2 hoàn thành`. Trước thời điểm đó, Phase 3 chỉ được chuẩn bị
backlog/tài liệu, không bắt đầu implementation.
