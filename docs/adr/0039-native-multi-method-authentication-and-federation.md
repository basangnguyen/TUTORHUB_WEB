# ADR-0039: Xác thực đa phương thức nội tại và liên kết OAuth2/OIDC trực tiếp trên Go BFF

- Status: Accepted
- Date: 2026-11-16
- Supersedes (một phần): điều khoản "từ chối tự xây password/MFA trong MVP" tại [ADR-0005](0005-oidc-bff-session.md) và sự phụ thuộc bắt buộc vào ZITADEL Cloud làm IdP trung tâm duy nhất tại [ADR-0008](0008-zitadel-oidc-provider.md)
- Related: [ADR-0012](0012-single-maintainer-direct-main-governance.md), [ADR-0018](0018-postgresql-leased-outbox-worker.md), [ADR-0020](0020-calendar-invitation-rsvp-icalendar-and-ses.md), [ADR-0038](0038-antigravity-orchestrated-multi-agent-workflow.md), [AUTH_MULTI_METHOD_RESEARCH_AND_PLAN.md](../AUTH_MULTI_METHOD_RESEARCH_AND_PLAN.md)

## Context

Sau khi hoàn thiện giao diện `/sign-in` (`UI-AUTH-01`..`UI-AUTH-03`), TutorHub V2 cần luồng đăng ký,
đăng nhập và khôi phục tài khoản thực tế theo chuẩn các nền tảng lớn (Microsoft Teams, Google Meet,
Zoom, Slack): **Email + mật khẩu**, **Email OTP (magic code)**, **Google**, **Facebook**, **Số điện
thoại (`+84` Zalo ZNS / SMS Brandname)**, **Microsoft / School SSO**, **Passkeys (WebAuthn)**, **TOTP

- mã khôi phục**, **step-up 10 phút** và **quản lý thiết bị/phiên**.

Kiến trúc MVP ở Phase 1 ủy thác toàn bộ màn hình đăng nhập cho ZITADEL Cloud qua OIDC redirect. Mô hình
này bộc lộ ba rào cản lớn khi chuyển sang sản phẩm thực tế:

1. ZITADEL Cloud Free giới hạn cứng 100 người dùng hoạt động mỗi ngày (DAU), trong khi các gói SaaS
   khác tính phí theo MAU hoặc tính năng SSO/MFA.
2. Redirect bắt buộc sang hosted login bên ngoài làm mất quyền kiểm soát trực tiếp trải nghiệm
   `/sign-in`, autofill Passkey, luồng OTP nội tuyến và tối ưu hóa kênh SĐT Việt Nam (Zalo ZNS trước
   SMS).
3. Tự vận hành thêm một máy chủ định danh riêng (Keycloak hoặc Ory Kratos) làm tăng gấp đôi bề mặt vận
   hành, bộ nhớ và chi phí container so với giới hạn hạ tầng Render hiện tại.

## Decision

### 1. Kiến trúc lõi: Native Go BFF Auth + Direct OAuth2/OIDC Federation

1. Triển khai trực tiếp các phương thức xác thực nội tại và bộ điều phối OAuth2/OIDC trực tiếp tới từng
   nhà cung cấp (`google`, `facebook`, `microsoft`, `apple` tùy chọn, `oidc`) ngay bên trong module
   `services/core-api/internal/modules/identity` của Go modular monolith (không tách microservice).
2. **Giữ nguyên** mô hình BFF của [ADR-0005](0005-oidc-bff-session.md): trình duyệt chỉ nhận opaque
   session cookie `__Host-tutorhub_session` (`HttpOnly`, `Secure` khi HTTPS, `SameSite=Lax`) và
   `__Host-tutorhub_csrf`; tuyệt đối không lưu access/refresh token ở `localStorage`/`sessionStorage`
   hoặc trả token nhà cung cấp về trình duyệt.
3. **Giữ tương thích ngược hoàn toàn** với `GET /api/v1/me`, `GET /api/v1/auth/login`,
   `GET /api/v1/auth/callback` và fake issuer `e2e-oidc` (`AUTH_PROVIDER_OIDC_DEFAULT_ENABLED=true`).
   ZITADEL trở thành một generic `oidc` provider tùy chọn thay vì điểm phụ thuộc bắt buộc.
4. Mọi phương thức mới đều nằm sau cờ cấu hình cấp deployment (`AUTH_*`) mặc định `false` cho đến khi
   vượt qua cổng kiểm chứng bảo mật tương ứng.

### 2. Sổ đăng ký phương thức đăng nhập & Cấm tự động gộp tài khoản (Zero Auto-Merge)

