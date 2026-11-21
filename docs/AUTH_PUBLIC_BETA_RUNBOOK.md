# Vận hành xác thực Public Beta

## Phát hành API và job bảo trì

| Tiến trình            | Biến kết nối                    | Quyền cần có                                                           |
| --------------------- | ------------------------------- | ---------------------------------------------------------------------- |
| Core API              | `DATABASE_POOL_URL`             | Runtime, kết nối pooled                                                |
| Outbox worker         | `DATABASE_WORKER_URL`           | Worker, kết nối pooled                                                 |
| Release job           | `DATABASE_MIGRATION_URL`        | Migration owner, kết nối direct                                        |
| Job Availability Poll | `DATABASE_POLL_MAINTENANCE_URL` | Login bảo trì poll, kết nối direct                                     |
| Job auth              | `DATABASE_AUTH_MAINTENANCE_URL` | Login riêng, kết nối direct, `USAGE` schema và `EXECUTE` hai hàm purge |

1. Release operator chuẩn bị hai login riêng cho API và job auth. Giữ bộ quyền nghiệp vụ hiện tại của API. Login bảo trì không có role membership, quyền quản trị hay sở hữu object. Cấp từng URL qua secret store của đúng tiến trình. Dùng TLS trên Neon.
2. Trên disposable, kiểm tra migration, ACL và các luồng đăng nhập. Khi được duyệt cho staging, chuyển API cũ sang chế độ bảo trì hoặc drain instance trước khi thu hồi quyền xóa flow. Chạy các lệnh dưới đây từ gốc repo bằng release job rồi khởi động API mới. Thay `api_runtime` và `auth_maintenance` bằng tên role thực tế.

   ```sh
   go run ./services/core-api/cmd/migrate up
   go run ./services/core-api/cmd/migrate version
   go run ./services/core-api/cmd/auth-maintenance/provision --runtime-role api_runtime --maintenance-role auth_maintenance
   ```

   Trong OCI release job dùng cùng image với Core API, cấp `DATABASE_MIGRATION_URL` cho job và chạy lần lượt:

   ```sh
   /app/tutorhub-migrate up
   /app/tutorhub-migrate version
   /app/tutorhub-auth-provision --runtime-role api_runtime --maintenance-role auth_maintenance
   ```

   Ledger mong đợi là `52 false`. Lệnh provision chạy cả hai hàm Go trong cùng giao dịch. Nếu quyền hiệu lực còn rộng, kiểm tra membership, grant cho PUBLIC và default privileges của owner rồi chạy lại. Với bảng lịch sử, giữ các grant theo cột của runtime trước khi cấp quyền auth.

