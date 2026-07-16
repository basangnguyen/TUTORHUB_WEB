# Authentication runbook

Tài liệu này mô tả contract xác thực của TutorHub V2 từ P1-06. Mọi thay đổi liên
quan OIDC, cookie, session, CSRF, identity mapping hoặc `/api/v1/me` phải cập nhật
OpenAPI, kiểm thử và tài liệu này trong cùng task.

## Quyết định hiện tại

- IdP local/staging: ZITADEL Cloud, theo ADR-0008.
- Protocol: OpenID Connect Authorization Code với PKCE `S256`.
- Mô hình browser: BFF. Trình duyệt chỉ giữ opaque TutorHub session cookie; access
  token, refresh token và ID token của IdP không được chuyển xuống web.
- Session state và revoke state: Neon PostgreSQL.
- Automated test: OIDC issuer giả có discovery, JWKS RSA, ID token ký số và
  UserInfo; test không phụ thuộc tài khoản ZITADEL thật.
- Hai ứng dụng OIDC bắt buộc tách biệt: `tutorhub-local` và `tutorhub-staging`.

ZITADEL có thể được thay bằng IdP chuẩn OIDC khác qua cấu hình và adapter mà không
đổi contract của browser. Không gọi API quản trị độc quyền của ZITADEL trong request
path đăng nhập.

## Luồng đăng nhập

1. Web chuyển trình duyệt đến `GET /api/v1/auth/login?return_to=/app/...`.
2. Core API kiểm tra `return_to` là path nội bộ, sinh `state`, `nonce`, browser
   binding và PKCE verifier bằng CSPRNG.
3. Database chỉ lưu HMAC của state/binding/nonce và bản mã AES-GCM của PKCE
   verifier. Flow có thời hạn tối đa 15 phút và chỉ consume được một lần.
4. Core API đặt browser-binding cookie `HttpOnly`, `SameSite=Lax`, rồi redirect đến
   authorization endpoint của IdP với PKCE `S256`.
5. Callback kiểm tra đồng thời state và binding cookie, atomically consume flow,
   đổi authorization code ở backend và xác minh chữ ký, issuer, audience, expiry,
   nonce của ID token.
6. Core API dùng access token ở backend để gọi OIDC UserInfo, bắt buộc `sub` phải
   trùng ID token, rồi mới lấy `email`, `email_verified`, tên và locale. Cách này
   tương thích với ZITADEL Authorization Code Flow, nơi profile/email không bắt
   buộc xuất hiện trong ID token.
7. Chỉ claims có email đã xác minh mới được ánh xạ sang user nội bộ. `(issuer,
   subject)` là khóa identity; verified email chỉ dùng để nối hồ sơ ban đầu từ IdP
   đáng tin cậy đang được cấu hình.
8. Core API tạo session token và CSRF token mới. PostgreSQL chỉ lưu keyed HMAC;
   token thô chỉ xuất hiện trong cookie/response tương ứng.
9. Browser nhận session cookie `HttpOnly` và CSRF cookie đọc được bởi web. Ở HTTPS,
   cookie dùng tiền tố `__Host-`, `Secure`, `Path=/`, không có `Domain`.
10. Web gọi `/api/v1/me` để hydrate user, active tenant, memberships và permissions.

Callback bị replay, nonce sai, binding cookie sai, flow hết hạn hoặc email chưa xác
minh đều bị từ chối. Provider error và lỗi nội bộ được trả bằng Problem Details,
không trả authorization code, token hoặc chi tiết nhạy cảm.

## Session và CSRF

| Thành phần | Thuộc tính chính |
|---|---|
| Session cookie | Opaque, `HttpOnly`, `SameSite=Lax`, `Secure` ở staging/production |
| CSRF cookie | Opaque, `SameSite=Lax`, web gửi lại bằng header `X-CSRF-Token` |
| Flow cookie | Browser binding, `HttpOnly`, hết hạn cùng OIDC flow và bị xóa ở callback |
| Idle timeout | Mặc định 8 giờ; mỗi `/me`/request xác thực cập nhật `last_seen_at` |
| Absolute timeout | Mặc định 24 giờ, không được kéo dài bởi idle refresh |
| Logout | Xác minh CSRF, revoke session server-side, xóa cookie rồi trả IdP logout URL |

CSRF được kiểm tra theo ba giá trị: header phải khớp cookie bằng constant-time compare,
sau đó token phải khớp keyed HMAC của session trong PostgreSQL. Không dùng
`localStorage` hoặc `sessionStorage` để lưu token.

## HTTP contract

| Endpoint | Hành vi |
|---|---|
| `GET /api/v1/auth/login` | Tạo flow và redirect sang IdP |
| `GET /api/v1/auth/callback` | Xác minh callback, tạo session và redirect về web |
| `GET /api/v1/auth/csrf` | Xoay CSRF token cho session hiện tại |
| `POST /api/v1/auth/logout` | Yêu cầu session cookie và `X-CSRF-Token`; revoke phiên |
| `GET /api/v1/me` | Trả user, active tenant, memberships và permissions |
| `POST /api/v1/tenants` | Tạo workspace đầu tiên, gán `org_admin`, đặt active tenant và xoay session/CSRF |
| `PUT /api/v1/session/active-tenant` | Xác minh membership, đổi active tenant và xoay session/CSRF |

Contract có thẩm quyền nằm tại `openapi/tutorhub.yaml`; TypeScript client được sinh ở
`packages/api-client/src/generated/schema.ts`.