1. Mở rộng `tutorhub.identities` làm sổ đăng ký thống nhất cho mọi phương thức đăng nhập qua cột
   `kind IN ('oidc', 'oauth2', 'email', 'phone', 'passkey')` (dùng namespace nội bộ
   `urn:tutorhub:email`, `urn:tutorhub:phone`, `urn:tutorhub:webauthn`). Identity `kind='email'` được
   tạo sau khi xác minh OTP email với `email_verified=true`, giúp luồng chấp nhận lời mời thành viên tổ
   chức/lớp học hiện có hoạt động liền mạch. `GET /api/v1/me/identities` tiếp tục chỉ trả các identity
   federated (`oidc`, `oauth2`) để không phá vỡ schema hiện tại.
2. **Cấm tuyệt đối tự động gộp tài khoản (auto-merge) theo email hoặc số điện thoại.** Khi đăng nhập
   social/federated gặp email trùng với tài khoản đã tồn tại mà chưa liên kết `(issuer, subject)`, hệ
   thống tạo `link_intent` trong `tutorhub.auth_transactions` (gắn với cookie `__Host-tutorhub_auth_tx`,
   `HttpOnly`, `SameSite=Strict`, TTL ≤ 15 phút) và yêu cầu người dùng xác thực vào tài khoản hiện có ở
   **đúng mức bảo đảm hiện hành (`AAL1` hoặc `AAL2`)** rồi xác nhận liên kết rõ ràng. Quy tắc này chặn
   toàn bộ các kịch bản Pre-Account Takeover.
3. Email trả về từ **Facebook** không bao giờ được tự coi là đã xác minh: đăng ký mới qua Facebook bắt
   buộc xác minh mã OTP email một lần (hoặc nhập + xác minh email nếu tài khoản Facebook đăng ký bằng
   SĐT/ẩn email). Bổ sung đầy đủ hai webhook bắt buộc của Meta (`data-deletion` và `deauthorize`) với
   kiểm tra chữ ký HMAC constant-time.

### 3. Chính sách mật khẩu, phiên đăng nhập và mức bảo đảm (chủ dự án đã chốt)

1. **Băm mật khẩu**: dùng `Argon2id` chuẩn OWASP (`m=19456 KiB`, `t=2`, `p=1`, salt 16 byte, key 32
   byte, lưu định dạng PHC), bảo vệ bởi semaphore toàn cục `PASSWORD_HASH_MAX_CONCURRENCY` để không gây
   OOM trên container Render 512 MB (quá thời gian chờ trả `503` + `Retry-After`).
2. **Chính sách mật khẩu**: độ dài tối thiểu mặc định **`PASSWORD_MIN_LENGTH=8`** (khớp giao diện
   `/sign-in` hiện hành, chuẩn hóa NFKC, tối đa 128 rune / 512 byte, khuyến khích 12+ ký tự hoặc
   Passkey), kết hợp danh sách cấm offline và kiểm tra lộ mật khẩu qua **HIBP Pwned Passwords range API**
   (k-anonymity 5 ký tự SHA-1 + `Add-Padding: true`, timeout ngắn, fail-open có metric).
3. **Thời hạn phiên & "Duy trì đăng nhập"**: giữ mặc định idle 8 giờ / absolute 24 giờ. Khi người dùng
   tích _"Duy trì đăng nhập"_ (`persistent=true`), thời hạn tuyệt đối của phiên được phép kéo dài lên
   **30 ngày** đối với tài khoản học sinh/giáo viên; tài khoản quản trị tổ chức (`org_admin`) vẫn bị
   giới hạn cứng tối đa **24 giờ**.
4. **Mức bảo đảm & Step-up**: `tutorhub.sessions` lưu `auth_methods`, `assurance_level` (`1` hoặc `2`)
   và `reauthenticated_at`. `Principal.AuthenticatedAt = GREATEST(auth_time, reauthenticated_at)` để
   các thao tác nhạy cảm (đổi mật khẩu, thêm/gỡ phương thức đăng nhập, quản trị bảo mật) thực thi
   step-up thật trong cửa sổ 10 phút và xoay session/CSRF token sau mỗi lần nâng quyền.

### 4. Chính sách Số điện thoại & Chống lạm dụng cước

1. **Mọi tài khoản giai đoạn đầu vẫn giữ Email chuẩn (`users.email NOT NULL`)**, bảo toàn tương thích
   OpenAPI `User.email`; Số điện thoại (`E.164`, mặc định `VN` `+84`) đóng vai trò phương thức đăng nhập
   nhanh và xác thực bổ sung. (Phương án tài khoản thuần SĐT không email được tách riêng thành tùy chọn
   tương lai `AUTH-11b` nếu có nhu cầu thực tế.)