3. Lập lịch job riêng mỗi giờ, chẳng hạn phút 15 UTC. Chỉ cấp `DATABASE_AUTH_MAINTENANCE_URL` cho job này. Trong OCI, đặt lệnh chạy là `/app/tutorhub-auth-maintenance --batch-size 500 --max-batches 10`. Từ mã nguồn, chạy:

   ```sh
   go run ./services/core-api/cmd/auth-maintenance --batch-size 500 --max-batches 10
   ```

   Theo dõi exit code và sáu số đếm mỗi vòng. Số đếm của transaction là số hàng cha. Vòng rỗng kết thúc job. Nếu liên tục chạm trần số vòng, xem độ trễ truy vấn và khóa trên DB qua tài khoản vận hành rồi điều chỉnh lịch hoặc số lô. Khi job lỗi, sửa cấu hình/quyền và chạy lại. Các vòng đã commit vẫn có hiệu lực. [Cửa sổ retention và quyền](DATABASE.md#retention-xác-thực-từ-migration-51)

4. Khi cần lùi ứng dụng, tắt luồng email trả phí và dừng scheduler trước khi chuyển về API tương thích ACL 51. Bản trước AUTH-13A còn xóa flow khi bắt đầu OAuth. Trên disposable sạch, kiểm tra `52 → 51 → 52` bằng `/app/tutorhub-migrate down -steps 1`, `version`, `up` rồi `version`. Down 52 lấy khóa độc quyền và báo SQLSTATE `55000` nếu còn bất kỳ biên nhận Resend nào. Giữ schema 52 khi lùi ứng dụng trong cửa sổ retention. Down 51 cũng chặn nếu còn biên nhận tách cha. Nếu runner báo dirty, dừng thao tác và đối chiếu schema, giao dịch thực tế với owner trước khi sửa ledger.

## Thứ tự mở phương thức đăng nhập

| Đợt                        | Thao tác và điều kiện chuyển tiếp                                                                                                                                                                                                                                                                                                                                                 |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Nền tảng                   | Giữ đường OIDC hiện hành. Xác nhận HTTPS, origin/cookie, session key, CSRF và hai tài khoản quản trị có đường khôi phục đã thử. Bật `AUTH_DEVICE_MANAGEMENT_ENABLED`, thử xem và thu hồi phiên.                                                                                                                                                                                   |
| Mật khẩu và email          | Bật `AUTH_METHOD_PASSWORD_ENABLED` cho tài khoản hiện có. Hoàn tất các bước nghiệm thu Resend bên dưới trước khi mở đăng ký, đăng nhập bằng mã email và khôi phục mật khẩu. |
| Liên kết tổ chức và social | Mở từng `AUTH_PROVIDER_GOOGLE_ENABLED`, `AUTH_PROVIDER_MICROSOFT_ENABLED`, `AUTH_PROVIDER_FACEBOOK_ENABLED` sau khi kiểm tra issuer, redirect, secret và callback tương ứng. Kiểm tra tài khoản trùng email vẫn yêu cầu xác nhận liên kết khi SSO tùy chọn.                                                                                                                       |
| MFA                        | Kiểm tra WebAuthn RP ID/origin, bật `AUTH_PASSKEYS_ENABLED` và `AUTH_MFA_TOTP_ENABLED`. Thử xác minh, recovery code, step-up và phiên cũ sau rotation. Chỉ bật `AUTH_REQUIRE_ADMIN_MFA` sau khi quản trị viên đã có yếu tố và đường phục hồi.                                                                                                                                     |
| School SSO                 | Bật `AUTH_SSO_DOMAINS_ENABLED` sau provider tương ứng. Xác minh DNS TXT, org ID và domain với SSO tùy chọn. Đăng nhập bằng IdP trường, xác nhận đúng tenant và MFA, sau đó mới đặt `enforce_sso=true` cho từng trường.                                                                                                                                                            |
| Điện thoại                 | Chốt quốc gia, quota và ngân sách. Khi được phê duyệt, đặt `AUTH_DELIVERY_BUDGET_APPROVED=true`, cấu hình primary/fallback đã nghiệm thu rồi bật `AUTH_METHOD_PHONE_ENABLED`. Mặc định sender là `disabled`. Theo dõi gửi, xác minh, chi phí từng nước trước khi mở rộng.                                                                                                         |

### Nghiệm thu Email OTP qua Resend

1. Xác minh SPF/DKIM cho domain gửi trong Resend. Cấp `AUTH_RESEND_API_KEY` có quyền gửi từ domain đó qua secret store của Core API. Đặt `AUTH_RESEND_FROM_EMAIL` thành địa chỉ trần viết thường trên domain đã xác minh. `AUTH_RESEND_FROM_NAME` mặc định là `TutorHub`. Để trống `AUTH_RESEND_API_URL` để dùng `https://api.resend.com/emails`. Tắt theo dõi lượt mở và nhấp trên domain gửi thư xác thực.
2. Sau khi duyệt ngân sách và `AUTH_EMAIL_DAILY_QUOTA`, đặt `AUTH_EMAIL_SENDER=resend`, `AUTH_DELIVERY_BUDGET_APPROVED=true`, giữ `AUTH_PHONE_SENDER=disabled`, `AUTH_PHONE_FALLBACK_SENDER=disabled` và phương thức điện thoại tắt. Triển khai cấu hình trên môi trường canary được duyệt.
3. Bật `AUTH_SIGNUP_ENABLED` và `AUTH_METHOD_EMAIL_CODE_ENABLED` cho canary. Dùng hộp thư thử để đăng ký, đăng nhập bằng mã và khôi phục mật khẩu. Đối chiếu mỗi lần gửi OTP với một lượt quota, biên nhận `email / resend / accepted` và kết quả nhận thư trong inbox. Xác minh mã đăng nhập tạo được phiên. Chỉ mở cho người dùng sau khi canary đạt. Theo dõi lỗi giao nhận và chi phí qua số đếm tổng hợp.

## Khi có sự cố

- **OTP pumping hoặc HTTP 503.** Đối chiếu mã `auth_phone_delivery_unavailable` với quota ngày UTC, quota theo nước, số challenge gửi/xác minh trong giờ vừa qua và tình trạng nhà cung cấp. Mặc định cầu dao mở nếu đã gửi ít nhất 50 lần trong một nước và tỷ lệ xác minh dưới 30%. Giữ biên nhận 30 ngày. Đối chiếu theo thời gian, kênh, provider, trạng thái và mã lỗi tổng hợp. Cho người dùng chọn phương thức đã liên kết được chính sách trường cho phép.
- **Ngừng phát sinh chi phí.** Với email, tắt `AUTH_SIGNUP_ENABLED` và `AUTH_METHOD_EMAIL_CODE_ENABLED`, đặt `AUTH_EMAIL_SENDER=disabled` và kiểm tra đường đăng nhập dự phòng. Với điện thoại, tắt phương thức và đặt cả `AUTH_PHONE_SENDER` và `AUTH_PHONE_FALLBACK_SENDER` về `disabled`. Hạ `AUTH_DELIVERY_BUDGET_APPROVED=false` khi đã tắt mọi sender trả phí. Reload/redeploy cấu hình, theo dõi quota và thử lại canary đã được phê duyệt trước khi mở từng kênh.
- **Quản trị viên gặp `409 auth_sso_lockout`.** Đăng nhập mới bằng chính Google Workspace/Entra của kết nối đích, dùng đúng domain và `hd`/`tid` đã cho phép. Hoàn tất MFA, thao tác lại bằng phiên còn bằng chứng SSO. Nếu IdP gián đoạn, dùng quản trị viên dự phòng đã được ủy quyền của cùng tenant. Nếu mất toàn bộ đường quản trị, chuyển owner và trường xác minh quyền sở hữu, khôi phục quyền tại IdP rồi đăng nhập lại. Giữ sự kiện thay đổi và request ID trong hồ sơ sự cố.
- **Hỗ trợ tài khoản chuyển đổi.** `403 auth_sso_required` sau xác minh passkey/điện thoại dẫn người dùng về School SSO. Khi khớp đúng hộp thư đã xác minh và bằng chứng tổ chức, lần đầu liên kết trường thu hồi phiên cũ. Nếu có MFA, hỗ trợ người dùng hoàn tất bằng yếu tố đã đăng ký. Tài khoản có sàn AAL2 mà thiếu yếu tố khả dụng cần phục hồi quyền truy cập trước khi cấp phiên.

## Q12 — điều kiện gỡ ZITADEL ở staging

- [ ] Gatekeeper nghiệm thu AUTH-05 và toàn bộ AUTH-13, bao gồm kiểm thử trình duyệt/a11y chéo phương thức.
- [ ] Đối soát số tài khoản active chỉ còn đường ZITADEL, kể cả quản trị viên, theo đúng các cờ và chính sách trường đang áp dụng. Hỗ trợ chuyển đổi và thử đăng nhập bằng phương thức thay thế đến khi số còn lại bằng 0.
- [ ] Lưu cấu hình rollback trong secret store. Tắt `AUTH_PROVIDER_OIDC_DEFAULT_ENABLED` ở staging, kiểm tra đăng nhập mới, MFA, khôi phục, callback và tỷ lệ lỗi. Khi có người dùng bị khóa, khôi phục cấu hình và đối soát lại.
- [ ] Sau thời gian quan sát do owner phê duyệt, thu hồi credential và redirect URI staging cũ. CI/local tiếp tục dùng `cmd/e2e-oidc` và bật OIDC mặc định trong profile kiểm thử.