## Workspace onboarding và tenant switching

Sau lần đăng nhập đầu tiên, tài khoản chưa có membership không được đi thẳng vào app
shell. Web hiển thị onboarding và gửi `POST /api/v1/tenants`. Core API khóa user và
session hiện tại, sau đó tạo tenant, membership `org_admin`, cập nhật active tenant và
ghi `tenant.created` vào outbox trong cùng transaction. Nếu một request đồng thời đã tạo
membership trước, request còn lại bị từ chối để không sinh workspace ngoài ý muốn.

Khi đổi workspace, browser chỉ gửi tenant đích. Backend không tin quyền từ client mà
kiểm tra membership đang hoạt động trong PostgreSQL trước khi cập nhật session. Cả hai
thao tác là privilege-context change nên session token và CSRF token đều được xoay; token
cũ mất hiệu lực ngay sau commit. Endpoint yêu cầu session hợp lệ, CSRF header/cookie và
CSRF HMAC server-side giống logout.

## Tạo ứng dụng ZITADEL

Trong một project ZITADEL dành riêng cho TutorHub, tạo hai Web applications. Không
dùng chung client secret giữa local và staging.

### Local

- Tên: `tutorhub-local`.
- Redirect URI: `http://localhost:8080/api/v1/auth/callback`.
- Post logout redirect URI: `http://localhost:5173/signed-out`.
- Response type/grant: Authorization Code.
- PKCE: bắt buộc `S256`.
- Scopes: `openid profile email`.

### Staging

- Tên: `tutorhub-staging`.
- Redirect URI: `https://<api-staging>/api/v1/auth/callback`.
- Post logout redirect URI: `https://<web-staging>/signed-out`.
- Chỉ HTTPS; không thêm wildcard redirect URI.
- Dùng client secret và secret store riêng của staging.

Email claim phải có `email_verified=true`. Chính sách MFA/passkey được cấu hình tại
IdP theo vai trò và giai đoạn triển khai; Platform Admin phải có MFA trước public beta.

## Cấu hình local

Tạo `.env.local` ignored từ `.env.example`. Sinh session secret mới ít nhất 32 byte:

```powershell
$bytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
[Convert]::ToBase64String($bytes)
```

Đưa kết quả vào `SESSION_SECRET` trong `.env.local`, không commit hoặc dán vào log.
Cấu hình các biến sau bằng giá trị của `tutorhub-local`:

```text
OIDC_ISSUER_URL=
OIDC_CLIENT_ID=
OIDC_CLIENT_SECRET=
OIDC_CALLBACK_URL=http://localhost:8080/api/v1/auth/callback
OIDC_POST_LOGOUT_URL=http://localhost:5173/signed-out
OIDC_SCOPES=openid profile email
SESSION_COOKIE_SECURE=false
```

Khi một trong các secret OIDC/session được cấu hình, toàn bộ nhóm giá trị bắt buộc
phải hợp lệ hoặc Core API sẽ fail-fast. Ở `staging`/`production`, auth, database,
HTTPS callback và secure cookie là bắt buộc.

## Kiểm thử

```powershell
go test ./services/core-api/internal/modules/identity
go test ./services/core-api/internal/httpapi
pnpm --filter @tutorhub/api-client test
pnpm --filter @tutorhub/web test
pnpm test:integration
pnpm verify
```

Integration test chạy migration, tạo identity/session trong transaction bao ngoài,
kiểm tra one-time flow, hash token, tenant permissions, workspace onboarding, tenant
switching, CSRF/session rotation và revoke rồi
rollback toàn bộ fixture.

## Trạng thái triển khai

- Code, migration v4, OpenAPI, generated client, unit test và Neon integration test
  đã hoàn thành cục bộ.
- `tutorhub-local` đã được provision ngày 2026-07-14 trong project `TutorHub V2`,
  instance `tutorhub-v2-dev`. Secret chỉ nằm trong `.env.local` đã Git-ignore.
- Browser smoke thật đã đạt: login/callback, `/api/v1/me`, reload giữ phiên,
  CSRF rotation, logout/revoke, post-logout redirect và route guard sau logout.
- Workspace onboarding và tenant selector đã hoàn thành cục bộ. Unit, HTTP, web và Neon
  integration test xác nhận tạo workspace đầu tiên, quyền `org_admin`, đổi tenant hợp lệ,
  từ chối tenant không có membership và vô hiệu hóa token phiên cũ.
- ZITADEL trả profile/email qua UserInfo trong Authorization Code Flow. Adapter đã
  được sửa để xác minh ID token trước, gọi UserInfo sau và từ chối khi `sub` không
  khớp; test hồi quy và `pnpm verify` đều đạt.
- `tutorhub-staging` đã được provision trong project ZITADEL riêng ngày 2026-07-16;
  callback HTTPS qua Cloudflare/Render, `/me`, reload session, logout và đăng nhập lại
  đã được smoke test thành công. Secret staging không dùng chung với local.
- Neon staging đã tách runtime role tối thiểu quyền và migration role. Migration
  `up/down/up` giữ `dirty=false`; Core API chỉ nhận pooled runtime URL.

Tài liệu chính thức: [ZITADEL OIDC endpoints](https://zitadel.com/docs/apis/openidoauth/endpoints),
[ZITADEL claims](https://zitadel.com/docs/apis/openidoauth/claims),
[ZITADEL logout](https://zitadel.com/docs/guides/integrate/login/oidc/logout) và
[ZITADEL pricing](https://zitadel.com/pricing).