2. Bộ định tuyến `+84` ưu tiên gửi qua **Zalo ZNS** trước, tự động chuyển dự phòng sang **SMS Brandname**
   khi ZNS không khả dụng; bảo vệ bằng rate limit đa tầng trên PostgreSQL (`tutorhub.rate_limit_windows`),
   quota cứng theo ngày (`AUTH_PHONE_OTP_DAILY_QUOTA`) và cầu dao tự ngắt.
3. **Xử lý SIM tái chế**: tách bạch quyền _"nhận lại số điện thoại"_ (`PHONE_RECLAIM_AFTER=90d` giải
   phóng số khỏi tài khoản cũ không hoạt động để chủ thuê bao mới dùng) khỏi quyền _"khôi phục tài khoản
   cũ"_ (SĐT đơn lẻ không bao giờ mở khóa tài khoản đã bật 2FA/Passkey hoặc cho phép chiếm tài khoản cũ).

### 5. Ngoại lệ có kiểm soát với ADR-0018 cho việc gửi mã OTP ngắn hạn

1. Mã xác thực một lần (OTP 6 chữ số từ `crypto/rand`, TTL 5–10 phút, tối đa 5 lần thử, cooldown gửi lại
   60 giây) chỉ được lưu dưới dạng HMAC phân tách miền (`Digest("auth-otp-v1", ...)`) trong
   `tutorhub.auth_challenges`, gắn chặt với transaction cookie `__Host-tutorhub_auth_tx`.
2. **Ngoại lệ đối với [ADR-0018](0018-postgresql-leased-outbox-worker.md)**: mã OTP plaintext chỉ tồn
   tại trong bộ nhớ tiến trình đến thời điểm gọi adapter gửi đi ngay **sau khi transaction DB đã commit**
   (ephemeral post-commit dispatcher) và **không** ghi vào `tutorhub.outbox_events` (nhằm tuân thủ quy
   tắc không lưu mã bí mật/PII trong outbox payload và tránh việc retry backoff làm giao mã đã hết hạn).
3. Mọi endpoint bắt đầu luồng OTP trả `202 Accepted` đồng nhất (kể cả khi định danh không tồn tại hoặc
   chạm giới hạn gửi theo đích) để chống dò tài khoản theo nội dung và thời gian phản hồi.
4. Ở `local`/`test`, mã OTP đi vào hộp thư nội bộ `devinbox` (chỉ mở trên loopback khi `APP_ENV` là
   `local`/`test`) hoặc Mailpit; ở `staging`/`production`, các adapter phát sinh chi phí (`ses`, `zns`,
   `sms_brandname`, `twilio`) bị khóa cứng trừ khi bật tường minh `AUTH_DELIVERY_BUDGET_APPROVED=true`.
   Các thông báo bảo mật không chứa mã bí mật (_mật khẩu đã đổi_, _liên kết phương thức mới_) vẫn đi qua
   outbox bền vững của ADR-0018 khi worker hoạt động.

### 6. Thứ tự triển khai chuẩn hóa

Triển khai tuần tự theo lộ trình chủ dự án đã phê duyệt:
`AUTH-01` (Nền tảng transaction, challenge OTP, delivery guard, migration `000043`) → `AUTH-02..03`
(Đăng ký Email + OTP + Mật khẩu Argon2id, đăng nhập mật khẩu/mã email, khôi phục mật khẩu) → `AUTH-04`
(Quản lý thiết bị/phiên & step-up 10 phút) → `AUTH-05..06` (Google & Facebook OIDC/PKCE + link intent +
Meta callbacks) → `AUTH-07..11` (Microsoft, Passkeys/WebAuthn, TOTP 2FA, Số điện thoại Zalo ZNS/SMS) →
`AUTH-12..13` (School Domain SSO & Public Beta Hardening).

## Consequences

- Đội ngũ kiểm soát trọn vẹn UX `/sign-in`, không bị chặn bởi giới hạn 100 DAU của ZITADEL Free và giữ
  nguyên chi phí hạ tầng `0 USD` ở môi trường dev/alpha khi chưa bật sender trả phí.
- Bề mặt bảo mật của `services/core-api/internal/modules/identity` mở rộng đáng kể; bắt buộc kiểm thử
  nghiêm ngặt theo OWASP ASVS 5.0 L2 (chống enumeration, login CSRF, replay, race condition verify OTP,
  provider mix-up, rò rỉ log) và exact runtime PostgreSQL ACL (không cấp quyền `DELETE` cho runtime role)
  tại từng gói trước khi bật cờ tính năng.
