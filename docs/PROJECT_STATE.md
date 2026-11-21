# TutorHub V2 - Trạng thái dự án

> Điểm vào nhanh để tiếp tục phát triển. Cập nhật sau mỗi task hoặc thay đổi hạ tầng.

## Snapshot

| Thuộc tính           | Trạng thái                                                                                                                                      |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| Ngày cập nhật        | 2026-11-21                                                                                                                                      |
| Repository           | `https://github.com/basangnguyen/TUTORHUB_WEB`                                                                                                  |
| Nhánh làm việc       | `main`                                                                                                                                          |
| Quy trình            | Antigravity làm PM/Gatekeeper điều phối Codex và Claude Code qua prompt; Antigravity nghiệm thu và commit/push thẳng `main`                     |
| Phase hoàn thành     | Phase 0, Phase 1, Phase 2, Phase 4, Phase 5 collaboration                                                                                       |
| Phase hiện tại       | Phase 3 deferred carry-over vẫn hoạt động; Phase 5 collaboration đã đóng                                                                        |
| Task `DONE` gần nhất | `AUTH-13A` Public Beta Auth Hardening phía Server (`000051_auth_retention`, Maintenance ACL/CLI, 2 biên `enforce_sso`, Privacy Audit & Runbook) |
| Mốc repository mới   | PG 17.12 disposable `51→50→51`, `62/62` `identity` (`0` skip), `auth-maintenance` `2/2`, `httpapi` privacy `10/10`, Go `40` package PASS        |
| Task hiện tại        | Giao `AUTH-13B` cho Claude Code (cập nhật UX `403 auth_sso_required` cho Passkey/Phone + Playwright E2E/a11y chéo phương thức `AUTH-01..13`)    |
| Task tiếp theo       | Nghiệm thu `AUTH-13B` để đóng trọn gói chương trình kỹ thuật `AUTH-01..13`, chuyển sang các gate vận hành mở cờ Public Beta & `Q12`             |

### Checkpoint `AUTH-13A` Hardening Xác thực phía Server trước Public Beta (`000051_auth_retention`, Maintenance ACL/CLI, Hai biên `enforce_sso`, Privacy Audit & Runbook) `DONE` — 2026-11-21

Codex triển khai lát cắt Server + Migration + Maintenance CLI + Privacy Audit + Runbook của `AUTH-13` (Phase 0 không có BLOCKER ngoài phạm vi, giữ nguyên toàn bộ cờ `AUTH_*` mặc định tắt và không sửa OpenAPI/Web); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng độc lập trên cụm PostgreSQL 17.12 loopback disposable tạo mới:

- **Migration `000051_auth_retention`, Role Bảo trì riêng & CLI `cmd/auth-maintenance`**:
  - Tách vòng đời biên nhận điều tra OTP pumping `auth_challenge_deliveries` (`30` ngày) khỏi `auth_transactions` / `auth_challenges` (`24` giờ sau `expires_at`): chuyển FK `challenge_id` sang cho phép `NULL` + `ON DELETE SET NULL`, đồng thời gắn trigger `BEFORE INSERT` `require_delivery_challenge` (`SQLSTATE 23502`) bắt buộc runtime `INSERT` luôn có `challenge_id`. `000051.down.sql` khóa độc quyền hai bảng và chặn rollback (`SQLSTATE 55000`) nếu đã phát sinh biên nhận tách cha (`challenge_id IS NULL`).
  - Bổ sung hàm `tutorhub.purge_expired_auth_artifacts(batch_size integer DEFAULT 500) RETURNS jsonb` (`SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, `batch_size` `1..5000`, một mốc `clock_timestamp()` thống nhất, `FOR UPDATE SKIP LOCKED`, không dynamic SQL) dọn đủ 6 nhóm theo đúng cửa sổ lưu giữ: `auth_transactions` (+ cascade `auth_challenges`, `> 24h` sau `expires_at`), `auth_challenge_deliveries` (`> 30` ngày), `user_phone_numbers` `released`/`removed` (`> 90` ngày, không chạm `active`), `mfa_recovery_codes` đã dùng/hủy (`> 30` ngày, không chạm mã còn hiệu lực), `auth_flows` (`> 24h` sau `expires_at` — đồng thời gỡ câu `DELETE` sớm trong `CreateFlow` và thu hồi quyền `DELETE` trên `auth_flows` khỏi runtime role) và `external_data_deletion_requests` (`> 1` năm); nâng `purge_expired_deletion_receipts()` lên `SECURITY DEFINER` có `search_path` cố định, thu hồi `PUBLIC EXECUTE` trên các hàm nội bộ lịch sử và bảo toàn quyền `EXECUTE` trên 3 hàm CHECK bất biến cho các role ghi bảng.
  - `ProvisionAuthMaintenanceACL` kiểm tra role không có quyền quản trị/membership/ownership, thu hồi mọi quyền bảng/cột/hàm (kể cả `attacl` theo cột) và chỉ cấp `USAGE ON SCHEMA tutorhub` + `EXECUTE` đúng hai hàm purge. CLI `cmd/auth-maintenance` chỉ đọc `DATABASE_AUTH_MAINTENANCE_URL` direct (từ chối rỗng và mọi host `-pooler.` kể cả fallback), tắt in cờ lỗi ra output, dừng sớm ở vòng rỗng và chỉ log JSON 6 bộ đếm nguyên; `cmd/auth-maintenance/provision` chạy `ProvisionAuthFoundationRuntimeACL` và `ProvisionAuthMaintenanceACL` trong cùng một giao dịch của owner.
- **Khép kín hai biên School SSO (`enforce_sso=true`)**:
  - **Biên bậc 1 nội bộ**: `FinishPasskeyLogin` (cả `UV=true` lẫn `UV=false`), `VerifyPhoneLogin` (giữ `StartPhoneLogin` trả `202` trung tính chống dò số) và `validateMFAFirstFactor` (khi trường bật `enforce_sso` lúc đang chờ MFA nội bộ) kiểm tra `allowLocalAccountLogin` trên `users.email` dưới khóa chia sẻ chính sách tới lúc commit; vi phạm tiêu thụ giao dịch, xóa cookie `auth_tx` và trả `403 auth_sso_required` không lộ email. Bước MFA bậc 2 sau School SSO (`methods[0] == "fed"`) và step-up `Reauthenticate` / `FinishPasskeyReauth` tiếp tục hoạt động bình thường.
  - **Biên chuyển đổi tài khoản cũ**: `finishExistingFederatedLogin` (cho cả Google Workspace và Microsoft Entra, ở cả nhánh chưa có identity lẫn identity `unlinked`, trong khi `erased` luôn bị từ chối) chỉ tự gắn/tái kích hoạt danh tính trường khi `realm.Required == true` + `school.Proof.matches(...)` + tài khoản `active` có `email_verified_at IS NOT NULL` và khớp đúng địa chỉ canonical (`enforce_sso=false` giữ nguyên `link_intent`). Thứ tự giao dịch đánh giá `beginFederatedMFA` và sàn `requireLegacyFederationFloor` **trước** khi thu hồi phiên cũ (`revoked_reason = 'identity_linked'`) và ghi sự kiện `identity_linked`; nếu tài khoản có sàn `AAL2` hoặc yếu tố đã đăng ký không khả dụng thì từ chối an toàn trước khi thu hồi hay cấp phiên.
- **Gia cố Log, Problem Details & Runbook (`httpapi`, `docs/AUTH_PUBLIC_BETA_RUNBOOK.md`, `docs/DATABASE.md`)**: chuẩn hóa đường dẫn qua `requestTelemetryPath` trong `writeIdentityProblem` và `Problem.Instance` (ẩn mã biên nhận công khai), ẩn giá trị lỗi/panic tùy ý trong `writeJSON` (`%T`) và `recoverMiddleware` (`panicFunctions()`), kiểm chứng bằng `auth_privacy_test.go` capture `slog` + response/redirect với bộ canary tổng hợp. Ban hành `docs/AUTH_PUBLIC_BETA_RUNBOOK.md` ghi rõ quy trình drain API cũ trước khi thu hồi `DELETE` trên `auth_flows`, thứ tự mở cờ, xử lý OTP pumping/`409 auth_sso_lockout` và điều kiện `Q12` gỡ ZITADEL.
- **Bằng chứng Gatekeeper**: trên cụm **PostgreSQL 17.12 loopback disposable** tạo mới (4 database tách biệt, tự xóa sạch sau khi chạy): vòng CLI migration `51 false → 50 false → 51 false` PASS, lệnh runbook `cmd/auth-maintenance/provision` & `cmd/auth-maintenance` PASS, bộ tích hợp `identity` **`62/62` test cấp cao PASS (`0` skip)**, `cmd/auth-maintenance` **`2/2` PASS**, `httpapi` privacy & Postgres HTTP **`10/10` test + `11/11` subtest PASS**, `cmd/api` School SSO **`2/2` PASS**. Toàn kho `go test -count=1 ./services/core-api/...` (**`40/40`** package có test PASS), `go vet`, `gofmt` (`0/36` tệp Go của task), `pnpm verify` (`exit 0`) và `git diff --check` đều PASS.
- **Các điều kiện Public Beta còn mở (ngoài phạm vi `AUTH-13A`)**: (1) lát cắt `AUTH-13B` phía Web/Playwright (cập nhật UX `403 auth_sso_required` trên Passkey/Phone/MFA và E2E/a11y chéo phương thức), (2) triển khai adapter gửi email xác thực chạy được trên staging/production (ví dụ SES) trước khi mở các cờ cần OTP email ở beta (`AUTH_SIGNUP_ENABLED`, `AUTH_METHOD_EMAIL_CODE_ENABLED`), (3) phê duyệt ngân sách + smoke nhà cung cấp thực cho ZNS/SMS/OAuth và (4) quyết định `Q12` gỡ ZITADEL sau khi đóng trọn `AUTH-13` và đối soát `0` tài khoản chỉ còn đường ZITADEL.

### Checkpoint `AUTH-12B` Giao diện Quản trị School SSO, Home Realm Discovery trên `/sign-in` & Playwright E2E (`AUTH-12` `DONE` — Hoàn tất `8/8` Phương thức Đăng nhập) — 2026-11-21

Claude Code triển khai lát cắt Web UI + `@tutorhub/api-client` + móc E2E loopback + Playwright cho `AUTH-12` (Phase 0 không có BLOCKER ngoài phạm vi, không chạm `openapi/tutorhub.yaml` hay `schema.ts`); Gatekeeper (Antigravity) review diff/bảo mật, sửa 1 biểu thức khớp URL `PATCH /sso-connections/{connection_id}` trong `e2e/auth-12-school-sso.spec.ts` và kiểm chứng trọn vẹn trên cụm PostgreSQL 17.12 loopback disposable + Chromium thật:

- **Typed Client (`packages/api-client/src/index.ts` & `authFoundation.test.ts`)**: bổ sung 8 hàm quản trị School SSO (`listTenantSSOConnections`, `createTenantSSOConnection`, `updateTenantSSOConnection`, `listTenantSSODomains`, `createTenantSSODomain`, `rotateTenantSSODomainChallenge`, `verifyTenantSSODomain`, `disableTenantSSODomain`); các hàm đọc đặt `cache: "no-store"`, các hàm ghi đính kèm CSRF + `Origin` và xoay CSRF từ phản hồi, chặn `tenantId` rỗng trước khi gửi mạng.
- **Bảng Quản trị School SSO (`TenantSchoolSSOPanel.tsx` trên `WorkspaceManagementPage.tsx`)**: chỉ hiển thị khi `sso_domains_enabled=true`, giới hạn thao tác cho `role === "org_admin"` + `tenant.manage` với đầy đủ trạng thái `loading`, `empty`, `error` có nút thử lại, `forbidden` và che dữ liệu khi gặp `401`/`403`/`404`. Hỗ trợ tạo kết nối Google Workspace (`hd`) hoặc Microsoft Entra (`tid`), bật/tắt kết nối, chỉnh danh sách tổ chức, chính sách tham gia (`auto_join_role`: gửi tường minh `null` cho chế độ cần lời mời hoặc `"student"` để tự động thêm học sinh), công tắc `enforce_sso` kèm hộp thoại cảnh báo khi đã có domain `verified`, khai báo/xoay/xác minh DNS TXT/tắt domain. Giá trị `txt_value` một lần chỉ giữ trong state cục bộ của component (`gcTime: 0`, `reset()` ngay sau khi nhận, không bao giờ ghi vào React Query cache hay Web Storage); nối `StepUpAuthDialog` chạy lại tối đa 1 lần cho cả recent-auth (`requireAal2: false`) và `AAL2` (`requireAal2: true`), đồng thời hiển thị rõ thông báo chống tự khóa khi gặp `409 auth_sso_lockout`.
- **Home Realm Discovery & Thực thi `enforce_sso` trên `/sign-in` (`SignInPage.tsx`, `SignInPage.shared.ts`)**: nút "Đăng nhập bằng SSO trường học" gọi `discoverAuthIdentifier`; khi `sso.available=true` hiện khối điều hướng về Google/Microsoft của trường (khóa mật khẩu/mã email nếu `sso.required=true`, vẫn giữ tùy chọn khi `required=false`); hủy sạch Passkey Conditional UI (`stopBackground`) trước khi chuyển hướng trang đầy đủ qua `buildOAuthStartURL(provider, returnTo, "login", { loginHint, domain })` và không lưu email trường vào `localStorage`/`sessionStorage`. Xử lý đồng nhất `403 auth_sso_required` trên đăng nhập mật khẩu, mã email, đăng ký, khôi phục mật khẩu và callback `?error=auth_sso_required` (xóa tham số lỗi khỏi thanh địa chỉ).
- **Móc E2E Loopback chỉ mở ở `APP_ENV=test` (`config`, `cmd/api`, `e2eoidc`, `scripts/e2e-local.mjs`)**: biến `AUTH_E2E_DNS_UDP_ADDR` từ chối khởi động ở mọi môi trường khác `test` và chỉ chấp nhận `127.0.0.1:<1-65535>` để bộ phân giải DNS gốc của Go truy vấn responder UDP trong bộ nhớ (`127.0.0.1:9095`) mà không chạm DNS công cộng; `e2eoidc` mở trường `custom_hd` (kiểm chặt domain chữ thường đa nhãn) khi `AllowCustomAccounts=true` trên loopback.
- **Bằng chứng Gatekeeper**: `pnpm verify` PASS (`@tutorhub/api-client` `131/131`, `@tutorhub/web` `899/899` trên `97` file, `e2e:infra:test` `18/18`, `security:test` `86/86`, `security:bundle` `208` file, Storybook build, `39` package Go), `TestSchoolSSOLoopbackDNSResolver` PASS, `gofmt` và `git diff --check` sạch. Trên Chromium thật + cụm **PostgreSQL 17.12 loopback disposable** (migration `50`, tự xóa sạch sau khi chạy): Phase A native-auth gộp `auth-02..10` + `auth-12` **`33/33` PASS** — gồm `auth-12-school-sso.spec.ts` ×2 (quản trị viên xác minh DNS TXT, kiểm lỗi `409 auth_sso_dns_mismatch` và `409 auth_sso_lockout`, ghi sự kiện vào Audit Log, liên kết Google trường rồi bật `enforce_sso` + `auto_join_role="student"`; HRD đồng nhất cho địa chỉ cũ/mới, chặn mật khẩu `403 auth_sso_required`, JIT thêm học sinh mới, từ chối Google khác `hd` về `/sign-in?error=auth_sso_required`, Axe `0` serious/critical, Web Storage sạch) — và Phase B hồi quy OIDC mặc định `p2-08` **`1/1` PASS**.
- **Ghi chú chuyển giao sang `AUTH-13` (`AUTH-13A`)**: `AUTH-12` đã hoàn tất đủ `8/8` phương thức đăng nhập theo kế hoạch. Sang gói khóa sổ `AUTH-13`, bên cạnh migration `000051_auth_retention` (`purge_expired_auth_artifacts` + maintenance ACL), audit ASVS L2/log và runbook public beta, Codex sẽ khép kín hai biên chuyển đổi `enforce_sso=true` cho tài khoản tồn tại từ trước trên domain trường: (1) chặn đăng nhập bậc 1 trực tiếp bằng Passkey/Phone OTP nội bộ khi domain của `users.email` đang chịu `enforce_sso=true` (vẫn giữ Passkey/TOTP làm bước MFA/step-up sau SSO), và (2) tự gắn danh tính SSO trường đã xác minh tổ chức (`hd`/`tid` khớp domain `verified` + `enforce_sso=true` + `users.email_verified_at IS NOT NULL`, vẫn bắt buộc qua MFA nếu tài khoản bật TOTP/Passkey và thu hồi phiên cũ) thay vì đẩy vào `/account-link` bị khóa mật khẩu.

### Checkpoint `AUTH-12A` School Domain SSO & Home Realm Discovery phía Server (`000050`, DNS TXT, `enforce_sso`, JIT & Audit) `DONE` — 2026-11-21

Codex triển khai lát cắt Server + Migration + OpenAPI + `@tutorhub/api-client` + danh mục Audit của `AUTH-12` (Phase 0 dừng đúng quy trình để Gatekeeper duyệt bổ sung 6 action SSO vào `audit/model.go` và ánh xạ nhãn `AuditLogPage.tsx`/`i18n.tsx` nhằm giữ `pnpm verify` xanh nguyên khối); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng độc lập trên cụm PostgreSQL 17.12 loopback disposable tạo mới:

- **Migration `000050_sso_domains` & Exact Runtime ACL**: thêm hai hàm bất biến `sso_school_domain_valid` (chặn IP/wildcard và các domain hộp thư công cộng `gmail.com`, `googlemail.com`, `outlook.com`, `hotmail.com`, `live.com`, `msn.com`, `yahoo.com`, `icloud.com`) và `sso_org_ids_valid` (`google_workspace` kiểm domain chuẩn, `entra` kiểm UUID chữ thường và loại trừ zero UUID cùng Microsoft consumer tenant `9188040d-6c67-4c5b-b112-36a304b66dad`), bảng `tutorhub.auth_sso_connections` (`UNIQUE(tenant_id, id)`, `enforce_sso` chỉ bật khi `status='enabled'`, `auto_join_role` là `NULL` hoặc `'student'`) và `tutorhub.auth_sso_domains` (FK kép `(tenant_id, connection_id)` chống tham chiếu chéo tenant, `verification_token_hash` `32` byte chỉ tồn tại khi `pending` và tự xóa `NULL` khi `verified`/`disabled`, unique partial index `(domain) WHERE status='verified'` đảm bảo mỗi domain chỉ thuộc 1 tenant xác minh tại một thời điểm). `000050.down.sql` khóa độc quyền hai bảng và chặn rollback khi còn bất kỳ cấu hình nào (kể cả `disabled`). `ProvisionAuthFoundationRuntimeACL` cấp đúng các cột `UPDATE` trạng thái (`auth_sso_connections`: `status, allowed_org_ids, enforce_sso, auto_join_role, updated_at`; `auth_sso_domains`: `status, verification_token_hash, verified_at, updated_at`) và cấm `DELETE`.
- **Quản trị cấp Tenant, Xác minh DNS TXT & Chống tự khóa `org_admin`**: cụm endpoint `/api/v1/tenants/{tenant_id}/sso-connections` (`GET`, `POST`, `PATCH`) và `/sso-domains` (`GET`, `POST`, `rotate`, `verify`, `disable`) kiểm chặt `active_tenant_id`, role `org_admin` + `tenant.manage`, CSRF, recent-auth `10` phút và `AAL2` (khi `AUTH_REQUIRE_ADMIN_MFA=true` hoặc khi bật TOTP và tài khoản có TOTP/passkey đang hoạt động). Tạo/xoay domain trả `txt_name = "_tutorhub-challenge.<domain>."` và `txt_value = "tutorhub-site-verification=<token>"` một lần (`Cache-Control: no-store`); tra cứu DNS chạy ngoài transaction với hạn `5s`, tối đa `32` bản ghi × `512` byte và semaphore `32` slot rồi khóa đối chiếu lại HMAC trong transaction. Khi thay đổi làm domain của chính `org_admin` bắt đầu chịu `enforce_sso=true` hoặc đổi danh sách org trên domain đó, server đòi biên nhận đăng nhập SSO trường hợp lệ gắn đúng phiên hiện hành (TTL `10` phút, bảo toàn hạn gốc khi step-up xoay phiên); thiếu bằng chứng trả `409 auth_sso_lockout`.
- **Home Realm Discovery (`POST /api/v1/auth/identifier/discover`) & Thực thi `enforce_sso`**: khi `AUTH_SSO_DOMAINS_ENABLED=false` giữ nguyên `sso: { required: false }` và API quản trị trả `404`. Khi bật cờ, email thuộc domain `verified` + connection `enabled` + provider bật trả `{ required, available: true, provider, domain }` đồng nhất cho cả địa chỉ đã có lẫn chưa có tài khoản; `GET /api/v1/auth/oauth/{provider}/start` nhận tùy chọn `login_hint` + `domain` (truyền gợi ý `hd` cho Google sau tra cứu, vẫn xác thực độc lập bằng ID token đã ký ở callback). Với domain `enforce_sso=true`, đăng nhập mật khẩu/mã email, đăng ký và khôi phục mật khẩu bị từ chối đồng nhất `403 auth_sso_required`, đồng thời giữ khóa chia sẻ `pg_advisory_xact_lock_shared` ở biên commit để chặn cả giao dịch OTP/MFA mở trước lúc bật chính sách.
- **Chính sách OAuth Trường học & JIT Membership**: Google yêu cầu `email_verified=true` + `hd` thuộc `allowed_org_ids`; Entra yêu cầu `email` + `xms_edov=true` + issuer khớp `tid` trong `allowed_org_ids` (mã OTP nội bộ không thay thế bằng chứng tổ chức; kiểm lại chính sách sau bước MFA). Mặc định giữ chế độ mời (`auto_join_role = NULL`); khi bật `auto_join_role = 'student'`, kiểm tra hạn mức thành viên `RequireMemberCapacity` và chỉ chèn `student/active` khi chưa có membership (giữ nguyên role `org_admin`/`teacher` và không tự kích hoạt lại thành viên `suspended`/`removed`).
- **Audit & Đồng bộ hợp đồng**: đăng ký 6 action (`tenant.sso_connection.create|update`, `tenant.sso_domain.create|rotate|verify|disable`) kèm domain event, OpenAPI `AuditAction`, `@tutorhub/api-client` (`buildOAuthStartURL` hỗ trợ `loginHint`/`domain`) và nhãn song ngữ `vi`/`en` trên `AuditLogPage.tsx` (ghi đủ `succeeded`, `failed`, `denied` vào `audit_events` không chứa token/PII).
- **Bằng chứng Gatekeeper**: `pnpm api:generate` + `pnpm api:check` PASS, `go test -count=1 ./services/core-api/...` PASS (`39/39` package có test), `pnpm verify` PASS (`@tutorhub/api-client` `124/124`, `@tutorhub/web` `874/874`), `gofmt` và `git diff --check` sạch. Trên cụm **PostgreSQL 17.12 loopback disposable** tạo mới độc lập (tự dọn sạch DB, role, cluster và binary tạm), toàn bộ bộ SSO + hồi quy migration `000043..000050` + `TestAuthFoundationRuntimeExactACL` + `TestPaidPhonePostgresDelivery` đều **PASS (`0` skip, ledger `50 false`)**.
- **Lưu ý vận hành**: `AUTH_SSO_DOMAINS_ENABLED=false` mặc định. Trước khi deploy bản API này lên môi trường dùng chung cần chạy migration `000050` và `ProvisionAuthFoundationRuntimeACL`. Lát cắt kế tiếp `AUTH-12B` giao Claude Code triển khai giao diện quản trị School SSO, luồng HRD trên `/sign-in` và Playwright E2E.

### Checkpoint `AUTH-11` Adapter Zalo ZNS, SMS Brandname, Twilio & Khóa Ngân sách OTP (`DONE`, `AUTH-11b` giữ hoãn) — 2026-11-20

Codex triển khai trọn gói phía Server của `AUTH-11` (Phase 0 không có BLOCKER ngoài phạm vi, không thêm Go dependency mới, giữ nguyên schema `000049` và không đổi OpenAPI/Web UI; nhánh `AUTH-11b` tài khoản thuần SĐT không email tiếp tục hoãn theo `ADR-0039` §4.1); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng độc lập trên cụm PostgreSQL 17.12 loopback disposable tạo mới:

- **Ba Adapter HTTP chuẩn hóa (`services/core-api/internal/modules/identity/phone_providers.go`)**:
  - **Zalo ZNS (`ZaloZNSSender`, kênh `zalo_zns`, provider `zns`, chỉ `+84`)**: gửi `POST` JSON kèm header `access_token`, số dạng `84...`, `template_id`, `template_data[<otp_param>]` và `tracking_id`; chỉ chấp nhận HTTP `2xx` **kèm** trường JSON `error` kiểu số nguyên bằng `0`, trích `data.msg_id` (mọi lỗi nghiệp vụ như `-118` không có Zalo, thiếu/sai kiểu `error` đều quy về `ErrAuthDeliveryFailed` để kích hoạt fallback).
  - **SMS Brandname (`SMSBrandnameSender`, kênh `sms`, provider `sms_brandname`, `+84`)**: gửi `POST` JSON kèm `Authorization: Bearer <token>`, `from` (`1..11` ký tự ASCII theo Nghị định 91/2020/NĐ-CP), `message` (UTF-8 `<= 320` ký tự, không ký tự điều khiển, đúng 1 placeholder `{{code}}`) và `idempotency_key`; từ chối cả lỗi nghiệp vụ trong `2xx` (`status` là `failed`/`error` hoặc `error_code != 0`, kể cả ký hiệu khoa học).
  - **Twilio Programmable Messaging (`TwilioSMSSender`, kênh `sms`, provider `twilio`, phủ các mã vùng trong `AUTH_PHONE_ALLOWED_COUNTRIES`)**: gửi `POST` form-urlencoded với HTTP Basic Auth (`AC...` + token) tới `/2010-04-01/Accounts/{AccountSID}/Messages.json`, hỗ trợ cả số gửi E.164 lẫn `MessagingServiceSid` (`MG...`).
- **Gia cố HTTP, Chống SSRF sau DNS & Chống rò rỉ dữ liệu**: mỗi adapter dùng `http.Client` riêng với deadline `8s` (nằm trong trần `10s` của dispatcher), `req.GetBody = nil` (cấm tự phát lại POST trả phí), `Proxy = nil` (không đi qua biến môi trường proxy), chặn redirect (`http.ErrUseLastResponse`), giới hạn header/body phản hồi `16 KiB` kèm kiểm tra biên chunked EOF, và tắt keep-alive (`DisableKeepAlives: true`) để ngăn `net/http` ghi byte lạ trên kết nối nhàn rỗi ra logger tiến trình. Ở `staging`/`production` chỉ nhận `https://` công khai; `DialContext` kiểm tra toàn bộ IP sau phân giải DNS qua `config.PublicPhoneProviderIP` (chặn loopback, RFC1918, link-local, CGNAT, NAT64/6to4, multicast) rồi quay số thẳng tới IP literal đã kiểm tra trong khi vẫn giữ hostname cho SNI/xác minh chứng chỉ TLS (`development`/`test` chỉ mở thêm ngoại lệ tường minh `http://127.0.0.1:<port>` và `http://localhost:<port>`). Toàn bộ struct credential/HTTP/message/receipt che `[redacted]` trên `String`/`GoString`/`slog.LogValue`; DB chỉ lưu nhãn cố định và `provider_message_ref` đã băm HMAC `64` ký tự hex (`auth-delivery-ref-v1`).
- **Định tuyến đa quốc gia & Khóa ngân sách trả phí (`config`, `cmd/api`, `scripts/check-auth-delivery-guard.mjs`)**: cả `AUTH_PHONE_SENDER` lẫn `AUTH_PHONE_FALLBACK_SENDER` khi chọn provider trả phí đều bắt buộc `AUTH_DELIVERY_BUDGET_APPROVED=true` (so khớp chuỗi đúng `"true"`) ở mọi môi trường và yêu cầu primary trả phí khác tên. Kiểm tra độ phủ quốc gia ngay lúc khởi động: `VN,SG` với `zns → twilio` định tuyến `+84` ưu tiên ZNS rồi fallback Twilio và `+65` đi thẳng Twilio ngay lần đầu (1 lượt quota), trong khi `VN,SG` với `zns → sms_brandname` bị từ chối khởi động vì thiếu tuyến cho `+65`. `PhoneSenderRouter` bắt buộc giữ chỗ thêm 1 lượt ngân sách ngày trước mỗi lần thử fallback (`budget == nil || !budget.AllowDelivery(...)` → `fallback_quota_denied`).
- **Bằng chứng Gatekeeper**: `node --test scripts/check-auth-delivery-guard.test.mjs` **`9/9` PASS**, `go test -count=1 ./services/core-api/...` **PASS (`39/39` package có test)**, `pnpm verify` **PASS (`exit 0`)**, `gofmt` và `git diff --check` sạch. Trên cụm **PostgreSQL 17.12 loopback disposable** tạo mới độc lập dưới `ProvisionAuthFoundationRuntimeACL` (không đọc `.env*.local`, tự xóa DB, role, cluster và binary tạm sau khi chạy), `TestPaidPhonePostgresDelivery` **`3/3` subtest PASS (`0` skip)**: `primary` (`1` ZNS, `1` lượt quota, hàng `zalo_zns/zns/accepted` kèm HMAC ref `64` hex, OTP tạo được phiên), `fallback` (ZNS trả `error=-118` → `1` ZNS + `1` Brandname, `2` lượt quota, hàng `zns/failed/send_failed` + `sms_brandname/accepted`, OTP từ Brandname tạo được phiên) và `fallback_quota_denied` (`quota=1`, ZNS lỗi → Brandname nhận **`0` request**, ghi `fallback_quota_denied`).
- **Lưu ý vận hành**: `AUTH_METHOD_PHONE_ENABLED=false`, `AUTH_PHONE_SENDER="disabled"`, `AUTH_PHONE_FALLBACK_SENDER="disabled"` và `AUTH_DELIVERY_BUDGET_APPROVED=false` giữ mặc định tắt. Bằng chứng hiện tại xác nhận Core API, runtime ACL, quota PostgreSQL và HTTP mock; trước khi mở cờ thực tế cần tài khoản Zalo OA + mẫu ZNS được duyệt, Brandname đăng ký theo Nghị định 91/2020/NĐ-CP và smoke nhận tin trên thiết bị di động thật.

### Checkpoint `AUTH-10B` Giao diện SĐT `+84` OTP, Settings SĐT, Step-up `AAL2` & Playwright E2E (`AUTH-10` `DONE`) — 2026-11-20

Claude Code triển khai lát cắt Web UI + Playwright của `AUTH-10` (Phase 0 không phát hiện BLOCKER ngoài phạm vi; ghi nhận rõ 2 điều chỉnh hợp lý về query key `["security", "phone"]` và gọi lại `startMyPhone` sau step-up do phiên/`context_version` xoay làm hết hiệu lực giao dịch cũ); Gatekeeper (Antigravity) review diff/bảo mật, sửa 1 dòng `reducedMotion: "reduce"` cho ngữ cảnh thiết bị E2E và kiểm chứng độc lập trên Chromium thật + PostgreSQL disposable:

- **Tiện ích định dạng SĐT cục bộ (`apps/web/src/app/phone.ts`)**: giới hạn danh sách mã vùng theo `phone_allowed_countries` (mặc định `VN` `+84`), định dạng nhóm chữ số khi gõ (`0912 345 678` / `+84 912 345 678`), xóa dấu cách/gạch nối trước khi gửi để server (`nyaruka/phonenumbers`) giữ vai trò nguồn chuẩn hóa E.164 duy nhất, che số an toàn và không lưu SĐT/mã OTP vào `localStorage` hay `sessionStorage`.
- **Đăng nhập bằng SĐT trên `/sign-in` & `/account-link` (`SignInPhoneForm.tsx`)**: nút "Đăng nhập bằng số điện thoại" chỉ hiện khi `phone_enabled=true` (ẩn cả tab "Ghi nhớ tôi" khi ở chế độ SĐT để không bao giờ ghi SĐT xuống trình duyệt); hủy sạch Passkey Conditional UI chạy nền khi chuyển sang SĐT hoặc gửi OTP; bước nhập mã hiển thị số đã che, đếm ngược `5` phút, gửi lại sau `60` giây, nút đổi số và xóa mã khỏi state ngay khi submit. Khi server trả `mfa_required`, chuyển sang bước 2 dùng chung (`SignInMfaStep`) cho cả tài khoản TOTP lẫn tài khoản chỉ có passkey; trên `/account-link` ở lại trang để xác nhận liên kết; bao phủ thông báo trung tính song ngữ `vi`/`en` cho `400`/`401`/`403 auth_phone_stale`/`403 auth_phone_second_factor_required`/`429`/`503`.
- **Settings (`/app/settings` — `PhoneSettingsCard.tsx` & `ProfileSettingsPage.tsx`)**: hỗ trợ thêm, đổi, xác minh lại (khi `stale=true` quá `60` ngày) và gỡ SĐT có hộp thoại xác nhận; nối hộp step-up dùng chung chạy lại đúng 1 lần (`requireAal2: false` với `403 auth_recent_authentication_required`, `requireAal2: true` với `403 auth_assurance_required`; nếu chạm step-up ở bước `verify`/`resend` thì tự khởi tạo lại `startMyPhone` vì step-up đã xoay phiên và thay `auth_tx`); xử lý `409 auth_phone_in_use` và `409 auth_last_identity`. Thẻ Bảo mật hiển thị trạng thái SĐT/cảnh báo `stale`, mở nút hủy liên kết identity federated cuối cùng khi tài khoản đã có SĐT `active` không `stale`, và gắn nhãn `phone_otp` ("Mã SĐT" / "Phone code") trong danh sách phiên đăng nhập; SĐT không bao giờ xuất hiện trong `StepUpAuthDialog` hay `SignInMfaStep`.
- **Gatekeeper sửa nhỏ cho E2E**: bổ sung `contextOptions.reducedMotion: "reduce"` vào `playwright.auth.config.ts` và `reducedMotion: "reduce"` vào hàm `openDevice` (`browser.newContext` thủ công trong `e2e/auth-04-device-sessions-stepup.spec.ts` và `e2e/auth-10-phone-otp.spec.ts`) để tránh Axe chụp trúng khung chuyển tiếp `opacity` của `@keyframes th-dialog-in` khi kiểm tra `color-contrast` trên `StepUpAuthDialog`.
- **Bằng chứng Gatekeeper**: `pnpm verify` (`874/874` web unit tests trên `95` file, `@tutorhub/api-client` `122/122`, `e2e:infra:test` `18/18`, `security:test` `86/86`, `security:bundle` `209` file, Storybook build, `39` package Go) và `git diff --check` đều PASS. Trên Chromium thật + PostgreSQL disposable từ xa (migration `49`, xóa sạch DB sau mỗi lần chạy): Phase A native-auth `auth-02..10` **`31/31` PASS** — gồm `auth-10-phone-otp.spec.ts` ×4 (thêm số di động + từ chối số bàn `024` + đăng nhập SMS kèm `keep_signed_in`, tài khoản chỉ có passkey bắt buộc hoàn tất passkey sau OTP SĐT + phiên `AAL1` nâng `AAL2` khi gỡ số, tài khoản TOTP đăng nhập bằng SĐT trên `/sign-in` và `/account-link` + chặn xóa phương thức cuối cùng trước khi gỡ số, kiểm tra a11y Axe `0` serious/critical + phản hồi trung tính chống dò số + Web Storage sạch không lưu SĐT/OTP) và `auth-02..09` `27/27` PASS — cùng Phase B hồi quy OIDC mặc định `p2-08` **`1/1` PASS (`4,2` phút)**.
- **Lưu ý vận hành**: `AUTH_METHOD_PHONE_ENABLED=false` và `AUTH_PHONE_SENDER="disabled"` giữ mặc định tắt trên kho/production. Gói tiếp theo `AUTH-11` triển khai các adapter HTTP gửi thật (Zalo ZNS + SMS Brandname + Twilio) và nối chuỗi fallback `+84` dưới cổng ngân sách `AUTH_DELIVERY_BUDGET_APPROVED`; nhánh tài khoản thuần SĐT không email (`AUTH-11b`) giữ hoãn theo `ADR-0039` §4.1.

### Checkpoint `AUTH-10A` Phone OTP Lõi phía Server, Chuẩn hóa E.164 `+84`, Anti-Pumping Quota/Breaker & SIM Reclaim (`000049`) `DONE` — 2026-11-20

Codex triển khai lát cắt Server + Migration + OpenAPI + `@tutorhub/api-client` của `AUTH-10` (Phase 0 dừng đúng quy trình để Antigravity chốt 3 quyết định về bắt buộc MFA với tài khoản chỉ có passkey, giữ bất biến `verified_at` trong runtime ACL và đường dẫn `services/core-api/go.mod`); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng độc lập trên PostgreSQL disposable từ xa:

- **Migration `000049_phone_numbers` & Exact Runtime ACL**: thêm bảng `tutorhub.user_phone_numbers` (tối đa 1 số `active` trên toàn hệ thống theo `e164_lookup_hash` và 1 số `active` mỗi tài khoản, `masked_number` bắt buộc có `*`, xóa `e164_ciphertext = NULL` ngay khi chuyển `released` hoặc `removed`) và cột `auth_challenges.phone_country_code` (phục vụ tính quota/breaker sau khi xóa payload mã hóa). `000049.down` chặn rollback trên đủ 4 nguồn (`user_phone_numbers`, `identities` loại `phone`, `auth_transactions` loại `phone`, `auth_challenges` mục đích `phone_login`/`phone_verify`). `ProvisionAuthFoundationRuntimeACL` chỉ cấp `UPDATE` đúng 7 cột trạng thái (`e164_ciphertext, status, release_reason, last_used_at, released_at, removed_at, updated_at`), giữ bất biến `verified_at`/`user_id`/`identity_id`/`e164_lookup_hash` và cấm `DELETE`.
- **Chuẩn hóa E.164, mã hóa purpose-bound & Router kênh gửi**: dùng `github.com/nyaruka/phonenumbers v1.8.1` (MIT + metadata Apache-2.0), nhận `+84...`, `84...`, `0...`, không tin `country_code` client khi chuỗi có `+`, chỉ cho phép vùng trong `AUTH_PHONE_ALLOWED_COUNTRIES` và loại số `MOBILE`/`FIXED_LINE_OR_MOBILE` (từ chối số bàn `024`/`028`, tổng đài `1800`/`1900`, premium, VoIP). SĐT tra cứu bằng HMAC `phone-lookup-v1`, bọc AES-GCM khóa riêng `phone_number` với AAD `"phone_number:v1:<user_id>:<phone_id>"` và redact toàn diện trong log. `PhoneSenderRouter` định tuyến theo calling code (hỗ trợ chuỗi fallback có kiểm soát ngân sách và ghi nhận từng lần thử trong `auth_challenge_deliveries`); `AUTH_METHOD_PHONE_ENABLED=false` và `AUTH_PHONE_SENDER="disabled"` mặc định, các sender trả phí chưa nối ở `AUTH-10A` fail-fast ngay lúc khởi động.
- **Anti-Pumping đa tầng, Hạn mức ngày & Cầu dao tỷ lệ chuyển đổi**: kết hợp giới hạn gửi/xác minh theo IP (`20/1h`, `50/15m`), theo đích E.164 (`5/1h` — luồng đăng nhập vượt ngưỡng vẫn trả `202` đồng nhất kèm challenge mồi không gửi để chống dò số), theo quốc gia (`AUTH_PHONE_COUNTRY_HOURLY_QUOTA=100` → `503`), hạn mức ngày toàn cục tuần tự hóa đa replica (`AUTH_PHONE_OTP_DAILY_QUOTA=50` → `503`) và cầu dao trượt 1 giờ theo quốc gia (`>= 50` challenge gửi thật mà tỷ lệ tiêu thụ `< 30%` → `503 auth_phone_delivery_unavailable`). Challenge điện thoại giữ TTL `5` phút, tối đa `5` lần thử, gửi lại sau `60` giây.
- **Chính sách `ADR-0039` §4.3 (Passkey/TOTP MFA), SIM Stale (`60` ngày) & SIM Reclaim (`90` ngày)**:
  - Phone OTP chỉ là yếu tố bậc 1 (`AAL1`, không vào `reauth_methods` hay phương thức MFA bậc 2). Tài khoản có TOTP `active` **hoặc** Passkey `active` không bao giờ nhận phiên `AAL1` thẳng từ Phone OTP: nếu bước MFA khả dụng trả `200 { status: "mfa_required", methods }` (kể cả tài khoản chỉ có passkey → `methods: ["passkey"]`, bắt buộc `UV=true`, kiểm lại độ mới và trạng thái số dưới khóa trước khi cấp `AAL2` và cập nhật `last_used_at`); nếu yếu tố đã đăng ký không khả dụng thì tiêu thụ giao dịch, xóa cookie và từ chối `403 auth_phone_second_factor_required`.
  - Số quá `60` ngày (`AUTH_PHONE_STALE_AFTER`) khi tài khoản còn phương thức khác bị từ chối đăng nhập đơn lẻ (`403 auth_phone_stale`); chủ hiện tại xác minh lại cùng số trong Settings cập nhật `last_used_at` (giữ nguyên `verified_at` ban đầu) và xóa cờ `stale`.
  - Số quá `90` ngày (`AUTH_PHONE_RECLAIM_AFTER`) cho phép người mới chứng minh OTP để gắn vào chính tài khoản của họ (khóa tuần tự chống deadlock): bản ghi cũ chuyển `released`/`reassigned` (`e164_ciphertext=NULL`), hủy liên kết identity `phone` cũ, thu hồi các phiên mở từ identity đó và gửi đúng 1 thông báo `phone_released` tới email chủ cũ mà không cấp quyền vào tài khoản cũ. Bộ đếm phương thức đăng nhập cuối cùng (`requireAnotherLoginMethod`) xét đúng khả năng đăng nhập thực tế của SĐT, mật khẩu/mã email và passkey.
- **OpenAPI & `@tutorhub/api-client`**: bổ sung 6 endpoint (`/api/v1/auth/phone/start|verify`, `/api/v1/me/phone`, `/api/v1/me/phone/start|verify`, `DELETE /api/v1/me/phone`) + hỗ trợ gửi lại qua `/api/v1/auth/challenges/resend`, giữ nguyên `GET /api/v1/me` (`User.email` bắt buộc), tự xoay CSRF trên mutation Settings và xóa cache CSRF khi tạo/xoay phiên.
- **Bằng chứng Gatekeeper**: PostgreSQL disposable từ xa (`24/24` test cấp cao cho phone + exact runtime ACL, `54/54` kết quả kể cả subtest, cộng với `56/56` test cấp cao / `73/73` kết quả trên cụm PG 16.4 cục bộ của worker, `0` FAIL, `0` SKIP, ledger `49 false`), Go unit không cache toàn kho (`1.024` test cấp cao / `2.256` kết quả), `pnpm api:check`, `pnpm verify` (`@tutorhub/api-client` `122/122`, `@tutorhub/web` `829/829`, `209` file bundle security) và `git diff --check` đều PASS.
- **Lưu ý vận hành**: `AUTH_METHOD_PHONE_ENABLED=false` và `AUTH_PHONE_SENDER="disabled"` mặc định. Trước khi deploy bản API mới lên môi trường dùng chung cần chạy migration `000049` và `ProvisionAuthFoundationRuntimeACL`. Adapter HTTP thật cho Zalo ZNS / SMS Brandname thuộc `AUTH-11`.

### Checkpoint `AUTH-09B` Giao diện TOTP QR nội bộ, Bước xác minh 2 lớp, Step-up `AAL2` & Gỡ kẹt `org_admin` (`AUTH-09` `DONE`) — 2026-11-20

Claude Code triển khai lát cắt Web UI + Playwright của `AUTH-09` (Phase 0 phát hiện tác động lên test xóa phương thức cuối cùng của `auth-08` khi bật `AUTH_MFA_TOTP_ENABLED=true` và 2 dòng cấu hình Playwright — đều đã được chủ dự án duyệt mở rộng phạm vi tối thiểu); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng trực tiếp toàn bộ stack trên Chromium thật + PostgreSQL disposable:

- **Encoder QR SVG nội bộ (`apps/web/src/app/totpQr.ts`)**: tự cài đặt chuẩn ISO/IEC 18004 (byte mode, Version `1..20` tới `858` byte, Reed-Solomon GF(256), ECC `M`/`L`, BCH(15,5) & BCH(18,6), chọn mask tối thiểu hóa điểm phạt `N1..N4`, quiet zone 4 module) trực tiếp bằng TypeScript không thêm npm dependency và không gửi `otpauth_uri`/seed ra ngoài; `<svg>` dùng `aria-label` chung không lộ khóa bí mật; chuỗi vượt ngưỡng fallback sang khóa thiết lập chia nhóm 4 ký tự.
- **Bước xác minh hai lớp dùng chung (`SignInMfaStep.tsx`)**: xử lý đồng nhất `mfa_required` trên đăng nhập mật khẩu, mã email, passkey `UV=false` (hủy sạch Conditional UI nền trước khi sang bước 2), `/sign-in?mfa=pending` sau callback OAuth (bảo toàn `return_to`), `/account-link` (ở lại trang để xác nhận liên kết sau bước 2) và hộp thoại khôi phục mật khẩu (nhận `password_reset` thì đóng hộp thoại, báo thành công, không tự đăng nhập); hỗ trợ đổi qua lại TOTP 6 số, mã khôi phục và passkey (khi tài khoản có passkey), đếm ngược hết hạn và tôn trọng `Retry-After` trên `429`.
- **Hộp thoại `StepUpAuthDialog` & chính sách `requireAal2`**: chuyển sang đọc `AccountSecurityResponse`, hoạt động khi bật `device_management_enabled` hoặc `mfa_totp_enabled`, thứ tự ưu tiên `["passkey","totp","password","email_code"]`; khi gặp `403 auth_assurance_required` hoặc `403 auth_admin_mfa_required` tự bật chế độ `requireAal2` (chỉ cho phép `passkey` hoặc `totp`, nếu chưa có yếu tố nào thì hiện hướng dẫn dẫn tới `/app/settings`).
- **Settings (`/app/settings` — `TwoFactorSettingsCard.tsx`) & Gỡ kẹt `org_admin`**: thẻ "Ứng dụng xác thực" hỗ trợ enroll QR + sao chép khóa chia nhóm, xác nhận mã 6 số, hộp thoại 10 mã khôi phục hiển thị đúng một lần (sao chép, tải `.txt` bằng `Blob` cục bộ + `revokeObjectURL`, chặn `Escape`/click ngoài cho tới khi tick xác nhận đã lưu), cảnh báo khi còn `<= 3` hoặc `0` mã, tạo lại mã và tắt TOTP (xử lý êm `409 auth_mfa_conflict`). Khi `active_tenant === null`, route `/app/settings` vẫn mở được để `org_admin` thiết lập 2FA; trang chọn/tạo workspace bổ sung `AdminMfaRequiredNotice` cho phép xác minh `AAL2` ngay tại chỗ rồi tự chạy lại thao tác một lần hoặc sang `/app/settings`. Các luồng thêm/xóa passkey, đặt mật khẩu lần đầu và liên kết danh tính cũng nối `auth_assurance_required` vào step-up `AAL2`.
- **Bằng chứng Gatekeeper**: `pnpm verify` (`829/829` web unit tests trên `90` file, `@tutorhub/api-client` `119/119`, `e2e:infra:test` `18/18`, `security:test` `86/86`, `security:bundle` `209` file, Storybook build, `39` package Go) và `git diff --check` đều PASS. Trên Chromium thật + PostgreSQL disposable (migration `48`, xóa sạch DB sau mỗi lần chạy): Phase A native-auth gộp `auth-02..09` **`27/27` PASS (`15,2` phút)** — gồm `auth-08` ×3 đã cập nhật bước step-up `AAL2` và `auth-09-mfa-totp.spec.ts` ×4 (enroll QR + confirm + 10 mã khôi phục một lần, đăng nhập mật khẩu `mfa_required` + chặn replay cùng bước + tiêu thụ mã khôi phục, khôi phục mật khẩu hoãn qua MFA không tự đăng nhập, `org_admin` bị chặn bởi `AUTH_REQUIRE_ADMIN_MFA` tự gỡ kẹt bằng step-up/Settings, tạo lại mã + tắt TOTP, Axe `0` serious/critical, Web Storage không rò secret/code) — và Phase B hồi quy OIDC mặc định `p2-08` **`1/1` PASS (`4,4` phút)**.
- **Lưu ý vận hành**: `AUTH_MFA_TOTP_ENABLED` và `AUTH_REQUIRE_ADMIN_MFA` giữ mặc định `false` trên kho/production cho tới đợt mở cờ có kiểm soát.

### Checkpoint `AUTH-09A` TOTP 2FA, Mã khôi phục, Giao dịch `mfa_required` & Chính sách AAL2 (`000048`) `DONE` — 2026-11-20

Codex triển khai lát cắt Server + Migration + OpenAPI của `AUTH-09` (Phase 0 xin ý kiến chủ dự án về khóa thao tác đổi yếu tố khi đã có yếu tố khôi phục và đã được chốt); Gatekeeper (Antigravity) review diff/bảo mật, tối ưu bộ giới hạn tốc độ trượt MFA trên PostgreSQL từ xa và kiểm chứng độc lập trên DB disposable:

- **Migration `000048_mfa_totp_recovery` & Exact Runtime ACL**: thêm bảng `mfa_totp_factors` (tối đa 1 `active` và 1 `pending` mỗi tài khoản, `secret_ciphertext >= 29` byte, kẹp đơn điệu mốc thời gian bằng `GREATEST(...)`) và `mfa_recovery_codes` (`code_hash` HMAC-SHA256 `32` byte, 10 mã 80-bit Crockford mỗi lô). `000048.down` chặn rollback khi còn yếu tố TOTP, mã khôi phục hoặc giao dịch `auth_transactions` loại `mfa`. `ProvisionAuthFoundationRuntimeACL` chỉ cấp `UPDATE` các cột trạng thái/bộ đếm (`mfa_totp_factors`: `status, last_used_step, confirmed_at, disabled_at, updated_at`; `mfa_recovery_codes`: `consumed_at, invalidated_at`), giữ bất biến `sessions.assurance_level` và cấm `DELETE` trên toàn bộ bảng xác thực.
- **Bảo vệ seed, chống phát lại & giới hạn tốc độ trượt**: seed TOTP 160-bit (`github.com/pquerna/otp v1.5.0`, Apache-2.0) được bọc AES-GCM bằng khóa dẫn xuất riêng theo mục đích và AAD `"totp_secret:v1:<user_id>:<factor_id>"`. Mọi lần xác minh (`ConfirmTOTP`, `VerifyMFA`, `Reauthenticate` với `totp`) dùng chung khóa `FOR UPDATE` và ràng buộc đơn điệu `step > last_used_step` trong cửa sổ `±1` bước 30 giây, chặn phát lại chéo giao dịch. Giới hạn tốc độ trượt `5` lần/`300s` theo tài khoản và `20` lần/`300s` theo IP (`AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER` chỉ nhân nhánh `.ip` khi `APP_ENV=test`).
- **Luồng `mfa_required` toàn diện & Khôi phục mật khẩu an toàn**: đăng nhập mật khẩu, mã email, passkey `UV=false`, callback OAuth (`google`/`facebook`/`microsoft`) và `/auth/callback` khi tài khoản có TOTP `active` đều dừng trước khi tạo phiên, phát cookie `auth_tx` (`kind='mfa'`, TTL 5 phút, bảo toàn `keep_signed_in`, `return_to`, `identity_id` và làm mới email đã xác minh của provider); passkey `UV=true` cấp thẳng `AAL2`. Khôi phục mật khẩu khi tài khoản có TOTP hoặc passkey `active` hoãn cập nhật hash trong payload mã hóa cho tới khi `VerifyMFA` thành công mới ghi mật khẩu mới, thu hồi mọi phiên, xóa cookie và trả `password_reset` (không tự đăng nhập).
- **Quyết định chủ dự án — Bảo vệ thay đổi yếu tố ở mức `AAL2` & `AUTH_REQUIRE_ADMIN_MFA`**: khi `AUTH_MFA_TOTP_ENABLED=true` và tài khoản đã có TOTP `active` hoặc passkey `active`, phiên `AAL1` bị từ chối (`403 auth_assurance_required`) khi đăng ký thêm/thu hồi passkey (kiểm ở cả bước options lẫn finish), enroll/confirm/disable TOTP, sinh lại mã khôi phục, tạo mật khẩu lần đầu và liên kết/step-up federated; đổi tên passkey giữ recent-auth. `AUTH_REQUIRE_ADMIN_MFA` (mặc định `false`) chặn phiên `AAL1` chọn/tạo/truy cập tenant `org_admin` (`403 auth_admin_mfa_required`), ẩn `active_tenant`/`permissions` của `org_admin` trên `/api/v1/me` nhưng vẫn cho phép gọi `/me`, cài đặt bảo mật, step-up và đăng xuất.
- **Tương thích client & Tối ưu Gatekeeper**: `@tutorhub/api-client` bổ sung `AccountSecurityResponse` (giữ `MySecurityResponse` tương thích ngược cho web hiện tại trước `AUTH-09B`) và chỉ xóa cache CSRF sau khi xác minh MFA/TOTP thành công. Gatekeeper gộp `pg_advisory_xact_lock` + CTE kiểm tra/ghi cửa sổ trượt bằng `pgx.Batch` trong `mfa_rate_limiter.go` để không giữ khóa tư vấn qua nhiều lượt khứ hồi mạng tuần tự trên kết nối TLS từ xa.
- **Bằng chứng Gatekeeper**: PostgreSQL disposable (`identity` `164/164` test cấp cao + `httpapi` `245/245` test cấp cao, `0` FAIL, `0` SKIP, ledger `48 false`) xác nhận round-trip `48↔47↔48`, 3 nhánh rollback preflight, exact runtime ACL, chống replay đồng thời 2 giao dịch TOTP, passkey recovery + chặn race đăng ký passkey song song, sàn `AAL2` cấp tài khoản khi phiên cũ hết hạn, `AUTH_REQUIRE_ADMIN_MFA` và burst giới hạn tốc độ trượt; `pnpm api:check`, `pnpm verify` (`782/782` web, `119/119` api-client, `209` bundle files, toàn bộ package Go) và `git diff --check` đều PASS.
- **Lưu ý vận hành**: cả `AUTH_MFA_TOTP_ENABLED` và `AUTH_REQUIRE_ADMIN_MFA` giữ mặc định `false`. Trước khi deploy bản API này hoặc bật cờ trên môi trường dùng chung cần chạy migration `000048` và `ProvisionAuthFoundationRuntimeACL`. Seed TOTP gắn với khóa dẫn xuất từ `SESSION_SECRET`; khi xoay `SESSION_SECRET` sau này bắt buộc có bước giải mã – mã hóa lại `mfa_totp_factors.secret_ciphertext`.

### Checkpoint `AUTH-08B` Giao diện Passkeys, Conditional UI, Signal API & CDP Virtual Authenticator (`AUTH-08` `DONE`) — 2026-11-19

Claude Code triển khai lát cắt Web UI + Playwright của `AUTH-08` (Phase 0 không có BLOCKER, không thêm npm dependency); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng toàn bộ stack trên Chromium thật + PostgreSQL disposable:

- **Module WebAuthn dùng chung (`apps/web/src/app/passkeys.ts`)**: ưu tiên `PublicKeyCredential.parse*FromJSON` / `toJSON()` (có kiểm tra shape chống extension ghi đè), kèm bộ chuyển đổi base64url thuần TypeScript làm fallback; gợi ý tên mặc định thô theo allowlist nền tảng ("Windows · Chrome", sửa được `1..80` ký tự); gọi `PublicKeyCredential.signalUnknownCredential` **chỉ khi** server trả `401 auth_invalid_passkey` kèm `unknown_credential === true` (nuốt lỗi êm, không gọi khi hủy/mạng lỗi/credential đã thu hồi).
- **`/sign-in` & `/account-link`**: ô email gắn `autocomplete="username webauthn"`; Conditional UI chạy nền với một `AbortController` dùng chung (tự hủy sạch trước khi mở modal bấm nút, đổi tab đăng ký/mã email/khôi phục hoặc rời trang); nút "Đăng nhập bằng Passkey" tôn trọng "Duy trì đăng nhập"; trên `/account-link` ở lại trang xác nhận liên kết; đóng hộp thoại hệ điều hành (`NotAllowedError`) xử lý êm không hiện banner đỏ.
- **`StepUpAuthDialog` & `queryClient.ts`**: nâng thứ tự ưu tiên thành `["passkey","password","email_code"]`; thêm `auth_invalid_passkey` vào `credentialRejectionCodes` để step-up thất bại không xóa phiên; chuyển từ passkey sang mã email giữa chừng hoạt động trơn tru; phiên `AAL2` (chỉ còn `["passkey"]`) hiển thị giải thích rõ.
- **Settings (`/app/settings`)**: thẻ Bảo mật & Phương thức đăng nhập hiển thị `passkey_count`; thẻ **Passkeys** mới hỗ trợ danh sách (huy hiệu "Đã đồng bộ" / "Trên thiết bị", ngày tạo, lần dùng gần nhất), thêm/đổi tên/xóa có xác nhận, nối step-up dùng chung khi gặp `403 auth_recent_authentication_required`, xử lý `409 auth_passkey_exists` và `409 auth_last_identity`; mở nút hủy liên kết identity duy nhất khi tài khoản đã có passkey (khớp đếm hai chiều phía server).
- **Cấu hình `localhost` cho E2E `--native-auth`**: chỉ stack `--native-auth` chuyển `PUBLIC_WEB_ORIGIN=http://localhost:5173` và `PUBLIC_API_ORIGIN=http://localhost:8080` kèm `WEBAUTHN_RP_ID=localhost` (do WebAuthn từ chối địa chỉ IP); stack mặc định `pnpm e2e` giữ nguyên `127.0.0.1`.
- **Bằng chứng Gatekeeper**: Playwright Chromium trên DB disposable (migration `47`, xóa sạch sau khi chạy): native auth gộp `auth-02..08` **`23/23` PASS (`11,0` phút)** — gồm cả cross-site callback `127.0.0.1:909x → localhost:8080` cho Google/Facebook/Microsoft và `auth-08-passkeys.spec.ts` ×3 bao phủ đủ 7 kịch bản với CDP virtual authenticator (đăng ký + step-up + đổi tên, step-up AAL1 hủy êm → chuyển sang mã email, đăng nhập nút bấm hủy autofill nền + `keep_signed_in`, step-up AAL2 xoay cookie, autofill tự đăng nhập, gỡ Google khi có passkey + chặn xóa phương thức cuối cùng + xóa khi có mật khẩu + từ chối credential đã thu hồi không gọi Signal, xóa credential lạ qua Signal API, Axe `0` serious/critical, Web Storage sạch) — và hồi quy OIDC `p2-08` **`1/1` PASS (`4,9` phút)**; `pnpm verify` (`782/782` web unit tests, `209` file bundle security) PASS.
- **Giới hạn đã ghi nhận**: nếu bật `AUTH_PASSKEYS_ENABLED` mà tắt `AUTH_DEVICE_MANAGEMENT_ENABLED` thì step-up tại chỗ không dùng được passkey (giữ fallback đăng nhập lại vì `/me/security` phụ thuộc cờ device management); nút hủy liên kết identity duy nhất phía client hiện mở theo passkey, chưa mở theo mật khẩu (có thể đồng bộ thêm ở gói tinh chỉnh sau). `AUTH_PASSKEYS_ENABLED` giữ mặc định `false`.

### Checkpoint `AUTH-08A` Passkeys / WebAuthn phía Server (`000047`, `go-webauthn v0.18.2`) `DONE` — 2026-11-19

Codex triển khai lát cắt Server + Migration + OpenAPI của `AUTH-08` (Phase 0 không có BLOCKER); Gatekeeper (Antigravity) review diff/bảo mật, bổ sung kẹp đơn điệu mốc thời gian chống lệch đồng hồ đa replica và kiểm chứng trên PostgreSQL disposable thật:

- **Migration `000047_webauthn_credentials` & Exact Runtime ACL**: thêm `users.webauthn_user_handle` (`64` byte ngẫu nhiên, gán một lần khi `IS NULL`) và bảng `webauthn_credentials` (nguồn dữ liệu duy nhất cho passkey; không nhân đôi sang `identities`). `ProvisionAuthFoundationRuntimeACL` chỉ cấp `UPDATE` đúng các cột trạng thái/bộ đếm, cấm `DELETE` và cấm sửa khóa công khai/chủ sở hữu. `000047.down` chặn rollback khi còn handle, credential hoặc giao dịch `passkey`.
- **Ceremony & Assertion (`go-webauthn v0.18.2`, BSD-3-Clause)**: `SessionData` lưu mã hóa trong `auth_transactions` (TTL `5` phút, tiêu thụ một lần dưới `FOR UPDATE`, xóa payload khi hoàn thành); đăng ký và step-up ràng buộc đúng `user_id`, `session_id` và `context_version`; chuyển từ passkey sang mã email giữa phiên step-up tự tạo giao dịch email mới. Assertion kiểm challenge/Origin/RP ID/chữ ký: `UV=true` tạo `AAL2`, `UV=false` tạo `AAL1`; counter `0 → 0` hợp lệ với passkey đồng bộ; phát hiện clone lưu bền vững `clone_warning=true` + outbox `passkey_clone_detected` và từ chối phiên; chỉ trả `unknown_credential=true` khi ceremony hợp lệ và DB xác nhận credential ID chưa từng tồn tại.
- **Step-up & Quản lý phương thức cuối cùng**: step-up passkey tạo phiên thay thế `assurance_level=2` (giữ bất biến `sessions.assurance_level` trong runtime ACL), thu hồi phiên cũ, xoay cả session token và CSRF, bảo toàn thời hạn/persistence và áp cap `org_admin` `24` giờ. Gỡ identity và thu hồi passkey kiểm tra hai chiều, không tính passkey đã bị cảnh báo clone hoặc khi cờ `AUTH_PASSKEYS_ENABLED=false`.
- **Gatekeeper sửa**: kẹp đơn điệu `GREATEST(...)` cho `last_used_at`, `revoked_at`, `updated_at` trong `postgres_passkeys.go` để tránh vi phạm CHECK thứ tự thời gian của `000047` khi đồng hồ giữa các container lệch vài mili-giây.
- **Bằng chứng Gatekeeper**: PostgreSQL disposable (`8/8` test cấp cao, `10/10` bản ghi kể cả subtest, `0` FAIL/SKIP, ledger `47 false`) xác nhận round-trip `46↔47`, 3 nhánh rollback preflight, exact ACL, đăng ký/đăng nhập/step-up dưới runtime role, hai verify đồng thời cùng challenge chỉ tạo một phiên và chặn gỡ phương thức cuối cùng; Go unit không cache, `pnpm api:check`, `pnpm verify` (`736/736` web, `113/113` api-client) và `git diff --check` đều PASS.
- **Lưu ý triển khai & lát cắt kế tiếp**: `AUTH_PASSKEYS_ENABLED=false` mặc định; khi deploy bản API này lên môi trường có DB cần chạy migration `000047` + provision runtime ACL trước vì `/me/security` và `/me/login-methods` truy vấn `passkey_count`. Với E2E `AUTH-08B`, WebAuthn bắt buộc RP ID là tên miền (`localhost`), không nhận địa chỉ IP (`127.0.0.1`).

### Checkpoint `AUTH-07B` Giao diện Microsoft, Đăng ký chờ/Liên kết & Fake Microsoft E2E (`AUTH-07` `DONE`) — 2026-11-19

Claude Code triển khai lát cắt Web UI + Playwright của `AUTH-07` (Phase 0 không có BLOCKER server/fake; phát hiện BLOCKER ngân sách `SignupIP` khi chạy gộp `auth-02..07`); Gatekeeper (Antigravity) review diff, chốt hướng xử lý giới hạn theo IP chỉ dành cho `APP_ENV=test` và kiểm chứng trên Chromium thật + PostgreSQL disposable:

- **`/sign-in`**: nút "Đăng nhập bằng Microsoft" / "Sign in with Microsoft" chỉ hiển thị khi `oauth_providers` có `microsoft` (không fallback OIDC, ẩn khi đang tải), điều hướng toàn trang tới `/api/v1/auth/oauth/microsoft/start`, logo 4 ô vẽ trong mã theo theme sáng, thứ tự Facebook → Google → Microsoft. Chuyển `?registration=pending&provider=microsoft` → `/complete-registration` và `?link=pending&provider=microsoft` → `/account-link`. Khi đang đăng nhập mà callback trả `?error=email_unverified` → chuyển sang `/app/settings?identity=email_unverified` (không dùng Web Storage).
- **`/complete-registration` & `/account-link`**: hỗ trợ đầy đủ provider `microsoft`; `/account-link` chuyển sang `getOAuthLinkIntent()` (hỗ trợ cả `microsoft` mà không đổi api-client).
- **Settings**: nút "Liên kết Microsoft" dùng chung hook `useLinkProviderIdentity` và hộp step-up dùng chung khi gặp `403 auth_recent_authentication_required` (chạy lại đúng 1 lần); hộp hướng dẫn khi gặp `?identity=email_unverified` kèm nút "Đăng xuất" về `/sign-in`; nhãn Microsoft từ `provider_slug`. Microsoft không xuất hiện trong hộp step-up.
- **Fake Microsoft** (`e2e-oidc` flavor `microsoft`, cổng `9094`): discovery theo mode `v2.0`, JWKS cùng origin kèm `issuer` từng khóa, ID token ký RS256 với `iss` canonical theo `tid`, `tid`/`oid`, `email`/`xms_edov` tùy chọn; chỉ `--native-auth` bật `AUTH_PROVIDER_MICROSOFT_ENABLED=true` với secret sinh mới mỗi lần chạy.
- **Quyết định Gatekeeper cho ngân sách rate limit E2E**: thêm biến `AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER` (`1..100`, mặc định `1`, **bắt buộc `APP_ENV=test`** — startup lỗi trên `development`/`staging`/`production`), chỉ nhân các bucket `*.ip` (giữ nguyên hạn mức theo đích/user/hạn mức gửi hằng ngày); `--native-auth` đặt `10` để chạy gộp `auth-02..07` và các gói sau trong một lần chạy. Kèm unit test config, policy và `scripts/e2e-local.test.mjs`.
- **Bằng chứng Gatekeeper**: Playwright Chromium trên DB disposable (migration `46`, xóa sạch sau khi chạy): native auth **`20/20` PASS (`7,5` phút)** gồm `auth-07-microsoft.spec.ts` ×6 (work `xms_edov=true`, MSA thiếu `xms_edov` → OTP, email trùng → `/account-link`, hai tenant cùng email không tự gộp, liên kết từ Settings sau step-up + hướng dẫn `email_unverified`, Axe + Web Storage sạch) và hồi quy OIDC `p2-08` **`1/1` PASS (`4,8` phút)**; Go `e2eoidc` (`21` test gồm `7` test tương thích `NewMicrosoftProvider`), Go `config`/`httpapi`/`cmd/api`, `scripts/e2e-local.test.mjs` (`18/18`) và toàn kho `pnpm verify` (`736/736` web unit tests, `107/107` api-client, `39` package Go) đều PASS.
- **Còn mở trước rollout Microsoft (cờ vẫn tắt)**: đối chiếu nút với bộ asset "Sign in with Microsoft" chính thức, hoàn tất Entra App Registration + optional claim `xms_edov` + publisher verification và smoke tài khoản công việc/MSA thật.

### Checkpoint `AUTH-07A` Microsoft Sign-in phía Server (Entra ID multi-tenant + MSA) `DONE` — 2026-11-19

Codex triển khai (Phase 0 không có BLOCKER, giữ migration `46`); Gatekeeper (Antigravity) review diff/bảo mật, sửa một điểm nhỏ và chạy cổng PostgreSQL disposable độc lập:

- **Xác minh token**: RS256 qua key set riêng giữ trường `issuer` của từng khóa JWKS (khóa giới hạn theo tenant chỉ dùng cho tenant đó); `iss` phải đúng `https://login.microsoftonline.com/{tid}/v2.0` với `tid` UUID trong token; callback `iss` (nếu có) phải trùng issuer đã ký; kiểm `aud`, `exp`, `iat`, `nbf`, nonce, PKCE `S256`, state/browser binding; chống mix-up 4 provider kể cả route legacy (issuer Microsoft không được dùng chung với provider khác).
- **Chế độ tenant**: `common` / `organizations` (loại MSA) / `consumers` (chỉ MSA); danh sách chặn ưu tiên trước danh sách cho phép; giao dịch OTP/liên kết đang chờ áp lại chính sách provider/tenant hiện hành.
- **Định danh**: khóa `(issuer theo tenant, sub)`; lưu `tid` → `provider_org_id`, `oid` → `provider_stable_id` qua cả đăng ký trực tiếp, OTP và xác nhận liên kết; hai tenant cùng `sub`/email là hai identity khác nhau; email/`preferred_username` không dùng làm định danh hay phân quyền (chống nOAuth).
- **Email**: chỉ email hợp lệ + `xms_edov=true` mới đăng ký thẳng; thiếu bằng chứng → pending registration + OTP; email trùng → link intent cho đúng chủ. Liên kết từ Settings cần recent-auth, CSRF, đúng phiên và email Microsoft tin cậy. Microsoft không vào `reauth_methods`. `AUTH_PROVIDER_MICROSOFT_ENABLED=false` mặc định.
- **Gatekeeper sửa**: cho `nbf` cùng biên sai lệch đồng hồ 1 phút như `iat` (tránh đăng nhập thất bại ngẫu nhiên khi đồng hồ server chậm vài giây).
- **Bằng chứng Gatekeeper**: PostgreSQL disposable toàn package identity **`146/146`** test cấp cao (`388` bản ghi) PASS, `0` FAIL/SKIP, ledger `46 false`, gồm `TestMicrosoftPostgresTenantSubjectAndAccountCollision` và `TestMicrosoftRegistrationOTPUsesChosenEmailAndPreservesTenantIdentity` (chặn tenant sau khi phát hành giao dịch); Go unit không cache PASS; `pnpm api:check` + `pnpm verify` PASS; `git diff --check` sạch.
- **Trước rollout Microsoft (chưa đạt, cờ tắt)**: Entra App Registration (multi-tenant + tài khoản cá nhân), Web redirect URI `/api/v1/auth/oauth/microsoft/callback`, optional claim `xms_edov`, publisher verification, smoke với tài khoản công việc/trường học và MSA thật (kiểm tra OTP khi thiếu `xms_edov`).

### Checkpoint `AUTH-06B` Giao diện Facebook, Hoàn tất đăng ký chờ & Biên nhận xóa dữ liệu (`AUTH-06` `DONE`) — 2026-11-19

Claude Code triển khai lát cắt Web UI + Playwright của `AUTH-06` (Phase 0 không có BLOCKER); Gatekeeper (Antigravity) review diff và kiểm chứng trên Chromium thật + PostgreSQL disposable:

- **`/sign-in`**: nút "Tiếp tục với Facebook" (logo "f" vẽ trong mã) điều hướng toàn trang khi server bật `facebook`; `?registration=pending` chuyển sang `/complete-registration`.
- **`/complete-registration`** (công khai): email gợi ý đã che + đếm ngược, người dùng tự nhập email (thông báo trung tính), OTP + điều khoản + "Duy trì đăng nhập"; có phiên thì vào app, email đã có chủ thì sang `/account-link`; đủ trạng thái `401`/`403`/`409`/`429`/`503`, lỗi mạng, hết hiệu lực, Hủy.
- **`/account-link`** hỗ trợ Facebook; đăng xuất khi sai tài khoản quay lại trang liên kết (`signOut({ returnTo })` chỉ nhận đường dẫn cùng origin). **Settings** lấy nhãn từ `provider_slug` (fallback theo issuer); không thêm nút liên kết Facebook vì server chỉ cho `oidc|google`.
- **`/legal/data-deletion`** (công khai): "Đã nhận" / "Đã hoàn tất" / "Không tìm thấy mã", mã sai định dạng không gọi API, hướng dẫn chung vi/en. Văn bản pháp lý chưa sửa; đề xuất nội dung Chính sách quyền riêng tư chờ chủ dự án duyệt.
- **Fake Facebook** (`e2e-oidc` flavor `facebook`, cổng `9093`) phục vụ đúng đường dẫn Meta `v27.0`, ASID dạng số, tài khoản có thể không chia sẻ email; secret sinh mới mỗi lần chạy.
- **Bằng chứng Gatekeeper**: Playwright Chromium trên DB disposable (migration `46`, xóa sạch sau khi chạy): native auth **`14/14` PASS (`6,1` phút)** gồm `auth-06-facebook.spec.ts` ×4 (không email → OTP → vào app; email trùng → liên kết sau đăng nhập mật khẩu; data-deletion → biên nhận `completed` → ASID cũ quay lại đăng ký chờ; deauthorize thu hồi phiên; hủy đăng ký chờ; mã biên nhận lạ; Axe; Web Storage sạch) và hồi quy OIDC `p2-08` **`1/1` PASS**; Go `e2eoidc` PASS; `pnpm verify` PASS.
- **Quyết định Gatekeeper**: chấp nhận nút Facebook nền trắng + logo "f" xanh (chữ trắng trên `#1877F2` chỉ đạt `4,2:1`, dưới WCAG AA) — rà lại khi Meta App Review; chấp nhận xóa `facebook.svg` không chính thức.

### Checkpoint `AUTH-06A` Facebook Login phía Server, Pending Registration & Xóa dữ liệu Meta (`ADR-0040`) `DONE` — 2026-11-18

Codex triển khai sau khi Phase 0 dừng đúng quy trình và `ADR-0040` gỡ BLOCKER; Gatekeeper (Antigravity) review diff/bảo mật và chạy cổng PostgreSQL disposable độc lập:

- **Facebook OIDC**: Authorization Code + PKCE `S256` (Graph `v27.0`), kiểm state/nonce/issuer/audience/thời hạn/chữ ký ID token, ASID hợp lệ; email Facebook luôn là gợi ý chưa xác minh; không lưu access token; issuer Facebook không được dùng chung với provider khác. `AUTH_PROVIDER_FACEBOOK_ENABLED=false` mặc định.
- **Pending registration**: kind `federated_registration`, cookie riêng `__Host-tutorhub_registration_tx`, TTL ≤ 15 phút; `/api/v1/auth/pending-registration` (GET, `start`/`resend` trả `202` trung tính, `complete`, `cancel`). Sau OTP: email mới chỉ tạo tài khoản khi signup bật, kèm identity email đã xác minh; email đã có chủ → link intent đúng chủ, không cấp phiên.
- **Migration `000046` + `ADR-0040`**: hàm `erase_provider_identity` `SECURITY DEFINER`, `search_path` cố định, chỉ nhận `facebook`, runtime chỉ có `EXECUTE` (PUBLIC không có); runtime không có `DELETE`/`UPDATE (subject)`; trạng thái `erased`; `provider_subject_digest` cho giao dịch chờ (trigger xóa payload/digest khi giao dịch kết thúc); biên nhận `external_data_deletion_requests` không lưu ASID, idempotency theo HMAC sự kiện, hàm purge owner-only sau 1 năm; down chặn khi còn identity `erased`, biên nhận hoặc giao dịch `federated_registration`.
- **Meta callbacks**: data-deletion xóa và hoàn tất biên nhận trong cùng transaction, ASID không tồn tại vẫn `completed`, callback trùng trả cùng `confirmation_code`; deauthorize chỉ unlink + thu hồi phiên, idempotent; `/me/identities` có `provider_slug`; Facebook không vào `reauth_methods`.
- **Bằng chứng Gatekeeper**: PostgreSQL disposable toàn package identity **`138/138`** test cấp cao (`324` bản ghi) PASS, `0` FAIL/SKIP, ledger `46 false`, gồm đủ 17 test chỉ định và subtest xóa dữ liệu/callback/đăng nhập email dưới runtime role; Go unit không cache PASS; `pnpm verify` PASS. Lưu ý vận hành: package integration chạy với DB từ xa cần `go test -timeout 40m` (mặc định 10 phút bị vượt).
- **Trước rollout Facebook (chưa đạt, cờ tắt)**: UI đăng ký chờ/liên kết/biên nhận (`AUTH-06B`); Meta App + Business Verification + Advanced Access `email`; đăng ký 3 URL HTTPS (callback, deauthorize, data-deletion); smoke với app thử nghiệm; lịch owner chạy `purge_expired_deletion_receipts()`; cập nhật Chính sách quyền riêng tư.

### Quyết định ADR-0040 Xóa dữ liệu do provider cấp (gỡ BLOCKER Phase 0 của `AUTH-06A`) — 2026-11-18

Phase 0 của `AUTH-06A` dừng đúng quy trình: Meta yêu cầu xóa cả app-scoped user ID lưu ở `identities.subject` trong khi runtime ACL cấm `DELETE`/`UPDATE (subject)`. Antigravity chốt `ADR-0040`: hàm `SECURITY DEFINER` hẹp (chỉ provider `facebook`) thay subject bằng giá trị ngẫu nhiên, xóa dữ liệu provider, đặt trạng thái `erased`, thu hồi phiên và xóa payload giao dịch chờ; runtime chỉ có `EXECUTE`. Biên nhận không lưu ASID; idempotency theo HMAC của `signed_request`; hoàn tất đồng bộ.

### Checkpoint `AUTH-05B` Giao diện Đăng nhập Google, Xác nhận liên kết & Liên kết trong Settings (`AUTH-05` `DONE`) — 2026-11-18

Claude Code triển khai lát cắt Web UI + Playwright của `AUTH-05` (task đầu tiên áp dụng Phase 0 nghiên cứu bắt buộc, không có BLOCKER); Gatekeeper (Antigravity) review diff/bảo mật và kiểm chứng trên Chromium thật + PostgreSQL disposable:

- **`/sign-in`**: Khi `oauth_providers` có `google`, nút "Đăng nhập bằng Google" / "Sign in with Google" điều hướng toàn trang tới `/api/v1/auth/oauth/google/start` (không fetch); logo "G" chuẩn vẽ trong mã theo light theme của Google Branding Guidelines; thiếu Google giữ nguyên fallback OIDC cũ. Dòng đồng ý điều khoản dưới cụm nút social mở `SignInLegalDialog`. 7 mã `?error=` cố định map sang thông báo vi/en trung tính, không phản chiếu query và được xóa khỏi URL; `?link=pending` chuyển sang `/account-link`.
- **Route công khai `/account-link`**: Hiển thị email đã che + đếm ngược, đăng nhập tại chỗ (dùng lại form của `SignInPage`) rồi ở lại trang để xác nhận; xác nhận xoay CSRF trước khi gọi, làm mới cache, về `return_to` đã lọc lại; đủ trạng thái sai tài khoản (giữ intent, nút đăng xuất), assurance, hết hạn, `401`/`409`/`429` (Retry-After)/`503`.
- **Settings**: Nút "Liên kết Google" dùng step-up dùng chung khi gặp `403 auth_recent_authentication_required`; nhãn Google trong danh sách danh tính/phương thức đăng nhập. Không thêm Google vào hộp step-up.
- **Fake Google cho E2E**: instance `e2e-oidc` thứ hai (cổng `9092`) với form tài khoản thử nghiệm tùy chọn (mặc định tắt, chỉ bật cho stack `--native-auth`), secret Google sinh ngẫu nhiên mỗi lần chạy; không gọi Google thật.
- **Bằng chứng Gatekeeper**: Playwright Chromium trên DB disposable (migration `45`, xóa sạch sau khi chạy): native auth **`10/10` PASS (`4,6` phút)** gồm `auth-05-google-federation.spec.ts` ×6 (đăng ký Google, liên kết với tài khoản mật khẩu tại `/account-link`, hủy intent, từ chối sai tài khoản, map mã lỗi callback, liên kết từ Settings sau step-up) và hồi quy OIDC `p2-08` **`1/1` PASS (`4,3` phút)**; Go `e2eoidc` PASS; `pnpm verify` PASS.
- **Quyết định Gatekeeper**: chấp nhận đổi màu chữ badge/chữ nhấn/subtitle vùng hero `/sign-in` để đạt Axe color-contrast; xóa `apps/web/public/images/auth/icon/google.svg` (bản vẽ lại không chính thức, không còn dùng).
- **Còn mở**: `GET /me/identities` chưa trả `provider_slug` (UI đang suy ra nhãn từ issuer) — bổ sung vào contract ở `AUTH-06A`; khi sai tài khoản, nút đăng xuất đưa người dùng rời `/account-link` (intent vẫn còn đến khi hết hạn).

### Checkpoint `AUTH-05A` Google OAuth2/OIDC phía Server: Provider Registry, Anti-Mix-Up, Link Intent & Federated Re-auth `DONE` — 2026-11-18

Codex triển khai lát cắt Server + Migration + OpenAPI của `AUTH-05`; Gatekeeper (Antigravity) review diff/bảo mật,
sửa 3 fixture kiểm thử PostgreSQL và kiểm chứng trên PostgreSQL disposable + Chromium thật:

- **Migration `000045_federation_providers`**: `auth_flows.provider` (mặc định `oidc`, CHECK 5 provider), purpose
  `reauth`, dựng lại `auth_flows_identity_link_binding_check` cho `login`/`identity_link`/`reauth`, index
  `identities (provider_slug, user_id)`; down có preflight chặn khi còn flow/identity federation.
- **Exact runtime ACL**: `auth_flows`/`identities` chỉ còn `UPDATE` trên cột trạng thái (`consumed_at`; `status`,
  `unlinked_at`, `email_at_provider`, `email_verified`, `last_authenticated_at`, `updated_at`), không `UPDATE`
  ownership/issuer/subject/provider metadata, không thêm `DELETE`. `ensureNativeEmailIdentity` không còn ghi lại
  `provider_slug` khi relink và đã được kiểm chứng dưới runtime role.
- **Registry & route**: `oidc` + `google`; `GET /api/v1/auth/oauth/{provider}/start|callback` tiêu thụ flow theo đúng
  provider trước khi đổi token (RFC 9700), kiểm `iss` (RFC 9207), lỗi callback redirect `303` với mã cố định. Route
  legacy `/auth/login` + `/auth/callback` giữ `provider=oidc` và hợp đồng lỗi JSON cũ.
- **Google**: `AUTH_PROVIDER_GOOGLE_ENABLED=false` mặc định; thiếu client ID/secret thì startup lỗi; issuer tùy chỉnh chỉ
  ở `development`/`test`; RS256 + PKCE `S256` + state/browser binding + nonce + `aud`/`iss`/`exp`/`iat`.
- **Không tự gộp tài khoản**: email trùng hoặc identity `unlinked` tạo `link_intent` (TTL 15 phút, payload mã hóa,
  cookie riêng `__Host-tutorhub_link_tx`); confirm yêu cầu đúng user + CSRF/Origin + AAL, nguyên tử link/relink,
  thu hồi phiên khác, xoay token/CSRF/`context_version`, ghi outbox `identity_linked`. Unlink đếm mọi identity active
  và password credential active.
- **Bằng chứng Gatekeeper**: PostgreSQL disposable toàn package identity `128/128` test cấp cao (`276` bản ghi kể cả
  subtest) PASS, `0` FAIL/SKIP, ledger cuối `45 false`; Go unit không cache cho `config`/`httpapi`/`identity`/`cmd/api`
  PASS; `pnpm verify` PASS; Playwright Chromium trên DB disposable (migration `45`): native auth `4/4` PASS (`2,6`
  phút) và hồi quy OIDC `p2-08` `1/1` PASS (`3,7` phút).
- **Gatekeeper sửa fixture (không đổi mã sản phẩm)**: cấp membership role tạm trong transaction rollback-only trước
  `SET LOCAL ROLE` (PostgreSQL 16+ chỉ cấp ADMIN cho người tạo role); dùng ciphertext AEAD thật cho `auth_flows`
  (CHECK `>= 29` byte); nâng timeout fixture `ListIdentities` từ `1s` lên `20s` cho DB từ xa.
- **Cổng rollout Google (chưa đạt, giữ cờ tắt)**: smoke Google thật với client/callback do chủ dự án cấu hình; xác minh
  Google có trả `auth_time` cho luồng `intent=reauth` (`prompt=login` + `max_age=0`) — nếu không, reauth Google
  fail-closed và không được quảng bá như phương thức step-up; gửi email thông báo `identity_linked` thực tế. Trước
  `AUTH-09` (TOTP) phải thay chính sách AAL tài khoản "mức cao nhất của phiên active" bằng chính sách theo factor đã
  đăng ký.

### Checkpoint `AUTH-04B` Hộp thoại Step-up dùng chung, Phương thức đăng nhập & Quản lý thiết bị/phiên (`AUTH-04` `DONE`) — 2026-11-18

Claude Code triển khai lát cắt Web UI + Playwright E2E của `AUTH-04` và đã qua Gatekeeper (Antigravity) kiểm
chứng trực tiếp trên trình duyệt Chromium thật kết nối Go Core API + PostgreSQL disposable:

- **Global `401` Guard & Hook bảo mật (`queryClient.ts`, `security.ts`)**: Bổ sung `auth_invalid_verification_code`
  bên cạnh `auth_invalid_current_password` trong `credentialRejectionCodes` để nhập sai mã OTP step-up không xóa
  cache hay đăng xuất toàn ứng dụng; `401` không code tiếp tục xóa cache như cũ. Module `apps/web/src/app/security.ts`
  cung cấp các query `/me/security`, `/me/login-methods`, `/me/sessions` (không tự retry `4xx`/`503`), các
  mutation step-up/gửi mã/thu hồi phiên (`gcTime: 0`, xoay CSRF trước POST, đồng bộ lại CSRF và làm mới
  `["auth","me"]` + dữ liệu bảo mật sau khi step-up thành công) và `useStepUpRequest()` tự chạy lại đúng một lần
  thao tác bị chặn sau khi xác nhận thành công.
- **Hộp thoại `StepUpAuthDialog` ("Xác nhận đó là bạn") & Nối thao tác nhạy cảm**: Chỉ gọi API khi mở; khi
  `device_management_enabled` thiếu/tắt giữ nguyên fallback OIDC `signIn` / native `signOut`. Khi bật, ưu tiên
  `password` → `email_code` (cooldown `60s`, nhắc hết hạn, gửi lại, xóa ngay mật khẩu/OTP khỏi state sau request,
  trả focus về nút kích hoạt trước khi chạy lại thao tác bị chặn). Đã nối vào đặt/đổi mật khẩu, liên kết/gỡ
  identity (chuyển sang nhận diện chuẩn `403 auth_recent_authentication_required`) và tạo/thu hồi lời mời trong
  `MembershipInvitationPanel`.
- **Thẻ "Bảo mật tài khoản" & "Thiết bị đang đăng nhập" trên `/app/settings`**: Chỉ hiển thị khi
  `providers.device_management_enabled === true` (thiếu cờ không gọi endpoint nào); hiển thị trạng thái xác minh
  email, mật khẩu, MFA, cửa sổ xác thực gần đây kèm nút "Xác nhận ngay", danh sách phương thức đăng nhập song ngữ
  `vi`/`en`, danh sách phiên hoạt động (xếp "Thiết bị này" lên đầu, không cho tự revoke phiên hiện tại, nút đăng
  xuất từng thiết bị khác và hộp xác nhận "Đăng xuất khỏi các thiết bị khác" hiển thị số phiên đã thu hồi).
- **Bằng chứng kiểm chứng Gatekeeper**:
  - Toàn kho `pnpm verify` PASS (`574/574` web unit tests trên `76` file, `@tutorhub/api-client` `83/83`,
    security `86/86`, `e2e-local` script `11/11`, bundle security `201` file).
  - Playwright Chromium thật trên DB disposable (`44 false`, xóa sạch DB sau khi chạy):
    - **Phase A (bật cờ native + device management)**: **`4/4` PASS (`3,1` phút)** — gồm `auth-02` ×2, `auth-03`
      ×1 và `auth-04-device-sessions-stepup.spec.ts` ×1 (`Chrome · Windows` + `Firefox · Linux` + `Safari · macOS`,
      thu hồi đơn lẻ `204` làm phiên đích nhận `401`, thu hồi hàng loạt `revoked_count: 1`, step-up mật khẩu từ
      banner — gõ sai mật khẩu nhận `401 auth_invalid_current_password` vẫn giữ phiên, gõ đúng xoay cookie phiên —
      và đổi mật khẩu bị chặn `403 auth_recent_authentication_required` tự hoàn tất sau step-up bằng mã email qua
      `devinbox`, mutation CSRF tiếp theo `PATCH /me/profile` trả `200`, Axe `0` vi phạm serious/critical, cookie
      `HttpOnly`, không lộ secret/token vào Web Storage).
    - **Phase B (tắt toàn bộ cờ native, kiểm tra hồi quy OIDC)**: `e2e/p2-08-core-workflows.spec.ts` **PASS
      (`4,6` phút)**.
- **Giới hạn đã ghi nhận**: Spec E2E tiêm tín hiệu `403 auth_recent_authentication_required` / hết hạn cửa sổ một
  lần qua `page.route` vì phiên E2E mới tạo luôn trẻ hơn cửa sổ cố định `10` phút trên server (toàn bộ bước xác
  thực lại, `devinbox`, xoay cookie, chạy lại thao tác và thu hồi phiên đều chạy với Core API thật); giao diện
  chuyển chủ sở hữu lớp học chưa mở trên web nên chưa nối hộp thoại step-up; `AUTH_DEVICE_MANAGEMENT_ENABLED` tiếp
  tục mặc định `false` trên shared/staging/production.

### Checkpoint `AUTH-04A` Quản lý phiên/thiết bị, Step-up Re-authentication & Tổng quan bảo mật phía Server `DONE` — 2026-11-18

Codex triển khai phần Server + OpenAPI Contract của `AUTH-04` trên schema `000044` (không thêm migration mới,
dùng các cột và enum `reauthenticated_at`, `device_label`, `reauth` đã dự phòng tại `000043`) theo `ADR-0039` và
đã qua Gatekeeper (Antigravity) rà soát bảo mật, gia cố ACL trên `tutorhub.sessions`, sửa 2 dòng seed fixture
trong test tích hợp và nghiệm thu đầy đủ:

- **Cờ cấu hình & Chuẩn hóa mã lỗi `recent-auth`**: Bổ sung capability `device_management_enabled` (optional,
  mặc định `false`) trên `GET /api/v1/auth/providers`, kiểm tra điều kiện tiên quyết `DATABASE_POOL_URL` +
  `SESSION_SECRET` khi bật `AUTH_DEVICE_MANAGEMENT_ENABLED` mà không ép kích hoạt OIDC discovery. Chuẩn hóa mọi
  nhánh trả `ErrRecentAuthenticationRequired` (`auth.go`, `classroom.go`, `membership_invitation.go` ×2,
  `native_auth.go`) về HTTP `403` kèm `code: "auth_recent_authentication_required"`, và gắn mã
  `auth_invalid_verification_code` cho lỗi OTP `401` để client không nhầm lỗi bằng chứng người dùng nhập với hết
  phiên.
- **Tổng quan bảo mật, Sổ phương thức đăng nhập & Step-up `10` phút**:
  - `GET /api/v1/me/security`: trả `{has_password, email_verified, mfa_enabled: false, reauth_methods, recent_auth_expires_at}`
    giới hạn theo principal (chỉ quảng bá `password`/`email_code` khi `AssuranceLevel <= 1` và sender khả dụng;
    tính hạn theo `GREATEST(auth_time, reauthenticated_at) + 10m`).
  - `GET /api/v1/me/login-methods`: liệt kê trạng thái mật khẩu và toàn bộ identity đang `active` mọi `kind`
    (`email`, `oidc`, `oauth2`, `phone`, `passkey`) của chính người dùng, không lộ hash/token.
  - `POST /api/v1/auth/reauth/email-code/start` & `POST /api/v1/auth/reauth`: gửi mã step-up tới email đã xác
    minh từ server (gắn `user_id`, `session_id`, `context_version`; giữ nguyên cooldown `60s` và trần `5` lần gửi
    kể cả khi xóa cookie `auth_tx`); xác minh mật khẩu Argon2id ngoài khóa DB (kiểm tra lại credential + lockout
    dưới khóa) hoặc consume nguyên tử mã `reauth`; cập nhật `reauthenticated_at`, thêm phương thức vào
    `auth_methods`, xoay session/CSRF token (`context_version + 1`), bảo toàn hạn persistent cho non-admin và áp
    trần `24` giờ cho `org_admin`.
- **Quản lý phiên/thiết bị & Thứ tự khóa thống nhất**: Tính `device_label` server-side từ allowlist thô họ trình
  duyệt + hệ điều hành (≤ `80` ký tự, không lưu User-Agent thô hay geo-IP) trên mọi đường tạo phiên (signup,
  password login, email-code login, OIDC callback). Cung cấp `GET /api/v1/me/sessions` (opaque `sessions.id`,
  ẩn phiên đã revoke/quá hạn kể cả trần admin `24` giờ, sắp xếp `last_seen_at DESC`),
  `POST /api/v1/me/sessions/{session_id}/revoke` (`204`, `user_revoked`; phiên hiện tại trả `400`
  `auth_cannot_revoke_current_session`; phiên khác user/đã revoke trả `404` chung) và
  `POST /api/v1/me/sessions/revoke-others` (`200 {revoked_count}`, `user_revoked_others`, không đòi step-up, có
  rate limit theo user + IP). Thống nhất thứ tự khóa `user -> sessions (ORDER BY id FOR UPDATE) -> identities -> credential`.
- **Gia cố Gatekeeper, OpenAPI Client & Bằng chứng kiểm chứng**:
  - Gatekeeper mở rộng `ProvisionAuthFoundationRuntimeACL` kiểm tra `relowner` cho `sessions`/`identities` và cấp
    quyền cột chính xác trên `tutorhub.sessions` (`SELECT`/`INSERT` các cột nền tảng auth, `UPDATE` chỉ trên
    `auth_methods, reauthenticated_at, persistent`, cấm `DELETE` và cấm `UPDATE` các cột bất biến), lọc
    `reauth_methods` khi `AssuranceLevel > 1`, và sửa 2 dòng seed SQL trong
    `postgres_security_sessions_integration_test.go`.
  - Xuất 7 typed helpers trong `@tutorhub/api-client` (`getMySecurity`, `listMyLoginMethods`,
    `startReauthEmailCode`, `reauthenticate` tự xóa cache CSRF khi thành công, `listMySessions`,
    `revokeMySession`, `revokeOtherSessions`; `83/83` tests PASS).
  - Toàn bộ `29/29` suite PostgreSQL integration (`0` SKIP, `0` FAIL) trên DB disposable trống tách biệt staging
    PASS (kết thúc sạch tại `44 false` rồi xóa DB tạm); toàn kho `pnpm verify` PASS (`86` security tests, `552`
    web unit tests, Go `core-api` PASS). Giới hạn chưa thực thi: giao diện hộp thoại step-up, thẻ phiên/thiết bị
    và Playwright E2E của `AUTH-04` thuộc `AUTH-04B`; chưa bật `AUTH_DEVICE_MANAGEMENT_ENABLED` trên
    shared/staging/production.

### Checkpoint `AUTH-03B` Giao diện Đăng nhập mã email, Khôi phục mật khẩu & Đặt/đổi mật khẩu (`AUTH-03` `DONE`) — 2026-11-18

Claude Code triển khai lát cắt Web UI + Playwright E2E của `AUTH-03`; Gatekeeper (Antigravity) rà soát, sửa thêm
và kiểm chứng trên Chromium thật kết nối Go Core API + PostgreSQL disposable:

- **Đăng nhập bằng mã email** (`SignInPage.tsx`, chỉ hiện khi `email_code_enabled=true`): lấy mã → OTP 6 số →
  xác thực, cooldown `60s`, nhắc hết hạn, tự mở giao dịch mới khi gửi lại trả `400`, bỏ mã cũ và phản hồi muộn khi
  đổi email, thông báo trung tính chống dò tài khoản, `mfa_required` không bao giờ được coi là đã đăng nhập; thành
  công đi qua `completeNativeSignIn()`. Ref "chủ giao dịch" ngăn luồng đăng nhập bằng mã và luồng khôi phục gửi lại
  nhầm mã của nhau (server chỉ gắn một cookie giao dịch).
- **Khôi phục mật khẩu** (`NativeRecoverPasswordForm`, chỉ khi `password_recovery_enabled === true`, thiếu cờ giữ
  placeholder OIDC): lỗi policy chỉ xóa ô mật khẩu mới; thành công gọi `resetAfterPasswordRecovery()` (xóa cache
  riêng tư, tải lại `/me` → chưa đăng nhập), đóng dialog, hiện banner yêu cầu đăng nhập lại — **không tự đăng nhập**.
- **Thẻ "Mật khẩu" trên `/app/settings`**: đủ trạng thái tải/lỗi/thử lại/không có quyền, đặt mật khẩu lần đầu hoặc
  đổi mật khẩu (kèm `updated_at`), khóa form khi `password_enabled=false`; xử lý riêng `403` cần xác thực lại,
  `auth_verified_email_required`, `auth_invalid_current_password`, `auth_password_unchanged`, `auth_password_policy`,
  `429`/`503` + `Retry-After`; xoay CSRF trước POST và đồng bộ lại sau khi thành công.
- **Gatekeeper sửa thêm**:
  - `apps/web/src/app/queryClient.ts`: hook toàn cục coi mọi `401` của mutation là hết phiên (xóa cache, reset
    `/me`, form bị remount và mất thông báo lỗi). Nay bỏ qua `401` mang mã `auth_invalid_current_password` (chỉ từ
    chối mật khẩu người dùng gõ, phiên vẫn hợp lệ); thêm unit test. Lỗi này chỉ lộ ra khi chạy trình duyệt thật.
  - `e2e/auth-03-native-email-code-recovery.spec.ts`: tạo workspace qua UI trước bước đổi mật khẩu (tài khoản mới
    chưa có workspace bị điều hướng `/app` về trang onboarding).
  - `playwright.config.ts` `testIgnore` → `**/auth-0[23]-native-*.spec.ts` (+ assertion `scripts/e2e-local.test.mjs`)
    để `pnpm e2e` mặc định không thu thập spec native.
- **Bằng chứng**: `pnpm verify` PASS (web `552/552`, api-client `71/71`, security `86/86`, bundle `201` file,
  `e2e-local` script `11/11`). Playwright Chromium thật trên DB disposable (`44 false`, xóa DB sau khi chạy):
  `auth-02-native-signup-login.spec.ts` `2/2` + `auth-03-native-email-code-recovery.spec.ts` `1/1` PASS (đăng nhập bằng
  mã email, email lạ trông y hệt email đã đăng ký và không nhận thư, khôi phục không tự đăng nhập, notice
  `password_changed` không chứa mã, mật khẩu cũ bị từ chối, đổi mật khẩu trong settings giữ phiên và mutation CSRF
  tiếp theo vẫn chạy, Axe `0` vi phạm serious/critical, không có mật khẩu/OTP trong Web Storage, cookie `HttpOnly`).
  Hồi quy OIDC `e2e/p2-08-core-workflows.spec.ts` PASS khi tắt cờ native (`4,7` phút do độ trễ Neon US-East; timeout
  mặc định `240s` không đủ cho DB từ xa, CI dùng PostgreSQL loopback).
- **Giới hạn**: nút "Đăng nhập lại" cho tài khoản chỉ có native hiện chỉ đăng xuất (step-up `/auth/reauth` thuộc
  `AUTH-04`); người dùng chưa có workspace chưa vào được `/app/settings` để đổi mật khẩu; chưa bật cờ native trên
  shared/staging.

### Checkpoint `AUTH-03A` Đăng nhập OTP Email, Khôi phục mật khẩu & Đặt/đổi mật khẩu phía Server `DONE` — 2026-11-17

Codex triển khai phần Server + OpenAPI Contract của `AUTH-03` trên schema `000044` theo `ADR-0039` và đã qua
Gatekeeper (Antigravity) rà soát bảo mật, chuẩn hóa `QueryTimeout` cho `NativeAuthService`, sửa truy vấn
`pg_backend_pid()` cho kiểm thử khóa trên Neon proxy và nghiệm thu đầy đủ:

- **Đăng nhập mã OTP email & Gửi lại mã tổng quát**: Cung cấp `POST /api/v1/auth/email-code/start` và
  `POST /api/v1/auth/email-code/verify` (khi `AUTH_METHOD_EMAIL_CODE_ENABLED=true`) cho tài khoản mật khẩu và
  OIDC đang `active`, tự ghi nhận `users.email_verified_at` và identity `urn:tutorhub:email` (`subject=user_id`,
  không tự gộp tài khoản theo email, không tạo mật khẩu ngoài ý muốn, áp trần `24` giờ cho `org_admin`). Mở rộng
  `POST /api/v1/auth/challenges/resend` xử lý cả `signup_email`, `login` (`login_code`) và `recovery`
  (`password_recovery`) theo đúng cờ tính năng từ transaction gắn cookie.
- **Khôi phục mật khẩu nguyên tử & Chống dò qua Password Policy**: Cung cấp
  `POST /api/v1/auth/password/recovery/start` và `POST /api/v1/auth/password/recovery/complete` (đòi cả
  `AUTH_METHOD_PASSWORD_ENABLED` và `AUTH_METHOD_EMAIL_CODE_ENABLED`; `GET /api/v1/auth/providers` bổ sung
  `password_recovery_enabled`). Snapshot chỉ đọc kiểm tra bằng chứng OTP trước khi đưa `display_name` vào
  password policy (chặn dùng lỗi policy để dò tên người dùng, đồng thời không làm mất lượt OTP khi gõ mật khẩu
  yếu), băm Argon2id ngoài khóa DB rồi verify lại trong `pgx.Tx` nguyên tử: upsert mật khẩu `active`, reset
  lockout/`compromised_at`, thu hồi **toàn bộ** phiên hiện có (`revoked_reason='password_reset'`), xóa cookie
  phiên/CSRF/transaction mà **không tự động đăng nhập**, và đẩy thông báo bảo mật `password_changed` (không chứa
  OTP/mật khẩu) sau commit qua dispatcher có quota.
- **Đặt/đổi mật khẩu đã xác thực (`GET/POST /api/v1/me/password`) & Thứ tự khóa chống Deadlock**: Cho phép tài
  khoản OIDC có email đã xác minh đặt mật khẩu lần đầu hoặc tài khoản đã có mật khẩu đổi mật khẩu mới; bắt buộc
  xác thực gần đây `10` phút trên cả principal lẫn dòng `sessions` đã khóa, kiểm tra `current_password` kèm
  lockout có trần, từ chối mật khẩu trùng sau chuẩn hóa NFKC, thu hồi mọi phiên khác
  (`revoked_reason='password_changed'`), giữ và xoay phiên/CSRF hiện tại (`context_version + 1`, bảo toàn hạn
  persistent cho non-admin và áp trần `24` giờ cho `org_admin`). Mở rộng `ProvisionAuthFoundationRuntimeACL` thêm
  `UPDATE(status, compromised_at)` trên `user_password_credentials` (vẫn cấm `DELETE` và `UPDATE(user_id, created_at)`);
  thống nhất thứ tự khóa `user -> sessions (ORDER BY id) -> identity` tương thích OIDC, hồ sơ, tổ chức và lời mời.
- **OpenAPI, `@tutorhub/api-client` & Bằng chứng kiểm chứng Gatekeeper**: Bổ sung 6 typed helpers
  (`startEmailCodeLogin`, `verifyEmailCodeLogin`, `startPasswordRecovery`, `completePasswordRecovery`,
  `getMyPasswordStatus`, `updateMyPassword`). Toàn bộ `21/21` suite PostgreSQL integration (`0` SKIP, `0` FAIL)
  trên DB disposable trống tách biệt hoàn toàn với staging PASS (kết thúc sạch tại `44 false` rồi xóa DB tạm);
  toàn kho `pnpm verify` PASS (`86` security tests, `71` `@tutorhub/api-client` tests). Giới hạn chưa thực thi:
  phần giao diện Web & Playwright E2E của `AUTH-03` sẽ kiểm chứng ở `AUTH-03B`; thông báo `password_changed` hiện
  đi qua hàng đợi bộ nhớ best-effort có quota (chưa phải hàng đợi bền vững); chưa bật cờ mới trên shared/staging.

### Checkpoint `AUTH-02B` Giao diện Đăng ký Email + OTP & Đăng nhập mật khẩu trên `/sign-in` (`AUTH-02` `DONE`) — 2026-11-17

Claude Code triển khai lát cắt Web UI + Playwright E2E của `AUTH-02` và đã qua Gatekeeper (Antigravity) kiểm
chứng trực tiếp trên trình duyệt Chromium thật kết nối Go Core API + PostgreSQL disposable:

- **Nạp năng lực xác thực & Bảo toàn hồi quy OIDC**: `apps/web/src/pages/SignInPage.tsx` nạp `getAuthProviders`
  ngay khi vào trang (giữ nguyên bố cục hai cột và `SignInSlider` khi đang tải, có trạng thái lỗi kèm `Thử lại`).
  Khi `password_enabled=false` và `oidc_default_enabled=true` (mặc định repo), nút `Đăng nhập`, `Google`,
  `Facebook` tiếp tục điều hướng sang luồng BFF OIDC như cũ; khi bật `password_enabled=true` /
  `signup_enabled=true`, form kích hoạt đăng nhập mật khẩu và đăng ký OTP nội tại.
- **Luồng Đăng ký OTP, Điều khoản & Đăng nhập mật khẩu**:
  - `SignUpForm`: `startEmailSignup` → đếm ngược cooldown `60s` & theo dõi hạn `10` phút → `resendAuthChallenge`
    (tự mở lại transaction mới nếu hết hạn `400`, hủy kết quả muộn nếu người dùng đổi email giữa chừng) →
    `completeEmailSignup` kèm `legal_terms_version`. Thông báo sau khi lấy mã luôn trung tính chống dò tài khoản;
    xử lý riêng `auth_terms_version_changed` (tải lại providers, bỏ tích và mời xem điều khoản mới),
    `auth_password_policy` (chỉ xóa ô mật khẩu), và `429`/`503` có đếm ngược `Retry-After`.
  - `LoginForm`: `loginWithPassword` kèm `keep_signed_in` (cập nhật hint tối đa `30` ngày cho non-admin, `24`
    giờ cho `org_admin`), `Remember Me` chỉ lưu email sau khi đăng nhập thành công, thông báo `401` đồng nhất và
    xử lý nhánh dự phòng `mfa_required`. Nâng màu nút chính và nút chuyển form sang `#047857` (`5.48:1`) đạt
    chuẩn tương phản WCAG AA trên Axe.
  - `apps/web/src/app/session.tsx`: bổ sung `completeNativeSignIn()` thực hiện tuần tự
    `clearPrivateSessionCache` → `rotateCSRFToken` → `refresh()` (`/api/v1/me`) trước khi điều hướng tới
    `returnTo` nội bộ đã lọc chống open-redirect (có nút `Thử lại` riêng nếu bước `/me` lỗi mạng).
- **Kiểm chứng Gatekeeper**:
  - Toàn kho `pnpm verify` PASS (`522/522` web unit tests, riêng `SignInPage.test.tsx` `55` tests song ngữ
    `vi`/`en` và `session.test.tsx` `10` tests; bundle security `201` file; `security:actions` PASS sau khi bổ
    sung `pnpm e2e:auth` vào `.github/workflows/ci.yml`).
  - Playwright Chromium thật trên PostgreSQL disposable (`44 false`):
    `e2e/auth-02-native-signup-login.spec.ts` **PASS `2/2` (`26,7s`)** — đăng ký tài khoản mới qua OTP
    `devinbox` → vào `/app/home` → kiểm tra cookie `HttpOnly`, không lộ mật khẩu/OTP vào Web Storage, Axe `0`
    vi phạm `serious`/`critical` → đăng xuất → bấm lấy mã lại cho email đã đăng ký xác nhận UI y hệt nhưng
    `devinbox` chỉ nhận `notice_kind="account_exists"` không chứa mã → đăng nhập lại bằng mật khẩu thành công
    và kiểm tra ca sai mật khẩu trả lỗi chung. Đồng thời xác nhận luồng OIDC login/workspace/invitation của
    `e2e/p2-08-core-workflows.spec.ts` hoạt động đúng khi tắt cờ mật khẩu.

### Checkpoint `AUTH-02A` Đăng ký Email + OTP + Argon2id & Đăng nhập mật khẩu phía Server `DONE` — 2026-11-17

Codex triển khai phần Server + OpenAPI Contract của `AUTH-02` theo `ADR-0039` và đã qua Gatekeeper (Antigravity)
rà soát bảo mật, tinh chỉnh transaction cleanup trong integration test lời mời và nghiệm thu đầy đủ:

- **Schema & Exact Runtime ACL (`000044_password_credentials`)**: Thêm `tutorhub.users.email_verified_at` (backfill
  từ thời điểm sớm nhất của identity `active` đã xác minh khớp email), tạo `tutorhub.user_password_credentials`
  (CHECK định dạng PHC `$argon2id$v=19$%`) và `tutorhub.user_legal_acceptances` (`user_agreement`,
  `privacy_policy`), kèm rollback preflight chặn hạ cấp khi đã có dữ liệu xác thực/pháp lý. Mở rộng
  `ProvisionAuthFoundationRuntimeACL` đảm bảo `user_legal_acceptances` append-only (không `UPDATE`/`DELETE`),
  `user_password_credentials` chỉ `UPDATE` đúng 6 cột trạng thái (không `DELETE`), và giữ nguyên ACL `users`
  hiện có kèm quyền đọc/ghi `email_verified_at`.
- **Argon2id, Semaphore, Blocklist & HIBP**: Chuẩn hóa Unicode NFKC (`8..128` rune, ≤ `512` byte), băm Argon2id
  (`m=19456 KiB, t=2, p=1`), parse PHC an toàn trước khi cấp phát RAM, rehash-on-login, semaphore toàn cục
  `PASSWORD_HASH_MAX_CONCURRENCY` (mặc định `2`, chờ tối đa `2s` trả `503` + `Retry-After`), danh sách cấm
  offline + kiểm tra lộ mật khẩu HIBP range API k-anonymity có `Add-Padding: true` (mặc định `offline`, fail-open
  có bộ đếm và log sạch PII).
- **Tách khởi động Native-Only, Atomic Signup & Session Persistence**: Cho phép khởi động chế độ xác thực nội tại
  với `SESSION_SECRET` + `DATABASE_POOL_URL` mà không bắt buộc cấu hình/discovery OIDC khi tắt OIDC mặc định, đồng
  thời bảo toàn 100% hồi quy OIDC. `POST /api/v1/auth/signup/email/complete` thực thi trong **một `pgx.Tx` nguyên
  tử duy nhất** (verify OTP + consume `auth_transactions` + ghi `users`, `identities` `urn:tutorhub:email`,
  `user_password_credentials`, 2 dòng `user_legal_acceptances` và `sessions`). Phiên `persistent=true` giữ `30`
  ngày cho học sinh/giáo viên qua cả các bước xoay phiên/chuyển tổ chức, và tự động khóa trần `24` giờ ngay cả
  khi người dùng được nâng quyền hoặc tạo tổ chức thành `org_admin` sau khi đăng nhập.
- **OpenAPI, Rate Limit & Anti-Enumeration**: Cung cấp `POST /api/v1/auth/signup/email/start`,
  `POST /api/v1/auth/challenges/resend`, `POST /api/v1/auth/signup/email/complete`,
  `POST /api/v1/auth/password/login`, mở rộng `GET /api/v1/auth/providers` thêm `legal_terms_version` và xuất
  typed helpers trong `@tutorhub/api-client`. Nối đầy đủ rate limit IP/đích, phản hồi `202` đồng nhất kèm thông
  báo `account_exists` không chứa mã OTP và `DummyVerify` chống dò tài khoản.
- **Bằng chứng & Giới hạn kiểm chứng**: Toàn bộ `14/14` suite PostgreSQL integration (`0` SKIP) trên DB disposable
  trống mới tạo PASS (`43 -> 44 -> 43 -> 44`, kết thúc sạch tại `44 false` rồi xóa DB tạm), toàn kho `pnpm verify`
  PASS, benchmark Argon2id local PASS. Giới hạn chưa thực thi: chưa benchmark Argon2id trực tiếp trên container
  Render `512 MB`, chưa gọi mạng thật tới HIBP (đã kiểm chứng qua mock HTTP server), chưa chạy migration `000044`
  lên shared/staging và hai cờ `AUTH_SIGNUP_ENABLED` / `AUTH_METHOD_PASSWORD_ENABLED` tiếp tục mặc định `false`.

### Checkpoint `AUTH-01` Nền tảng xác thực đa phương thức `DONE` — 2026-11-17

Codex triển khai theo `ADR-0039` và đã qua Gatekeeper (Antigravity) rà soát diff, sửa tinh chỉnh test kiểu thời gian
PostgreSQL và nghiệm thu đầy đủ:

- **Schema & Exact Runtime ACL (`000043_auth_foundation`)**: Mở rộng `tutorhub.identities` (`kind`,
  `provider_slug`, `provider_org_id`, `provider_stable_id`, backfill `oidc`, lọc `GET /api/v1/me/identities`
  chỉ trả `oidc`/`oauth2`) và `tutorhub.sessions` (`auth_methods`, `assurance_level`, `reauthenticated_at`,
  `device_label`, `persistent`, đọc `GREATEST(auth_time, COALESCE(reauthenticated_at, auth_time))` không làm
  đổi JSON `/api/v1/me`). Tạo `tutorhub.auth_transactions`, `tutorhub.auth_challenges`,
  `tutorhub.auth_challenge_deliveries` kèm CHECK độ dài hash 32 byte, giới hạn TTL 15p/10p/5p và down
  preflight chặn rollback khi có dữ liệu xác thực cần bảo toàn. Bổ sung `ProvisionAuthFoundationRuntimeACL`
  chỉ chạy bằng kết nối owner riêng, cấp `SELECT, INSERT` và `UPDATE` đúng cột trạng thái, cấm `DELETE` và
  chặn lách quyền qua `NOINHERIT` role membership.
- **Transaction, OTP Challenge & Ephemeral Delivery**: Cookie `__Host-tutorhub_auth_tx` (`SameSite=Strict`,
  `HttpOnly`, chỉ lưu HMAC `auth-tx-binding`); OTP 6 chữ số `crypto/rand` lưu HMAC `auth-otp-v1`, cooldown
  60s, tối đa 5 lần gửi/transaction và 5 lần thử/challenge, khóa hàng cha–con chống race verify đồng thời,
  challenge mồi đi cùng đường ghi DB; hàng đợi gửi bất đồng bộ sau commit kèm biên nhận đã redact, hộp thư
  `devinbox` + SMTP loopback cho `development`/`test` và chốt ngân sách `scripts/check-auth-delivery-guard.mjs`.
- **Pre-Auth Guard, Rate Limit & Contract**: Bảo vệ `Origin`, `Sec-Fetch-Site`, `Content-Type: application/json`
  strict decoder; mở rộng `tutorhub.rate_limit_windows` phân tách miền `tutorhub.auth-rate-limit.bucket.v1`;
  cung cấp `GET /api/v1/auth/providers`, `POST /api/v1/auth/identifier/discover` (OpenAPI + `@tutorhub/api-client`)
  và `GET /api/v1/dev/auth-inbox` khóa chặt theo direct TCP peer loopback (`directPeerContextKey`). Toàn bộ
  cờ `AUTH_*` mới mặc định `false`; OIDC mặc định vẫn `true`.
- **Bằng chứng nghiệm thu Gatekeeper**: Toàn bộ 4 suite PostgreSQL disposable (`TestAuthFoundation*`) cùng
  toàn bộ integration suite của `identity` (OIDC session, profile/identity linking, membership invitation)
  PASS trên database trống mới tạo tách biệt hoàn toàn với staging (`0 -> 43 -> 42 -> 43`, kết thúc `43 false`
  rồi dọn sạch DB tạm); toàn kho `pnpm verify` PASS, `0` dependency mới, `0` secret rò rỉ. Shared staging
  chưa chạy migration `000043`.

### Checkpoint `ADR-0039` Chốt kiến trúc & chính sách xác thực đa phương thức `DONE` — 2026-11-16

Chủ dự án đã chốt toàn bộ chính sách xác thực cốt lõi; Antigravity ban hành
[ADR-0039](adr/0039-native-multi-method-authentication-and-federation.md) (`Accepted`) và đồng bộ
`docs/SECURITY_BASELINE.md`, `docs/adr/0005-oidc-bff-session.md`,
`docs/adr/0008-zitadel-oidc-provider.md`, `docs/AUTH_MULTI_METHOD_RESEARCH_AND_PLAN.md`, `README.md`:

- **Kiến trúc**: Chốt **Hướng 1** — triển khai xác thực nội tại và OAuth2/OIDC federation trực tiếp
  (`google`, `facebook`, `microsoft`, `oidc`) ngay trong `services/core-api/internal/modules/identity`,
  giữ nguyên BFF cookie `__Host-tutorhub_session` / `__Host-tutorhub_csrf`, `GET /api/v1/me` và fake
  issuer `e2e-oidc`; cấp ngoại lệ có kiểm soát với `ADR-0018` cho phép gửi mã OTP ngắn hạn trực tiếp sau
  commit DB mà không ghi mã bí mật vào durable outbox.
- **Thứ tự triển khai đã khóa**: `AUTH-01..03` (Email + Mật khẩu + OTP + Khôi phục) → `AUTH-04` (Quản lý
  thiết bị/phiên & Step-up 10 phút) → `AUTH-05..06` (Google & Facebook + Meta deletion/deauthorize
  callbacks) → `AUTH-07..11` (Microsoft, Passkey/WebAuthn, TOTP 2FA, Số điện thoại Zalo ZNS → SMS).
- **Chính sách mật khẩu, phiên & SĐT**: `PASSWORD_MIN_LENGTH=8` (khớp UI `/sign-in`) + chặn mật khẩu lộ
  qua HIBP + Argon2id có semaphore giới hạn đồng thời; tích _"Duy trì đăng nhập"_ giữ phiên `30 ngày` cho
  học sinh/giáo viên (`org_admin` giữ tối đa `24 giờ`); mọi tài khoản giai đoạn đầu giữ Email chuẩn
  (`users.email NOT NULL`), SĐT là phương thức đăng nhập/xác thực bổ sung ưu tiên Zalo ZNS trước SMS.

### Checkpoint `AUTH-V2-00` Nghiên cứu & Kế hoạch tổng thể xác thực đa phương thức `DONE` — 2026-11-16

Claude Code hoàn thành nghiên cứu chuyên sâu và lập kế hoạch triển khai tại
`docs/AUTH_MULTI_METHOD_RESEARCH_AND_PLAN.md` (1.526 dòng, 10 sơ đồ Mermaid), đã qua Gatekeeper
(Antigravity) thẩm định kiến trúc và bảo mật:

- **Kiến trúc đề xuất (Hướng 1 — điểm trọng số `4.15` so với SaaS `3.40`, ZITADEL Session API `3.30`,
  Self-host OSS `3.25`)**: Mở rộng trực tiếp module `services/core-api/internal/modules/identity` trong
  Go modular monolith kết hợp OAuth2/OIDC federation trực tiếp tới Google, Facebook, Microsoft (và Apple
  tùy chọn); giữ nguyên BFF cookie `__Host-tutorhub_session` / `__Host-tutorhub_csrf`, `GET /api/v1/me`
  và fake issuer `e2e-oidc` (ZITADEL trở thành provider `oidc` tùy chọn). Giữ trọn giao diện `/sign-in`,
  không chạm trần 100 DAU của ZITADEL Free và không phát sinh container phụ trên Render.
- **Thiết kế bảo mật trọng tâm**:
  - Mở rộng `tutorhub.identities` làm sổ đăng ký phương thức đăng nhập (`oidc`, `oauth2`, `email`,
    `phone`, `passkey`) kèm `urn:tutorhub:email` để tương thích liền mạch với luồng chấp nhận lời mời
    vào tổ chức/lớp học hiện có.
  - **Cấm tự động gộp tài khoản (auto-merge) theo email/SĐT**: khi trùng định danh sẽ tạo `link_intent`
    trong `tutorhub.auth_transactions` (`__Host-tutorhub_auth_tx`), bắt buộc xác thực vào tài khoản hiện
    có ở đúng mức bảo đảm (`AAL1`/`AAL2`) rồi xác nhận liên kết — chặn cả 5 lớp tấn công Pre-Account
    Takeover (USENIX Security 2022).
  - **Mật khẩu & OTP**: Argon2id (`m=19 MiB, t=2, p=1`) có semaphore giới hạn đồng thời chống tràn RAM
    `512 MB` trên Render + kiểm tra lộ mật khẩu qua HIBP k-anonymity; mã OTP 6 số CSPRNG chỉ lưu HMAC
    trong PostgreSQL, phản hồi `202` đồng nhất chống dò tài khoản, gửi qua ephemeral post-commit
    dispatcher (`devinbox` loopback cho local/E2E, Mailpit/SES/Zalo ZNS/SMS có khóa ngân sách).
  - **Facebook & Số điện thoại**: Facebook dùng OIDC Code + PKCE, coi email từ Facebook là chưa xác minh
    (xác thực OTP 1 lần), bổ sung 2 callback bắt buộc của Meta (`data-deletion`, `deauthorize`); số điện
    thoại chuẩn hóa E.164 (`nyaruka/phonenumbers`), ưu tiên Zalo ZNS trước SMS Brandname cho `+84`, chống
    SMS pumping đa tầng và tách quyền "nhận lại số" khỏi "khôi phục tài khoản" để chống chiếm đoạt SIM
    tái chế.
  - **Passkeys, 2FA & Quản trị phiên**: WebAuthn Conditional UI autofill ngay trên ô nhập định danh,
    TOTP + mã khôi phục, step-up thật 10 phút (`GREATEST(auth_time, reauthenticated_at)`), trang quản lý
    thiết bị đang đăng nhập và School SSO xác minh domain qua DNS.
- **Lộ trình thực thi**: Phân rã thành 13 gói công việc độc lập `AUTH-01` → `AUTH-13` (migrations
  `000043` → `000051`), tách rõ ranh giới Contract/Web/E2E (Claude Code) và Go Core/SQL/ACL (Codex),
  toàn bộ nằm sau cờ `AUTH_*` mặc định `false`. Chưa thay đổi runtime, schema database hay cấu hình.

### Checkpoint `UI-AUTH-03` Slider trượt ngang liền mạch & Progress Pill trên `/sign-in` `DONE` — 2026-11-16

Claude Code triển khai theo prompt điều phối của Antigravity và đã qua nghiệm thu Gatekeeper đầy đủ:

- Tách riêng `apps/web/src/pages/SignInSlider.tsx`, dựng track ngang `[clone cuối, 1, 2, 3, clone đầu]`
  di chuyển bằng `translate3d` (`560ms`, `cubic-bezier(0.22, 1, 0.36, 1)`) và tự nhảy lặng lẽ về slide thật
  qua `onTransitionEnd` + fallback `600ms` (không bị tua ngược khi quay vòng `3 -> 1` hoặc `1 -> 3`; hai slide
  clone giữ `aria-hidden="true"` để screen reader chỉ đọc đúng `1/3..3/3`).
- Bật tự động chuyển cảnh ngay khi tải trang (`rotationEnabled = true` kể cả khi OS Windows bật
  `prefers-reduced-motion: reduce` do tắt hiệu ứng cửa sổ), đồng thời tuân thủ WCAG 2.2.2 bằng nút `⏸`/`▷`,
  tự dừng khi hover, focus bàn phím, đang kéo vuốt hoặc tab trình duyệt ẩn (`visibilitychange`), và bảo toàn
  thời gian còn lại để khớp đồng bộ với thanh tiến trình CSS.
- Nâng cấp cụm chỉ báo thành **Progress Pill** (`30×8px`, thanh `#10B981` chạy đầy trong `4000ms`), hỗ trợ
  kéo/vuốt (`pointer` swipe ngưỡng `40px`, `touch-action: pan-y`) và phím `ArrowLeft`/`ArrowRight`.
- Kiểm chứng Gatekeeper: toàn kho `pnpm verify` PASS (`489/489` web unit tests trong `75` file, `24/24` test
  tại `SignInPage.test.tsx`, bundle security `201` file, toàn bộ Go `test`/`vet`) và kiểm thử trực tiếp trên
  Chrome cấu hình Windows hiện tại xác nhận tự trượt đủ chu kỳ `1 -> 2 -> 3 -> 1`, mobile `375×812` không tràn
  ngang, `0` lỗi console.

### Checkpoint `UI-AUTH-02` Hộp thoại Thỏa thuận người dùng & Chính sách quyền riêng tư `DONE` — 2026-11-16

Claude Code triển khai theo prompt điều phối của Antigravity và đã qua nghiệm thu Gatekeeper đầy đủ:

- Bổ sung `apps/web/src/pages/signInLegalContent.ts` với nội dung song ngữ `vi`/`en` gồm hộp **Tóm tắt
  điểm chính** và **7 điều khoản** cho từng văn bản (_Thỏa thuận người dùng_ và _Chính sách quyền riêng tư_),
  đối chiếu chuẩn xác với kiến trúc bảo mật/phân quyền TutorHub V2.
- Xây dựng `apps/web/src/pages/SignInLegalDialog.tsx` tái sử dụng `Dialog` & `Tabs` từ `@tutorhub/ui`:
  header cố định kèm pill phiên bản, chuyển đổi mượt giữa 2 tab trong một vùng cuộn chung (tự reset
  `scrollTop = 0` khi đổi tab), footer cố định có nút `Đóng` và nút `Tôi đồng ý & Tiếp tục` (màu `#047857`
  đạt tương phản WCAG AA 5.48:1, tự tích checkbox và xóa lỗi thiếu tick).
- Cập nhật `apps/web/src/pages/SignInPage.tsx` & `SignInPage.css`: tách hai nút mở hộp thoại ra khỏi
  `<label>` để không bấm nhầm checkbox, bảo toàn dữ liệu đang nhập trong form đăng ký và trả focus chính xác
  khi đóng dialog; bổ sung 6 test case mới (3 `en`, 3 `vi`) vào `apps/web/src/pages/SignInPage.test.tsx`.
- Kiểm chứng Gatekeeper: toàn kho `pnpm verify` PASS (`482/482` web unit tests, bundle security `201` file,
  toàn bộ Go `test`/`vet`) và kiểm thử trình duyệt trực tiếp tại `http://localhost:5173/sign-in` xác nhận hiển
  thị, cuộn, đổi tab và focus hoạt động sạch.

### Checkpoint `UI-AUTH-01` Giao diện đăng nhập Web theo thiết kế V1 `DONE` — 2026-11-16

Claude Code triển khai theo prompt điều phối của Antigravity và đã qua nghiệm thu Gatekeeper đầy đủ:

- Sao chép nguyên vẹn tài nguyên gốc từ V1 (`logomoi.png`, `slide_login/login1..3.png`, `facebook.svg`,
  `google.svg`) vào `apps/web/public/images/auth/` và gắn favicon tại `apps/web/index.html`.
- Dựng mới `apps/web/src/pages/SignInPage.tsx` + `SignInPage.css` (lazy-load tại `apps/web/src/app/routes.tsx`),
  tái hiện bố cục hai cột `65% / 35%`, slider 3 ảnh tự chuyển `4000ms` kèm điều khiển tạm dừng/chuyển ảnh
  đạt WCAG 2.2.2, chế độ chuyển đổi `Sign Up` và hộp thoại `Forgot Password` dùng `Dialog` của `@tutorhub/ui`
  (focus trap, `Escape`, trả focus), hỗ trợ song ngữ `vi`/`en`.
- Nút `Log In`, `Facebook` và `Google` kết nối trực tiếp vào BFF OIDC (`useSession().signIn(returnTo)`);
  `Remember Me` chỉ lưu email gợi ý (`tutorhub.remembered_email`), không lưu mật khẩu hay token.
- Kiểm chứng Gatekeeper: `pnpm verify` toàn kho PASS (`476/476` web unit test, client bundle security `201`
  file, toàn bộ Go test/vet), ngân sách bundle `pnpm test:collaboration:p514` PASS (`entryRawBytes=491331`),
  và kiểm thử trình duyệt thực tế tại `http://localhost:5173/sign-in` xác nhận hiển thị ảnh, chuyển slide,
  Sign Up, Forgot Password cùng luồng đăng nhập OIDC đầu–cuối vào `/app/home` đều PASS.
- Hạng mục có thể tinh chỉnh ở bước sau nếu chủ dự án cần: nén bộ ảnh `slide_login` sang WebP/AVIF để giảm
  dung lượng tải mạng yếu, gom chuỗi bản địa hóa vào `apps/web/src/app/i18n.tsx`, và thêm nút chuyển nhanh
  `VI / EN` ngay trên màn hình `/sign-in`.

### Cập nhật quản trị điều phối `DONE` — 2026-11-16

Chủ dự án chuyển giao vai trò quản lý dự án và chốt chặn chất lượng (PM / Architect / Gatekeeper) cho
**Antigravity**, phối hợp cùng **Codex** và **Claude Code (Claude Pro)** làm worker thực thi theo
[ADR-0038](adr/0038-antigravity-orchestrated-multi-agent-workflow.md) (bổ sung cho
[ADR-0012](adr/0012-single-maintainer-direct-main-governance.md)). Antigravity chịu trách nhiệm lập kế
hoạch, xuất Prompt giao việc chuẩn theo phạm vi file đóng (`Allowed Scope`) để chủ dự án dán vào Codex
hoặc Claude Code, sau đó trực tiếp review `git diff`/bảo mật, chạy `pnpm verify`, cập nhật
`docs/PROJECT_STATE.md` và thực hiện `git commit` / `git push origin main`. Đã bổ sung `CLAUDE.md` tại
gốc repository và đồng bộ `AGENTS.md`, `docs/AGENT_COORDINATION.md`, `README.md`. Không thay đổi runtime,
schema database hoặc cấu hình triển khai.

### Closure P5-COLLAB-20 `DONE` — 2026-09-18

Owner đã authorize completion retry duy nhất cho exact candidate
`e6cf0277258e944947fabec129b73937a4f14137` trên inherited disposable target và existing exact-two
tenant manifest. Packet v2 mới được materialize ở trạng thái `authorized-pending-live-validation`.
Direct deploy command trả generic failure sau provider handoff; fail-closed `adopt-live` sau đó chỉ
PASS khi cả hai Render disposable service cùng khớp exact candidate, áp initial `off` và giữ đúng
target đã phê duyệt. `provision-base` tái tạo đúng hai synthetic tenant ở ledger `42 false`; exact-two
provider preflight và negative cross-tenant isolation đều PASS.

Provider-observed hold chạy đủ 3.600 giây với `12.000` operation tổng (`6.000` mỗi document), `120`
metric sample, `12` semantic check và `50` reconnect event. P95
join/reconnect/convergence/ack/artifact là `3072/1632/575/446/1502 ms`; đúng 20 artifact
create/read-back và 20 restore/reuse sample. Cả tám drill reconnect/Control/Neon/B2/credential/
force-off/export-restore/revoke cùng soak cleanup đều PASS, không phát sinh chi phí.

Same-process finalizer materialize đủ sáu review PASS sau `18.001s` kể từ report end, nằm trong
freshness `300s`; completed packet chuyển sang `authorized`. Mandatory rollback PASS
`read_only -> off`. Final cleanup PASS: ledger `42 false`, whiteboard `off`, runtime not ready,
synthetic tenant/document/edit connection đều `0`, `identifiersLogged=false`. Không production,
shared staging, migration rollback, paid capacity hoặc log UUID/secret. P5-COLLAB-20 chuyển
`VERIFY -> DONE` và Phase 5 collaboration đóng. Supported profile vẫn là `FREE_PRIVATE_ALPHA` một
instance, không HA/autoscaling; whiteboard cuối cùng force-off. Bất kỳ enablement/ramp sau này cần
authorization mới và không được suy diễn từ closure này.

### Checkpoint P5-COLLAB-20 `VERIFY` — 2026-09-18

Exact candidate `4412bbdad87e425b5cdba00a05abcd8cf6b30008` đã được bind với inherited
disposable target và exact-two private manifest theo authorization hai giai đoạn. Exact Control/
Runtime candidate, initial `off`, evaluator mode, negative cross-tenant preflight và provider
readiness đều PASS; production/shared staging không bị chạm và không phát sinh paid capacity.

Hai provider-observed hold đều chạy đủ 3.600 giây cùng full reconnect/Control/Neon/B2/credential/
force-off/export/restore/revoke drill matrix. Lượt 1 có P95 join/reconnect/convergence/ack/artifact
`1229/1474/664/441/2926 ms`; lượt retry cuối là `1349/1793/587/444/6480 ms`. Cả hai chỉ FAIL
`artifactP95Ms <= 2500`; evaluator lượt cuối có đúng một error category và soak cleanup xác nhận
zero database/runtime/B2 residue. Ngưỡng không bị nới và six-review completion packet không được tạo.

Rollback cuối PASS theo `read_only -> off`; runtime not ready, document/edit connection `0/0`.
Cleanup cuối PASS với ledger `42 false`, whiteboard `off`, synthetic tenant/document `0/0`.
P5-COLLAB-20 giữ `VERIFY`; retry authorization hiện tại đã hết. Chẩn đoán local và measurement
correction đã hoàn tất; cần exact candidate cùng authorization mới trước live completion attempt khác.

Local diagnosis xác nhận harness cũ chỉ thu 4 artifact sample, vì vậy nearest-rank P95 luôn là
giá trị lớn nhất; mỗi sample còn bao trùm immutable existence check, upload và verified read-back.
Forward-only P5-COLLAB-20 contract v2 nay yêu cầu đúng 20 artifact sample và 20 restore/reuse sample,
giữ nguyên ngưỡng artifact P95 `2500 ms`. Restore RTO nay dùng riêng restore/reuse latency; finalizer
fail closed nếu thiếu sample. Mặc định lịch sử P5-COLLAB-19 vẫn là 4 sample và không bị viết lại.

Không có provider call trong checkpoint sửa này. Aggregate `pnpm test:collaboration:p520` đạt
`57/57`; P5-COLLAB-19 local regression, runtime `152 passed / 2 skipped`, lint, typecheck và build
đều PASS. Kết quả live vẫn cần một exact candidate mới và authorization mới. Review:
[P5_COLLAB_20_RAMP_EXIT_REVIEW.md](P5_COLLAB_20_RAMP_EXIT_REVIEW.md).

### Checkpoint P5-COLLAB-20 live v2 `VERIFY` — 2026-09-18

Owner đã authorize exact candidate `15cbef5f410833cf6299747bcdac3eaad6df0a64`. Packet
`p5-collab-20-ramp-exit-v2` được bind với inherited disposable fingerprint và existing exact-two
manifest. Exact candidate được deploy/adopt trên đúng hai Render disposable service; adapter áp
`off` trước evaluator mode. Preflight đầu fail closed vì base fixture đã bị cleanup ở lượt trước;
runner rollback an toàn, `provision-base` tái tạo đúng 2 synthetic tenant ở ledger `42 false`, sau đó
exact-two cross-tenant/provider preflight PASS với 2 document, 10 connection và 500 shape/document.

Provider hold chạy đủ 3.600 giây: `6.000/6.000` operation, `120` metrics sample, `12` semantic check,
`50` reconnect event. P95 join/reconnect/convergence/ack/artifact là
`1247/1697/653/450/2117 ms`; artifact gate `<=2500 ms` PASS với đúng 20 create/read-back và 20
restore/reuse sample. Full reconnect/Control 600s/Neon/B2/credential/force-off/export/restore/revoke
matrix, cost `0 USD` và soak cleanup `1257 ms` đều PASS.

Soak validator đã PASS report trong freshness window. Tuy nhiên task bị gián đoạn trước bước review;
khi tiếp tục, finalizer chạy lúc report age `4008s`, vượt contract `300s`, và từ chối chỉ với
`endedAt must be within five minutes of the trusted clock`. Exact binding, preflight, repository,
dependency/accessibility scope, sample counts, cost và publication đều PASS; six-review packet không
được materialize và freshness rule không bị nới.

Mandatory rollback retry PASS `read_only -> off`; runtime not ready, document/edit connection `0/0`.
Final cleanup PASS ledger `42 false`, whiteboard `off`, synthetic tenant/document `0/0`. Không
production/shared staging, không rollback migration, không paid capacity và không log UUID/secret.
P5-COLLAB-20 vẫn `VERIFY`.

Sau postmortem freshness, `p5-collab-20:soak-live` được harden để gọi live-review finalizer ngay
trong cùng tiến trình sau khi soak PASS và trước khi command trả về. Nếu soak fail, finalizer không
được gọi; nếu finalizer fail, command cũng fail closed. Regression xác minh đúng ordering này.
P5-COLLAB-20 local aggregate đạt `59/59`, P5-COLLAB-19 regression và ESLint PASS. Safeguard này
không gọi provider; cần authorization riêng cho exact candidate mới trước một completion attempt.

### Checkpoint P5-COLLAB-19 `DONE` — 2026-09-17

Render Control deploy `dep-dal159ek1f9s73db1bcg` và Runtime deploy `dep-dal15k6k1f9s73db2730` đã
được đồng bộ/redeploy trên exact disposable binding của commit
`22ebfe1bfecc95d782ee35f4a8049c32f25fdc50`; preflight PASS ở ledger `42 false`.

Provider run `p519-private-alpha-20260916T035303778Z` chạy đúng 3.600 giây trong cửa sổ
`2026-09-16T17:08:38.126Z..2026-09-16T18:08:38.126Z`, đạt `6.000/6.000` operation, `120` metrics
sample, `12` semantic check và `50` reconnect event. P95 join/reconnect/convergence/ack/artifact lần
lượt `867/1083/1134/445/1503 ms`; reconnect, Control outage đúng 600 giây, Neon, B2, credential,
force-off, incident/export/restore/revoke, publication và current-run owner sign-off đều PASS.

Lượt wrapper đầu xác minh report trong freshness window và chạy drill PASS nhưng cleanup `finally`
gặp `artifact_queue_unavailable`. Recovery và standalone cleanup sau đó PASS zero-residue; ledger cuối
`42 false`, whiteboard `off`, readiness false, document/edit connection `0/0`. Lượt wrapper thứ hai
bị freshness gate từ chối; validator không bị nới lỏng. Owner chấp nhận ngày 2026-09-17 dùng lần PASS
đúng hạn cùng cleanup PASS sau recovery làm closure. Local aggregate P5-19 và direct P5-16 matrix đã
PASS lại. Không rollback, production hoặc shared staging. P5-COLLAB-19 chuyển `VERIFY -> DONE`;
P5-COLLAB-20 mở ở `TODO` và cần authorization riêng. Acceptance:
[`P5_COLLAB_19_PRIVATE_ALPHA_ACCEPTANCE.md`](P5_COLLAB_19_PRIVATE_ALPHA_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-18 `DONE` — 2026-08-24

Workspace `P5-COLLAB-18 Internal Canary` có Organization Admin/Teacher/Student active membership,
synthetic fixture và exact-one allowlist. Quota 2 documents, 10 connections, 64 MiB, 600 operations/
phút được server/data plane enforce. Shared P5-09/P5-10 PostgreSQL integration gates và exact canary
audit xác nhận capability/tenant isolation, đúng roles/quota/fixture và whiteboard residue zero.

`pnpm test:collaboration:p518`, full `pnpm verify`, candidate `8d65898` và packaging fix `ccc1f13`
đều PASS; GitHub Verify `32712385493` và Security `32712385386` xanh cho packaging runtime. Render
Core API, control plane và collaboration runtime healthy/ready. Authenticated live metrics có đủ bốn
dependency signal; connections/documents/dirty/drain đều zero và không chứa board body/credential.
Disposable Neon/B2 giữ final `41 false`, `RPO=last_verified_artifact`, `RTO_MS=2042`. P5-COLLAB-15
physical Chrome/Edge + NVDA và P5-COLLAB-16/P5-COLLAB-17 force-off/outage/export/restore evidence
được kế thừa có kiểm soát vì P5-COLLAB-18 không đổi browser semantics, provider topology hoặc
snapshot format; P5-18 regression tương ứng PASS. Exact-one canary tenant enabled, tenant khác vẫn
fail closed/force-off. Final cleanup zero-residue PASS. P5-COLLAB-19 đã được mở. Acceptance:
[`P5_COLLAB_18_STAGING_ACCEPTANCE.md`](P5_COLLAB_18_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-17 `DONE` — 2026-08-24

Local force-off candidate đã khóa `.env.example`, Core API config/catalog/deployment guard và web
fallback ở `FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false`. Tenant override không thể vượt
deployment guardrail; toàn bộ 14 route whiteboard xác thực trước rồi fail closed `503
whiteboard_unavailable`, có privacy headers và không lộ tenant/document. Local aggregate PASS Core
API force-off packages, web `6/6`, P5-COLLAB-16 runtime `20/20`, outage `8/8`, collaboration client
`9/9` và Core API authority. Disposable environment validator PASS `3/3` và chỉ chấp nhận bốn exact
same-branch PostgreSQL roles cùng scoped B2 disposable. Full repository verify PASS; Go test/vet
được chạy lại bằng writable temporary `GOCACHE` vì Windows sandbox chặn default cache, không phải lỗi
source.

Exact candidate `637e8b5` đã push và GitHub Verify `32649917938`/Security `32649917980` đều PASS.
Disposable Neon/B2 PASS tại final `41 false`, idempotent, exact ACL/database gates, B2
`RTO_MS=1853` và `RPO=last_verified_artifact`. Sau disposable report, shared staging forward
`37 false -> 41 false -> 41 false`, worker ACL và read-only audit đều PASS. Render deploy
`dep-da5igfgu01pc73fca160` chạy đúng SHA, health/readiness/status trực tiếp và qua Pages proxy đều
PASS, log xác nhận deployment force-off.

Authenticated Teacher không thấy whiteboard entry point; không có provider/retry activity. Privacy
headers và authentication boundary PASS; exact Core API tests giữ private 503 contract cho 14 route.
Physical Chrome/Edge + NVDA regression của P5-COLLAB-15 được giữ làm bằng chứng a11y. Final cleanup
snapshot có zero document/relation/binding/B2 object và ledger vẫn `41 false`. P5-COLLAB-17 chuyển
`VERIFY -> DONE`; whiteboard chưa bật và P5-COLLAB-18 cần authorization riêng. Acceptance:
[`P5_COLLAB_17_STAGING_ACCEPTANCE.md`](P5_COLLAB_17_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-16 `DONE` — 2026-08-23

Candidate đã thêm exact production-module validation cho control-authority timeout/new-room
fail-closed, existing-room `enabled -> read_only -> off`, artifact binding-key overlap/revoke và
provider-independent next-generation restore giữ nguyên semantic hash. Root aggregate runner PASS
whiteboard runtime `20/20`, control/Neon/B2 outage guards `8/8`, collaboration-client portable/
reconnect `9/9` và Core API collaboration authority. Provider-backed disposable evidence 600 giây,
B2 rotation, immutable recovery và `RTO_MS=3096` từ P5-COLLAB-01/07/08/09 được reuse và ghi rõ
là historical evidence, không phải live drill mới.

Full `pnpm verify` PASS trong 35,9 giây, gồm format/OpenAPI drift/security/lint/typecheck/test/build,
Storybook, client bundle scan, toàn bộ Go test và `go vet`.

Failure contract công bố room mới fail closed; room hiện tại chỉ giữ read/export trong
`read_only`, sau đó `off` nếu không còn an toàn. RPO là last verified durable artifact; RTO
private-alpha do operator thực hiện tối đa 5 phút. Exit trigger, owner, provider-independent
generation swap, read-only fallback và rollback không dual-write đã khóa trong acceptance/runbook.
Không migration, provider mutation, shared-staging write hoặc deploy; production whiteboard tiếp tục
force-off. Exact candidate `1c1111c` đã push lên `origin/main`; GitHub Verify `32643119744` và
Security `32643119716` đều PASS. P5-COLLAB-16 chuyển `VERIFY -> DONE`; P5-COLLAB-17 là task runnable
tiếp theo. Acceptance:
[`P5_COLLAB_16_STAGING_ACCEPTANCE.md`](P5_COLLAB_16_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-15 `DONE` — 2026-08-23

Production semantic fallback đã được giới hạn 50 phần tử/trang, có focus handoff rõ giữa canvas và
danh sách văn bản. Adapter pinned Excalidraw `0.18.1` sửa accessible name của menu và landmark footer
lồng sai; portal có visible focus, forced-colors và reduced-motion riêng. Web unit `457/457`, typecheck,
lint và installed headful matrix đều PASS. Chrome `152.0.7977.54` và Edge `151.0.4129.101` đều Axe
`0/0`, PASS keyboard/tool/focus/reconnect/reflow. Firefox/Safari/mobile được ghi `UNAVAILABLE` đúng
phạm vi pilot. Owner physical Chrome/Edge + NVDA ở browser zoom 200% đã PASS dialog, named toolbar,
semantic fallback, reconnect/failure/restore announcement, forced-colors/reduced-motion và focus
recovery; Axe chỉ là bằng chứng hỗ trợ. Không Neon/B2/Render/shared-staging/deploy; production
whiteboard tiếp tục force-off.
Full repository `pnpm verify` trên final local candidate tree cũng PASS, gồm format, contract/client,
security, lint, typecheck, build, Storybook, Go test và `go vet`. Exact candidate `09449fc` đã push
lên `origin/main`; GitHub Verify `32638928557` và Security `32638928501` đều PASS. P5-COLLAB-15
chuyển `VERIFY -> DONE`; P5-COLLAB-16 là task runnable tiếp theo. Acceptance:
[`P5_COLLAB_15_STAGING_ACCEPTANCE.md`](P5_COLLAB_15_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-14 `DONE` — 2026-08-23

Candidate local đã bổ sung exact real-WebSocket benchmark cho `2 x 500`, `10 x 500` và `50 x 2.000`,
production runtime compaction/backpressure/cleanup soak, cùng production bundle lazy-load guard có raw/
gzip budgets. Ba profile đều PASS. Profile `50 x 2.000` quan sát join p95 `1.074,8 ms`, convergence p95
`897,5 ms`, input p95 `77,9 ms`, reconnect/recovery `123,8 ms`, snapshot encode `8,7 ms` với
`1.579.667 B`, CPU `28.687 ms`, heap delta `255.498.024 B`, received `62.197.063 B` và cleanup
`25,0 ms` về đúng zero.

Production runtime PASS 10 compaction cycles ở p95 `16,0 ms`, giữ semantic hash của 2.000 shapes;
50 reservations được release về zero. Noisy tenant/socket bị quota/backpressure chặn mà quiet tenant/
socket vẫn hoạt động. Production build PASS `4.466` modules; initial entry `485.826 B` không statically
import lazy whiteboard closure `2.253.775 B` raw/`660.631 B` gzip. Focused profile `3/3`, runtime `1/1`,
bundle regression `2/2`, TypeScript, ESLint và Prettier đều PASS. Cap private alpha giữ nguyên
`50 connections / 2.000 shapes`; không cần hạ thấp. Task không cần migration, Neon/B2/Render,
shared-staging write hoặc deploy; production tiếp tục force-off. Full repository `pnpm verify` và final
diff/no-secret review PASS. Exact candidate `e1b1201` đã push lên `origin/main`; GitHub Verify
`32621951917` và Security `32621951910` đều PASS. P5-COLLAB-14 chuyển `VERIFY -> DONE`;
P5-COLLAB-15 là task runnable tiếp theo.
Acceptance: [`P5_COLLAB_14_STAGING_ACCEPTANCE.md`](P5_COLLAB_14_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-13 `DONE` — 2026-08-23

Candidate local đã bổ sung exact artifact round-trip giữ SHA-256/semantic hash, quarantine không stage
restore cho corrupt/incompatible/oversize/malicious artifact, bounded B2 retry/last-good recovery và
stale-generation fence. Focused matrix PASS `7/7`; production envelope/object-store/worker regression
PASS `8/8`; secret-safe runner validation PASS `3/3`; full repository `pnpm verify` PASS với
whiteboard-runtime aggregate `136 passed`, `2 skipped`. Owner-authorized disposable replay PASS
`37 false -> 41 false -> 41 false`; exact collaboration-worker ACL và PostgreSQL control-plane PASS.
Hai Neon/B2 aggregate liên tiếp PASS tại final ledger `41 false`, gồm immutable binding, quarantine,
generation swap/stale fence, purge/retry và last-good recovery `RPO=last_verified_artifact`, RTO quan sát
`2654 ms`/`2472 ms`. Gate đã phát hiện và sửa cleanup fixture không idempotent; rerun cuối xanh và cleanup
zero. Final full `pnpm verify` PASS. Task không thêm migration mới, không log credential, không rollback,
shared-staging write hoặc deploy; production whiteboard tiếp tục force-off. Final diff/no-secret review
PASS. Exact candidate `e27b205` đã push lên `origin/main`; GitHub Verify `32617970222` và Security
`32617970234` đều PASS. P5-COLLAB-13 chuyển `VERIFY -> DONE`; P5-COLLAB-14 là task runnable tiếp theo.
Acceptance:
[`P5_COLLAB_13_STAGING_ACCEPTANCE.md`](P5_COLLAB_13_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-12 `DONE` — 2026-08-23

Production canonical authority đã PASS 25 deterministic offline cycles với ba actor, concurrent edits,
duplicate/out-of-order delivery và actor-local undo/redo sau remote edits. Mọi peer cùng restored provider
state hội tụ về exact semantic hash và causal watermark. Hai Hocuspocus WebSocket client với actor độc lập
cũng PASS 10 chu kỳ disconnect/edit/reconnect luân phiên.

Focused aggregate bao gồm authority guard, incompatible-session race và checkpoint compaction PASS `26/26`;
full repository `pnpm verify` PASS khi đặt `GOCACHE` trong workspace để phù hợp sandbox. Task này không cần
migration, Neon/B2 disposable, shared-staging write hoặc deploy; production whiteboard tiếp tục force-off.
Final diff/no-secret review PASS. Exact candidate `0b4ee83` đã push lên `origin/main`; GitHub Verify
`32607940566` và Security `32607940492` đều PASS. P5-COLLAB-12 chuyển `VERIFY -> DONE` và
P5-COLLAB-13 là task runnable tiếp theo. Acceptance:
[`P5_COLLAB_12_STAGING_ACCEPTANCE.md`](P5_COLLAB_12_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-11 `DONE` — 2026-08-23

Local candidate đã đóng khe TOCTOU giữa one-time grant exchange và session reservation bằng exact lease
revalidation lần hai. Revoke trong handshake hoặc control-plane validation outage nay fail closed trước
`connection_ok`; reader direct mutation không tới observer hoặc durable checkpoint. Broker có test TTL tối
đa 60 giây, malformed credential, consume/revoke race và replay denial. Runtime aggregate bao phủ raw frame,
unauthenticated queue, awareness, update/document, connection/reconnect/rate cap, deterministic malformed
fuzz và cumulative CRDT amplification với bounded error vocabulary.

`pnpm test:collaboration:p511` PASS focused Go và runtime `62/62`; full repository `pnpm verify` cũng PASS.
P5-COLLAB-11 không cần migration, Neon/B2 disposable, shared-staging write hay deploy; production whiteboard
tiếp tục force-off. Final diff/no-secret review PASS. Exact candidate `f9d1381` đã push lên `origin/main`;
GitHub Verify `32605702341` và Security `32605702349` đều PASS. P5-COLLAB-11 chuyển `VERIFY -> DONE`;
P5-COLLAB-12 là task runnable tiếp theo. Acceptance:
[`P5_COLLAB_11_STAGING_ACCEPTANCE.md`](P5_COLLAB_11_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-10 `DONE` — 2026-08-23

Local candidate đã hoàn tất authorization matrix cho organization admin/owner/teacher/TA/student/guest/
removed/inactive, kiểm chứng cả `view/edit/present` projection và lifecycle allow/conceal. Unknown hoặc
mixed forged organization role nay fail closed trước repository. Snapshot list có bounded opaque cursor
scope theo tenant, actor, document, current generation và limit; PostgreSQL dùng deterministic
`(created_at, id)` keyset cùng exact tenant/document predicate. HTTP/OpenAPI/generated client đã cập nhật;
malformed hoặc cross-scope cursor bị từ chối.

Focused Go tests, integration-tag compile, runner environment tests và full `pnpm verify` PASS; diff check
và local no-secret review cũng PASS. Task không thêm migration;
exact disposable gate yêu cầu owner direct + runtime pooled trên cùng branch đã ở clean ledger `41 false`.
Lần preflight disposable đầu tiên đã nạp file local theo allowlist, xác nhận exact role/cùng branch PASS rồi
dừng an toàn vì ledger thực tế là `37 false`; không migration/rollback/fixture write. Chưa write shared
staging và chưa deploy. Sau khi được phép, retained forward-only migration PASS
`37 false -> 41 false -> 41 false`; exact PostgreSQL IDOR/cursor/idempotency, cross-tenant
document/snapshot/export/restore concealment, forged/inactive authority, runtime authorization và cleanup
đều PASS tại `41 false`. Gate đã phát hiện typed snapshot seed parameter `42P08`; fixture được explicit
cast `timestamptz` trước aggregate rerun xanh. Exact candidate `383f6c7` đã push lên `origin/main`;
GitHub Verify `32596587875` và Security `32596587867` đều PASS. P5-COLLAB-10 chuyển `VERIFY -> DONE`;
P5-COLLAB-11 là task runnable tiếp theo. Acceptance:
[`P5_COLLAB_10_STAGING_ACCEPTANCE.md`](P5_COLLAB_10_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-09 `DONE` — 2026-08-22

ADR-0037 và local candidate đã triển khai feature `classroom_whiteboards` default false/deployment
force-off, bốn per-tenant quota clamp cho document/connection/storage/operation và emergency runtime
mode `enabled/read_only/off`. Core API, artifact workflow, grant broker và Hocuspocus runtime cùng
fail closed; `read_only` giữ projection/list/export nhưng khóa mutation/snapshot/restore và ép grant về
`view`, còn `off` conceal public surface. Noisy tenant operation/connection budget tách biệt; metrics
chỉ có bounded label vocabulary và không nhận raw tenant/document/user/provider ID hoặc content.
Migration forward `000041` chỉ mở rộng typed feature/quota constraint và retained integration tests đã
nâng latest ledger lên `41 false`. ADR/runbook, focused kill-switch/quota/cardinality tests và full
`pnpm verify` PASS. Exact Neon disposable đã forward-only `37 false -> 41 false -> 41 false`; exact role
preflight, default/force-off, concurrent document/storage quota, tenant isolation và runtime focused `30/30`
đều PASS, final ledger giữ `41 false`. Gate đầu đã phát hiện và sửa truy vấn dung lượng dùng cột snapshot
không tồn tại; gate database và full verify PASS sau sửa. Complete diff/no-secret review PASS; candidate
`983ada6` đã push lên `origin/main`, GitHub Verify `32586930775` và Security `32586930710` PASS. Một retained
media ACL expectation ở ledger `40` được CI phát hiện, căn chỉnh về latest ledger `41`, focused Go test và
commit hooks PASS trước lần chạy xanh cuối. P5-COLLAB-09 chuyển `VERIFY -> DONE`; P5-COLLAB-10 runnable.
Không rollback/shared-staging write/deploy; production tiếp tục force-off và disposable branch được giữ lại.
Acceptance: [`P5_COLLAB_09_STAGING_ACCEPTANCE.md`](P5_COLLAB_09_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-08 `DONE` — 2026-08-22

ADR-0036 và local candidate đã hoàn tất: Yjs state-vector reconnect giữ in-memory unsent update;
copy-on-write compaction kiểm tra exact state vector/full encoded state, giữ delete tombstone và không
đụng live actor-local undo. Browser có one-shot deterministic terminal classification; authority change
buộc refetch Core API projection/grant mới, còn runtime outage không tạo retry loop. Real Hocuspocus
reconnect test, corrupt/oversize/tombstone/semantic/undo tests và generation/revoke fence tests đều xanh.
Runtime `120` PASS, collaboration client `9/9`, web `456/456`, Go collaboration, lint/typecheck và
P5-08 secret-safe runner `2/2` PASS. Exact Neon/B2 disposable recovery PASS tại ledger `40 false`:
`RPO=last_verified_artifact`, `RTO_MS=3096`, corrupt quarantine, generation/revoke fence và cleanup đều
PASS; full `pnpm verify` cũng PASS. Exact candidate `4f05ae6`, GitHub Verify `32571244058` và Security
`32571244052` PASS. Không migration/shared-staging write/deploy; production tiếp tục force-off.
P5-COLLAB-09 runnable. Acceptance:
[`P5_COLLAB_08_STAGING_ACCEPTANCE.md`](P5_COLLAB_08_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-07 `DONE` — 2026-08-22

Local candidate đã có ADR-0035, forward migrations `000038`-`000040`, durable artifact
command/checkpoint/purge
queue, Core API async snapshot/export và restore-after-worker flow. Runtime tạo signed bounded
envelope, bind exact immutable B2 object version, verify checksum/scope/engine/schema trước publish
hoặc restore, quarantine portable scene không an toàn và stage target checkpoint trước atomic
generation swap. Maintenance dùng dedicated entrypoint, exact version deletion, bounded lease/batch
và PostgreSQL `SKIP LOCKED` functions.

Runtime PASS `115` local test và full `pnpm verify` PASS. Portable scene, Core API,
OpenAPI/generated client, integration-tag ACL compile, OCI/SBOM/security, lint, typecheck, build,
Storybook, bundle security và Go test/vet đều xanh. Runner
`run-p507-disposable.mjs` chỉ allowlist bốn Neon URL và scoped B2 variables, kiểm tra same-branch/four
roles/direct-vs-pooled và không in credential. Neon/B2 disposable đã PASS final aggregate ở ledger
`40 false`: exact four-role ACL/PUBLIC deny, immutable B2 version binding/checksum/tamper rejection,
snapshot/export, corrupt quarantine, valid one-generation restore, hai claimant `SKIP LOCKED`, bounded
retry và exact cleanup đều xanh. Migration `000040` giữ active restore bắt buộc exact source nhưng cho
terminal restore redaction source sau retention purge. Review/no-secret và exact candidate `060fcc7`
đã PASS GitHub Verify `32566880308` cùng Security `32566880332`. Không shared-staging write/deploy;
production whiteboard giữ force-off. P5-COLLAB-08 runnable. Acceptance:
[`P5_COLLAB_07_STAGING_ACCEPTANCE.md`](P5_COLLAB_07_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-06 `DONE` — 2026-08-22

Classroom media shell đã có typed tool registry, lazy whiteboard drawer, focus handoff và lifecycle
prepare/open/suspend/resume/close theo exact capability do Core API chiếu. Student `view` được ép
read-only ở browser adapter lẫn data plane; client không suy quyền từ role. Excalidraw/canonical
session chỉ mount khi document đang open và grant được phép exchange, rồi cleanup provider/Y.Doc khi
drawer đóng.

Vite production build tách Excalidraw khỏi initial entry; automated gate xác nhận media tile và
LiveKit room không remount. Targeted UI/page `59/59`, collaboration-client `2/2`, Core API, typecheck,
build và full repository verify đều PASS. Không có migration, live Neon/shared-staging write, Render
deploy hoặc secret rotation; production whiteboard giữ force-off tới P5-COLLAB-17. Acceptance:
[`P5_COLLAB_06_ACCEPTANCE.md`](P5_COLLAB_06_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-05 `DONE` — 2026-08-22

Whiteboard runtime Hocuspocus/Yjs riêng đã dùng authenticated WebSocket và exchange exact one-time
grant/authority lease từ P5-COLLAB-04; Core API REST/LiveKit DataChannel không làm document transport.
Server bind Origin/document/tenant/actor/capability/generation/writer fence, ép `view` read-only và
đóng connection khi runtime mode/revoke authority thay đổi. Awareness chỉ là ephemeral state, được
giới hạn size/rate/depth, ép một state mỗi connection, thay identity client bằng actor đã xác thực và
không ghi checkpoint.

Runtime fail closed với frame/update/document cap, rolling message/byte budget, reconnect storm và
quota global/tenant/document/actor. Profile `FREE_PRIVATE_ALPHA` giữ đúng một authority bằng dedicated
PostgreSQL session advisory lock; duplicate process bị chặn trước listen, mất session/lock làm readiness
đỏ và đóng active authority. `/livez`, `/readyz`, bearer `/metrics`, checkpoint-failure latch và bounded
SIGTERM drain đều được harden; authentication hoàn tất sau khi drain bắt đầu vẫn bị deny.

Runtime package PASS `100/100` test trên 12 file; typecheck, ESLint, build, OCI dependency boundary và
full repository verify đều xanh. Advisory-lock gate dùng hai PostgreSQL session giả lập độc lập;
P5-COLLAB-05 không chạy live Neon disposable mới, migration, shared Neon write, Render deploy hoặc
external secret rotation.
`public.collaboration_document_checkpoints` vẫn chỉ là disposable Gate F fixture; durable
snapshot/import/export/restore worker thuộc P5-COLLAB-07. Production whiteboard giữ force-off tới
P5-COLLAB-17. Acceptance:
[`P5_COLLAB_05_STAGING_ACCEPTANCE.md`](P5_COLLAB_05_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-04 `DONE` — 2026-08-21

Core API đã có one-time collaboration grant broker với credential ngẫu nhiên chỉ lưu digest, TTL
hard-cap 60 giây, rate/capacity bounds và atomic single-winner exchange. Grant cùng authority lease
bind exact tenant/actor/session/document/capability/Origin/provider/generation/writer fence. Exchange
và refresh đều revalidate current membership, source, capability, generation và revoke state từ
PostgreSQL; suspend/close/restore purge pending grant cùng active lease.

Internal exchange/validate API dùng bearer token digest constant-time, strict bounded body, no-store
và privacy-safe errors. Whiteboard runtime revalidate lease mỗi 750 ms, enforce `view` read-only và
đóng exact connection code `4403` khi authority mất. Replay, expiry, forged binding, 32-way concurrent
single winner, abuse limit, revoke, privacy/no-secret-log và bounded disconnect đều PASS. Runtime
5 files/17 tests, PostgreSQL integration-tag compile, `git diff --check` và full `pnpm verify` PASS.

Task không thêm migration, không kết nối/write Neon mới, không forward shared staging và không deploy.
Accepted private-alpha broker là process-local cho một Core API instance; restart invalidates mọi
grant/lease fail closed. Shared atomic broker trước multi-instance và transport quota/backpressure
được giữ cho P5-COLLAB-05/P5-COLLAB-09. Production whiteboard tiếp tục force-off tới P5-COLLAB-17.
Acceptance: [`P5_COLLAB_04_STAGING_ACCEPTANCE.md`](P5_COLLAB_04_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-03 `DONE` — 2026-08-21

Đã thêm strict OpenAPI và generated TypeScript client cho create/get/open/suspend/resume/close,
capability, grant exchange, snapshot list/create, export, portable import validation và restore.
Core API đã có module service/PostgreSQL repository/HTTP handlers với exact media-space authority,
session + CSRF + expected-tenant guard, opaque provider document name, uniform concealed `404`,
bounded JSON và no-store/privacy headers. Lifecycle/restore dùng optimistic version, expected
generation và idempotency receipt; replay trả đúng historical receipt projection thay vì current
document đã đổi tiếp.

Grant broker thật được giữ cho P5-COLLAB-04 và snapshot/export worker cho P5-COLLAB-07; endpoint hiện
tại fail-closed `503` khi dependency chưa được inject. Local service/HTTP/package tests, OpenAPI
generation/check, integration-tag compile và full `pnpm verify` PASS ngày 2026-08-21. Deployment guard
`COLLABORATION_CONTROL_PLANE_ENABLED` mặc định `false`, nên Core API không khởi tạo control plane và
tenant override không thể bypass production force-off. Runner disposable tự nạp đúng ba biến allowlist,
kiểm tra owner direct/runtime pooled cùng branch và không in credential. Neon P5-COLLAB-03 disposable
repository gate đã PASS ở ledger `37 false`: runtime create/read/capability exact ACL, lifecycle
CAS/idempotency receipt, snapshot projection, restore generation swap, tenant concealment và cleanup đều
xanh. Review/no-secret PASS; exact candidate `647ffe4` đã PASS GitHub Verify `32461523646` và Security
`32461523627`. P5-COLLAB-03 chuyển `DONE`; P5-COLLAB-04 là task runnable tiếp theo. Không có
migration/shared-staging forward hoặc deploy mới; production whiteboard vẫn force-off tới P5-COLLAB-17.
Acceptance: [`P5_COLLAB_03_STAGING_ACCEPTANCE.md`](P5_COLLAB_03_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-02 `DONE` — 2026-08-21

Shared acceptance candidate `ef2b277` đã PASS GitHub Verify `32436740864` và Security
`32436740906`. Neon shared owner preflight read-only xác nhận `36 dirty=false`; forward migration và
idempotency đạt `36 false -> 37 false -> 37 false`. Exact runtime column ACL, maintenance/PUBLIC deny,
schema/ownership boundary và final repeatable-read snapshot đều PASS.

Final snapshot xác nhận đúng năm relation P5-COLLAB-02, tất cả `0` row, không có scene/Yjs operation/
history/awareness/undo authority trong PostgreSQL. Không rollback, không production migration, không
UI/runtime deploy và whiteboard production tiếp tục force-off tới P5-COLLAB-17. P5-COLLAB-02 chuyển
`DONE`; P5-COLLAB-03 là task runnable tiếp theo. Acceptance:
[`P5_COLLAB_02_STAGING_ACCEPTANCE.md`](P5_COLLAB_02_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-02 local candidate — 2026-08-20

Đã thêm forward migration `000037_whiteboard_control_plane` cho tenant/source/document binding,
lifecycle/version, current/revoke generation, capability policy, immutable snapshot catalog và
bounded command idempotency receipt. Migration pin Excalidraw `0.18.1`, Yjs `13.6.27`, Hocuspocus
`4.6.0`, retention private-alpha 14 ngày và không lưu scene/Yjs operation/history/awareness/undo.

Exact ACL harness cấp runtime table-level `SELECT` nhưng chỉ column-level `INSERT/UPDATE`; không
`DELETE`, DDL hoặc ownership. Generation/snapshot/receipt immutable; maintenance và `PUBLIC` không có
DML P5-COLLAB-02. PostgreSQL integration gate kiểm tra lifecycle CAS/idempotency, atomic restore
generation swap, foreign-tenant deny và information-schema authority boundary. Local unit/static,
retained media regression, integration-tag compile và full `pnpm verify` PASS; CI PostgreSQL 17 đã
được nối gate.

Neon disposable đã PASS forward/idempotent `36 false -> 37 false -> 37 false`, exact ACL, lifecycle
CAS, restore generation swap, foreign-tenant denial và no-second-authority proof; branch được giữ
lại. Candidate schema `0374249` và shared acceptance harness `ef2b277` đều PASS GitHub
Verify/Security. Shared staging đã forward/idempotent tới `37 false`, exact ACL/final snapshot PASS;
không rollback/production migrate/deploy và whiteboard production tiếp tục force-off. Acceptance:
[`P5_COLLAB_02_STAGING_ACCEPTANCE.md`](P5_COLLAB_02_STAGING_ACCEPTANCE.md).

### Checkpoint P5-COLLAB-01 `DONE` — 2026-08-20

Owner đã chấp thuận `FREE_PRIVATE_ALPHA`: một Render runtime instance Singapore, không HA/Redis/
multi-region, chấp nhận spin-down/cold-start và gián đoạn ngắn, hard cap `0 USD`; chạm quota thì
whiteboard giữ force-off. RPO/RTO candidate, `SEV-1/SEV-2` và B2 Object Lock disabled cho private
alpha cũng được chấp thuận.

Quota evidence chỉ giữ aggregate redacted: Render `35.12/750` free instance hours, `454 MB/5 GB`
bandwidth, `15/25` services, `42/500` pipeline minutes và current/projected `0.00 USD`; Neon
`9.55/100` CU-hours, `0.12/0.5 GB` storage, `0.02/5 GB` transfer, `8/10` branches; B2 `1.0 GB` average
stored, `0 GB` downloaded, `709` transactions và current cost `0.00`. Không lưu ảnh/account ID/email/
credential vào repository.

Primary on-call `Bá Sáng`, backup `Duy Mạnh`, security incident owner `Bá Sáng`, cost owner `Bá Sáng`.
Gate A-F đều đóng; ADR-0034 chuyển `Accepted`, P5-COLLAB-01 và P5-COLLAB-02 đều `DONE`.
P5-COLLAB-03 là task runnable tiếp theo. Collaboration plane production chưa deploy và whiteboard
production tiếp tục force-off tới P5-COLLAB-17 exact staging.

### Checkpoint P5-COLLAB-01 Gate F.3 provider drill PASS — 2026-08-20

Ba sustained outage disposable đã giữ đủ `600` giây với 10 probe mỗi 60 giây: Control authority và
Neon làm Runtime `/readyz=503` nhưng giữ `/livez=200`, B2 giữ runtime/checkpoint available trong khi
credential bị revoke. Cả ba đều tự/được khôi phục và final readiness/provider probe PASS.

Neon chỉ đổi exact role disposable `tutorhub_collab_f3` sang `NOLOGIN` rồi tự `LOGIN` trong `finally`;
không suspend compute hoặc chạm branch `staging`. B2 r1 negative `401` trong toàn bộ cửa sổ, r2
positive read/write/checksum và post-redeploy PASS. Control current/next overlap, Runtime cutover và
retirement PASS; cả hai token trước trả `401`, replacement trả `200`. Một token test xuất hiện trong
tool output đã được coi là compromised và rotate ngay, không còn được chấp nhận.

Final provider result PASS `hocuspocus_sync`, `checkpoint_recovery`, `b2_read_after_write`,
`snapshot_dependency_up`, portable Excalidraw semantic restore round-trip và `cleanup_zero`. Provider
sub-gate F.3 đã đóng; Gate F/P5-COLLAB-01 vẫn `VERIFY/IN PROGRESS` vì quota dashboard, named
primary/backup/security/cost owner và explicit no-HA/cold-start/RPO/RTO/retention approval còn thiếu.
ADR-0034 giữ `Proposed`; shared staging và production tiếp tục force-off.

### Checkpoint P5-COLLAB-01 Gate F.3 SIGTERM/drain + post-redeploy recovery — 2026-08-20

Render đã manual deploy lại exact candidate `7febbce` bằng deploy `dep-da39i0ibkg8c7381o8i0`. Instance
cũ `[tnmnl]` nhận SIGTERM và ghi đúng bounded evidence `drain_started`, sau đó `drain_complete` với
`outcome=ok`, `duration_bucket=lt_100ms`; không có `drain_failed`. Instance mới `[kz697]` lên `live`,
`/readyz=200`.

Post-redeploy provider drill PASS `hocuspocus_sync=true`, `checkpoint_recovery=true`,
`b2_read_after_write=true`, `cleanup_zero=true`, `cold_start_bucket=lt_5s`. Checkpoint này đóng sub-gate
SIGTERM/drain + post-redeploy recovery theo disposable runbook. Gate F vẫn `3/4 VERIFY`; còn sustained
control/Neon/B2 outage, credential rotation/restore và owner/provider checklist. Shared staging và
production tiếp tục force-off.

### Checkpoint P5-COLLAB-01 Gate F.3 baseline provider drill — 2026-08-20

Disposable-only preflight PASS với region `singapore`, hard cap `0 USD` và toàn bộ secret bắt buộc
được phát hiện mà không in giá trị. Neon provision PASS `schema_ready=true`, `acl_exact=true`. Runtime
Render Free đã deploy exact commit `7febbce`; GitHub Verify `32335427818` và Security `32335427799`
PASS, gồm Browser E2E `28 passed / 1 skipped`, secret scan và whiteboard OCI/SBOM/vulnerability scan.

Provider baseline trên Render/Neon/B2 thật PASS bounded evidence:
`hocuspocus_sync=true`, `checkpoint_recovery=true`, `b2_read_after_write=true`,
`cleanup_zero=true`, `cold_start_bucket=lt_5s`. Candidate sửa B2 immutable/content-addressed write theo
GET/checksum/PUT/read-after-write vì B2 S3 trả `501` cho conditional `If-None-Match` trên `PutObject`;
runtime đồng thời cập nhật document gauge sau Hocuspocus unload. Runtime unit `14/14`, lint,
typecheck và build PASS.

Baseline không tự đóng toàn bộ F.3. Còn real SIGTERM/drain + post-redeploy recovery, sustained
control/Neon/B2 outage, credential rotation/restore và owner/provider checklist. Gate F giữ
`3/4 VERIFY`, P5-COLLAB-01 giữ `IN PROGRESS`, ADR-0034 giữ `Proposed`; shared staging/production vẫn
force-off. Exact runbook: `docs/P5_COLLAB_01_GATE_F3_DISPOSABLE.md`.

### Checkpoint P5-COLLAB-01 Gate F.3 automation prepared — 2026-08-19

Đã thêm disposable-only control fixture, strict secret-safe preflight, Render Free Singapore
Blueprint, Neon checkpoint schema/exact-ACL provision, real Hocuspocus/Neon/B2 provider round-trip và
600-second control-outage runner. Local control/preflight tests PASS `4/4`; whiteboard runtime tiếp tục
PASS lint, typecheck, build và unit/integration `9/9`.

Exact candidate tree `def10c0` đã PASS GitHub Verify `32255600426` và Security `32255600491`;
Secret scan xác nhận không có leak. Đây là CI acceptance của automation, chưa phải provider evidence.

Chưa có provider evidence: chưa tạo/kết nối Render/Neon/B2 disposable, chưa chạy cold-start,
SIGTERM/redeploy, sustained Neon/B2 outage hoặc credential rotation. Gate F giữ `3/4 VERIFY`,
P5-COLLAB-01 giữ `IN PROGRESS`, ADR-0034 giữ `Proposed`, shared staging/production vẫn force-off. Runbook:
`docs/P5_COLLAB_01_GATE_F3_DISPOSABLE.md`.

### Checkpoint P5-COLLAB-01 Gate F.2 exact OCI/SBOM — 2026-08-19

GitHub exact builder đã build production Dockerfile của candidate `2731387`. Security run
`32245999597` PASS toàn bộ secret/repository/Core API container/CodeQL và whiteboard runtime OCI
gates; riêng job whiteboard `96046520294` PASS build, production dependency boundary, Trivy
HIGH/CRITICAL, CycloneDX SBOM validation và artifact retention. Artifact `whiteboard-runtime-sbom`
ID `9362612946` chứa SBOM cùng exact built OCI image ID; archive digest là
`sha256:6016ae0c30ead0b837868b1884e1d66cb042262d1179caf66999a04ca3e7bef7`. Verify run
`32245999557` PASS quality/integration, local environment smoke và Browser E2E (`28 passed,
1 skipped`).

Gate F đạt `3/4 VERIFY`. Còn bắt buộc: chạy exact candidate trên disposable Render Free Singapore với
Neon/B2 để đo cold-start/restart/reconnect/cleanup-zero, sustained outage, SIGTERM/drain,
credential rotation và restore; sau đó owner/ops xác nhận single-instance/no-HA/cold-start/free quota,
on-call, retention, RPO/RTO và chuyển ADR-0034 khi đủ evidence. Không deploy provider, không database
write, không đọc `.env*.local`; production whiteboard tiếp tục force-off.

### Checkpoint P5-COLLAB-01 Gate F.1 OCI source candidate — 2026-08-19

Đã tách runtime production candidate vào `services/whiteboard-runtime`: Hocuspocus/Yjs server thật,
exact control-plane/grant exchange, Neon binary checkpoint, B2 immutable portable snapshot,
readiness/liveness/authorized metrics, bounded abuse controls và SIGTERM drain fail-closed.
Dockerfile multi-stage pin Node `24.15.0` bằng public manifest digest, chạy user `node` và chỉ đóng gói
bốn dependency production exact-pin. Security workflow đã có image build, Trivy HIGH/CRITICAL,
CycloneDX SBOM validator và SARIF/artifact upload pin SHA.

Runtime unit/integration `9/9`, lint, typecheck, build, OCI static guard, GitHub Actions security guard,
production dependency audit không có known vulnerability và isolated production package
(`runtime_dependencies=4`, không có test provider) đều PASS. Không đọc `.env*.local`, không kết nối
provider, không database write, không deploy và không push. Máy hiện tại
không có Docker/Trivy/Syft nên actual image digest/SBOM/vulnerability scan vẫn mở; P5-COLLAB-02 phải
tạo schema/ACL và P5-COLLAB-04 phải cung cấp control endpoints trước khi service có thể ready thật.
Gate F giữ `2/4 VERIFY`, P5-COLLAB-01 giữ `IN PROGRESS`, ADR-0034 giữ `Proposed`.

### Checkpoint P5-COLLAB-01 Gate F isolated runtime/operations — 2026-08-19

Đã khóa exact candidate Node `24.15.0`, Hocuspocus `4.6.0`, Yjs `13.6.27`, Excalidraw `0.18.1`.
Ngày 2026-08-19 owner chọn `FREE_PRIVATE_ALPHA`: một Render Free instance Singapore, không Redis,
Neon binary checkpoint và B2 immutable portable snapshot trong free allowance; hard cap `0 USD` và
không automatic upgrade. Render Standard x2 + Redis Cloud paid Multi-AZ chỉ là production HA path đã
hoãn. Chưa provision hoặc deploy collaboration topology.

Executable Gate F contract PASS `18/18`: two-node atomic one-time grant/global quota, opaque keyed
document name, single-writer lease/fencing/takeover, watermark/checkpoint/drain, sustained 10 phút
control/coordination/persistence outage fail closed, snapshot-storage degradation, server kill switch,
rotation 4 credential kind, snapshot binding-key recovery qua retention rồi old-key denial, provider exit,
bounded log/metric labels và cost/quota threshold. Full unit/integration `53/53`, lint, typecheck, build,
dependency/license và bundle structure/security regression tiếp tục PASS.

Gate F hiện `2/4 PASS` và giữ `VERIFY`: isolated outage/operations cùng observability/runbook đã xanh;
disposable Render Free/Neon/B2 cold-start/restart/outage/rotation/restore drill, exact OCI/SBOM,
free-quota evidence, no-HA/on-call/retention/RPO/RTO owner approval và ADR-0034 `Accepted` còn bắt
buộc. Paid two-node/Redis drill chỉ bắt buộc trước public beta/production. P5-COLLAB-01 giữ
`IN PROGRESS`, production force-off; không shared staging, không database write và không đọc
`.env*.local`. Chi tiết:
[P5_COLLAB_01_RUNTIME_OPERATIONS.md](P5_COLLAB_01_RUNTIME_OPERATIONS.md).

### Checkpoint P5-COLLAB-01 Gate E installed browsers — 2026-08-19

Physical harness headed đã chạy trên browser Windows cài thật: Chrome `151.0.7922.140` và Edge
`151.0.4129.86`. Cả hai PASS 11 toolbar tool accessible role/name, semantic pagination page 2/10,
200%-equivalent reflow không horizontal overflow, forced colors/reduced motion, Axe default/forced
colors `0/0` và deterministic cleanup focus về "Mở bảng Excalidraw". Bằng chứng bounded được lưu ở
ignored path `test-results/p5-collab-01-gate-e-physical/evidence.json`; không chứa secret hoặc nội
dung bảng riêng tư.

Harness đã phát hiện nested Excalidraw footer tạo landmark `contentinfo` không top-level. Adapter sửa
bounded thành named group và thêm regression assertion, song song với mobile main-menu repair trước
đó. Full run còn phát hiện projection programmatic có thể bị browser khác echo thành local mutation,
làm actor-local undo không lùi đúng semantic contribution. Adapter nay suppress projection echo cho
tới trusted pointer/keyboard input; canonical undo/redo bỏ qua projection-only revision. Sau fix,
Playwright A–E PASS `6/6`, targeted undo regression PASS, full unit/integration `39/39`, lint,
typecheck và build đều PASS.

UI automation đã kiểm tra accessibility tree thật nhưng không tự suy thành âm thanh PASS. Owner sau
đó xác nhận NVDA `2026.1.1.55980` đã đọc đúng toolbar/mode/semantic fallback và
reconnect/error/focus recovery trên Chrome/Edge. Gate E đạt `4/4 PASS`. P5-COLLAB-01 vẫn
`IN PROGRESS` vì Gate F owner/provider closure còn mở; ADR-0034 vẫn `Proposed` và production vẫn
force-off. Không có database/shared staging/deploy hoặc `.env*.local` access trong checkpoint này.

### Checkpoint P5-COLLAB-01 Gate E automated — 2026-08-19

Exact Excalidraw/Y.Doc/Hocuspocus candidate đã PASS local profile 2 × 500, 10 × 500 và 50 × 2.000.
Đã đo join/convergence p95, aggregate CPU, heap delta, client receive bytes, encoded state và
cleanup; 50-client profile chạm đúng connection ceiling 50, hội tụ cùng semantic hash rồi cleanup
`activeConnections=0`. Số đo/budget cụ thể nằm tại
[P5_COLLAB_01_LOAD_ACCESSIBILITY.md](P5_COLLAB_01_LOAD_ACCESSIBILITY.md), không tự suy thành
production SLO/multi-node approval.

Candidate có semantic companion từ cùng canonical scene: type/text/binding/image warning,
position/size, 50 mục mỗi trang và focus handoff hai chiều; companion vẫn đọc được khi engine đóng
và không trở thành writer thứ hai. Automated Axe/keyboard/open-close/focus/200%-equivalent reflow/
forced-colors/reduced-motion PASS; upstream Excalidraw 0.18.1 mobile main-menu thiếu accessible name
được sửa bounded tại adapter, không allowlist violation. Playwright A–E PASS `6/6`; Gate E load
`3/3`, semantic unit `1/1` và full unit/integration `39/39` PASS. Lint, typecheck, build,
dependency/license cùng bundle structure/security đều xanh.

Gate E automated `3/3` đã đóng nhưng physical NVDA speech vẫn bắt buộc, nên Gate E
giữ `3/4`, P5-COLLAB-01 vẫn `IN PROGRESS`, ADR-0034 vẫn `Proposed` và production vẫn force-off.
Không có database/shared staging/deploy hoặc `.env*.local` access trong checkpoint này.

### Checkpoint P5-COLLAB-01 Gate D snapshot/recovery — 2026-08-19

Đã triển khai durable snapshot fixture cô lập cho exact Excalidraw/Y.Doc authority. Immutable artifact
khóa Excalidraw `0.18.1`, canonical schema v1, Yjs `13.6.27`, causal watermark, semantic hash,
byte/object cap, SHA-256 và HMAC tenant/document/generation binding; object key ngẫu nhiên không chứa
raw scope. Catalog chỉ publish sau temporary write, read-back verification và atomic rename.

Restart lấy đúng last-good provider state; artifact bị gián đoạn trước catalog publication không thể
được restore. Corrupt/checksum/binding/provider mismatch bị quarantine với bounded reason và không đổi
current generation. Restore hợp lệ dựng authority mới trước rồi atomic generation `1 -> 2`; lock chặn
hai restore worker cùng sở hữu next generation và Gate C từ chối stale grant. Portable canonical JSON
không chứa Yjs/provider state nhưng round-trip sang authority mới giữ nguyên semantic hash; external
fetch, active SVG/script, path traversal, tamper và version không hỗ trợ đều fail closed.

Gate D `4/4 DONE`; portable tests `2/2`; full unit/integration `34/34`, lint, TypeScript, Excalidraw-only
build, exact dependency/license và bundle structure/security đều PASS. Expected large-chunk warning
vẫn thuộc Gate E. Đây chưa phải PostgreSQL/B2/durable-worker hoặc runtime production approval;
P5-COLLAB-01 vẫn `IN PROGRESS`, ADR-0034 vẫn `Proposed`, production force-off. Chi tiết tại
[P5_COLLAB_01_SNAPSHOT_RECOVERY.md](P5_COLLAB_01_SNAPSHOT_RECOVERY.md).

### Checkpoint P5-COLLAB-01 Gate C authorization — 2026-08-19

Đã triển khai authorization fixture cô lập cho exact Excalidraw/Y.Doc/Hocuspocus candidate. Control
plane phát opaque one-time grant mặc định 30 giây/tối đa 60 giây từ server-resolved membership và bind
exact Origin/session/tenant/document/generation/actor/capability; chỉ lưu hash trong memory và response
dùng `no-store`/`no-referrer`. Browser không lưu grant vào URL, DOM, cookie, localStorage hoặc
sessionStorage; provider document là opaque name do server chọn.

Data plane enforce `view` receive-only kể cả direct protocol mutation; forged capability, cross-tenant/
document, replay, stale generation và authority outage bị deny. `revoke`, `close`, `restore` tăng
generation và đóng socket trong budget 1.000 ms. HTTP/frame/update/awareness/depth/rate, connection
actor/document/tenant, grant issue và reconnect-storm budget đều fail closed.

Authorization integration PASS `7/7`; full unit/integration PASS `28/28`; Playwright Excalidraw A/B/C
PASS `3/3`; concurrency/undo stress riêng PASS `5/5`. Lint, TypeScript, build, dependency/license và
bundle security guard đều PASS. Full suite đã phát hiện và sửa projection race bằng actor-delta merge
cùng local-mutation barrier, không đổi test sang tuần tự. Gate C `4/4 DONE`; P5-COLLAB-01 vẫn
`IN PROGRESS` vì Gate E-F còn mở sau Gate D, ADR-0034 vẫn `Proposed`, production vẫn force-off. Chi tiết tại
[P5_COLLAB_01_AUTHORIZATION.md](P5_COLLAB_01_AUTHORIZATION.md).

### Checkpoint P5-COLLAB-01 Gate B adapter — 2026-08-19

Đã triển khai `CanonicalExcalidrawAuthority` cô lập: `Y.Doc` `13.6.27` là authority duy nhất theo
exact tenant/document/generation; Hocuspocus `4.6.0` là transport/provider candidate và Excalidraw
chỉ là projection. Canonical scene v1 giữ actor/element revision register, CRDT z-order, page/file,
deterministic semantic hash và actor-scoped `Y.UndoManager`; không dùng Excalidraw local history làm
writer thứ hai. Same-element conflict/undo không còn xóa remote value.

Lint, TypeScript và unit/provider suite `20/20` PASS. Evidence bao phủ lossless
shape/text/binding/image/page round-trip, concurrent/same-element convergence, actor-local undo/redo,
two-way offline, duplicate/out-of-order update, reconnect, compact restore và bounded fail-closed
scene/update errors. Browser E2E `2/2` PASS với hai Excalidraw instance, concurrent add, equal
hash/render count, Teacher undo giữ Student change và redo hội tụ. Projection feedback-loop bị
suppress, internal history bị clear/Ctrl+Z route qua canonical undo; exact upstream fonts được
self-host. Structure/security PASS 182 asset/180 text asset. Gate B `5/5` `DONE`; P5-COLLAB-01 vẫn
`IN PROGRESS` vì Gate E-F còn mở sau checkpoint Gate D, ADR-0034 vẫn `Proposed`, production vẫn
force-off. Chi tiết tại
[P5_COLLAB_01_CANONICAL_AUTHORITY.md](P5_COLLAB_01_CANONICAL_AUTHORITY.md).

### Checkpoint P5-COLLAB-01 Gate A — 2026-08-19

Đã tạo Excalidraw-only candidate build cô lập trong `apps/whiteboard-spike`, không nối `apps/web`,
Core API, database, shared staging hoặc deploy. Exact manifest/installed pin `0.18.1`, engine MIT
metadata, build 2.313 module, lint, TypeScript, unit `14/14`, structural bundle guard và Playwright
lazy-load/render fixture 500 đều PASS. Structural scan kiểm tra 180 asset, không có tldraw code và
initial entry không static-import engine; browser gate PASS `1/1`, gồm Radix-backed sidebar/tablist.
Production dependency audit
`pnpm audit --prod --audit-level high` cũng PASS, không có known vulnerability tại checkpoint.

Gate A đã DONE. Exact upstream `v0.18.1` được xác minh tại commit
`a2ec2889babf7d2295469c6d90ebe77fae57df84`. Candidate dùng exact Radix Tabs `1.1.21` có React 19 peer
support, ship notice cho Excalidraw/dependency/9 font family và nhận packaged `LICENSE-MIT` của
`fuzzy@0.1.3`. Build-only sanitizer fail closed theo exact finding count; strict final scanner PASS
178 text asset, không còn Google API-key pattern, demo Firebase host/config hoặc demo collaboration
config. Không dùng peer override hoặc bundle allowlist, không log giá trị finding.

Checklist chính xác và lệnh tái lập nằm tại
[P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md](P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md). Toàn bộ Gate A đã PASS;
Gate B/C/D đã đóng ở các checkpoint phía trên, Gate E-F vẫn mở. ADR-0034 giữ `Proposed`,
P5-COLLAB-01 giữ `IN PROGRESS`, production whiteboard giữ
force-off.

### Final engine checkpoint P5-COLLAB-01 — owner chọn Excalidraw ngày 2026-08-18

Owner đã chốt **Excalidraw + self-managed collaboration** làm hướng whiteboard chính thức. Excalidraw
là editor/projection; collaboration plane phải có đúng một canonical document/history/undo authority.
Yjs/Hocuspocus là leading candidate nhờ generic network evidence đã có, nhưng chưa được chấp nhận
cho tới khi scene adapter, actor-local undo, durable recovery, authorization, load và operations đạt.

Quyết định này thay thế target tldraw trước đó nhưng **chưa phải production approval**. ADR-0034 vẫn
`Proposed`; P5-COLLAB-01 được mở lại thành `IN PROGRESS`, P5-COLLAB-02..20 vẫn bị chặn và production
whiteboard tiếp tục force-off. Không thêm dependency vào `apps/web`, không tạo runtime/migration,
không ghi shared staging và không deploy trong checkpoint này. Exit gate forward-only nằm tại
[P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md](P5_COLLAB_01_EXCALIDRAW_ACCEPTANCE.md).

### Retained evidence P5-COLLAB-01 — superseded tldraw candidate ngày 2026-08-18

Isolated tldraw official-sync candidate đã PASS lint/typecheck, 13 unit tests và 15 Playwright tests.
Evidence bao phủ two-browser convergence, actor-local undo/redo, offline merge, SQLite restart,
one-time 30-second grant, exact Origin/document binding, tenant/read-only isolation, replay/revoke,
connection/rate/frame caps, checksum snapshot, corrupt denial, generation swap và stale-writer denial.
Profile 2/10/50 client với fixture 500/500/2.000 shape đều hội tụ và cleanup session về 0; automated
Axe/keyboard/200%/forced-colors/reduced-motion PASS.

Tldraw-only build tách riêng đã PASS client bundle secret/config guard và không kéo Excalidraw/Yjs/
Hocuspocus; `pnpm audit --prod --audit-level high` không có known vulnerability, còn tldraw `5.3.1`
công bố peer range bao phủ React/ReactDOM `19.2.7`. Đây vẫn là local evidence, không phải production
Core API/runtime. [P5_COLLAB_01_AUTOMATED_ACCEPTANCE.md](P5_COLLAB_01_AUTOMATED_ACCEPTANCE.md) ghi exact
commands, số đo và giới hạn.

Các kết quả này chỉ còn là historical comparison/provider-exit evidence. Không mục nào được chuyển
sang PASS cho Excalidraw chỉ vì tldraw đã đạt. Không có thay đổi ở `apps/web`, migration, shared
staging, deploy hoặc feature flag.

### Superseded selection checkpoint P5-COLLAB-01 — tldraw ngày 2026-08-18

Owner từng chọn **tldraw SDK + official self-hosted sync** làm target tiếp theo sau research. Tldraw
store cùng đúng một authoritative sync room cho mỗi document generation là topology đã được kiểm thử;
không ghép Yjs/Hocuspocus hoặc operation log thứ hai vào cùng bảng.

Checkpoint đó chưa bao giờ là production approval và hiện đã được final Excalidraw checkpoint phía
trên thay thế. Trial/commercial license tldraw không được kích hoạt; tldraw chưa được thêm vào
`apps/web`, chưa tạo production service/migration và chưa deploy. Prototype được giữ để đối chiếu,
không phải writer lane hiện hành.

### Checkpoint P5-COLLAB-00 `DONE` ngày 2026-08-18

P5-COLLAB-00 đã bắt đầu bằng source/license audit bốn candidate và prototype cô lập tại
`apps/whiteboard-spike`. tldraw `5.3.1` và Excalidraw `0.18.1` cùng chạy fixture 500/2.000 object,
snapshot envelope/restore-callback smoke, JSON-corruption denial, lazy production build,
control-shell keyboard/Axe, CSS 200%/forced-colors/reduced-motion và ba cold-context load/heap
observations. tldraw 2.000 object biến thiên 2.937–7.806 ms; scene đã thay đổi cùng semantic
object-count/hash recovery chưa đạt. Các engine dependency chỉ nằm
trong research workspace; security resolution dùng bounded workspace overrides và chưa nối
`apps/web`, Core API, migration, shared staging hoặc deploy.

Nhánh Excalidraw có thêm generic Yjs `13.6.27` + Hocuspocus `4.6.0` network evidence: hai client
concurrent convergence, offline/reconnect hai chiều, binary snapshot restore, viewer receive-only,
invalid/cross-tenant credential denial và raw Hocuspocus/WebSocket frame caps đều PASS. Raw frame cap
có thể chặn legitimate full-state resync lớn nên chưa phải semantic complexity budget. Đây chưa phải
scene adapter hoặc TutorHub auth topology; persistence/restart, multi-node và profile 10/50 vẫn mở.

[P5_COLLAB_00_RESEARCH_RESULTS.md](P5_COLLAB_00_RESEARCH_RESULTS.md) ghi evidence/limitation và ma trận
tạm thời; [ADR-0034](adr/0034-whiteboard-engine-document-authority-and-collaboration-topology.md) giữ
`Proposed`, không chọn production engine. [PHASE_5_BACKLOG.md](PHASE_5_BACKLOG.md) đã tách task
P5-COLLAB-01..20. P5-COLLAB-00 kết thúc đúng phạm vi research với hai finalist và không có production
winner; engine-native two-browser convergence/actor-local undo, durable recovery, exact
grant/revoke/origin/rate/quota, 10/50 profile, manual NVDA và owner approval license/cost/runtime là
hard gate của P5-COLLAB-01.
Bundle guard còn chặn upstream Excalidraw do public Google API key cùng Firebase/collaboration config
được nhúng trong package build. Các Radix dependency được Excalidraw pin cũng chỉ công bố peer range
đến React 18, chưa bao phủ React 19.2.7 của TutorHub. Production audit riêng đã sạch nhưng full
workspace dev audit còn baseline ngoài spike.

### Checkpoint P4-12 `DONE` và Phase 4 closure ngày 2026-08-16

P4-12 đã đóng source-resolver/product-launch gap cho ClassSession/StudyMeeting, fail closed
missing/foreign/non-member và loại P1 route khi P4 feature bật. Candidate đầu `fb2df3e` không được
dùng vì Verify fail; safety candidate `3cd6448` PASS Verify `31932897680`, Security
`31932897725`, còn final admission-race fix `cca93c5` PASS Verify `31946763549`, Security
`31946763545`, Browser E2E và Cloudflare Pages `95164026803`.

P4-12 không có migration mới; disposable/shared giữ `36 false`, không rollback. Disposable exact
ACL và resolver/authority/concurrency/privacy PASS; final disposable snapshot `dirty=false` và
retention violation `0`. Teacher Chrome + Student Edge PASS official lobby/admit, hai participant,
controlled end và terminal cleanup. Organization Admin đã tắt cả hai media capability; final shared
read-only snapshot PASS với `ledger=36`, `dirty=false`, diagnostics `52`, expired/retention violation
`0`, enabled media override `0`. Safari/VoiceOver, physical Firefox và low-end vẫn `UNAVAILABLE`.

Phase 4 đã `DONE`; biên bản tại [PHASE_4_COMPLETION.md](PHASE_4_COMPLETION.md). Phase 3 deferred
carry-over tiếp tục giữ nguyên và task kế tiếp là P5-COLLAB-00 research spike, chưa cho phép thêm
production whiteboard/collaboration dependency trước evidence và ADR.

### Checkpoint P4-11 `DONE` ngày 2026-08-16

Acceptance contract đã được khóa tại
[P4_11_STAGING_ACCEPTANCE.md](P4_11_STAGING_ACCEPTANCE.md): physical Chrome/Edge/Firefox Windows,
Safari macOS, standard/low-end 360p/540p/720p, NVDA/VoiceOver, profile 25 trước 50, Core API health,
provider outage/recovery/temporary-key rotation và optional-effect `None` fallback đều có exact
PASS/FAIL boundary. Playwright supplement chạy full deterministic matrix trên Chromium/Firefox và
P4-03 prejoin trên WebKit; LiveKit fake-room helper cần `canvas.captureStream` nên không được dùng để
suy Safari room PASS. Source evidence không thay physical Safari/Edge hoặc support statement.

Candidate ban đầu bổ sung config browser supplement, deterministic local unreachable-provider gate
và isolated LiveKit load harness. Provider harness chỉ nhận profile 25/50, giữ tải 120 giây, synthetic
opaque identities, exact quota confirmation và từ chối database/shared-stage variables; output chỉ có
bounded metrics cùng cleanup boolean. P4-11 không có migration, không cần Neon disposable, không bật
media feature và chưa deploy. Các gate chưa chạy vẫn là `PENDING/UNVERIFIED`, không ghi PASS sớm.

Scoped local evidence đã PASS: browser supplement `54/54` gồm Chromium `24/24`, Firefox `24/24` và
WebKit prejoin `6/6`; deterministic unreachable-provider; Prettier, lint, E2E typecheck, Go media
unit/integration compile và integration vet. Windows sandbox ban đầu chặn Firefox tab subprocess;
exact rerun ngoài sandbox PASS. Full `pnpm verify` trước provider refinement PASS trong 218,1 giây,
gồm API client `53/53`, web `69/69` files và `437/437` tests, build/security cùng toàn bộ Go test/vet.
Exact implementation candidate `50c256eb29bb0438016690a91803c302ac6e0a02` PASS GitHub Verify
`31891519968` và Security `31891520024`. Windows 11 Chrome/Edge physical devices/accessibility sau
đó PASS 10/10 ở local evidence. Sustained active-room outage/existing-room recovery cũng PASS ngày
2026-08-16; final candidate local verify và exact GitHub Verify/Security đều PASS, nên P4-11 đã
chuyển `IN PROGRESS -> VERIFY -> DONE`.

Windows installed-browser automated supplement sau đó PASS `48/48` trong `84.1 s`: Chrome
`151.0.7922.138` đạt `24/24`, Edge `151.0.4129.78` đạt `24/24`, gồm deterministic permission/prejoin,
layout 2/5/25/50, pagination/focus, 320px/200%, Axe, forced-colors, reduced-motion và
roster/signals/moderation. Host là Windows 11 build `26200`, i5-13450HX, RAM `15.7 GiB`, Intel UHD +
RTX 3050 6GB; NVDA `2026.1.1` có cài. Đây không thay physical webcam/headset/NVDA speech; macOS/Safari,
Firefox cài thật và máy low-end không có trong lượt này nên giữ `UNAVAILABLE/UNVERIFIED`.

Isolated LiveKit profile 25 đã PASS sau khi health warm-up tách cold start khỏi load window, synthetic
publisher phát ngay khi kết nối và các load client dùng relay-only managed TURN: `25/25` join,
connect p95 `4331 ms`, TTM p95 `6021 ms`, `24/24` delivery, sustain `120 s`; Core API `126` probe,
`0` failure, p95 `426 ms`; provider room cleanup về `0`. Active heap/goroutine delta là
`70593368/1175`, sau cleanup + GC là `26133368/1`; cần profile 50 để so slope trước khi khóa cap.
Post-refinement format, E2E typecheck, deterministic outage, integration-tag test/vet, security
`24/24`, diff-check và candidate secret scan đều PASS.

Isolated LiveKit profile 50 cũng PASS: `50/50` join, connect p95 `6761 ms`, TTM p95 `8190 ms`,
`49/49` delivery, sustain `120 s`; Core API `128` probe, `0` failure, p95 `658 ms`; cleanup về `0`.
Active heap/goroutine delta là `93658344/2350`, sau cleanup + GC là `53639968/1`. Maximum tested cap
hiện là `50`; load 25/50 đã PASS, nhưng CPU/network/provider dashboard và physical/outage gates chưa
được suy PASS.

Isolated provider resilience cũng PASS với logger privacy đã khóa trước Room construction: 10/10
join/leave cycle đều room/participant cleanup về `0`, post-cleanup heap/goroutine delta là
`634632/1`; credential cố ý sai fail closed thành typed unavailable mà không làm gián đoạn room hiện
hữu, provider hợp lệ tạo/cleanup successor smoke thành công. Rate-limit/credential HTTP mapping và
issuer/provider normalization scoped tests PASS, TTL vẫn 5 phút.

Actual isolated credential rotation sau đó PASS trên project `tutorhub-v2-p411-load`, evidence ghi
lúc `2026-08-15T22:34:12+07:00`: key mới pre-revoke tạo/cleanup room về `0`; sau khi owner revoke key
cũ, credential cũ fail closed thành typed unavailable và không tạo room; credential mới tiếp tục
tạo/cleanup room về `0`. Probe nạp hai file local Git-ignored trong process riêng, không in URL/key/
secret. Owner đã xóa file credential cũ sau khi evidence được chốt; physical A/V/accessibility đã
được đóng ở physical matrix 10/10.

Profile 50 được chạy lại cùng host sampler `61` mẫu và tiếp tục PASS: `50/50` join, connect/TTM p95
`7711/8679 ms`, `49/49` delivery, `132` health probe không lỗi, cleanup `0`; CPU tổng peak `48%`,
network tổng peak `2263941 B/s`, free RAM tối thiểu `6201416 KiB`. Các số host-wide này không thay
LiveKit dashboard. Final pre-commit candidate `pnpm verify` PASS trong `173.3 s`;
format/generated drift, local infra, security `24/24`, lint/typecheck/test/build/Storybook/bundle
security và toàn bộ Go test/vet đều xanh.

LiveKit Cloud Overview evidence sau đó PASS trên đúng project test với filter `Past 7 days`:
connection success `99.7%`, Windows `100%`, TURN/UDP `71.1%/28.9%`, `428` WebRTC participant-minutes;
bucket `20:00-21:00` ngày 2026-08-15 có project-level max active participants `150`, downstream/
upstream `55.18/2.8 MB`. Dashboard ghi `14` room session, average room size `19`, duration `1` phút.
Đây là aggregate nhiều session test, không đổi maximum tested classroom cap `50`. Privacy review ba
ảnh PASS: không có key/secret/token, participant identity hay session detail.

Physical Chrome/Edge + NVDA harness local-only hiện đã được triển khai: Go boundary bind đúng
`127.0.0.1:4179`, chỉ nhận Origin Vite loopback, tự nạp file LiveKit ignored mà không log giá trị,
cấp token 10 phút vào memory và từ chối cleanup khi participant còn hoạt động. Vite có entry riêng
chỉ cho development, explicit permission/preview, camera/mic/speaker selection, speaker test và
publish profile thật 720p/540p/360p; classroom shell dùng LiveKit thật, còn hand/reaction/moderation
được gắn nhãn projection mô phỏng chỉ để kiểm tra NVDA. Go boundary test, web typecheck và lint PASS.
Web regression `437/437` và production build PASS; exact exclusion probe xác nhận physical entry
không xuất hiện trong production output. Read-only LiveKit test smoke cũng PASS với trạng thái sạch
`room_exists=false`, `participant_count=0`, `cleanup_zero=true`, không tạo room. Exact launcher smoke
trả page `200`, preflight `204`, exact CORS PASS và không để port listener sau khi dừng.

Bốn physical paired round ở profile 720p được owner xác nhận PASS ngày 2026-08-16. Vòng 1 gồm typed
`media_device_busy_or_unreadable` đúng lúc Chrome giữ webcam, retry thành công sau khi giải phóng
camera, speaker test và cleanup `room_exists=false`, `participant_count=0`, `cleanup_zero=true`;
không đưa ảnh nguồn có device label thật vào repository. Vòng 2 + NVDA PASS heading navigation,
keyboard controls, hand/reaction/moderation state và live-region announcements, rồi cleanup zero.
Vòng 3 share/layout PASS picker cancel, start/stop, remote visibility, Grid/Speaker/Presentation,
pin/unpin, roster và cleanup zero. Vòng 4 reconnect PASS sau network loss 10-15 giây: UI/NVDA
`reconnecting`, A/V tự khôi phục, không duplicate participant/phòng, Leave và cleanup zero PASS.
Physical paired 540p round 5/10 media controls sau đó được owner xác nhận PASS: mute/unmute, camera
off/on, remote audio duy trì 30 giây khi video tắt, video phục hồi, không duplicate participant và
cleanup zero. Round 6/10 device recovery ở 540p cũng được owner xác nhận PASS: device-change/recovery,
trạng thái UI, media recovery không duplicate participant và cleanup zero đều đạt. Round 7/10 540p
reflow/keyboard tiếp tục PASS ở 200% zoom/narrow viewport, focus-visible, keyboard traversal không
trap, media/Leave còn dùng được và cleanup zero. Round 8/10 degraded audio-only ở 360p cũng được
owner xác nhận PASS: remote audio/controls duy trì khi camera tắt 60 giây, video phục hồi không
duplicate participant và cleanup zero. Round 9/10 360p forced-colors/reduced-motion tiếp tục PASS
cho contrast, text/focus/control/state visibility, keyboard/media operation và cleanup zero. Evidence
physical Windows 11 Chrome/Edge hoàn tất ở round 10/10 360p lifecycle cleanup: teacher-first Leave
không auto-rejoin, remaining participant/media tiếp tục hoạt động, final Leave tắt camera/mic/share
indicator và bounded cleanup trả zero. Physical distribution đạt `10/10` (`4×720p`, `3×540p`,
`3×360p`). Firefox Windows, Safari/VoiceOver macOS và low-end vẫn `UNAVAILABLE`.

Sustained active-room outage/recovery PASS ngày 2026-08-16: hai participant cùng ở exact room trước
khi temporary key bị revoke; boundary và old-key probe fail closed thành typed unavailable, không tạo
room mới. Wi-Fi loss 120 giây đưa UI vào `reconnecting`, sau đó terminal/prejoin và không auto-rejoin;
manual retry bằng revoked key bị `401/503` từ chối. Recovery credential cleanup exact opaque room về
`room_exists=false`, `participant_count=0`, `cleanup_zero=true`; successor/post-rotation probe và
recovery/concurrency/idempotency regressions PASS. Không lưu screenshot DevTools/token fragment.
Final candidate local verify sau đó PASS trong `171 s`: web `70/70` files, `438/438` tests,
format/API/lint/typecheck/build/Storybook/security và toàn bộ Go test/vet xanh; candidate secret scan
không có `.env`, ảnh, cache hoặc high-risk pattern. Candidate `abf4eb7a1bb3bd5ba280ed07a5132583844021e7`
PASS Verify `31928773967`; Security `31928773979` chỉ phát hiện hai synthetic participant UUID là
`generic-api-key` false positive. Fix `edecf84ecc45ae4c290e5b76df6ffc5b0a6bcfa9` loại literal khỏi
harness, dùng participant key do boundary trả về và chỉ ignore hai historical fingerprint chính xác.
Exact fix candidate PASS Verify `31929104866` và Security `31929104924`; secret scan báo
`No leaks detected`. P4-11 chuyển
`IN PROGRESS -> VERIFY -> DONE`; không migration, shared database write, deploy hoặc feature enable.

### Checkpoint P4-10 `DONE` ngày 2026-08-15

ADR-0033 đã khóa telemetry schema hữu hạn, no-raw-data privacy boundary, retention đúng 30 ngày,
role-separated maintenance purge và Organization Admin-only audited export. Forward migration
`000036_media_join_diagnostics` thêm exact tenant/space/room/participant FK, idempotent event,
PUBLIC zero privilege và bounded `SECURITY DEFINER ... SKIP LOCKED` purge. Core API re-resolve actor
thay vì nhận identity từ client; export chỉ trả tenant-scoped pseudonym, enum/duration/timestamp và
aggregate join success, p95 time-to-media, reconnect outcome.

Web phát diagnostics best-effort ở join/credential/media/reconnect/disconnect/leave, không chặn
LiveKit flow, URL hoặc storage. Organization Admin có panel 24 giờ với loading/error/retry và redacted
JSON download. Full `pnpm verify` và integration-tag compile PASS; API client `53/53`, web
`437/437` tests, all package build/security và Go test/vet đều xanh. Neon disposable sau đó PASS
owner boundary, forward-only/idempotent `35 false -> 36 false -> 36 false`, exact runtime/PUBLIC/
maintenance ACL, retention/metrics/tenant isolation và `SKIP LOCKED`. Focused P4-10 đạt `4/4`;
retained media đạt đủ `11/11` nhóm sau khi harness được nâng latest ledger 36 và rate-window fixture
được cô lập. Final read-only snapshot giữ `36 dirty=false`, diagnostics/expired/retention violation
đều `0`, media deployment guard force-off; không rollback và branch được giữ lại.

Exact candidate `c960f77753fa14475b84e7f0e0242bfcc458dacc` PASS Verify `31881117029`,
Security `31881116916`, Browser E2E và Cloudflare Pages check. Shared owner preflight PASS ở
`35 dirty=false`; forward-only/idempotent đạt `35 false -> 36 false -> 36 false`, exact
runtime/PUBLIC/maintenance/dependency ACL và read-only snapshot đều xanh. Render deployment
`dep-da04pjegekts7395k1h0` đạt `Live`; Cloudflare deployment
`b8a42033-ce9a-486b-aa1b-f9d0302452c7` chạy đúng exact SHA.

Live HTTP/privacy đạt `10/10`; Admin xác nhận hai media feature vẫn tắt, synthetic MediaSpace bị
conceal, workspace/concealment accessibility lần lượt `52/52` và `11/11`, không duplicate ID,
media resource hoặc console warning/error. Post-live shared snapshot giữ `36 dirty=false`, media
force-off và diagnostics/expired/retention violation đều `0`. Không rollback, không temporary-enable
capability và disposable branch tiếp tục được giữ lại. P4-10 chuyển `VERIFY -> DONE`; physical
browser/device, 25/50 load, provider-outage và optional-effect gates thuộc P4-11. Runbook:
[P4_10_STAGING_ACCEPTANCE.md](P4_10_STAGING_ACCEPTANCE.md).

### Checkpoint P4-09 `DONE` ngày 2026-08-15

ADR-0032 đã chốt ba luồng riêng: LiveKit transient reconnect không gọi lại TutorHub API; terminal
disconnect xóa credential rồi tải lại server authority; provider room failure tạo successor
RoomInstance bằng recovery command thay vì nhân đôi MediaSpace. Forward migration
`000035_media_room_recovery` chỉ mở rộng exact receipt allowlist/result invariant, không thêm runtime
privilege. Core API khóa exact latest failed attempt, space/source/feature/current actor authority và
one-active-instance barrier; provider call vẫn chạy sau transaction. Signed `room_finished` chuyển
active/provisioning instance cùng participant còn hoạt động sang failed bounded để host có thể recovery.

Web đã phân loại terminal disconnect theo LiveKit reason, không auto-rejoin khi bị remove, room bị
đóng/xóa, duplicate identity hoặc người dùng tự rời. Network/unknown chỉ quay lại prejoin sau khi GET
MediaSpace còn xác nhận open + active room. Audio-only là one-way camera shutdown: microphone, remote
audio và leave vẫn hoạt động, camera không tự bật lại khi chất lượng phục hồi.

Local candidate PASS `pnpm verify`: web `67/67` files, `431/431` tests; API client `7/7` files,
`52/52` tests; format, OpenAPI generated drift, lint, typecheck, build, Storybook, security bundle và
Go test/vet đều xanh. Focused P4-09 integration-tag compile/vet và `git diff --check` cũng PASS.

Neon disposable owner preflight PASS ở `34 dirty=false` với ba principal tách biệt trên cùng exact
branch/database. Migration chạy forward-only/idempotent `34 false -> 35 false -> 35 false`; không
rollback. Recovery concurrency tạo đúng một successor, replay hội tụ, foreign tenant bị conceal và
lock thắng. Exact runtime/PUBLIC/maintenance/dependency ACL cùng toàn bộ `9/9` media PostgreSQL
regression programs PASS. Regression P4-02 đã được cập nhật theo semantics mới: signed
`room_finished` chuyển active RoomInstance và active participant sang `failed`, còn participant đã
`left` giữ nguyên. Final read-only snapshot PASS tại `35 dirty=false`, effective media feature
force-off, recovery receipt/event cân bằng `1/1`; retained override/active intent được báo cáo bounded
thay vì xóa. Disposable branch vẫn được giữ lại.

Exact final candidate `fe33ffaba19d8f82f2034ddeb4b16d4e919e5014` PASS Verify
`31868991020`, Security `31868991007`, Browser E2E và Cloudflare Pages. Shared staging PASS owner
preflight `34 dirty=false`, forward-only/idempotent `34 false -> 35 false -> 35 false`, exact ACL và
zero-side-effect snapshot. Render `dep-da00d7dbedkc739jgt60` đạt `Live`; Cloudflare preview
`https://2a8eed45.tutorhub-web.pages.dev` success cùng SHA. Live public/anonymous HTTP đạt `10/10`;
Admin xác nhận hai media feature vẫn off và synthetic space được conceal. Workspace accessibility
đạt `53/53`, concealment `12/12`, không duplicate ID/media resource/console issue. Post-live DB vẫn
`35 dirty=false`, media force-off, `recovery_side_effects=0`. Không rollback hoặc temporary-enable;
P4-09 chuyển `IN PROGRESS -> VERIFY -> DONE`. Evidence:
[P4_09_STAGING_ACCEPTANCE.md](P4_09_STAGING_ACCEPTANCE.md).

### Checkpoint P4-07 `DONE` ngày 2026-08-14

ADR-0030 và OpenAPI chốt PostgreSQL/Core API là authority; browser chỉ gửi opaque participant key và
chỉ render server-projected operations. Forward migration `000033_media_moderation_commands` thêm
assignment co-host đúng tenant/space/RoomInstance, durable provider-effect receipt, PUBLIC zero
privilege và fail-closed down guard khi còn co-host động hoặc effect bắt buộc chưa hội tụ.

Core API đã có exact matrix cho host/co-host/TA/attendee/safety-admin, expected space/room/projection
version, stable idempotency và cùng effective-role resolver cho moderation/signal/lobby/credential.
Lock/unlock, promote/demote, remote mute, remove và end đều tái xác thực tenant/source/room/target;
không có remote unmute. Safety-admin chỉ được remove với bounded reason/audit, không được tự nhận
quyền tham gia. PostgreSQL actor/room/operation-family rate limit dùng database clock, trả exact
`429`/`Retry-After` và fail closed.

Provider call chạy sau transaction. Durable reconciler dùng `FOR UPDATE SKIP LOCKED`, lease/CAS,
bounded retry và giữ nguyên actor chỉ làm audit metadata; actor bị vô hiệu hóa không làm stranded
effect. End-room provider failure có typed `503` nói rõ business đã commit mà không tuyên bố provider
success; UI chỉ nhận trạng thái này khi exact status/space/payload khớp, còn generic `503` vẫn fail.
UI có confirmation, one-way mute language, focus return, keyboard/zoom/reduced-motion/forced-colors
và loading/forbidden/stale/provider-pending/reconcile/terminal states.

Full `pnpm verify` PASS: web `66/66` files, `424/424` tests; API client `7/7` files, `52/52` tests;
toàn bộ lint/typecheck/build/Storybook/security và Go test/vet đều xanh. Focused Go, integration-tag
compile/vet, OpenAPI generation, format và diff check PASS; deterministic P4-07 Playwright/Axe fixture
PASS `4/4`. Opt-in harness cho mute/remove/delete thật và NotFound replay PASS trên isolated LiveKit
test resource. Neon disposable owner preflight, forward-only `32 false -> 33 false -> 33 false`, exact
runtime/PUBLIC/maintenance/dependency ACL, P4-07 authority/concurrency và toàn bộ `9/9` media
PostgreSQL regression programs đều PASS. Final disposable snapshot giữ `33 false`, feature effective
force-off và `unsafe_unresolved_effects=0`; retained audit fixtures được báo cáo thay vì xóa.

Exact runtime candidate `2c309eabed9a4b8425f12895df071ee5f06edfb0` đã commit/push trực tiếp
`main`. GitHub Verify `31814509810` và Security `31814509808` đều PASS trên đúng SHA. Shared owner
preflight PASS tại `ledger=32 dirty=false three_principals=true url_boundary=true media_features=false`;
migration chạy forward-only `32 false -> 33 false -> 33 false`, lần chạy lại idempotent, exact ACL
PASS. Read-only snapshot trước deploy giữ
`ledger=33 dirty=false media_features=false moderation_side_effects=0`.

Render deployment `dep-d9vjhvp5efls73ea5l3g` đạt `Live` trên exact runtime candidate; Cloudflare
Pages deployment `1b935dc9-7498-4a3c-81c6-2571ca080c53` cũng thành công trên cùng SHA. Public
health/readiness/status đạt `6/6`, anonymous moderation privacy đạt `10/10`, tổng cộng `16/16` live
HTTP probes. Authenticated Organization Admin xác nhận cả hai media capability vẫn off và synthetic
prejoin được conceal. Browser acceptance đạt `main=1`, `h1=1`, `nav=1`, `12/12` exposed controls có
accessible name; duplicate ID, media/provider reference, device control, console warning và console
error đều bằng `0`.

Post-live read-only snapshot tiếp tục giữ
`ledger=33 dirty=false media_features=false moderation_side_effects=0`; toàn bộ bounded P4-07 count và
side effect đều bằng `0`. Không temporary-enable capability, không rollback; disposable branch được
giữ lại, `CanPublishData=false` và cả hai media capability tiếp tục deployment-force-off. P4-07 chuyển
`IN PROGRESS -> VERIFY -> DONE`; physical/manual browser-device, 25/50 provider load, sustained outage
và optional-effect gates vẫn `UNVERIFIED — P4-11`, không được suy PASS từ slice này. Evidence:
[P4_07_STAGING_ACCEPTANCE.md](P4_07_STAGING_ACCEPTANCE.md).

### Checkpoint P4-08 `DONE` ngày 2026-08-15

ADR-0013/0025 đã được review/amend: room chat là kind thứ ba của canonical conversation aggregate,
liên kết một-một với MediaSpace và tiếp tục dùng messages/message_receipts của P3-07A. Forward
migration `000034_persistent_room_conversations` thêm tenant-composite FK, partial unique index và
shape constraint; không tạo bảng chat song song, không lưu LiveKit DataChannel/provider content.

Core API đã có endpoint ensure room conversation, tenant/source concealment và write barrier khóa
MediaSpace -> source -> active RoomInstance -> ParticipantSession trước conversation mutation.
Official room đọc theo current `class.view`; StudyMeeting đọc theo owner/active explicit member.
Write chỉ mở khi source cho phép, MediaSpace `open` và participant còn
`admitted|joining|connected|reconnecting`; room end hoặc participant `left|removed|failed` chuyển ngay
sang read-only. Exact idempotent replay vẫn đọc lại committed row nên reconnect không nhân đôi message.

Classroom shell đã có labelled chat drawer dùng lại ConversationMessages, unread/read, pagination,
sanitization và composer; loading/empty/error/forbidden/retry, focus return và long-content wrapping đã
có automated coverage. Full local verify gates, OpenAPI/generated client, integration-tag compile/vet
và toàn bộ web test `67/67` files, `430/430` tests đã PASS. Neon disposable PASS owner preflight
`33 dirty=false`, forward-only/idempotent `33 false -> 34 false -> 34 false`, exact runtime/PUBLIC và
dependency column ACL, canonical concurrency, tenant/source/member authority, removal-vs-send barrier,
ended-room replay cùng privacy. Final snapshot giữ `ledger=34 dirty=false`,
`room_conversations=2`, `retained_enabled_media_overrides=2`; không rollback và disposable branch vẫn
được giữ. Exact candidate `fd2c3fc70f7e32c252523367e8aa56e8b466b810` PASS GitHub Verify
`31858451744` và Security `31858451822`. Shared owner preflight PASS trước forward-only
`33 false -> 34 false -> 34 false`; exact runtime/PUBLIC ACL, final và post-live zero-side-effect
snapshots đều PASS tại `ledger=34 dirty=false`, `room_conversations=0` và
`enabled_media_overrides=0`.

Render deployment `dep-d9vsqjojo6nc73d6f6n0` đạt `Live` đúng SHA; Cloudflare Pages deployment
`d64338e9-8116-4021-8f5e-90e261868ecb` success trên cùng candidate. Live HTTP acceptance đạt
`10/10`. Organization Admin xác nhận hai media capability vẫn off; synthetic MediaSpace được
conceal mà không render chat/device/LiveKit resource. Workspace có `53/53` và concealment có
`11/11` exposed controls mang accessible name; mỗi trang có đúng một `main`, `h1`, `nav`, không
duplicate ID và không console warning/error. Không temporary-enable capability, không rollback;
disposable branch vẫn được giữ. P4-08 chuyển `IN PROGRESS -> VERIFY -> DONE`; P4-09 hiện đang
`VERIFY`. Evidence/runbook: [P4_08_STAGING_ACCEPTANCE.md](P4_08_STAGING_ACCEPTANCE.md).

### Checkpoint P4-06 `DONE` ngày 2026-08-13

Local candidate đã thay roster fallback của P4-05 bằng projection có version do Core API/PostgreSQL
sở hữu. Forward migration `000032_media_participant_signals` bổ sung opaque participant key,
monotonic roster/signal sequence, hand state, reaction event TTL 10 giây và idempotency receipt giữ
24 giờ với composite same-tenant FK/PUBLIC zero privilege. Hai bounded maintenance function dùng
`SECURITY DEFINER`, static search path và `FOR UPDATE SKIP LOCKED`; Core API runtime không có direct
table `DELETE`. Neon disposable và shared staging đều đã chạy forward-only
`31 false -> 32 false -> 32 false`; không rollback.

Contract/API mới cung cấp bounded snapshot tối đa 50 participant và typed hand/reaction mutation.
Mọi mutation reauthorize current tenant/source/active ParticipantSession, dùng expected versions,
stable idempotency, shared-read/exclusive-write tenant lock, PostgreSQL-authoritative timestamp,
server FIFO, moderator lower-one/lower-all và PostgreSQL cross-instance rate limit. Projection không
trả email, user/session/join/provider ID; LiveKit chỉ nhận signed opaque
`tutorhub.participant_key`, giữ `CanPublishData=false` và cấm client tự sửa metadata.

Classroom shell đã dùng canonical roster sequence/key cho grid, roster drawer, hand queue, six-reaction
allowlist, tối đa 3 visual cluster nhưng vẫn giữ bounded summary, 2 giây polling/resync và các trạng
thái loading/error/retry/rate-limit. Reduced-motion, forced-colors, keyboard và polite live-region
đã có automated coverage. Exact full `pnpm verify` PASS trong `92.4 s`: web `64/64` files và `400/400` tests,
API client `50/50`, toàn bộ Go test/vet, lint/typecheck/build/Storybook/security đều xanh;
PostgreSQL integration-tag compile/vet cũng PASS. Vite-only Playwright/Axe production-shell fixture
đạt P4-06 `7/7`; combined P4-05/P4-06 đạt `14 passed`, `1` isolated provider test được skip đúng cấu
hình.

Neon disposable đã PASS read-only owner preflight với ba principal riêng biệt: direct owner và
maintenance, pooled runtime trên cùng endpoint/database, bắt đầu ở ledger `31 false`. Exact runtime,
PUBLIC, maintenance và dependency ACL đều PASS. PostgreSQL gates xác nhận tenant/source/participant
authorization, privacy/opaque key, roster/FIFO/idempotency/lower/terminal cleanup, receipt replay
24 giờ, reaction TTL/grouping/allowlist, cross-instance limits actor `3/5 s` + `20/60 s` và room
`100/5 s`, shared/exclusive locks, invalid purge batch, future preservation và two-transaction
`SKIP LOCKED`. Harness được harden bằng explicit timestamp cast, dependency-ordered cleanup,
deterministic rate windows và confirmation/pre-postflight riêng P4-06. Final disposable snapshot giữ
`32 false`, hai media feature force-off, P4-06 side-effect count `0`; disposable branch được giữ lại.

Exact candidate `d773641f796076b90f31a876ee840a427db43372` đã commit/push trực tiếp `main` và
PASS GitHub Verify/Security. Shared owner preflight sau đó PASS; migration forward-only giữ chuỗi
`31 false -> 32 false -> 32 false`, exact runtime/PUBLIC/maintenance ACL và final read-only snapshot
đều PASS. Hai media feature tiếp tục force-off, toàn bộ bounded P4-06 business count giữ `0` và không
có rollback.

Render deployment `dep-d9ul9q6417fc738gfa3g` đã `Live` đúng candidate; Cloudflare Pages cũng chạy
exact `d773641f`. Public health/readiness/status, anonymous privacy/concealment, authenticated
feature-off, automated browser accessibility/privacy/network và no-side-effect acceptance đều PASS;
không temporary-enable capability hoặc chạy positive signal canary trên shared. Post-live snapshot
giữ ledger/ACL/feature/count như trước live.

P4-06 chuyển `IN PROGRESS -> VERIFY -> DONE`; P4-07 là task `TODO` runnable tiếp theo. Physical/
manual browser-device, 25/50 provider load, outage và optional-effect gates tiếp tục
`UNVERIFIED — P4-11`, không được suy PASS từ P4-06. Evidence:
[P4_06_STAGING_ACCEPTANCE.md](P4_06_STAGING_ACCEPTANCE.md).

### Checkpoint P4-05 `DONE` ngày 2026-08-12

Canonical room đã được tách khỏi prejoin thành lazy route riêng và dùng custom LiveKit shell thay
cho prebuilt conference UI. Local slice hiện có stage/toolbar/participant rail và responsive drawer;
Grid/Active speaker/Presentation; grid cap `12/6/4`, rail tối đa `6`, local pin, hysteresis
`800/2500/1500 ms` và deterministic screen-share restore. Publish controls chiếu đúng credential
grants; `autoSubscribe=false`, remote audio/video được subscribe thủ công theo bounded projection,
hidden presenter camera/off-page video bị gỡ subscription và listen-only không gọi publish API.
Trong khi feature còn deployment-force-off, participant dùng opaque local-first session-local append-stable
fallback không đọc provider identity/join time; canonical server roster sequence/key thuộc P4-06
và bắt buộc trước feature enable.
Deterministic degradation đã có đủ chuỗi normal -> reduced video -> lower quality -> stage-only ->
audio-only với cửa sổ xuống cấp `5 s`, phục hồi `15 s`, ưu tiên presentation và giữ audio/control.

Device selector chỉ mount sau explicit mở panel và không enumerate input device khi grant publish
false. LiveKit/component logger bị tắt ở canonical room, provider identity không render vào DOM,
terminal leave/disconnect/error là first-wins và pending camera/mic/share/device promise bị invalidate
khi leave/unmount. TutorHub sở hữu trực tiếp `RoomContext`/connect/publish/disconnect; Room chỉ được tạo
sau StrictMode commit, synthetic cleanup không disconnect, terminal callback cũ không thể đổi scope mới
và wrapper/session cleanup dùng chung một idempotent disconnect lifecycle. Production baseline tiếp tục
chỉ có `effect=None`; không thêm processor/model/WASM, không migration/OpenAPI/schema/ACL delta và
không bật media capability.

Focused P4-05/P4-03 regression tests đạt `76/76`; full web đạt `62` files/`376` tests. Targeted/full
web lint, TypeScript web/E2E typecheck, production build, Vite-only Chromium regression P4-03/P4-04/
P4-05 `16/16`, Playwright P4-05 `7/7`, Prettier/diff check, client bundle security `20` files, exact
local security suite `24/24` và diff/secret-marker audit trên `29-file` pre-hotfix candidate cùng
`2-file` hotfix đều PASS. Rolldown tách LiveKit
thành vendor chunk chỉ được
static-import bởi room routes; app entry và canonical prejoin không static-import SDK. Application
room chunk hiện `38.02 kB` raw/`11.93 kB` gzip dưới budget `45/15 kB`; tổng room application +
LiveKit vendor + CSS là `642.90/173.66 kB`, dưới budget `700/190 kB`.

Neon disposable read-only đã PASS với endpoint boundary riêng, ledger `31 false`, `dirty=false`, hai
media feature effective false và exact runtime ACL; `53` historic enabled override rows được giữ
nguyên vì deployment guard vẫn force-off. Không có migration, rollback, ACL provision, seed hoặc
mutation. Isolated LiveKit Go two-participant grant matrix đạt `2/2`; Chromium actual-media gate đạt
`1/1` khi publisher và subscribe-only subscriber đều mount real `ClassroomLiveKitRoom`: subscriber
nhận remote camera/audio và chỉ nhận screen share sau explicit action; signed grant matrix chặn data,
privacy check PASS, mọi captured track `ended`, remote media detach khi Leave và exact room cleanup về `0`.

Exact runtime candidate `dcbdfef3c209a7c6d17197ccbcf737b58cd9e315` sửa fail-closed response
contract để auth failure chỉ ghi một Problem Details, sau khi live probe phát hiện response cũ nối
`401` và `403`. Focused MediaSpace regression, toàn bộ Go packages và `pnpm verify` PASS. GitHub
Verify `31598671906`, Security `31598671939` và Cloudflare deployment
`e2e72ca9-0173-4f2c-8221-f14203551c42` đều PASS; Render deployment
`dep-d9u6sdbm8hqs73em447g` Live đúng SHA. Public/anonymous matrix đạt `13/13`; authenticated
Organization Admin xác nhận hai media feature vẫn off, synthetic route fail closed và production
resource audit không tải LiveKit/effect/model. Shared before/after read-only snapshot giống nhau:
ledger `31 false`, exact ACL, mọi media aggregate/outbox/audit count không đổi.

P4-05 chuyển `IN PROGRESS -> VERIFY -> DONE`. P4-06 là task runnable tiếp theo. Physical device
indicator, browser/hardware/load, provider-outage và optional-effect matrix tiếp tục
`UNVERIFIED — P4-11`. Evidence ledger:
[P4_05_STAGING_ACCEPTANCE.md](P4_05_STAGING_ACCEPTANCE.md).

### Checkpoint P4-04 `DONE` ngày 2026-08-10

P4-04 local candidate đã thêm contract/API cho self join-attempt poll/cancel, moderator admission
queue cùng admit/deny/restore và explicit same-tenant StudyMeeting member list/invite/revoke/restore.
Core API reauthorize current tenant/source/membership, bind exact MediaSpace/RoomInstance/actor,
dùng expected version + idempotency receipt + CAS và giữ lock order cho race admit/deny/end/cancel/
timeout. Denied/removed member chỉ có thể rejoin sau explicit restore; waiting participant không có
provider credential/connection và device/effect state không đổi admission hoặc capacity.

Forward migration `000031_media_lobby_admission_restore` mở rộng exact receipt-operation allowlist
và thêm participant rejoin-restore marker cùng same-tenant FK/check/partial index. Migration không
revive terminal session hoặc mở public/anonymous authority. Raw email
chỉ là exact lookup input cho invite; projection, audit/outbox và log không lưu email, token,
provider/device identifier hoặc media content.

Web có waiting timeout/cancel/retry cùng denied/cancelled/meeting-ended/provider-unavailable states,
moderator lobby và invite panel fail-closed, bounded focus/error/empty/forbidden handling và không
enumerate tenant roster. Local evidence hiện có: OpenAPI generation/check PASS; API client `7` files/
`49` tests; web typecheck và `59` files/`305` tests PASS; focused P4-04 web `23/23`, targeted lint/build/
E2E typecheck/format/diff PASS; Go test/vet cùng integration-tag compile PASS. Isolated P4-04
Playwright đạt `3/3`; security/static sign-off cho deployment guard, audit, exact ACL columns,
lock/TTL/privacy/restore PASS. Full exact `pnpm verify` PASS với workspace-local Go cache;
integration compile không phải bằng chứng PostgreSQL runtime.

Neon disposable đã PASS owner preflight, forward-only `30 false -> 31 false -> 31 false`, exact
runtime/default/future-table ACL, lobby/admission/invite race/restore, lifecycle và RoomInstance
integration gates; final ledger `31 false`, không rollback. Full post-disposable `pnpm verify` cũng
PASS. Exact runtime candidate `735a5e5579d6e5efe7c4efca2b8a48c3de1b1f23` sau đó PASS GitHub
Verify `31356295542` và Security `31356295518`. Shared owner preflight, forward-only
`30 false -> 31 false -> 31 false`, exact ACL và read-only snapshot đều PASS; không chạy shared
mutation/provider fixture.

Cloudflare Pages check `93356654021`/deployment `7f180bf1-3736-4e40-a6f3-7ced6df2be27` và Render
deployment `dep-d9sli7740ujc73dfic70` chạy exact runtime SHA. Live đạt `6/6` health/readiness/status,
`22/22` anonymous privacy checks và authenticated Admin feature-off/prejoin fail-closed acceptance.
Shared snapshot trước/sau giống hệt ở ledger `31 false`; tất cả P4 media aggregate vẫn `0`, trừ
`livekit_webhook_events=16` có sẵn, và không có media feature override bật. Không rollback, không xóa
disposable branch; `classroom_media_rooms`/`instant_study_rooms` tiếp tục force-off, `effect=None`,
`CanPublishData=false`. P4-04 chuyển `VERIFY -> DONE`; P4-05 là task runnable tiếp theo.
Runbook/evidence: [P4_04_STAGING_ACCEPTANCE.md](P4_04_STAGING_ACCEPTANCE.md).

### Checkpoint P4-03 `DONE` ngày 2026-08-10

P4-03 đã thêm canonical join-attempt API trước credential, current-role/lobby reauthorization,
same-tenant concealment và exact runtime ACL không cho `joining_at` ở participant `INSERT`. Prejoin
không capture ban đầu, chỉ probe sau explicit action; có device/speaker/audio-profile/network states,
bounded errors, listen-only, memory-only credential handoff và purge khi route/principal/workspace/
disconnect thay đổi.

Fresh `pnpm verify` PASS trên exact candidate tree: web `57` files/`289` tests, API client `47` tests, format,
generated contract, lint/typecheck/build/Storybook/security và toàn bộ Go test/vet đều xanh. Isolated
Chromium Playwright đạt `6/6`; focused prejoin/room Vitest đạt `27` tests và stress test xác nhận 20
device-switch cycles để lại zero owned resource.

Neon disposable đạt exact ACL provision/ledger-role, runtime ACL, lifecycle/concurrency/quota/privacy
và join-attempt/credential/webhook gates. LiveKit test-provider smoke PASS; final read-only ledger giữ
`30 false`, không migration mới và không rollback. Lần fail đầu chỉ ra
fixture timestamp rơi đúng ranh giới Unix second; fixture được lệch `125ms`, unit + disposable rerun
đều PASS. Evidence: [P4_03_STAGING_ACCEPTANCE.md](P4_03_STAGING_ACCEPTANCE.md).

Exact candidate `e49a8cc38f464e3ec56655823bcbb1ee77cbc651` PASS GitHub Verify
`31330663644` và Security `31330663663`. Shared exact ACL reprovision + focused read-only probe
PASS, ledger giữ `30 false`; không migration, rollback hoặc shared fixture. Cloudflare Pages check
`93288377456` và Render deployment `dep-d9sd4ne7bikc739bf7l0` đều chạy exact SHA.

Live đạt 6/6 health/ready/status `200 + no-store`, 4/4 anonymous MediaSpace/join-attempt privacy
`401`, Organization Admin xác nhận hai media feature off và prejoin inaccessible route fail-closed
với Retry, zero video/audio element, URL sạch credential và console sạch. Transaction read-only
trước/sau giữ bảy media relation ở `0 -> 0`; không có join/provider/database side effect. Disposable
branch tiếp tục được giữ; `.env*.local` và `.tmp-gocache/` không thuộc candidate. P4-03 chuyển
`VERIFY -> DONE`; P4-04 sau đó đã triển khai local candidate và chuyển `VERIFY`.

### Checkpoint P4-MEDIA-UX-00 `DONE` ngày 2026-08-09

P4-MEDIA-UX-00 đã benchmark nguồn chính thức hiện hành của Google Meet, Zoom, LiveKit, W3C,
Mozilla/WebKit/Chrome và MediaPipe; audit V1 read-only; pin ba checkout LiveKit tham khảo; lưu
[báo cáo evidence](P4_MEDIA_UX_00_RESEARCH_REPORT.md) và chấp nhận
[ADR-0031](adr/0031-classroom-media-ux-devices-layout-effects-and-signals.md). Không sao chép UI/
asset đối thủ, không port V1 client-owned admission/DataChannel authority và không dùng bốn ảnh
nền V1 thiếu license/provenance đủ mạnh.

Prototype `apps/media-ux-spike` không nối production route, media device, Core API hoặc LiveKit.
Nó khóa fixture Grid/Active speaker/Presentation 2/5/25/50, cap 12/6/4, local pin, server-sequence
hand queue và reaction allowlist/TTL/grouping/rate-limit; unit/component 22/22, lint, typecheck,
build và Playwright/Axe browser/a11y 5/5 PASS. Processor harness Git-ignore chạy video tổng hợp
360p/540p/720p: Chrome/Edge đạt 30 frame/case và observable cleanup giới hạn nhưng có long task tới
172 ms;
Firefox fallback fail cả 9 case, Safari/macOS/máy yếu chưa test vật lý.

Repository gate tương đương `pnpm verify` đã PASS: format/API generation/local-infra/security,
8 workspace lint/typecheck/test/build, Storybook và client bundle đều xanh. Bước Go ban đầu chỉ bị
sandbox từ chối cache mặc định; exact `go test ./services/core-api/...` và
`go vet ./services/core-api/...` chạy lại với workspace `GOCACHE` đều PASS.

Vì vậy P4 production baseline là `effect=None`. `@livekit/track-processors@0.7.2` chỉ là optional
candidate force-off đến khi self-host immutable model/WASM/background, privacy/MediaPipe metrics,
CSP/network, exact Firefox/Safari/low-end performance và cleanup gates đạt ở P4-11. WebRTC
speech + explicit original-sound là audio baseline; Krisp/direct MediaPipe không vào MVP. Hand/
reaction vẫn qua Core API với `CanPublishData=false`. Không migration, shared staging write,
provider config, deploy hoặc feature activation trong research. P4-03, P4-04 và P4-05 sau đó đã đạt
`DONE` theo toàn bộ decision của ADR-0031; P4-06 là task runnable tiếp theo.

### Checkpoint P4-02 `DONE` ngày 2026-08-09

P4-02 đã có forward migration `000030_room_instance_livekit_binding`, exact runtime ACL/runbook,
official LiveKit server SDK adapter và canonical RoomInstance credential endpoint. Credential chỉ
nhận `join_attempt_id`, reauthorize active tenant/session/source, bind exact active RoomInstance,
tạo opaque ParticipantSession identity độc lập, áp dụng shared `30/10m` rate limit và mint JWT mặc
định 5 phút với camera/mic, screen-share và subscribe grant tách riêng; `CanPublishData=false`.
Response không trả provider room/participant identity và luôn `private, no-store`.

Provider lifecycle commit intent trước khi gọi LiveKit, create/reuse room idempotent rồi activation
CAS; provider outage trả typed/redacted `503`. Signed webhook dùng official verifier trước database,
lookup exact persisted room/SID và participant identity, receipt `(provider_kind,event_id)` chống
replay, xử lý mismatch/stale/out-of-order/terminal và không lưu raw payload/provider identifiers.
Legacy P1 class-wide route được gate mutual exclusion; runtime P4 có zero grant trên receipt legacy.
Review cuối còn siết activation phải reauthorize `session.start`, chuẩn hóa lỗi database tại webhook
boundary và map provider lifecycle outage thành `media_provider_unavailable`. `room_finished` giải
phóng toàn bộ active participant capacity; provisioning intent chuyển `failed` có thể được owner kết
thúc/cleanup an toàn; timestamp LiveKit độ chính xác một giây được clamp vào state PostgreSQL có
microsecond mà không hồi sinh hoặc đảo lifecycle.

Official verifier HTTP test chứng minh valid signature mới tới processor; unsigned, wrong-key và body
tamper đều dừng trước mutation. PostgreSQL harness có explicit disposable opt-in trước khi đọc URL,
chặn owner/runtime trùng role và khóa same-key credential, signed duplicate, connect/leave cùng
provider reconciliation concurrency; failed/unknown/composite-FK/retention/privacy branches cũng có
assertion thật khi chạy trên disposable. Raw driver cause không còn được render ra service error/test
failure.

Disposable owner/runtime preflight PASS; migration forward-only và idempotent đạt
`29 false -> 30 false -> 30 false`. Exact runtime ACL provision/probe PASS, không broad/PUBLIC/legacy
grant; gate thực tế phát hiện và sửa thiếu `SELECT attempt_number` cho truy vấn latest RoomInstance.
Focused RoomInstance/credential/webhook PostgreSQL gate PASS trong 223.7 giây; lifecycle exact ACL,
authority, quota, concurrency và privacy suite PASS trong 200.2 giây. Final ledger giữ `30 false` và
không rollback, không chạm shared staging.

LiveKit test-provider smoke tạo/reuse một opaque room, mint token 5 phút least-privilege,
connect/disconnect và exact cleanup PASS trong 29.8 giây. Official verifier valid/wrong-key/tamper
PASS với signed synthetic webhook. Full `pnpm verify` sau các sửa lock-order, tenant concealment, lobby/
Unix-second fixture và ACL PASS trong 182.5 giây; rerun sau khi bỏ log endpoint LiveKit tiếp tục
PASS trong 26.2 giây với cache. File `.env.p4-02-disposable.local` chỉ được nạp
trong cùng command, không in/log giá trị. Exact candidate
`f622e5f4b4c5efd6b877914e35aff16d765fba53` đã push `main`; GitHub Verify `31303424310` PASS
trong 3 phút 13 giây và Security `31303424335` PASS trong 2 phút 55 giây. Shared safety candidate
`d223daf0f2d504e6a0088071239aa9daeb36372c` PASS Verify `31304103932` trong 4 phút 28 giây và
Security `31304103916` trong 2 phút 40 giây. Shared owner/runtime preflight, forward-only
`29 false -> 30 false -> 30 false`, exact ACL provisioning và read-only ACL integration đều PASS;
final ledger giữ `30 false`, không rollback.

Render deployment `dep-d9s3vh0n74is73ftetr0` chạy exact safety candidate và đạt `live`.
Direct Render/Pages health, ready và status đạt 6/6 HTTP `200` + `no-store`; anonymous canonical
credential/unsigned webhook fail closed `401` qua cả hai đường; authenticated Organization Admin
UI xác nhận hai media feature tiếp tục off. LiveKit test-provider smoke rerun PASS và Render nhận,
xác minh rồi ignore đúng ba provider-emitted webhook của unknown test room; exact provider cleanup
PASS. Read-only shared probe giữ năm bảng MediaSpace/RoomInstance/session/mutation receipt/provider
receipt ở `0`; bảng receipt legacy có `16` dòng lịch sử nhưng không có dòng mới kể từ deployment.
Không log secret/URL/token, không xóa disposable branch. P4-02 đã chuyển
`IN PROGRESS -> VERIFY -> DONE`; task runnable tiếp theo là P4-03. Chi tiết nằm tại
[P4_02_STAGING_ACCEPTANCE.md](P4_02_STAGING_ACCEPTANCE.md).

### Checkpoint P4-01 `DONE` ngày 2026-08-09

P4-01 đã có implementation candidate local cho forward migration
`000029_classroom_media_spaces`, MediaSpace/RoomInstance lifecycle, source binding,
idempotency/concurrency barrier, shared policy actions, audit allowlist, REST
create/get/start/end/cancel, OpenAPI/generated client và feature/quota controls. Hai feature
`classroom_media_rooms`/`instant_study_rooms` giữ compiled/deployment default off; child phụ
thuộc parent và thiếu LiveKit prerequisite phải fail closed. Slice không mint token, gọi
LiveKit, xử lý webhook hoặc mở room end-user; provider binding thật thuộc P4-02.

Focused local feature-control/config/HTTP, API client, web, generated-contract và full
`pnpm verify` đã xanh; integration-tag PostgreSQL compile/skip sạch khi chủ động bỏ DB env.
Review còn sửa concealment mutation cùng actor-scoped receipt để loại hai existence oracle.
Typed `room.create.instant` đã nối đúng ADR-0021 cho mọi active member, nhưng instant command
vẫn reauthorize tenant, membership và StudyMeeting ownership; focused policy/media/HTTP cùng
full local verify sau policy đều PASS.

Neon disposable `p4-01-disposable-20260809` đã PASS owner/runtime preflight, forward-only
`28 false -> 29 false -> 29 false`, exact runtime ACL và full media PostgreSQL integration bằng
pooled runtime login; final ledger giữ `29 false`, không rollback. Integration suite xác nhận
authority/tenant concealment, same-key concurrency, source lifecycle barrier, feature/quota,
privacy/audit và không có provider side effect. Fresh `pnpm verify` sau hai sửa lỗi harness
(khởi chạy đủ hai concurrent operation và giữ append-only audit fixture) PASS trong 195.8 giây.
Exact candidate `183ca338557fafd6e8fe502d67763bb2a73d9aa0` đã PASS GitHub Verify
`31291917865`, Security `31291917871` và Cloudflare Pages check `93190579210`. Shared owner/
runtime preflight, forward-only `28 false -> 29 false -> 29 false`, exact ACL provisioning và
focused ACL integration đều PASS; final shared ledger giữ `29 false`, không rollback. Render
deployment `dep-d9rv61ajobas73e9pq90` chạy exact candidate và đạt `live`.

Live acceptance đạt 6/6 health/ready/status qua direct Render và Pages proxy; Organization Admin
UI xác nhận hai media feature off; anonymous media route đạt 4/4 `401` với privacy/cache headers.
Read-only shared probe sau live giữ bốn relation lifecycle ở `0`, nên không có MediaSpace,
RoomInstance, participant session, token/webhook/provider room hoặc provider side effect. P4-01
đã chuyển `IN PROGRESS -> VERIFY -> DONE`. Runbook và evidence đầy đủ nằm tại
[P4_01_STAGING_ACCEPTANCE.md](P4_01_STAGING_ACCEPTANCE.md). Task runnable tiếp theo là P4-02;
`P4-MEDIA-UX-00` sau đó đã `DONE`; quyết định áp dụng từ P4-03 theo ADR-0031.

### Checkpoint P4-00 `DONE` ngày 2026-08-08

P4-00 đã tạo `docs/PHASE_4_BACKLOG.md` với P4-00 đến P4-12, dependency graph, reuse
matrix, risk/test matrix và exit gate. ADR-0030 `Accepted` chốt ba cấp authority
`MediaSpace -> RoomInstance -> ParticipantSession`; official ClassSession/occurrence và
member-owned StudyMeeting giữ source/ownership riêng, còn instant command tạo/bind một
StudyMeeting thay vì tạo authority thứ ba. Không quyền nào được suy từ client, JWT hoặc
provider state.

P1-07 được kiểm kê và tái sử dụng token issuer, signed webhook verifier, bounded telemetry,
prejoin/LiveKit room UI, camera/mic/screen share và reconnect. Deterministic class-wide room,
participant identity chứa user/session và webhook parse tenant/class từ room name chỉ còn là
compatibility spike, không phải Phase 4 lifecycle authority. P4-01 là vertical slice kế tiếp:
schema/domain/OpenAPI/ACL/concurrency cùng feature `classroom_media_rooms` và
`instant_study_rooms` mặc định/deployment-force-off; chưa mint provider token trong slice này.

Baseline cũng chốt lobby/moderation server-authorized, không remote-unmute,
`CanPublishData=false`, persistent chat không dùng DataChannel, recording/egress off,
provider/participant identifiers opaque, identifiable diagnostics retention tối đa 30 ngày và
legacy/P4 authority mutual exclusion. P4-00 không thêm dependency, migration, credential,
provider config, shared staging write hoặc deploy. Full P3-14/Phase 3 carry-over vẫn giữ nguyên.

### Lịch sử khởi tạo P4-MEDIA-UX-00 ngày 2026-08-09

[P4-MEDIA-UX-00](P4_MEDIA_UX_00_RESEARCH_SPIKE.md) đã được thêm làm decision spike cho
prejoin/lobby, layout lớp học 2-50 người, hand raise/reaction và media effects. Task benchmark
current official Zoom/Google Meet/LiveKit, audit V1 read-only và so sánh native browser capability,
LiveKit layout primitives/track-processors, MediaPipe fallback, WebRTC audio processing cùng
Krisp go/no-go trên một ma trận performance/privacy/license/accessibility có thể tái lập.

Task đã chuyển `TODO -> DONE`; kết quả chốt grid/active-speaker/presentation 2/5/25/50, server
FIFO/resync/rate-limit với `CanPublishData=false` và ship baseline không effect. Physical browser/
device/load/manual accessibility còn lại là rollout gate P4-11, không phải PASS ngầm từ source;
các gate tự động thuộc P4-05 đã được nghiệm thu riêng.

Research setup ngày 2026-08-09 đã shallow-clone LiveKit Meet revision
`665e1cb7841ab872de0d8e5c310744009a763b08` vào `.tmp/research/livekit-meet`. Checkout Apache-2.0
được Git ignore và chỉ dùng audit read-only; chưa cài dependency, tạo `.env` hoặc kết nối provider.

Cùng research setup đã shallow-clone LiveKit Track Processors revision
`9ef5191da7fb6d82e55876fa04d0e6048d49859b` vào `.tmp/research/track-processors-js`. Revision
Apache-2.0/NOTICE khai báo `@livekit/track-processors@0.7.2` và
`@mediapipe/tasks-vision@0.10.14`; dependency/model chỉ được dùng trong checkout Git-ignore để
build và benchmark synthetic, không được thêm vào production app.

### Forward plan P5-COLLAB-00 đăng ký ngày 2026-08-09

[P5-COLLAB-00](P5_COLLAB_00_RESEARCH_SPIKE.md) đã được thêm làm decision spike đầu tiên cho
Phase 5. Task sẽ so sánh tldraw + official sync, Excalidraw + self-managed sync và
Yjs/provider cho shared notes/custom CRDT trên cùng ma trận license/cost, convergence,
snapshot/restore, tenant/role security, scale và accessibility. ADR phải chọn đúng một
document/history/undo authority trước khi thêm production dependency hoặc runtime mới.

Đây chỉ là forward plan ở trạng thái `TODO`: không đổi phase/task hiện tại, không chặn P4-01,
không cài dependency, tạo service, migration, shared-staging write hoặc deploy.

### Checkpoint P3-14-CORE `DONE` ngày 2026-08-08

Core Exit review xác nhận toàn bộ runnable lane P3-02A/B/C, P3-02D-A, P3-06/07A,
P3-08/09, P3-11A/12/13 vẫn ở `DONE`. Review tìm và sửa ba regression/harness gap:
Calendar toolbar wrap để không clip action sau khi thêm Messages/Poll; Classroom integration
giữ business mutation ở exact runtime pool nhưng đọc audit/outbox bằng owner assertion pool;
ClassSession create lấy shared owner-time advisory lock trước class row lock để loại deadlock
với StudyMeeting insert.

Full `pnpm verify` PASS với 55 web file/262 test, generated contract, lint/typecheck,
build/Storybook/security, toàn bộ Go test và vet. Calendar production-route Playwright PASS
15/15 gồm Axe, keyboard, 200% zoom, forced colors, performance và ba visual baseline. Neon
disposable chạy tuần tự Classroom/Calendar/Conversation/Content/Discovery/Feature Control/
Security đều PASS; focused owner-time race PASS 3/3 và ledger giữ `28 false`, không migration
hay rollback. Baseline live 6/6 public + 2/2 anonymous privacy PASS; Admin xác nhận file upload
và in-app notification đều effective-off. Carry-over register đã được tạo tại
`docs/P3_14_CORE_ACCEPTANCE.md`.

Exact candidate `f5f1eb32d1ed59dbf8f5848103bb6bc9f38fbafd` PASS Verify `31265719929`,
Security `31265719927`, Browser E2E và Cloudflare Pages. Render deployment
`dep-d9rl6bp42hec738p7ed0` Live đúng SHA; 6/6 public và 2/2 anonymous privacy probe PASS.
Authenticated Admin xác nhận file upload/in-app notification effective-off; live Calendar có
năm action đều nằm trong main region, wrap đúng và không horizontal overflow, console sạch.
P3-14-CORE chuyển `VERIFY -> DONE`, cho phép bắt đầu Phase 4. Full P3-14/Phase 3 vẫn `TODO`;
worker/provider/delivery/processing carry-over tiếp tục theo register và không side effect nào
được bật sớm.

### Checkpoint P3-13 `DONE` ngày 2026-08-08

ADR-0029 chốt vertical slice không migration: chỉ admin feature/quota draft không nhạy cảm được
lưu trong `sessionStorage` với TTL/size/scope actor-tenant; logout, 401 và workspace switch purge
đúng prefix. Mutation chỉ opt-in auto-retry một lần cho network/5xx khi request đã có stable
idempotency key; 4xx/quota/conflict và B2 transfer tiếp tục không auto-retry.

Web catalog bao phủ exact 9 feature/17 quota. Go quota-rejection metrics lấy label từ compiled
catalog và bỏ qua runtime label không xác định. Full `pnpm verify` PASS với 55 web file/262 test,
toàn bộ Go test/vet, build/Storybook/security. Neon disposable feature-control integration PASS
với exact runtime ACL và ledger giữ `28 false`; không migration, rollback hay shared staging write.
Exact candidate `25a323ad` đã PASS Verify `31260418987`, Security `31260418985`, Browser E2E và
Cloudflare Pages. Render deployment `dep-d9rjdnf10e5c7387mh60` Live đúng full SHA; 6/6 public
probe và 2/2 anonymous privacy probe PASS. Live Admin same-tab recovery, workspace-switch purge,
logout purge và console check PASS; server value giữ nguyên vì không bấm lưu hay tạo quota fixture.
P3-13 chuyển `VERIFY -> DONE`; bằng chứng chi tiết ở `docs/P3_13_STAGING_ACCEPTANCE.md`.

### Checkpoint P3-12 `DONE` ngày 2026-08-08

P3-12 đã có ADR-0028 và vertical slice không migration. Home tái dùng các API Calendar,
Notification và Conversation cho các card độc lập; discovery module mới cung cấp recent ready
files và PostgreSQL search metadata-only cho session, conversation và file mà actor còn quyền
truy cập. Query được bound theo tenant/user, có giới hạn cứng, xử lý `%`/`_` như ký tự thường và
không trả message content, file content, private snippet hoặc provider selector.

Web Home có loading/empty/error, retry từng card, partial degradation và search keyboard-first;
workspace/principal change purge cache cũ. OpenAPI/generated client, Go/API-client/web tests,
Playwright keyboard/Axe và full exact-tree `pnpm verify` đều PASS; web đạt 53 file/255 test.
Neon disposable discovery integration PASS cho Teacher/Student, direct/class authorization,
inactive membership, foreign-tenant concealment và ready-file visibility. Không thêm migration;
disposable giữ `28 false`, không rollback và không forward shared staging.

Exact candidate `1c73c52782ca8b4139af0802bbace8e82b0c288b` đạt Verify `31256747702`
và Security `31256747681`; Cloudflare Pages check PASS, Render deployment
`dep-d9rhu4ugekts739t6ssg` Live đúng SHA. Sáu health/readiness/status probe đạt HTTP 200
`no-store`; bốn discovery privacy probe đạt 401 với `no-store/no-referrer/nosniff`.

Live Teacher/Student authorized session và class-conversation search PASS; notification failure
chỉ degrade card riêng. Teacher switch P2-08 -> P2-12 loại ngay kết quả cũ; Student foreign
workspace term không xuất hiện. Live workspace không có ready file nên giữ empty-state evidence,
không tạo shared fixture; ready/pending/file isolation dùng Neon integration PASS. Viewport 375 px
không overflow, semantic accessibility tree/console sạch và exact-build keyboard/Axe PASS.
P3-12 chuyển `VERIFY -> DONE`; bằng chứng chi tiết ở `docs/P3_12_STAGING_ACCEPTANCE.md`.

### Checkpoint P3-11A `DONE` ngày 2026-08-08

P3-11A đã có vertical slice contract-first mà không bật upload cho end user. Core API bổ sung
class-scoped list có keyset cursor bind tenant/class, reauthorize từ PostgreSQL và projection
`can_upload`/`can_download`/`can_retry_upload`. Student chỉ thấy file `ready`; creator/upload
manager mới thấy transfer chưa sẵn sàng, foreign tenant và quyền bị thu hồi tiếp tục conceal.

Web class detail có Class Files loading/empty/error/forbidden/off state, attachment-only
download, background SHA-256, single PUT progress/retry và multipart progress/resume trong
phiên. Cache key chứa tenant/class và được purge/invalidate khi switch workspace, class archive/
restore hoặc roster/role thay đổi. Active content không được preview, signed URL/provider proof
không lưu vào query cache hay storage.

OpenAPI/generated client, API-client/web/Go unit gate và Neon disposable content integration
đã PASS; integration xác nhận exact runtime ACL, owner/student visibility, ready download,
foreign-tenant conceal và archived-class không còn retry capability. Không có migration mới,
không rollback, không forward shared staging. `file_uploads` vẫn fail-closed cho tới P3-10/P3-11B.
Full exact-tree `pnpm verify` đã PASS. Exact candidate CI/security và live Teacher/Student
feature-off acceptance được ghi tại `docs/P3_11A_STAGING_ACCEPTANCE.md`.

Candidate `73467ae665c5aa26a901585f59d41fa32eeff585` đã đạt Verify `31247921859` và
Security `31247921851`; Cloudflare Pages PASS và Render deployment
`dep-d9rechlbedkc73bj80v0` Live đúng SHA. Sáu public health/readiness/status probe trực tiếp
Render và qua Pages đều HTTP 200 với `no-store`. Live Teacher/Student feature-off, workspace
switch, role `student -> teaching_assistant -> student` và archived fixture restore/archive
đều PASS; fixture đã được trả về trạng thái ban đầu, không tạo intent/object, không lộ file
picker/provider URL. Owner đã phê duyệt rõ automated Axe/keyboard/accessibility-tree evidence
thay manual NVDA riêng cho P3-11A sau khi quick-navigation không phản hồi trong embedded
browser. Ngoại lệ không áp dụng cho P3-11B, activation, pilot/public release hoặc UI tương lai.
P3-11A chuyển `VERIFY -> DONE`; `file_uploads` tiếp tục fail-closed tới P3-10/P3-11B.

### Checkpoint P3-09 hoàn tất ngày 2026-08-08

P3-09 đã chuyển `VERIFY -> DONE`. Final candidate `d6365b5` đạt Verify `31245311233` và
Security `31245311235`; Browser E2E, local smoke, quality/integration, CodeQL, Trivy và secret
scan đều xanh. Cloudflare Pages deployed successfully; Render deploy
`dep-d9rdbnajobas73d6an40` Live đúng commit. Direct Render và same-origin Pages health/readiness
đều HTTP 200.

Shared Neon owner preflight xác nhận `25 false`, role migration/runtime tách biệt và runtime
không đặc quyền; forward-only tới `28 false`, exact ACL re-provision idempotent và ba content
PostgreSQL integration tests đều PASS. Final ledger giữ `28 false`, không rollback. B2
single/multipart smoke, CORS và lifecycle read-back/idempotency đã PASS trước khi mở shared gate.

Live acceptance phát hiện `file_uploads` từng effective-on vì thiếu deployment ceiling. Không có
intent/object nào được tạo. Render đã thêm `FEATURE_CONTROL_DISABLE_FILE_UPLOADS=true` và redeploy;
Teacher projection xác nhận “Tải tệp lên — Đang tắt”. Code config cùng `.env.example` cũng đổi sang
fail-closed mặc định. Capability không credential trả 401 với `no-store/no-referrer`, Problem Details
và không rò provider data. Feature phải giữ off tới P3-10 exact-version hash/scan authority.

### Checkpoint P3-09 forward design `000027` ngày 2026-08-08

ADR-0027 và `docs/P3_09_STAGING_ACCEPTANCE.md` đã khóa provider-first contract. Local
presigner unit gate PASS, nhưng disposable B2 Gate 0 đã chứng minh S3-compatible presigned
PUT không cung cấp SHA-256 authority đã yêu cầu bởi P3-08:

- exact PUT và Head size/MIME/ETag/version PASS, nhưng Head không trả SHA-256;
- B2 vẫn nhận same-length wrong bytes với signed `x-amz-checksum-sha256` và cả
  `x-amz-content-sha256` trong signed headers;
- actual SHA-256 làm presigned SigV4 payload hash bị B2 trả 403 cả payload đúng;
- test object được cleanup theo exact version; không log secret/URL/query/object key.

Owner đã duyệt forward path: migration `000027` cho phép finalize ghi exact-version
size/MIME/ETag nhưng không ghi checksum giả; P3-10 stream-hash + scan exact version trước
`ready`/download. Core API hiện có upload capability tối đa 5 phút, finalize nhận
`storage_version_id` làm selector và download capability tối đa 2 phút chỉ cho file `ready`.

Disposable Neon đã forward-only `26 false -> 27 false -> 27 false`. Lần chạy đầu bị dirty
marker do row P3-08 còn checksum chưa được provider chứng minh; exact-schema preflight xác nhận
transaction không commit, ledger được sửa về `26 false`, rồi migration forward xóa riêng proof
không đáng tin ở `uploaded/processing`. Exact runtime ACL thu hồi quyền update checksum và full
PostgreSQL content integration PASS. B2 smoke mới PASS exact PUT, HEAD exact version, versioned
GET byte-match và exact-version cleanup. Full local `pnpm verify` cũng PASS. Không rollback,
không shared staging, không deploy.

### Checkpoint P3-09 multipart `000028` ngày 2026-08-08

ADR-0027 đã chốt durable multipart ownership và migration `000028` đã triển khai session/part
tables, exact ACL cùng initiate/part/complete/abort API. Local `pnpm verify` PASS. Neon
disposable owner/runtime preflight PASS, forward-only `27 false -> 28 false`; full content
integration PASS cho ownership, tenant isolation, single-PUT barrier, part retry/length
conflict, complete/abort và expiry. Final disposable giữ `28 false`, không rollback.

B2 bucket-admin preflight xác nhận cấu hình cũ rỗng. Một CORS rule allow đúng origin staging,
`PUT`/`GET`/`HEAD`, expose `ETag`/`x-amz-version-id` và hai lifecycle rule abort incomplete sau
một ngày cho `tenants/`/`smoke/` đã được provision, read-back và kiểm tra idempotent. Runtime app
key không được nâng quyền. Single/multipart smoke PASS part PUT/CORS, complete immutable version,
exact-version GET, explicit abort và cleanup. P3-09 chuyển `IN PROGRESS -> VERIFY`; exact candidate
`04e30649` đã push và Security PASS. Verify phát hiện race thật ở feedback/focus sau gửi tin nhắn:
hook chờ cache refresh trước khi báo success và focus có thể chạy lúc composer còn disabled. Bản sửa
tách cache refresh thành background, chỉ focus sau khi mutation hết pending và có regression test với
refresh cố ý không hoàn tất. Full local `pnpm verify` PASS; host không có Docker nên exact Playwright
được giao cho CI. Shared staging vẫn `25 false`, chưa deploy.

### Checkpoint P3-08 hoàn tất ngày 2026-08-08

P3-08 đã chuyển `VERIFY -> DONE`: ADR-0026, vertical slice local, Neon disposable và exact
candidate CI/security đều đạt; shared staging chưa migrate và ứng dụng chưa deploy theo đúng
ranh giới task:

- forward migration `000026` tạo `content_files`, `tenant_file_usage`, feature
  `file_uploads` và bốn quota file; `PUBLIC` bị revoke và runtime provisioning dùng exact
  column-level `INSERT/UPDATE`;
- Go module `content` tạo/replay intent, reserve/reclaim quota, reauthorize class trong
  transaction, conceal foreign/inaccessible ID và finalize qua hai transaction tách khỏi
  lệnh `HeadObject` tới B2;
- finalize chỉ commit `pending -> uploaded` khi size, normalized MIME, SHA-256, ETag và
  immutable version đều đúng; object key/provider proof không xuất hiện trong public API,
  audit, outbox hoặc log;
- OpenAPI/generated client, capability projection, deployment guardrails và admin feature/
  quota controls đã cập nhật; chưa có presigned transfer, download/share, processing worker
  hoặc Class Files UI;
- `pnpm verify`, Go unit/vet, OpenAPI generated-contract, API client `37/37`, web `247/247`,
  lint/typecheck/build/Storybook và local security gates đều PASS;
- disposable owner preflight PASS với admin residual được ghi nhận; forward-only
  `25 false -> 26 false -> 26 false`, exact column ACL/runtime-role safety và `PUBLIC`
  zero-grant đều PASS, không rollback;
- PostgreSQL content integration package PASS, zero `SKIP`: concurrent intent/finalize,
  idempotency conflict, expiry/quota accounting, archived-class và tenant isolation đều đạt.

- CI đã phát hiện dependency ACL thật của `SELECT ... FOR SHARE`: runtime cần `UPDATE` trên ít
  nhất một cột của `classes` và `class_enrollments`. Isolated CI role hiện chỉ nhận
  `UPDATE(updated_at)`, exact-login probe fail closed nếu grant này bị mất; disposable rerun và
  stress concurrency `count=5` đều PASS.
- Exact candidate `6a50c3e4` đạt Verify #185: Quality/integration, local smoke và Browser E2E
  attempt 2 PASS. Browser attempt đầu chỉ flake focus ở conversation P3-07A; rerun riêng PASS.
  Security #183 PASS toàn bộ; final diff/secret review không phát hiện credential.

Acceptance và exact ACL được ghi tại `docs/P3_08_STAGING_ACCEPTANCE.md`. Real B2 proof cho
exact presigned PUT thuộc P3-09; nếu provider không trả checksum/version thì giữ feature off,
không nới điều kiện finalize.

### Checkpoint P3-07A hoàn tất ngày 2026-08-05

P3-07A đã chuyển `VERIFY -> DONE`: ADR-0025 được chấp nhận; migration forward
`000025`, REST API, OpenAPI/generated client và web message history/composer đã hoàn tất
local gate và disposable database acceptance.

- PostgreSQL giữ message/tombstone, server sequence, global author/client idempotency,
  monotonic self-read marker và O(1) `tenant_message_usage` counter dưới tenant advisory
  lock; runtime ACL chỉ cấp exact table/column allowlist qua provisioning riêng.
- Direct/class permission được reauthorize trong transaction; class archive và inactive
  direct peer giữ history read-only. Message content không đi vào audit, outbox, log,
  metric, cursor hay error detail; P3-07A không tạo notification/delivery side effect.
- API có keyset pagination, CAS edit/delete, tombstone projection, expected-tenant + CSRF,
  body `1..4000` Unicode code point/`16 KiB`, tenant storage/hourly quota, actor `60/phút`
  và typed `409/429` với `Retry-After` đã validate.
- Web `/app/messages` có persistent history, unread/read, memory-only retry ID, focus/keyboard,
  fail-closed khi quyền bị thu hồi và hàng đợi CSRF chung cho conversation mutations. Mark-read
  refetch message/detail/list để không bỏ sót message đến đồng thời.
- Local `go test -count=1 ./...`, `go vet ./...`, API client `34/34`, web `247/247`, lint,
  typecheck, build, format, generated-contract, security/bundle và Storybook gates đều PASS;
  integration-tag PostgreSQL code compile PASS bằng `-run '^$'`.
- Neon disposable owner preflight PASS ở `24 false`; direct/pool endpoint và runtime/
  migration role boundary đạt. Migration owner có admin residual được ghi tổng hợp nhưng
  runtime không nhận superuser/role/database/replication/bypass-RLS hoặc migration membership.
- Forward-only `24 false -> 25 false -> 25 false` PASS, không rollback. Exact runtime
  table/column ACL, `PUBLIC` zero-grant và role safety PASS; final ledger giữ `25 false`.
- Full PostgreSQL conversation suite 5/5, zero `SKIP`: P3-07A idempotency/lifecycle/read/
  quota/privacy, counter constraint/cascade/4.096-row query plan và P3-06 concurrency
  regression đều PASS. Riêng fixture query-plan 4.096 row được rollback và statistics
  được re-ANALYZE; các mutation fixture khác được giữ trên disposable để điều tra nếu cần.

- Exact candidate `a21ec385272d6f6da4200b1d76a6c438e45ba727` đã qua toàn bộ PR #33 và
  push-event CI/security; Cloudflare production xác nhận đúng candidate.
- Shared Neon owner preflight PASS, forward-only `24 false -> 25 false -> 25 false`, exact
  runtime ACL, `PUBLIC` zero-grant và role safety PASS. Không rollback và không chạy
  mutation/concurrency fixture suite trên shared.
- Render deployment `dep-d9pjeg8ae00c73f651t0` của exact candidate đã Live; direct Render
  và Pages proxy health/readiness/status `6/6` PASS, health/ready giữ `no-store`.
- Authenticated live acceptance PASS: Teacher gửi/reload không duplicate; Student nhận
  unread, đọc, trả lời và reload bền vững; archived class chỉ đọc; Admin ngoài direct bị
  conceal ở list và URL trực tiếp. Exact-candidate Browser E2E đóng retry/keyboard/focus/Axe;
  hai controlled canary zero-match trong Render logs.
- UI chưa cung cấp edit/delete nên CAS/stale `409`/tombstone/non-author được chứng minh bằng
  HTTP/PostgreSQL suite, không can thiệp SQL live. Unread query còn residual hiệu năng ở
  conversation cực dài; chỉ tối ưu sau load evidence bằng forward migration riêng.

Disposable branch tiếp tục được giữ theo quyết định của owner. P3-07B delivery vẫn
`DEFERRED/TODO`; runnable lane tiếp theo là P3-08 file metadata/upload intent/finalize.

### Checkpoint P3-06 ngày 2026-08-04

P3-06 đã khóa phạm vi REST-only cho conversation container; message persistence,
unread/read, realtime và notification vẫn thuộc P3-07A/P3-07B. ADR-0013 được amend:

- direct conversation đúng hai active member cùng tenant; request chỉ nhận exact target
  email, server tự resolve/canonicalize participant set và create lặp/concurrent phải trả
  cùng một conversation;
- mỗi class có tối đa một conversation, không copy roster; quyền đọc luôn lấy từ implicit
  owner, organization policy và enrollment authoritative;
- archived class giữ lịch sử read-only, chặn create/write mới; restore vẫn phải reauthorize;
- feature `conversations` có server-side emergency-off; quota tổng thể được giữ cho P3-13.

Vertical slice đã hoàn tất migration `000024`, Go module/API, OpenAPI/generated client và
web `/app/messages`; không bật worker, realtime hoặc delivery side effect. Toàn bộ exit gate
đã xanh:

- disposable rồi shared Neon được forward-only `23 false -> 24 false`; migrate lặp
  idempotent vẫn giữ `24 false`, exact runtime ACL cùng PostgreSQL canonical-create,
  authoritative-membership, archive, tenant/privacy/audit và concurrency gates đều PASS;
- candidate `756ca60aee579e60ae386c50020ecaf166d1a034` có Quality/integration,
  Browser E2E, Local environment smoke, Secret scan, CodeQL Go/JavaScript, repository và
  container scan cùng Cloudflare Pages đều PASS; dependency review skip đúng policy push;
- `pnpm verify` PASS với API-client `32/32`, web `237/237`, toàn bộ Go test/vet, lint,
  typecheck, build, Storybook, format và client-bundle/security checks;
- Render deployment `dep-d9ov689t0dsc73bu99a0` của exact candidate đã Live. Direct Render
  và Pages-proxied health/readiness đều trả `200 + Cache-Control: no-store`;
- authenticated live acceptance chứng minh Teacher/Student reverse-create trả cùng direct,
  projection đúng hai display name và không lộ email; Admin ngoài participant bị conceal
  `404`; class conversation bị conceal trước enrollment, trả cùng ID sau enrollment, bind
  sau archive trả `409` nhưng history cũ vẫn đọc `200` với trạng thái read-only;
- Browser E2E chạy UI thật với ba role đã PASS canonical direct/class, tenant concealment,
  privacy projection, Enter/Escape focus và Axe không violation trên direct/class page.
  Live acceptance cũng PASS heading focus, dialog autofocus, Escape trả focus, không
  overflow ngang; selector màu heading-action được thu hẹp sau khi Axe phát hiện contrast
  `1.55:1`, và exact candidate hiển thị chữ trắng trên nền primary đúng policy.

Acceptance class riêng được giữ ở trạng thái archived. Không có message persistence,
unread/read, realtime hoặc notification side effect được bật; các phần đó tiếp tục ở
P3-07A/P3-07B. P3-06 chuyển `VERIFY -> DONE` ngày 2026-08-04; runnable lane tiếp theo là
P3-07A.

### Cập nhật P3-02D-A ngày 2026-08-03

Automated database/deployment acceptance đã hoàn tất trên candidate
`8585864198ae0d9539c480e21e7b5efbbab0d389`:

- shared Neon forward-only từ `21 false` tới `23 false`; migrate lặp idempotent vẫn
  `23 false`, không rollback;
- owner preflight, exact runtime ACL, dedicated maintenance ACL/login, purge cascade/
  `SKIP LOCKED`, poll ownership/quota/tenant isolation/capability privacy/rate,
  StudyMeeting/ClassSession two-writer barrier và feature-control concurrency đều PASS;
- `corepack pnpm verify`, integration-tag compile và focused Playwright/axe desktop,
  public, mobile, keyboard, forced-colors `3/3` PASS;
- candidate Live trên Render deployment `dep-d9nvul5aeets73cpc330`; Cloudflare Pages,
  Browser E2E, Secret scan, Local environment smoke và CodeQL success. Direct Render và
  Pages-proxied health/readiness trả `200 + no-store`; public Pages/resolve giữ concealment
  và các header no-store/no-referrer/noindex/CSP đúng policy;
- notification/canary side-effect flags vẫn false; P3-02D-B auto-close/fan-out/delivery
  và LiveKit lifecycle chưa được bật. Disposable branch tiếp tục được giữ.

Authenticated live browser checkpoint đã chạy trên exact candidate:

- Teacher owner lifecycle, privacy projection, StudyMeeting và ClassSession business flow PASS;
- Student own-response/coarse projection, capability-negative finalize chỉ ra StudyMeeting và
  StudyMeeting cancellation PASS;
- non-owner class scheduler có authoritative `session.schedule` thấy individual projection/
  exact aggregate nhưng không có owner controls; ordinary class member không có capability chỉ
  thấy own/coarse projection;
- Admin negative response/owner-control matrix, cross-workspace concealment, inaccessible-class
  bind `404` không mutation, public fragment scrub/privacy và revoke cascade đều PASS;
- fixture poll đã hủy, ClassSession/StudyMeeting đã hủy và class fixture đã lưu trữ lại.

API-only safety-admin recovery checkpoint cũng PASS:

- exact Teacher/Admin role và P2-08 tenant preflight PASS;
- whitespace-only, 257-byte ASCII và 258-byte multibyte recovery reason đều bị từ chối mà
  không đổi version;
- Admin cross-owner capability revoke, poll cancel và StudyMeeting cancel PASS; revoked public
  token trở về uniform `404`, response không chứa raw token/share URL;
- audit actor/action/resource và exact metadata allowlist PASS; không có token/secret/email/
  title/description/answer/roster/session field;
- fixture poll và StudyMeeting đều terminal; cleanup PASS. Hai storage state app-origin-only và
  harness tạm nằm trong Git-ignored path đã bị xóa ngay sau test, không log credential.

Manual NVDA checkpoint trên exact organizer/public production route cũng PASS với NVDA
`2026.1.1.55980`:

- organizer heading/landmark, slot radio label/checked state, keyboard operation, save
  announcement và focus recovery PASS;
- stale two-tab `409` alert giữ draft và phục hồi focus PASS;
- public heading/deadline/timezone, slot label, low-cohort suppression, save announcement và
  focus recovery PASS;
- sau cancel/revoke, tab public đang giữ chỉ hiện trạng thái không khả dụng chung, không còn
  answer control và không lộ reason/identity/token; cleanup PASS.

Không lưu screenshot, trace, account identifier, capability token, cookie hoặc response payload.
Toàn bộ exit gate đã đạt và P3-02D-A chuyển `VERIFY -> DONE` ngày 2026-08-03. Hồ sơ chi tiết
nằm tại [`P3_02D_A_STAGING_ACCEPTANCE.md`](P3_02D_A_STAGING_ACCEPTANCE.md).

### Cập nhật P3-02D-A ngày 2026-08-01

P3-02D-A đã có vertical slice local ở trạng thái `VERIFY`: migration `000022`, normalized
poll/slot/participant/capability/response/answer/receipt và Study Meeting schema; Go API,
OpenAPI/generated TypeScript client; organizer/public React flow; manual lifecycle,
response/ranking/finalize; secure fragment exchange; owner-time conflict; audit/outbox;
feature kill switch, tenant quota và hard cap. Individual response dùng projection phân
trang riêng và chỉ mở cho owner, safety admin hoặc class member có authoritative
`session.schedule`; poll/public projection không nhúng dữ liệu này.

`VERIFY` không phải `DONE`: disposable đã xác nhận migration `000022` qua chuỗi up/down/up
`21 false -> 22 false -> 21 false -> 22 false`, sau đó forward `000023` và migrate lặp
idempotent đều ở `23 false`. Exact runtime/maintenance ACL, maintenance cascade/`SKIP LOCKED`,
poll ownership/quota/isolation/capability, StudyMeeting/ClassSession barrier, feature quota/rate
concurrency và full Calendar PostgreSQL package đã PASS. Shared staging, deploy Render/Cloudflare
và staging authorization/privacy/concurrency/accessibility smoke chưa có sign-off. Hồ sơ thực thi nằm tại
[`P3_02D_A_STAGING_ACCEPTANCE.md`](P3_02D_A_STAGING_ACCEPTANCE.md).

Security/architecture review ngày 2026-08-02 đã chấp nhận ADR-0024 và áp dụng migration
forward-only `000023`: purge function chuyển sang `SECURITY DEFINER`, fixed
`search_path = pg_catalog, pg_temp`, schema-qualified body, không dynamic SQL và revoke
`PUBLIC`. Maintenance contract sau provisioning chỉ còn schema `USAGE` + function
`EXECUTE`; không cấp direct table/column privilege. Disposable đã ở `23 false`; không có
rollback `000022` thêm.

P3-02D-B tiếp tục `DEFERRED/TODO`. Deadline worker auto-close, roster fan-out, email/ICS,
notification/reminder delivery và LiveKit room lifecycle đều chưa được bật; các feature/
worker/delivery/LiveKit consumer gate liên quan vẫn `false`.

Cross-writer code gate đã được harden local: Study Meeting và mọi writer tạo busy edge từ
one-time/recurring ClassSession, internal audience addition hoặc organizer transfer dùng
chung legacy-compatible advisory key theo tenant/user, khóa UUID theo thứ tự ổn định và
reverse-check scheduled StudyMeeting trong cùng transaction. HTTP trả conflict generic,
không lộ meeting riêng tư. Full `go test -count=1 ./...`, `go vet ./...`,
`corepack pnpm verify`, focused test và integration-tag compile đã đạt. Barrier hai writer
thật, exact runtime ACL probe, poll ownership/capability test, maintenance cascade và
feature-control concurrency đều PASS trên disposable; P3-02D-A tiếp tục giữ `VERIFY` cho
tới staging/browser/manual sign-off.

Gap kề cạnh được phát hiện khi harden recurring split: child series giờ giữ đúng organizer
đã transfer thay vì mặc định thành mutation actor, nhưng audience/participation settings
của parent vẫn chưa được copy/re-seal sang child. Owner-time reverse check chỉ dùng
occupancy thực sự được persist (organizer) để không false-positive. Audience continuity
cho following split phải được xử lý như regression riêng trước khi tuyên bố flow đó hoàn
chỉnh; task này không copy ciphertext invitation hoặc mở rộng delivery scope.

### Disposable database checkpoint — 2026-08-02

- Các URL database cần cho probe được nạp trong cùng command kiểm thử có credential; không
  ghi URL, password, role name, token hoặc fixture payload vào log/artifact.
- Owner preflight trước forward: `ENV_LOAD=PASS`, `OWNER_PREFLIGHT=PASS`; chỉ ghi nhận
  `OWNER_ADMIN_RESIDUAL=true` ở migration owner, không truyền sang runtime/maintenance.
- Disposable đã forward `22 false -> 23 false`; migrate lặp idempotent vẫn `23 false`. Không
  rollback thêm, không migrate/deploy shared staging và giữ nguyên disposable branch.
- Maintenance role đã đồng bộ password từ `DATABASE_POLL_MAINTENANCE_URL`; login và
  re-provision exact ACL đều PASS. Function metadata, direct-DML denial, cascade/detach và
  `SKIP LOCKED` đều PASS dưới maintenance credential.
- Exact Core API runtime ACL/role safety, poll ownership/quota/tenant isolation/capability
  lifecycle/privacy/rate, StudyMeeting/ClassSession barrier và feature-control quota/rate
  concurrency đều PASS trên PostgreSQL thật. Full Calendar integration package PASS.
- Full Classroom/feature-control package còn test debt ngoài gate: một số assertion dùng
  runtime login để `SELECT outbox_events` trái exact insert-only ACL và ba fixture/lifecycle
  Classroom không thuộc P3-02D-A còn đỏ. Không nới ACL; targeted barrier nêu trên đã PASS.
- Shared staging vẫn giữ nguyên `21 false`; Render/Cloudflare/browser/manual accessibility
  gates chưa chạy cho P3-02D-A.

### Mô hình thoát Phase 3 (re-baseline 2026-07-31)

Phase 3 được theo dõi theo hai lane, không còn coi toàn bộ phase là một chuỗi tuần tự:

- **Core Exit (runnable lane):** hoàn thiện các slice có thể chạy và nghiệm thu trên
  Render/Neon/B2 hiện tại: P3-02D-A poll/Study Meeting core, conversation/message core,
  file transfer core và các phần dashboard/quality không cần worker hoặc provider live.
  Khi checklist Core Exit xanh, owner có thể bắt đầu Phase 4.
- **Deferred carry-over:** P3-03B durable worker, P3-04 activation, P3-CAL-02 live
  SES/domain/interoperability, P3-05A/B delivery, P3-10/P3-11B processing phụ thuộc
  worker. Các lane này vẫn mở, tiếp tục được thực hiện sau khi Phase 4 bắt đầu và không
  được ghi `PASS` chỉ vì chưa có môi trường chạy.

Core Exit là một mốc chuyển tiếp, **không** phải `Phase 3 DONE` và không thay thế biên
bản P3-14. Full Phase 3 chỉ đóng khi carry-over gates, side-effect safety, staging
acceptance và `PHASE_3_COMPLETION.md` đều được sign-off. Trong thời gian chờ, các cờ
notification và Class Files activation vẫn `false`; không phát email/ICS/reminder hoặc
worker-driven file processing/sharing tới end user.

## Kiến trúc đang chạy

- Web: React + TypeScript + Vite trên `https://tutorhub-web.pages.dev`.
- Edge/BFF origin: Cloudflare Pages Function proxy `/api/*` tới Core API.
- Core API: Go modular monolith, OCI container trên `https://tutorhub-core-api.onrender.com`.
- Identity: ZITADEL OIDC Authorization Code + PKCE, session cookie phía BFF.
- Database: Neon PostgreSQL; staging tách branch và role runtime/migration.
- Object storage: Backblaze B2, application key tối thiểu quyền.
- Media: LiveKit Cloud; token do backend cấp, webhook được xác minh và lưu idempotent.
- Hugging Face không còn chạy Core API; chỉ là lựa chọn cho dịch vụ AI độc lập sau này.

## Đã hoàn thành

- P1-01 toolchain, monorepo, CI foundation.
- P1-02 React web shell, routing, TanStack Query, i18n và responsive states.
- P1-03 design tokens, UI primitives, Storybook và accessibility baseline.
- P1-04 Go API foundation: config fail-fast, graceful shutdown, structured log,
  request ID, Problem Details, metrics, health/live/ready.
- P1-05 OpenAPI source of truth, generated TypeScript client, PostgreSQL
  migrations, tenant context, outbox và integration tests.
- P1-06 OIDC/BFF authentication, session/CSRF/logout và workspace onboarding.
- P1-06B class vertical slice list/create/detail với authorization và tenant isolation.
- P1-07 LiveKit token service, prejoin, room UI, media controls, reconnect,
  telemetry và webhook receipt idempotent.
- P1-08A Verify/Security pipeline, Gitleaks, Dependency Review, CodeQL, Trivy,
  Dependabot, CODEOWNERS và bundle secret guard.
- P1-08B Cloudflare Pages production deployment, same-origin API proxy, Render
  Core API deployment, provider auto-deploy, health/readiness và rollback smoke.
- P1-09 PostgreSQL/Redis local bằng Compose, migration + seed idempotent,
  `local:setup` và `dev:local` một lệnh cho Windows/Linux.
- P1-10 staging resources: Neon, B2, Cloudflare Pages, Render, ZITADEL và LiveKit.
- Chấp nhận ADR-0011 để thay Hugging Face bằng Render cho Core API staging/private alpha.
- Rà exit gate Phase 1 trên commit `ee597af`: Verify/Security CI, HTTPS staging,
  OIDC, Neon, B2, LiveKit, telemetry và rollback đều đạt.
- Chấp nhận ADR-0012 cho direct-main có kiểm soát trong giai đoạn một người duy trì.
- P2-00: policy engine deny-by-default dùng chung cho identity/classroom/media;
  organization/class role matrix, effective permission, 403/404 concealment,
  OpenAPI enums/error conventions, policy test helpers và static boundary test.
- Chấp nhận ADR-0013 cho mô hình role tổ chức/lớp và authorization policy dùng chung.
- P2-01: profile GET/PATCH, identity list/link/unlink, recent-auth + state/nonce,
  collision protection, last-identity guard, audit/outbox, migration `000006`,
  OpenAPI/generated client và React settings UI có i18n vi/en.
- P2-02: tenant list/detail/create/update/archive, permission `tenant.view`/`tenant.manage`,
  optimistic version, session-context CAS, session/CSRF rotation và migration `000007`;
  success event `tenant.created/updated/archived/switched` được ghi durable qua outbox.
- Workspace UI áp dụng principal mới ngay sau create/switch/archive, hủy và xóa cache
  tenant-scoped để không flash dữ liệu workspace cũ; list/detail/update/archive có query,
  mutation và trạng thái lỗi phù hợp với contract typed.
- P2-03: permission `tenant.manage_members`, migration `000008`, invitation CSPRNG chỉ
  lưu purpose-bound HMAC, TTL/state machine, list/create/revoke và preview/accept bằng
  verified linked identity trong transaction idempotent; accept không tự đổi active tenant.
- Invitation URL giữ token trong fragment, web xóa fragment ngay và chỉ gửi token trong
  JSON POST body; admin UI có list/create/copy-once/revoke, public UI có đủ loading,
  offline, unavailable, mismatch, retry và success states bằng tiếng Việt/Anh.
- P2-04: migration `000009` bổ sung class `timezone`, optimistic `version` và
  `archived_at`; list có status filter cùng opaque keyset cursor, update/archive/restore/
  transfer ownership dùng `expected_version` CAS và transactional outbox.
- Class đi từ draft sang active; archive draft/active và restore đúng trạng thái trước.
  `owner_user_id` là owner implicit cho đến P2-05/P2-06, không tạo enrollment sớm.
  Chỉ `org_admin` hoặc owner có `class.archive`/`class.transfer_ownership`; target
  transfer phải là active member cùng tenant đủ điều kiện tạo lớp và actor phải có
  recent authentication trong 10 phút.
- Classroom UI đã có create/edit/activate/archive/restore, conflict recovery, status
  filter và pagination. LiveKit token/telemetry chỉ chấp nhận class active.
- P2-05: migration `000010` thêm enrollment lifecycle
  `invited -> active -> suspended/left/removed`, class invite code có TTL/usage limit
  và index/constraint tenant-scoped; owner tiếp tục là implicit ở `classes.owner_user_id`.
- Invite code dùng CSPRNG 256-bit và purpose-bound HMAC; raw token chỉ trả một lần
  trong create response, nằm trong URL fragment rồi được web xóa ngay. Join truyền
  token trong JSON body, khóa/cập nhật usage atomically và không tiêu thêm lượt khi
  active member, owner hoặc organization manager gọi lại.
- Direct enrollment, suspend, remove, revoke, join/rejoin và self-leave đều đi qua
  shared policy cùng repository tenant-scoped; outbox payload dùng allowlist và không
  chứa token, hash hoặc email.
- Class detail/list và LiveKit token/event dùng `viewer_access` do server resolve từ
  owner, organization manager hoặc enrollment active. Web có management panel,
  copy-once invite, public join và self-leave với đầy đủ loading/empty/error/
  forbidden/retry states bằng tiếng Việt/Anh.
- P2-06: manager-only roster API có owner implicit ghim riêng, normalized search theo
  display name/email, status filter và opaque keyset pagination bind đúng tenant/class/
  filter. Shared policy áp dụng hierarchy `org_admin > owner > teacher/co_teacher >
teaching_assistant > student/guest`, chặn self/peer/owner mutation và chỉ ownership
  transfer mới đổi owner.
- Single role update và bulk `update_role/suspend/remove` tối đa 50 user ID có ordered
  updated/unchanged/failed outcomes. Bulk commit từng item độc lập; web refetch cả khi
  mutation thành công hoặc lỗi hạ tầng. Role-changed outbox payload dùng allowlist và
  không chứa email/display name/token.
- Roster UI có search/status, infinite pagination, row/bulk confirmation, selection
  bằng keyboard, partial-failure summary và loading/empty/error/forbidden/archived
  states. Class management dùng class-scoped lifecycle capabilities từ server.
- LiveKit grant mới lấy class role authoritative và ghi effective/organization/class
  role attributes; token cấp sau mutation phản ánh role mới. JWT/participant đã tồn tại
  không được sửa hoặc thu hồi retroactively.
- P2-07 chấp nhận ADR-0014 và migration `000011`: `audit_events` tenant-owned,
  append-only bằng trigger `ALWAYS`, actor user/system, action/resource/outcome,
  request correlation, privacy-reduced source hints và flat metadata allowlist.
- Success audit của mutation P2-02 đến P2-06 được ghi cùng business transaction và
  outbox; authenticated no-op/denied/failed attempt dùng fallback transaction có
  server-generated request-instance ID để không ghi sai sau post-commit failure. Panic
  ở sensitive handler vẫn được audit rồi chuyển tiếp tới recovery middleware. Accept
  invitation bind tenant đích do server resolve; bulk roster dedupe từng target và ghi
  cả item lỗi hạ tầng/chưa được thực hiện.
- Permission `audit.view` chỉ cấp cho active `org_admin`. API
  `GET /api/v1/tenants/{tenant_id}/audit-events` reload membership authoritative,
  khóa path theo active tenant, hỗ trợ time/action/resource/outcome filter cùng opaque
  filter-bound keyset cursor và trả `no-store` projection không có IP/UA hash.
- Web có route `/app/workspace/audit`, permission gate, tenant-scoped infinite query,
  cache isolation khi switch/archive, filter validation, pagination và đầy đủ loading/
  empty/filtered-empty/error/forbidden/stale-refresh states bằng tiếng Việt/Anh.

## Kết quả acceptance staging ngày 2026-07-16

- Web và API HTTPS hoạt động; `/health` và `/ready` đạt trực tiếp và qua proxy.
- Readiness báo `database=ready` và `object_storage=ready`.
- OIDC login/callback, `/me`, reload giữ session, logout và đăng nhập lại đạt.
- Neon migrate -> rollback -> migrate đạt, version `5`, `dirty=false`; nhánh smoke
  tạm đã được kiểm tra không còn dữ liệu webhook thử nghiệm.
- B2 PUT/GET/checksum/DELETE đạt với key staging đã rotate.
- LiveKit 2-5 người đạt camera, micro, screen share và reconnect.
- LiveKit webhook HTTPS, xác minh chữ ký và idempotency đạt.
- Secret chỉ nằm trong file local bị Git-ignore hoặc secret store của provider;
  không xuất hiện trong repository, frontend bundle hoặc log kiểm thử.

## Kết luận Phase 1

Phase 1 hoàn thành ngày 2026-07-16. Biên bản và ma trận bằng chứng nằm tại
`docs/PHASE_1_COMPLETION.md`. Repository chưa có ruleset công khai; đây là ngoại lệ
được ghi nhận trong ADR-0012, không phải kiểm soát đã bật. Ngoại lệ phải hết hiệu lực
trước pilot/public beta hoặc khi có người duy trì thứ hai.

## Phase 2 đã hoàn thành ngày 2026-07-22

Backlog có thẩm quyền: `docs/PHASE_2_BACKLOG.md`.

1. P2-00 đến P2-12 đã hoàn thành; biên bản staging và đóng phase đã đạt exit gate,
   product/engineering owner sign-off ngày 2026-07-22.
2. P2-08 nối các contract workspace/invitation/class/roster/audit thành luồng UI
   org admin, teacher và student; capability guard, cache tenant/class, trạng thái
   forbidden/retry và navigation đã được chuẩn hóa.
3. [Verify #59](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888239)
   (`836ae7e`) xanh ngày 2026-07-20: Quality/integration, Browser E2E
   PostgreSQL 17 + Chromium và Local environment smoke đều đạt. Web 130/130, API
   client 15/15, UI 6/6 và E2E infrastructure 8/8 tiếp tục xanh.
4. Scenario Playwright ba role với fake OIDC loopback/PKCE đã chạy xuyên suốt
   workspace/invitation/class/roster/archive/audit trên CI. Visual QA thủ công đạt tại
   1440x900, 1024x768 và 390x844.
   [Security #54](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29716888233)
   cùng commit cũng xanh.
5. Acceptance UI staging P2-08 được chạy lại ngày 2026-07-20 trên fixture dùng một
   lần với ba identity ZITADEL đã xác minh riêng biệt cho org admin, teacher và
   student. Luồng tạo/chỉnh/chuyển workspace; tạo/thu hồi/chấp nhận invitation;
   teacher tạo/chỉnh/kích hoạt lớp và tạo join link; student join; teacher đổi role,
   suspend, remove, thu hồi link và archive lớp đều đạt.
6. Org admin xem được audit đúng actor, action, resource, outcome và request ID cho
   toàn bộ chuỗi thao tác. Lượt nghiệm thu không dùng SQL/manual API và không lưu
   storage state, token hay secret vào repository hoặc artifact. Deployment/contract
   drift ghi nhận ở lượt kiểm tra trước đã được đồng bộ; P2-08 chuyển `DONE`.
7. P2-09 chấp nhận ADR-0015, migration `000012`, typed catalog với global safety
   ceiling và tenant override có optimistic version. Quota member/active class/
   invitation được enforce transactionally ở server; capability API/UI fail-closed,
   thay đổi override có audit và quota rejection có metric.
8. Anonymous invitation preview/accept và class join dùng signed edge context cùng
   shared PostgreSQL limiter. Web 139/139, API client 16/16, root format/lint/
   typecheck/build/test/security bundle cùng full Go non-integration suite và `go vet`
   đều xanh cục bộ.
9. Acceptance P2-09 ngày 2026-07-21 đạt trên commit `096620a`: Render và Cloudflare
   cùng chạy head này; health/readiness/status trực tiếp và qua Pages đều trả 200.
   Neon staging ở migration `12 false`, runtime grants đúng ma trận tối thiểu và role
   không sở hữu bảng/không có quyền nguy hiểm. Signed edge/public limiter trả 404 cho
   token giả và ghi window active với `used_count=1`.
10. Hai integration test feature-control chạy bằng runtime role trên Neon staging đã
    đạt: feature disabled, tenant isolation, audit/outbox và concurrent member/class/
    invitation quota đều giữ invariant. HTTP regression xác nhận
    `403 feature_disabled`, `404 tenant_not_found`, `429 quota_exceeded`; metric quota
    rejection dùng label bounded. Bounded cleanup xóa `0` rate-limit window và `0`
    tenant-quota window; P2-09 chuyển `DONE`.
11. P2-10 ngày 2026-07-22 đã bổ sung actor/resource matrix, PostgreSQL
    security fixture có rollback, exact foreign class/user/invite ID invariants, stale
    membership và workspace-switch token rotation. HTTP mutation dùng strict JSON object,
    từ chối unknown/duplicate/trailing/oversized payload; resource UUID ở path/query chỉ
    nhận dạng canonical. Class cursor v2 bind tenant/filter và class/roster cursor dùng
    strict decoder. Chín fuzz function cho JSON, UUID, invitation token, cursor, roster
    search và media identifier đều đạt. Commit `c4205b9` đã xanh trên
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539891), gồm
    PostgreSQL 17 matrix, và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29884539912),
    gồm CodeQL, Trivy repository/container cùng secret scan. Không có finding
    High/Critical chưa xử lý; P2-10 chuyển `DONE`.
12. P2-11 đã chấp nhận ADR-0016 và bổ sung migration `000013`, fixture JSON ẩn danh,
    CLI `v1-fixture-import`, external-ID mapping, per-record checkpoint/resume và
    reconciliation report. Importer chặn production, từ chối payload không strict hoặc
    email ngoài `.invalid`, không đọc dữ liệu/configuration V1 thật. PostgreSQL 17 local
    tạm xác nhận full integration; commit `f07d05d` đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333712) và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29891333728).
    Migration `13 -> 12 -> 13`, dry-run, apply/rerun, checkpoint/resume, reconciliation
    và cleanup/reset database tạm đều đạt; P2-11 chuyển `DONE`.
13. P2-12 đã mở rộng scenario Playwright ba role để kiểm tra class invite link đi từ
    `0/2` sang `1/2` lượt dùng, roster vẫn được giữ sau archive, invite link còn active
    không thể dùng để join lớp đã archive, và audit create-class có actor, resource ID
    cùng request ID. Implementation nằm ở commit `bf30605`; các commit `7563ed1` và
    `6fb4f84` thu hẹp locator audit để tránh match nhầm action có tiền tố giống nhau.
    Candidate `6fb4f84` đã đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962433), gồm
    Browser E2E PostgreSQL 17 + Chromium, và
    [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29910962424).
    Checkpoint `3c48964e3900b2a262c4026abf0174b3c39c5d93` tiếp tục đạt
    [Verify](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093175)
    và [Security](https://github.com/basangnguyen/TUTORHUB_WEB/actions/runs/29912093166);
    Cloudflare Pages check suite cùng full SHA cũng success.
14. Neon P2-12 ngày 2026-07-22 đã chạy trên branch dùng một lần từ staging:
    `12 false -> 13 false -> 12 false -> 13 false`. Importer dry-run lập kế hoạch 12
    record; apply nhập 10, skip 2, fail 0; rerun giữ 10 unchanged, không duplicate;
    reconciliation có 2 run, 10 mapping và 24 item, sau đó branch được xóa. Neon staging
    thật được forward `12 false -> 13 false`.
15. Lượt provider audit phát hiện default ACL cũ của Neon owner tự cấp CRUD cho runtime
    trên bảng mới. Provisioning đã thu hồi default table ACL global/schema-scoped và
    grant materialized trên ba ledger. Kết quả cuối: default ACL leak `0`,
    effective/direct ledger privilege `0`, runtime không owner/superuser/bypass RLS,
    schema `USAGE=true`, `CREATE=false`, audit chỉ `SELECT/INSERT`, future-table probe
    không có quyền. Không sửa migration lịch sử `000013` hoặc re-grant khi rollback.
16. Sau staging migration, Render đã live release candidate full SHA
    `3c48964e3900b2a262c4026abf0174b3c39c5d93` qua deploy
    `dep-d9gaiturnols73c75qp0`. Public health/readiness/status trực tiếp Render và qua
    Pages proxy đều HTTP 200 (6/6), readiness báo database/object storage ready.
17. Lượt UI staging S01-S07 chạy khoảng 19:06-19:36 ngày 2026-07-22 trên workspace
    `P2-12 Acceptance 202607221900`, class
    `f61e3344-251f-42eb-b3bc-90fd9f9cff5d`, đã đạt. Admin tạo workspace/mời hai role;
    teacher/student accept, login và switch; teacher tạo class active cùng link 1 ngày,
    2 lượt (`0/2 -> 1/2`); student join; roster đổi Trợ giảng, suspend, remove và refresh
    vẫn giữ `Đã xóa`. Cross-tenant UI conceal exact class và exact audit filter ở `KMA`
    trả 0; exact foreign roster/media-token tiếp tục dùng P2-10 automated baseline,
    không có direct staging POST room-token trong lượt này. Archive chặn identity chưa
    enroll join qua link còn `1/2` và vẫn giữ roster history. Audit workspace có 22 event,
    exact class có 5 event create/update/roster/archive, actor/resource/request ID đầy đủ
    và denied join cũng được audit.
18. S09 provider closure đạt khoảng 19:39-19:43 ngày 2026-07-22. Render live latest
    `0be98bb`, application rollback bằng `Deploy a specific commit` về `3c48964`, rồi
    forward lại `0be98bb`; mỗi bước đều đạt 6/6 probe direct Render và qua Pages. Native
    Rollback được cancel an toàn vì cảnh báo không tải được cấu hình live; không có thay
    đổi cấu hình được tuyên bố. Phiên Admin/audit vẫn hoạt động sau forward deploy.
19. Owner đã sign-off P2-12. Closure-record docs-only phải được hậu kiểm Verify/Security
    sau push; nếu một workflow thất bại thì mở lại P2-12 và khắc phục regression.

## Phase 3 bắt đầu ngày 2026-07-22

Backlog có thẩm quyền: `docs/PHASE_3_BACKLOG.md`.

1. P3-00 đã tạo task ID/dependency/acceptance/exit gate cho daily learning workspace.
2. ADR-0017 chốt P3-01 session một lần: instant UTC + IANA timezone, DST gap/overlap,
   optimistic version, tenant/class policy và boundary với recurrence/Phase 4 media.
3. ADR-0018 chốt worker process riêng trong cùng Go modular monolith: PostgreSQL lease
   có fencing token, at-least-once, retry/backoff, idempotency và dead-letter; chưa thêm
   Redis/NATS/Kafka hoặc provider mới.
4. P3-01 đã hoàn tất vertical slice local: migration `000014_class_sessions`,
   permission/feature control, OpenAPI/generated client, Go repository/service/HTTP,
   timezone/DST validation và UI tối thiểu trên class detail. Scope không gồm
   recurrence, reminder, calendar tổng hợp, worker runtime hoặc media lifecycle.
5. P3-CAL-00 đã audit Google Calendar, Microsoft Teams, Zoom, ClassIn, các lựa chọn
   mã nguồn mở và TutorHub V1; kết quả nằm tại
   `docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md`.
6. Đề xuất calendar-first dùng FullCalendar Standard chỉ làm renderer sau spike; domain,
   quyền, recurrence, conflict, reminder và LiveKit vẫn do TutorHub sở hữu. Chưa thêm
   dependency hoặc runtime code trong P3-CAL-00.
7. V1 chỉ được giữ làm nguồn nghiệp vụ: event/task/availability poll, quick create và
   panel agenda. Không port CalendarFX/DAO/model vì V1 hard-code user, JDBC trực tiếp,
   thiếu tenant/timezone/DST/version/audit và nhiều control chỉ là vỏ UI.
8. P3-CAL-01 đã chốt ADR-0019 về series/exception/occurrence, recurrence DST,
   conflict policy và exact dependency pin sau performance, accessibility automated,
   license và security spike. Manual NVDA được giữ làm explicit production-route gate.
9. P3-CAL-00B đã nghiên cứu lại Teams/Google và CSS live Vauliys; chốt Teams-inspired
   IA/editor, Warm Academic cream palette và professional everyday parity. Không sao
   chép asset/font/trade dress và chưa đổi runtime token.
10. Invitation/update/cancellation/reminder email, ICS và RSVP đã được đưa vào Phase 3
    exit gate. Owner đã chọn AWS SES làm provider target; P3-CAL-02/ADR-0020 vẫn phải
    xác minh account/region/sandbox/quota, adapter, provider-event ingress, iTIP/iMIP và
    deliverability.
    Trước khi có domain chỉ thử bằng owner-controlled verified identities trong SES
    sandbox; production vẫn cần domain/DNS cùng SPF/DKIM/DMARC. Mọi effect runtime chỉ
    chạy sau commit qua P3-03 worker. Đây là re-baseline tài liệu, chưa phải chức năng
    đã chạy.
11. ADR-0021 đã `Accepted` và chốt P3-02D Native Availability Poll: mọi active
    authenticated tenant member, gồm student, được tạo/quản lý poll và Study Meeting của
    mình theo feature/quota. Poll có class-only, invited-only và explicit anyone-link;
    public capability chỉ lưu hash, có expiry/revoke/scope/rate limit và không lộ roster/
    email/individual availability. Chỉ actor có `session.schedule` mới finalize thành
    ClassSession; actor khác chỉ tạo Study Meeting. When2meet chỉ là comparator, không
    phải runtime/API/iframe/fork/code dependency.
12. P3-CAL-00C đã rà soát readiness lần cuối bằng nguồn chính thức và upstream: tách
    P3-02A/B/C cùng P3-05A/B, kéo P3-03 lên trước consumer side effect, bổ sung
    WorkingSchedule/suggested-time contract, audience diff, reminder lifecycle,
    split-exception preview, direct StudyMeeting API, poll close/reopen và hardening
    capability link. SES không có caller idempotency token; timeout mơ hồ dùng app effect
    ledger + trạng thái `outcome_unknown`; canonical delivery state không gọi
    mail-server acceptance là inbox. Kế hoạch đã khóa required VCALENDAR/MIME một calendar
    part, full durable provider-event path tới inbox/consumer, iterator recurrence có
    cancellation/cap, DST suggested-time total order và giới hạn đúng mức của public-poll
    cohort/dedupe. Vòng hậu kiểm đã tách `CalendarDisplayPreference` về P3-02A,
    `WorkingSchedule` về P3-02C; tách P3-02D thành P3-02D-A core (có thể triển khai
    không cần durable worker) và P3-02D-B lifecycle delivery/auto-close/fan-out
    (`DEFERRED/TODO`, chờ P3-03B/P3-04 activation). P3-05B là delivery adapter downstream
    ở carry-over. P3-02D-A đã `DONE` sau exact staging/browser/API/NVDA acceptance; mọi
    delivery thuộc P3-02D-B vẫn chưa được triển khai hoặc bật.
13. P3-CAL-01 đã `DONE` ở cấp decision spike. FullCalendar Standard `7.0.1`,
    Temporal `1.0.1`, Warm Academic theme và adapter boundary nằm trong
    `apps/calendar-spike`; comparator v6.1.21 nằm riêng ở `apps/calendar-spike-v6`.
    Typecheck/lint, 8 unit/DOM test, build, dependency/license guard 3/3 và grouped
    Playwright interaction/a11y/performance đều đạt. Full rerun v7 hậu fix đạt
    `9 passed (23.6s)`, exit 0; Axe critical/serious bằng 0, waiver upstream
    `empty-table-header` bị khóa exact một node/target/HTML/scope.
    V7 render p95 `152/164/201 ms`, navigation p95 `204/327/548 ms`, long-task max
    `79/198/315 ms` ở 500/1.000/2.000 item, heap delta 2.000 item `26,34 MiB`,
    JS/CSS gzip `155.15/5.37 KiB`. Comparator parity-config v6 full run đạt
    `4 passed (17.5s)` và heap nhỏ hơn `7,44 MiB`, nhưng fail render 500
    `1.492 > 500 ms` cùng long-task 2.000 `404 > 400 ms`, nên bị loại. Agenda
    progressive `24 -> 48 -> 51` đạt keyboard/mobile automated evidence. ADR-0019 được
    `Accepted; manual NVDA gate PASS`; FullCalendar vẫn chưa nối route production vì
    còn route-level authorization/range/bundle/a11y gate.
14. P3-01 local verification ngày 2026-07-24 đạt web typecheck/test/build, API-client
    typecheck/test và các Go package liên quan. Contract chỉ hỗ trợ session một lần,
    lifecycle public `scheduled -> cancelled`, bounded list/range/duration, optimistic
    version, audit/outbox và feature kill switch.
15. Neon staging đã migrate `13 -> 14` và trả `14 false`; runtime role có
    `SELECT/INSERT/UPDATE` nhưng không có `DELETE/TRUNCATE`. Feature commit `b58666c`
    cùng security patch `a5741a1` đã được deploy; Render direct và Cloudflare same-origin
    health/readiness/status public probes đều xanh.
16. Web acceptance fix `e7dc161` sửa regression dialog edit không hydrate dữ liệu và thêm
    regression test. Browser staging xác nhận Teacher create/update/cancel, Student xem
    session nhưng không có create/edit/cancel, và exact foreign class ID trả trạng thái
    `404` không lộ tên lớp/session. P3-01 chuyển `VERIFY -> DONE`; biên bản tại
    `docs/P3_01_STAGING_ACCEPTANCE.md`. Lượt browser này không được mô tả là Playwright.
17. Go recurrence spike đã khóa query window `366 ngày`, series horizon `730 ngày`,
    `512 occurrence/series`, `2.000 occurrence/request` và deadline `250 ms`.
    COUNT compile validate occurrence cuối trong horizon, boundary test bao phủ
    DAILY/WEEKLY/MONTHLY/YEARLY và YEARLY golden đã đạt. Lượt post-fix unit PASS; hai
    fuzz target 10 giây đạt `238.755`/`199.088` executions. Benchmark 366 occurrence
    count=5 đạt `706.580 ns/op` UTC, `674.220 ns/op` Ho Chi Minh và `984.380 ns/op`
    New York. P3-02B đã đưa adapter typed vào production boundary, nhưng vẫn phải đạt
    integration/load/staging gate khi nối persistence và occurrence API.
18. P3-03A repository/runtime foundation đã đạt `VERIFY`: migration `000015` bổ sung lease,
    fencing token và retained dead-letter; `cmd/worker` dùng credential riêng, exact
    typed registry, bounded claim concurrency, retry/backoff, graceful shutdown và
    structured heartbeat/metric. Startup ACL probe fail-closed nếu worker role không có
    đúng direct-LOGIN exact ACL, có membership/ownership/DDL/quyền bảng nghiệp vụ khác,
    hoặc có INSERT/DELETE/TRUNCATE/REFERENCES/TRIGGER dư thừa.
    Unit test, integration compile gate, duplicate/poison/shutdown PostgreSQL fixtures,
    CI PostgreSQL step, OCI image chung và `docs/P3_03_OUTBOX_WORKER_RUNBOOK.md` đã có.
    Registry production gate-off rỗng có chủ ý và vẫn phát heartbeat định kỳ. Máy local
    không có Docker/psql hoặc migration URL nên PostgreSQL runtime suite phải chạy ở CI;
    worker role/worker grants, durable host và staging crash/reclaim vẫn là gate bên
    ngoài trước `DONE`.
19. P3-04 implementation đạt local `VERIFY` theo ADR-0022. Migration `000016` tạo
    notification projection và preference tenant/user-scoped. Worker chỉ đăng ký exact
    `notification.in_app_canary.requested.v1` khi
    `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY=true`; gate mặc định false, sink idempotent
    và `system.worker_canary` luôn bị API feed loại. API có list keyset, unread bounded,
    mark one/all read và preference GET/PUT CAS; scope authoritative lấy từ session,
    `X-TutorHub-Expected-Tenant-ID` chỉ là assertion chống workspace/cache race.
    Web có bell, center, preference và bounded polling 30 giây, không polling nền.
    `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS=false` ép visibility tắt mặc định.
    Tests/module/HTTP/config/worker/API-client/web liên quan đạt local; không gọi email
    provider. Neon staging đã ở `17 false` sau khi áp `000015 -> 000016 -> 000017`;
    API runtime exact grants đã được probe xanh, nhưng `tutorhub_worker` chưa provision nên
    durable host/canary/crash-reclaim acceptance chưa có. Vì vậy P3-03B vẫn
    `DEFERRED/VERIFY`, P3-04 vẫn `VERIFY` và chưa có end-user activation.
20. P3-02A read foundation đạt local `VERIFY` ngày 2026-07-26. Migration `000017` thêm
    bounded-read index cho ClassSession và `calendar_display_preferences` theo
    tenant/user. Go API có projection ổn định, overlap half-open, range tối đa 366 ngày,
    filter/search/keyset cursor bind tenant/actor/range/filter/timezone, source-policy
    capability và preference full-replacement CAS. HTTP yêu cầu expected-tenant assertion,
    PUT yêu cầu CSRF; OpenAPI/generated client đã đồng bộ.
21. Web có top-level `/app/calendar`, năm URL view Day/Work week/Week/Month/Agenda,
    mobile mặc định Agenda, mini month, filter/search, preference drawer, secondary
    timezone badge và semantic Agenda progressive. Cache key gồm tenant/user/timezone/
    range/filter; workspace switch/logout hủy/xóa calendar cache. Loading, empty,
    filtered-empty, error, forbidden, offline/degraded, retry và pagination đã có.
    Mutation preference dùng principal generation để chặn response cũ ghi lại cache sau
    switch/logout; drawer remount theo tenant/user và Agenda giữ đúng local date ở UTC+14.
    Warm Academic calendar tokens cùng production guard fail-closed chỉ cho exact
    FullCalendar Standard v7.0.1 đã duyệt; Premium, telemetry và CSS/assets ngoài allowlist
    vẫn bị chặn.
22. Local verification P3-02A đạt toàn bộ Go test + vet và integration-tag compile;
    API client 22/22; web 176/176 + lint/typecheck/build; format, security 19/19,
    production Calendar guard và 20 cặp contrast đều xanh. PostgreSQL integration thật
    không chạy vì process không có hai biến database và agent không đọc `.env*.local`.
    Quick create/full editor timed one-time ClassSession, production renderer và
    drag/resize/undo đã nối; manual NVDA đã PASS.
23. P3-02A staging rollout ngày 2026-07-26: tạo backup branch Neon
    `p3-calendar-pre-migration-20260726` (auto-delete 7 ngày, parent `staging`), rồi migrate
    direct staging `14 false -> 17 false`. Exact runtime ACL probe đạt: schema
    `USAGE=true/CREATE=false`; outbox chỉ column INSERT; notifications chỉ SELECT +
    UPDATE(read_at); notification preferences SELECT + allowlist INSERT/UPDATE; calendar
    display preferences SELECT/INSERT/UPDATE; mọi quyền DELETE/TRUNCATE ngoài allowlist đều
    `false`. `tutorhub_worker` chưa tồn tại nên worker ACL và durable-worker gate vẫn để
    P3-03B; public health/ready Render và Cloudflare đều HTTP 200.

24. Manual NVDA review P3-CAL-01 đã PASS ngày 2026-07-26 với NVDA 2026.1.1 trên môi
    trường cài đặt. Người dùng xác nhận PASS cho heading/landmark, event label/time/
    category, view switch/Agenda, keyboard alternative, focus restore sau 409 và live
    announcement. Đây là bằng chứng đóng NVDA gate; không thay thế route-level
    authorization/range/a11y/bundle, visual hoặc browser acceptance của P3-02A.
25. P3-02A renderer/editor integration ngày 2026-07-26: exact pin
    `@fullcalendar/react@7.0.1` đã được nối vào `/app/calendar` cho Day/Work week/Week/Month;
    Agenda semantic được giữ làm mobile view và keyboard alternative. Drag/resize one-time
    gọi lại command ClassSession với expected version, optimistic revert, undo và thông báo
    live-region khi server trả `409`. Route unit integration, web lint/typecheck/test/build,
    production Calendar guard và rerun spike 9/9 đều đạt. Calendar lazy chunk khoảng
    298,64 kB (82,29 kB gzip).
26. P3-02A chuyển `VERIFY -> DONE` ngày 2026-07-26. Production-route Playwright đạt 8/8
    với authorization/range/pagination, Axe, semantic Agenda, zoom/forced-colors/
    reduced-motion và visual snapshots desktop/tablet/mobile. Render/navigation/long-task
    p95 cho 500/1.000/2.000 item là `177/266/102`, `310/481/180`,
    `570/716/326 ms`, đều trong budget ADR-0019. Commit `0606813` đã manual deploy lên
    Render vì Auto-Deploy đang tắt; bốn public health/ready probe đều HTTP 200. Browser
    staging Admin xác nhận empty-state, Agenda URL state, Preference drawer và Quick Create
    no-class state, không tạo mutation. Biên bản:
    `docs/P3_02A_STAGING_ACCEPTANCE.md`.
27. P3-CAL-02 đạt local `VERIFY` ngày 2026-07-26. ADR-0020 khóa audience/organizer/RSVP,
    stable UID + monotonic sequence, RFC 5545/5546/6047 outbound subset, CTA-only
    `RSVP=FALSE`, one-recipient privacy, deterministic canonical payload, SES ambiguous
    outcome và đường provider event
    `Configuration Set -> EventBridge -> SQS/DLQ -> worker -> PostgreSQL inbox`.
    Package cô lập `internal/spikes/calendarinvitation` có audience diff, bounded
    iCalendar/VTIMEZONE/MIME renderer, trusted CTA origin/tzdata policy, opaque effect
    identity, deterministic sink và SES v2 `Content.Raw` adapter pin sender/configuration
    set với SDK retry tắt. Bảy golden lineage create/update/reschedule/role/override/
    split/cancel, focused coverage `85,4%/81,0%`, fuzz 10 giây hơn 1,28 triệu lượt,
    focused/full Core API test và vet đều đạt. Spike không được import vào runtime,
    không consume outbox và không gửi business email. ADR giữ `Proposed` vì SES sandbox,
    EventBridge/SQS/DLQ/inbox, bounce/complaint/suppression, sending domain/DNS và
    Gmail/Outlook/Apple interoperability chưa có live evidence.
28. P3-02B chuyển `IN PROGRESS -> VERIFY` ngày 2026-07-27: typed recurrence boundary,
    migration `000018/000019`, series/exception repository/service/HTTP, scope preview,
    split/cancel/idempotency/audit/outbox, override capability, recurring UI và
    recurrence metrics đã có. Local package tests, HTTP/classroom tests, `go vet`,
    migration-fragment checks, integration-tag compile, API client 22/22, web
    typecheck/lint/176 tests và format check đạt. Neon migration/grants, tenant canary
    và focused Teacher staging smoke đã đạt ngày 2026-07-28; query-plan/concurrency,
    authorization và cross-tenant acceptance vẫn mở nên chưa mô tả P3-02B là `DONE`.
    Runbook nghiệm thu nằm tại
    `docs/P3_02B_STAGING_ACCEPTANCE.md`.
29. Feature key `class_session_recurrence` đã nhất quán từ migration/catalog/config/
    guardrail tới tenant capability OpenAPI/generated client và UI.
    `FEATURE_CONTROL_ENABLE_CLASS_SESSION_RECURRENCE=false` mặc định fail-closed;
    Go package/config/HTTP tests, API check, web typecheck và 176 web tests đều xanh.
    Domain read-overlay đã có bounded expansion + cancel/override và DST/civil-time
    test; series repository/mutation/HTTP nối vào SQL Calendar projection. Deployment
    guardrail toàn cục vẫn fail-closed; staging chỉ mở bằng tenant override cho canary.
30. P3-02B staging checkpoint ngày 2026-07-28 đã đạt migration `19 false`, exact runtime
    ACL và Teacher UI mutation. Receipt append-only giữ `SELECT/INSERT`; commit `c622244`
    bỏ `FOR UPDATE` thừa khỏi receipt lookup thay vì cấp thêm `UPDATE`, toàn bộ Go test
    đạt và Render health `200`. Drag occurrence 2026-07-30 sang `11:30–12:30` theo
    `this_occurrence` giữ nguyên sau reload; hai tuần sau vẫn `13:00–14:00`. Bốn
    recurrence metrics hiện diện và có số liệu. P3-02B tiếp tục `VERIFY` cho concurrent/
    idempotency, split/exception retention, Student/Admin authorization, cross-tenant và
    bounded query-plan evidence; biên bản tại `docs/P3_02B_STAGING_ACCEPTANCE.md`.
31. P3-02B chuyển `VERIFY -> DONE` ngày 2026-07-28 tại acceptance commit `734d2b6`.
    Neon disposable branch `p3-calendar-pre-migration-20260726`
    (`br-silent-math-aozfo2ci`) đạt literal `19 false` và automated PostgreSQL test
    trong `25,75s`: concurrent identical replay 1/1, competing edit success/conflict
    1/1, Student read-only, Teacher override bị từ chối, Organization Admin override
    có reason, cross-tenant concealment, split giữ exception trước boundary và carry
    đủ exception tương lai. Query class-scoped bị chặn `LIMIT 129` và dùng được index
    `class_session_series_class_start_idx`. Full Core API test và vet đều xanh; không
    tạo/xóa Neon branch mới. P3-02C là bước nghiệp vụ Calendar tiếp theo.
32. P3-02C chuyển `TODO -> IN PROGRESS` ngày 2026-07-28 với lát cắt working schedule
    và privacy-safe availability đầu tiên. Migration `000020` thêm lịch làm việc theo
    tenant/user (nhiều interval/ngày, holiday/OOO/special hours, IANA timezone, CAS),
    attendee/audience persistence foundation và RSVP-capability foundation. Core API có
    `GET/PUT /api/v1/calendar/working-schedule` cùng
    `POST /api/v1/calendar/availability/query`; PostgreSQL repository fail-closed theo
    feature `class_session_scheduling`, tenant/class authorization và participant scope
    server-side (class owner/active roster hoặc external audience đã gắn class); ID không
    đủ scope bị conceal 404 và không project title/description/email/roster. Availability
    bị chặn ở 31 ngày lịch theo scheduling timezone/50 người/2.000 start/250 ms;
    external/no-sync là `unknown`; grid civil-time xử lý DST gap/overlap. Web
    có drawer Working hours với CAS/conflict recovery và accessibility cơ bản. Chưa đánh
    dấu task `DONE`: audience command, RSVP domain/API/UI, Scheduling Assistant gắn vào
    session editor và PostgreSQL staging acceptance vẫn còn lại.
33. P3-02C đạt internal one-time participation checkpoint ngày 2026-07-29. Core
    API/OpenAPI/client có privacy-filtered audience replacement và authenticated self RSVP
    với CAS/idempotency; PostgreSQL giữ current RSVP authority cùng encrypted neutral
    invitation/recipient snapshot, audit và outbox domain fact. Web có attendee/organizer
    summary, self RSVP và Scheduling Assistant manager-only với working interval, canonical
    free/busy status, conflict reason, dual timezone và semantic table; 192 web tests cùng
    lint/typecheck/build đều xanh. Task vẫn `IN PROGRESS` vì còn manager roster editor,
    external guest/capability, typed series/occurrence, organizer/archive lifecycle,
    concurrency/IDOR/E2E và Neon migration `000020/000021` + ACL/staging smoke.
34. P3-02C đạt recurring participation checkpoint ngày 2026-07-29. Manager roster editor
    có active-roster search/cursor, required/optional role, cap 128, 403 conceal/retry và
    409 focus recovery. Typed OpenAPI/client/HTTP hỗ trợ session/series/occurrence; occurrence
    đọc inherited audience revision `0` và copy-on-write thành revision `1` khi thay đổi hoặc
    RSVP đầu tiên, không làm đổi series hay occurrence khác. `go test ./... -count=1`,
    integration-tag compile, 10 web tests, 25 API-client tests, lint/typecheck và Vite build
    đều xanh. P3-02C vẫn `IN PROGRESS` vì external guest/capability, diff/lifecycle policy,
    full concurrency/IDOR/E2E và Neon migration `000020/000021` + ACL/staging acceptance còn lại.
35. P3-02C đạt local implementation gate và chuyển `IN PROGRESS -> VERIFY` ngày
    2026-07-29. OpenAPI/generated client, Core API và web đã nối external audience,
    purpose-bound RSVP capability hash/expiry/revoke/rate limit, public RSVP chỉ nhận
    capability trong POST body và privacy-safe projection. Audience replacement có
    deterministic added/removed/unchanged/role-change cùng RSVP retain/reset; organizer
    transfer giữ source authority; cancel session/series/occurrence đóng response,
    revoke capability và giữ immutable cancellation invitation snapshot. Focused HTTP,
    domain, PostgreSQL integration-tag compile và web/API-client coverage đã có ở local.
    Trạng thái chỉ là `VERIFY`: migration `000020/000021` đã đạt `21 false`, exact runtime
    ACL và role isolation đã đạt; browser staging mới xác nhận audience PUT và Student
    self-RSVP sau reload. Concurrent/IDOR, public capability và lifecycle live vẫn mở.
36. P3-02C đạt staging participation checkpoint ngày 2026-07-30 trên commit `32a770ac`.
    Teacher audience PUT và Student self-RSVP đều trả HTTP 200, reload giữ trạng thái và
    ordinary participant không thấy external roster/email. Public capability fixture sau
    đó được chạy trên disposable Codespace; task tiếp tục ở `VERIFY` cho đến khi full
    concurrency/IDOR, organizer/cancel/archive và accessibility live được nghiệm thu.
37. P3-02C regression local được chạy lại ngày 2026-07-30: focused Go
    calendar/classroom/http API suites, API client `27/27` và Calendar E2E `11/11` đều
    đạt. Evidence này không thay thế live fixture public RSVP, Teacher lifecycle,
    cross-tenant/IDOR và NVDA/observability acceptance; task vẫn `VERIFY`.
38. P3-02C Teacher lifecycle staging checkpoint ngày 2026-07-30: reload giữ working
    schedule gồm hai interval, timezone IANA và exception; Scheduling Assistant trả 10
    gợi ý bounded, privacy-safe. Fixture staging có audience nội bộ/yêu cầu RSVP được
    tạo rồi hủy, reload giữ `Đã hủy` và lịch sử. Public capability, organizer
    transfer/archive/capability revocation, concurrency/IDOR, NVDA và observability
    vẫn mở; P3-02C giữ `VERIFY`.
39. P3-02C public RSVP staging checkpoint ngày 2026-07-30: server-issued fixture trên
    disposable Codespace đạt fragment scrub, minimal resolve, respond/replay,
    collision/stale `409`, origin/malformed guard, secure headers và bounded resolve
    rate. Teacher remove/re-add external fixture qua business flow làm link cũ generic
    unavailable. Focused Go regression cho fixture, Calendar repository/service và API
    đều đạt. Full rate/expiry/hash-at-rest, organizer transfer/archive, PostgreSQL
    concurrency/IDOR vẫn mở; task giữ `VERIFY`.
40. P3-02C manual accessibility/privacy checkpoint ngày 2026-07-30: operator xác nhận
    PASS cho Teacher working-hours/audience, Student privacy/self-RSVP, keyboard flow,
    focus recovery sau `409`, NVDA trên Calendar/public RSVP và cancel/revocation
    fixture. Không lưu credential, capability URL/token hoặc dữ liệu người học. Manual
    gate đã đóng; full rate/expiry/hash-at-rest, organizer transfer/archive,
    PostgreSQL concurrency/IDOR và observability live vẫn mở nên task giữ `VERIFY`.
41. P3-02C chuyển `VERIFY -> DONE` ngày 2026-07-30. Neon staging ở `21 false`, exact
    ACL/runtime-role isolation đạt; `corepack pnpm verify`, focused Go
    Calendar/Classroom/HTTP/API/fixture và Calendar E2E `11/11` đều PASS. Operator xác
    nhận toàn bộ ma trận staging/disposable PostgreSQL và manual accessibility/privacy
    còn lại PASS, gồm concurrency/IDOR, capability digest/expiry/rate/revocation,
    organizer transfer/cancel/archive và log privacy. Exact runtime acceptance commit
    `7859c233` đã Live trên Render và Cloudflare; Render health trả HTTP `200`.
42. Quyết định vận hành được tái xác nhận ngày 2026-07-31: giữ **Render Web Service**
    cho Core API staging/private alpha; không chuyển sang OCI/Cloud Run/Oracle hay
    provider khác trong cập nhật này. Render Free có cold start/spin-down và không phải
    durable worker. Vì vậy các kiểm tra chạy được ở local/CI/disposable staging vẫn là
    bắt buộc; các gate phụ thuộc host không spin-down hoặc provider production được ghi
    `DEFERRED/VERIFY`, không được suy diễn thành PASS hay bỏ qua. P3-03B (worker role/
    grants, crash/reclaim, duplicate canary) và các gate live SES/domain/event
    topology tiếp tục mở; hai feature gate notification và Class Files activation vẫn
    `false`, không có asynchronous email/notification/reminder hoặc worker-driven file
    processing/sharing tới end user.

## Rủi ro đã biết

- P3-01 đã `DONE` cho phạm vi one-time ClassSession trên staging/private alpha. Kết quả
  này không mở rộng phạm vi sang recurrence, reminder, calendar tổng hợp, email/ICS,
  durable worker hoặc Phase 4 media lifecycle.
- ADR-0019 đã chấp nhận decision dùng FullCalendar v7 và recurrence bounded; manual NVDA
  đã PASS. P3-02A shell/read projection và P3-02B recurrence/class conflict đã `DONE`.
  P3-02C cũng đã `DONE` cho working-hours/free-busy, internal/external audience, typed
  series/occurrence copy-on-write, organizer transfer, cancellation lifecycle và
  internal/public RSVP. Email/ICS delivery vẫn thuộc P3-05A/P3-CAL-02; Availability
  Poll/Study Meeting vẫn thuộc P3-02D/P3-05B.
- AWS SES đã được chọn làm provider target; local SES v2 Raw adapter, deterministic
  renderer/sink và error semantics đã đạt trong spike cô lập, nhưng provider account/
  region/sandbox/quota chưa được live-verify và sending domain chưa có. Pre-domain chỉ
  cho phép owner-controlled verified identities trong SES sandbox; không được coi là
  production readiness. SPF/DKIM/DMARC, provider-event topology ADR-0020,
  bounce/complaint/suppression và cross-client ICS chưa được nghiệm thu. Không gửi
  business email tới end user trước khi các live gate của ADR-0020, P3-03B và P3-05A đạt.
- Warm Academic calendar shell, semantic Agenda, FullCalendar renderer và visual
  desktop/tablet/mobile đã được nghiệm thu trong P3-02A; recurrence/conflict và
  participant/RSVP đã được nghiệm thu trong P3-02B/P3-02C. Email/ICS và Availability
  Poll vẫn thuộc P3-CAL-02, P3-02D và P3-05A/B; Calendar chưa phải toàn bộ sản phẩm cuối
  cùng cho đến khi các phạm vi đó đạt gate.
- P3-02D-A đã `DONE` sau migration `000023`, exact ACL, PostgreSQL concurrency/cascade,
  schema/API/UI/capability exchange, automated authorization/privacy, authenticated staging
  browser/API matrix và manual NVDA organizer/public production-route acceptance đều PASS.
- External poll link có rủi ro token/PII leak và abuse. Local implementation dùng token
  entropy cao, hash-at-rest, fragment exchange, expiry/revoke/rate limit, log redaction
  và privacy-safe aggregate theo ADR-0021; exact staging đã xác nhận các boundary này.
  Minimum cohort/coarse bucket chỉ giảm rủi ro differencing/Sybil; anonymous link không
  thể hứa one-human-one-response.
- Quyền tạo instant study room đã được chốt làm authorization target, nhưng LiveKit
  token, lobby, moderation và media lifecycle vẫn thuộc Phase 4.
- P3-03A/P3-04 mới ở `VERIFY`: lease/fencing/dead-letter, worker binary, notification
  projection/API/UI, ACL probe và test đã
  có trong repository nhưng chưa chạy như durable service trên staging. Render Free web
  service không được xem là durable worker. Quyết định hiện tại là giữ Render Web Service
  cho Core API staging/private alpha; Render Background Worker trả phí chỉ là candidate
  tương lai, chưa được provision/phê duyệt và không có provider migration trong task này.
  Không bật notification delivery, email/ICS, reminder hoặc worker-driven file processing/
  sharing tới end user trước khi worker role/grants, non-spin-down host và crash/reclaim
  acceptance đạt. Migration `000015/000016` và exact API runtime grants đã áp dụng/probe
  xanh. Hai P3-04 gate phải giữ false; grant worker notification và worker gate
  phải chuyển cùng nhau khi process đã dừng, nếu không startup probe sẽ fail closed.
  P3-CAL-02 chỉ
  được chạy renderer/provider sandbox cô lập trong lúc gate này còn mở.

### Phân loại gate vận hành hiện tại (2026-07-31)

| Nhóm               | Quy tắc                                                                                                                                                                                                    |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Bắt buộc chạy ngay | Unit/integration/CI, static ACL/config checks, local/disposable PostgreSQL, API/browser/accessibility và sandbox/sink tests trong phạm vi hiện có.                                                         |
| `DEFERRED/VERIFY`  | Durable worker host không spin-down, live worker role/grants, crash/reclaim/duplicate canary, SES production access/quota/event ingress, domain/DNS và Gmail/Outlook/Apple interoperability.               |
| Không được bật     | `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY`, `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS`, Class Files sharing/processing và mọi asynchronous email/ICS/reminder side effect trước khi gate phụ thuộc đạt. |

- Render Free spin down khi không hoạt động và có thể cold start trên 50 giây;
  chỉ chấp nhận cho staging/private alpha.
- Direct-main chưa có pre-merge protection; `pnpm verify` và CI hậu kiểm là kiểm soát
  bù tạm thời theo ADR-0012.
- Chưa chọn managed Redis và observability provider cho quy mô lớn hơn.
- P2-09 thay limiter theo process bằng fixed-window PostgreSQL và chỉ tin client prefix
  do Cloudflare Pages ký. Staging đã đồng bộ `EDGE_CONTEXT_SECRET` ở edge/Core API và
  public limiter smoke đã đạt; assertion không hợp lệ vẫn fallback về direct peer
  prefix. Redis tiếp tục hoãn cho tới khi có số liệu tải.
- Migration `000012` không hardcode runtime role theo môi trường. Staging đã cấp grants
  tối thiểu và chạy bounded cleanup cho `tenant_quota_windows` cùng
  `rate_limit_windows`; maintenance định kỳ vẫn phải theo `docs/DATABASE.md`.
- Class projection chưa lộ `archived_from_status`, nên web chỉ dùng feature gate khi
  hiển thị restore; quota active-class vẫn được server enforce transactionally. Khi
  quota đã hết, restore lớp từng active có thể nhận 409 sau submit; bổ sung projection
  class-specific nếu UX này trở thành vấn đề thực tế.
- Verify #59 đã xác nhận PostgreSQL runtime, migration/integration và Browser E2E
  trên CI. Acceptance UI staging P2-08 ngày 2026-07-20 đã xanh sau khi web/Core API
  được đồng bộ; các lần nghiệm thu sau vẫn phải đối chiếu commit/image, migration và
  configuration trước khi kết luận lỗi contract.
- Host hiện tại thiếu Docker/PostgreSQL nên không thể lặp lại full browser scenario
  ngoài CI; nếu CI không sẵn có thì đây vẫn là hạn chế chẩn đoán cục bộ.
- P2-12 đã đóng sau khi CI/Cloudflare/Render/Neon/importer/public probe, UI staging
  S01-S07 và S09 application rollback/redeploy đều đạt. Native Rollback không được dùng
  vì Render cảnh báo không tải được cấu hình live; application rollback giữ config hiện
  tại là bằng chứng phục hồi đã chấp nhận. Closure-record CI sau push là hậu kiểm bắt buộc.
- Class/roster/audit cursor vẫn là payload client đọc được; scope hash ngăn replay sai
  tenant/filter nhưng không phải chữ ký bí mật. SQL luôn giữ tenant/class predicate nên
  finding hiện được xếp Low; quyết định HMAC toàn bộ cursor được hoãn sang backlog/ADR
  riêng sau P2-10 nếu threat model yêu cầu cursor chống giả mạo.
- Production retention/export, privacy erasure, partitioning và dedicated maintenance
  role cho audit được hoãn tới Phase 8. Audit của tenant archived được giữ nhưng chưa có
  recovery/export UI ngoài active-tenant API.
- Roster role update hiện dùng last-write-wins, chưa có enrollment version/CAS. Client
  refetch sau mutation; nếu concurrent editing trở thành rủi ro thực tế thì cần ADR và
  migration riêng.
- Ownership transfer tái dùng `auth_time` của session theo semantics P2-01; chưa ép
  OIDC `max_age`/`prompt`, nên recent-auth 10 phút chưa phải step-up tuyệt đối.
- Archive hoặc roster role mutation ngăn/đổi credential LiveKit cấp mới nhưng không
  thu hồi JWT đã phát hoặc kick/cập nhật participant đang ở trong room.
- Dữ liệu V1 chưa được migrate.
- LiveKit chunk phía web còn lớn và cần performance budget ở phase sau.

## Quy tắc Git hiện tại

- Làm việc và commit trực tiếp trên `main` để ưu tiên tốc độ phát triển.
- Không bắt buộc Issue, branch hoặc Pull Request cho mỗi task.
- Trước khi push: xem `git diff`, chạy kiểm tra phù hợp và quét secret.
- Không force-push `main`; branch tạm chỉ dùng cho thử nghiệm rủi ro cao.

## Tài liệu liên quan

- `docs/MASTER_PLAN.md`
- `docs/PHASE_1_BACKLOG.md`
- `docs/PHASE_1_COMPLETION.md`
- `docs/PHASE_2_BACKLOG.md`
- `docs/P2_12_STAGING_ACCEPTANCE.md`
- `docs/PHASE_2_COMPLETION.md`
- `docs/PHASE_3_BACKLOG.md`
- `docs/P3_03_OUTBOX_WORKER_RUNBOOK.md`
- `docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md`
- `docs/calendar/P3_CAL_01_SPIKE_EVIDENCE.md`
- `docs/DEPLOYMENT_BASELINE.md`
- `docs/DATABASE.md`
- `docs/AUTHENTICATION.md`
- `docs/E2E_TESTING.md`
- `docs/LIVEKIT_SPIKE_RUNBOOK.md`
- `docs/CI_SECURITY.md`
- `docs/adr/0011-render-core-api-staging.md`
- `docs/adr/0012-single-maintainer-direct-main-governance.md`
- `docs/adr/0013-shared-organization-class-authorization-policy.md`
- `docs/adr/0014-append-only-tenant-audit-log.md`
- `docs/adr/0015-server-evaluated-feature-controls-and-quotas.md`
- `docs/adr/0016-idempotent-v1-fixture-import.md`
- `docs/adr/0017-class-session-scheduling-and-civil-time.md`
- `docs/adr/0018-postgresql-leased-outbox-worker.md`
- `docs/adr/0019-calendar-renderer-recurrence-and-conflict.md`
- `docs/adr/0020-calendar-invitation-rsvp-icalendar-and-ses.md`
- `docs/adr/0021-native-availability-polls-and-member-owned-study-meetings.md`
- `docs/adr/0022-tenant-scoped-in-app-notification-projection.md`
- `docs/adr/0023-calendar-working-schedule-free-busy-and-rsvp-authority.md`
- `docs/adr/0024-forward-maintenance-purge-security.md`
