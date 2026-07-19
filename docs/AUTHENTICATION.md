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
   subject)` là khóa identity; verified email chỉ được bootstrap user mới. Nếu email
   đã thuộc user hiện hữu thì identity mới phải đi qua explicit authenticated link flow,
   không được tự động merge tài khoản chỉ theo email.
8. Core API tạo session token và CSRF token mới. PostgreSQL chỉ lưu keyed HMAC;
   token thô chỉ xuất hiện trong cookie/response tương ứng.
9. Browser nhận session cookie `HttpOnly` và CSRF cookie đọc được bởi web. Ở HTTPS,
   cookie dùng tiền tố `__Host-`, `Secure`, `Path=/`, không có `Domain`.
10. Web gọi `/api/v1/me` để hydrate user, active tenant, memberships và permissions.

Callback bị replay, nonce sai, binding cookie sai, flow hết hạn hoặc email chưa xác
minh đều bị từ chối. Provider error và lỗi nội bộ được trả bằng Problem Details,
không trả authorization code, token hoặc chi tiết nhạy cảm.

## Session và CSRF

| Thành phần       | Thuộc tính chính                                                             |
| ---------------- | ---------------------------------------------------------------------------- |
| Session cookie   | Opaque, `HttpOnly`, `SameSite=Lax`, `Secure` ở staging/production            |
| CSRF cookie      | Opaque, `SameSite=Lax`, web gửi lại bằng header `X-CSRF-Token`               |
| Flow cookie      | Browser binding, `HttpOnly`, hết hạn cùng OIDC flow và bị xóa ở callback     |
| Idle timeout     | Mặc định 8 giờ; mỗi `/me`/request xác thực cập nhật `last_seen_at`           |
| Absolute timeout | Mặc định 24 giờ, không được kéo dài bởi idle refresh                         |
| Logout           | Xác minh CSRF, revoke session server-side, xóa cookie rồi trả IdP logout URL |

CSRF được kiểm tra theo ba giá trị: header phải khớp cookie bằng constant-time compare,
sau đó token phải khớp keyed HMAC của session trong PostgreSQL. Không dùng
`localStorage` hoặc `sessionStorage` để lưu token.

## HTTP contract

| Endpoint                                                              | Hành vi                                                                                                       |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/auth/login`                                              | Tạo flow và redirect sang IdP                                                                                 |
| `GET /api/v1/auth/callback`                                           | Xác minh callback, tạo session và redirect về web                                                             |
| `GET /api/v1/auth/csrf`                                               | Xoay CSRF token cho session hiện tại                                                                          |
| `POST /api/v1/auth/logout`                                            | Yêu cầu session cookie và `X-CSRF-Token`; revoke phiên                                                        |
| `GET /api/v1/me`                                                      | Trả user, active tenant, memberships và permissions                                                           |
| `GET /api/v1/tenants`                                                 | Trả các tenant active mà user có membership active                                                            |
| `POST /api/v1/tenants`                                                | Tạo workspace cho user chưa có membership hoặc `org_admin`, gán owner, đặt active tenant và xoay session/CSRF |
| `GET /api/v1/tenants/{tenant_id}`                                     | Đọc tenant trong active scope với `tenant.view`                                                               |
| `PATCH /api/v1/tenants/{tenant_id}`                                   | Cập nhật metadata với `tenant.manage`, CSRF và `expected_version`                                             |
| `POST /api/v1/tenants/{tenant_id}/archive`                            | Archive mềm, xóa active context và xoay session/CSRF                                                          |
| `PUT /api/v1/session/active-tenant`                                   | Xác minh membership, đổi active tenant và xoay session/CSRF                                                   |
| `GET/POST /api/v1/tenants/{tenant_id}/invitations`                    | Admin list hoặc tạo membership invitation trong active tenant                                                 |
| `POST /api/v1/tenants/{tenant_id}/invitations/{invitation_id}/revoke` | Admin revoke invitation pending, idempotent khi revoke lặp lại                                                |
| `POST /api/v1/membership-invitations/preview`                         | Anonymous preview tối thiểu; token nằm trong JSON body                                                        |
| `POST /api/v1/membership-invitations/accept`                          | Session + CSRF; accept bằng verified linked identity khớp email                                               |
| `GET/POST /api/v1/classes`                                            | List status/cursor hoặc tạo class draft trong active tenant                                                   |
| `GET/PATCH /api/v1/classes/{class_id}`                                | Đọc hoặc cập nhật metadata/activate với `expected_version`                                                    |
| `POST /api/v1/classes/{class_id}/archive`                             | Archive draft/active với `class.archive`, CSRF và CAS                                                         |
| `POST /api/v1/classes/{class_id}/restore`                             | Restore đúng trạng thái trước archive với CSRF và CAS                                                         |
| `POST /api/v1/classes/{class_id}/transfer-ownership`                  | Transfer owner với CSRF, CAS và recent authentication 10 phút                                                 |
| `POST /api/v1/classes/{class_id}/enrollments`                         | Manager direct-enroll active tenant member theo normalized email; CSRF bắt buộc                               |
| `POST /api/v1/classes/{class_id}/enrollments/{user_id}/suspend`       | Manager suspend active enrollment; CSRF bắt buộc                                                              |
| `POST /api/v1/classes/{class_id}/enrollments/{user_id}/remove`        | Manager remove enrollment hợp lệ; CSRF bắt buộc                                                               |
| `GET/POST /api/v1/classes/{class_id}/invite-codes`                    | Manager list metadata hoặc tạo invite code có TTL/usage limit; raw token chỉ trả khi create                   |
| `POST /api/v1/classes/{class_id}/invite-codes/{code_id}/revoke`       | Manager revoke code active; CSRF bắt buộc                                                                     |
| `POST /api/v1/class-invitations/join`                                 | Session + CSRF; nhận class invite token trong JSON body và join/rejoin atomically                             |
| `POST /api/v1/classes/{class_id}/leave`                               | Active enrollee tự rời lớp; CSRF bắt buộc và replay idempotent                                                |

Contract có thẩm quyền nằm tại `openapi/tutorhub.yaml`; TypeScript client được sinh ở
`packages/api-client/src/generated/schema.ts`.

## Workspace onboarding và tenant switching

Sau lần đăng nhập đầu tiên, tài khoản chưa có membership không được đi thẳng vào app
shell. Web hiển thị onboarding và gửi `POST /api/v1/tenants`. Core API khóa session rồi
user, kiểm tra lại user vẫn chưa có membership active trong tenant active, sau đó tạo tenant,
membership `org_admin`, cập nhật active tenant và ghi `tenant.created` vào outbox trong cùng
transaction. User lock tuần tự hóa các session onboarding; nếu request trước đã tạo membership
thì request còn lại bị từ chối để không sinh workspace ngoài ý muốn.

Khi đổi workspace, browser chỉ gửi tenant đích. Backend không tin quyền từ client mà
kiểm tra membership đang hoạt động trong PostgreSQL trước khi cập nhật session. Cả hai
thao tác là privilege-context change nên session token và CSRF token đều được xoay; token
cũ mất hiệu lực ngay sau commit. Endpoint yêu cầu session hợp lệ, CSRF header/cookie và
CSRF HMAC server-side giống logout.

P2-02 mở rộng luồng thành lifecycle tenant đầy đủ. List chỉ trả tenant active có
membership active của user; detail yêu cầu `tenant.view`, còn update/archive yêu cầu
`tenant.manage` trong đúng active tenant. Update và archive bắt buộc `expected_version`;
SQL chỉ ghi khi version còn khớp rồi tăng version, vì vậy stale mutation trả conflict
thay vì âm thầm ghi đè. Status không được PATCH trực tiếp.

Create từ workspace hiện hữu và switch khóa tenant/membership trước session; create đối
chiếu active tenant authoritative của session với tenant đã được service authorize rồi chạy
lại shared policy trên role hiện tại. Update/archive cũng khóa membership và reauthorize trong
transaction. Vì vậy revoke/demotion đồng thời không thể lọt qua authorization snapshot cũ.
Lock order của archive là tenant/membership, session hiện tại, các session liên quan rồi user
để không tạo chu kỳ với profile update.

Mỗi session có `context_version`. Create, switch và archive khóa session rồi dùng
compare-and-swap trên version này trước khi thay active tenant và token hash, ngăn hai
response đồng thời cài lại context cũ. Mọi switch hợp lệ, kể cả chọn lại tenant hiện tại,
đều xoay session/CSRF. Archive xóa active tenant khỏi mọi session liên quan, xoay
credential của phiên thực hiện và bị từ chối nếu actor không còn tenant active khác với
role `org_admin`.

Các mutation thành công ghi `tenant.created`, `tenant.updated`, `tenant.archived` hoặc
`tenant.switched` vào transactional outbox cùng business transaction. Payload chỉ giữ
actor, from/to tenant và version cần thiết; audit query và failure event đầy đủ thuộc
P2-07. Web áp dụng principal trả về ngay, hủy request cũ và xóa cache tenant-scoped để
không hiển thị dữ liệu workspace trước sau create/switch/archive.

## Membership invitation

P2-03 thêm permission `tenant.manage_members`; chỉ active `org_admin` có quyền
list/create/revoke trong đúng active tenant. Flow này chỉ cấp `teacher`, `student`
hoặc `guest`; cấp `org_admin` cần mutation/step-up riêng ở task sau. Repository khóa
tenant và membership actor rồi reauthorize policy trong transaction để không tin
permission snapshot cũ.

Create chuẩn hóa email, sinh token CSPRNG 256-bit có prefix phiên bản `thinv1_` và chỉ
lưu purpose-bound HMAC 32 byte. Raw token chỉ được trả một lần trong `accept_url` dạng
`/invite#token=...`; web consume fragment, gọi `history.replaceState` ngay và giữ token
trong memory. Preview/accept đều là POST JSON body nên token không vào API path/query,
browser history, referrer, structured log hoặc TanStack Query key/cache data.

TTL mặc định 168 giờ, cấu hình bằng `MEMBERSHIP_INVITATION_TTL` trong khoảng 15 phút
đến 720 giờ. Một tenant/email chỉ có một invitation `pending`; existing membership
ở bất kỳ status nào làm create conflict. Invitation `revoked` hoặc `expired` cho phép
re-invite; revoke lặp lại là idempotent. State `pending/accepted/revoked/expired` và
timestamp hợp lệ được khóa thêm bằng constraint PostgreSQL.

Anonymous preview chỉ trả tenant name, masked email, intended role và expiry. Accept
yêu cầu session hợp lệ, CSRF và ít nhất một linked identity active có
`email_verified=true` với normalized provider email khớp chính xác invitation; chỉ
`users.email` không đủ. Transaction tuần tự hóa theo tenant, session, identity-user,
membership rồi invitation, tạo tối đa một membership/event và cho cùng acceptor replay
idempotent. Accept không tự chuyển active tenant hoặc xoay session; principal trả về có
membership mới để user chủ động switch sau đó.

Preview/accept có bounded in-process limiter theo action và `RemoteAddr` prefix. Đây là
guard private-alpha, chưa phải distributed quota: Cloudflare/Render có thể gộp client
vào proxy bucket. Không tin forwarded header khi Render origin còn public; trusted
proxy/origin authentication và limiter phân tán thuộc P2-09.

## Class lifecycle và ownership

P2-04 giữ `owner_user_id` làm owner implicit của class; P2-05 không tạo enrollment riêng
cho owner. List class luôn lấy tenant từ active session, hỗ trợ status và opaque keyset
cursor. Class tạo ở draft; update chỉ cho transition draft -> active. Archive nhận draft
hoặc active và restore trả chính xác về trạng thái trước archive.

Update/archive/restore/transfer ownership đều nhận `expected_version`; SQL
compare-and-swap rồi tăng version để stale request không ghi đè mutation mới hơn.
Repository khóa tenant, class và actor/target membership theo thứ tự ổn định, đọc lại
membership authoritative rồi dùng shared policy trong transaction. Resource ở tenant
khác được che như not found. Lifecycle và ownership event được ghi vào transactional
outbox cùng business mutation.

`class.archive` và `class.transfer_ownership` chỉ thuộc active `org_admin` hoặc owner
implicit của đúng class. Organization teacher/co-teacher không được suy rộng hai quyền
này. Target ownership phải là active member cùng tenant có effective permission
`class.create`; transfer vẫn được phép khi class archived, còn same-owner là no-op khi
version còn khớp.

Transfer yêu cầu `auth_time` trong principal/session không cũ hơn 10 phút và tái dùng
recent-auth semantics của P2-01. Hiện login không force OIDC `max_age`/`prompt`; trường
hợp provider không gửi `auth_time` được chuẩn hóa theo thời điểm login hiện tại. Vì vậy
guard này chưa phải một OIDC step-up tuyệt đối.

Class list/detail trả `viewer_access` do server tính từ class, organization role và
enrollment persisted hiện tại. Chỉ enrollment `active` được nạp thành class role; session
hoặc browser không được tự khai class role. Media token/event chỉ được chấp nhận khi
class active và policy cho phép join/publish từ projection authoritative này. Archive
không thu hồi JWT LiveKit đã phát hoặc kick participant đang kết nối.

## Class enrollment và invite code

P2-05 lưu một enrollment tenant-scoped cho mỗi cặp class/user với class role
`co_teacher`, `teaching_assistant` hoặc `student` và state
`invited/active/suspended/left/removed`. Lát cắt hiện tại direct-enroll active tenant
member theo normalized email vào role `student`; active replay là no-op. Manager có
`enrollment.manage` có thể suspend/remove hoặc direct-reactivate, còn active enrollee có
`enrollment.leave` có thể tự chuyển sang `left`. Suspended/removed không thể tự phục hồi
bằng invite code; `left`/`invited` student có thể rejoin.

Create invite code chỉ chạy trên class active, nhận TTL 900-2.592.000 giây và usage
limit 1-1000. Backend sinh token CSPRNG 256-bit với prefix `thciv1_`, digest bằng purpose
`class-invite-code-v1` và chỉ lưu HMAC 32 byte. Raw token chỉ xuất hiện một lần trong
`join_url` dạng `/class-invite#token=...`; web xóa fragment bằng `history.replaceState`
ngay khi đọc, giữ token trong memory và gửi `POST /api/v1/class-invitations/join` bằng
JSON body. Token không nằm trong path/query, browser storage hoặc TanStack Query key/data.

Join yêu cầu session, CSRF, active tenant membership và active class. Transaction khóa
scope, membership, enrollment và invite row; join mới/rejoin tăng usage đúng một lần và
lượt cuối chuyển code sang `exhausted`. Active enrollment replay và principal đã có quyền
quản lý không tiêu thụ lượt. Malformed, expired, exhausted, revoked, suspended/removed
hoặc cross-scope token dùng unavailable response thống nhất để giảm enumeration.

Archive chặn direct enrollment, create code, join và media request mới nhưng không xóa
lịch sử. Manager vẫn list/revoke code; active enrollee vẫn có thể leave. Restore không
tự phát lại token, thay đổi usage hay chuyển enrollment. Join có bounded in-process
limiter theo action và `RemoteAddr` prefix; đây chỉ là guard private-alpha với cùng hạn
chế trusted-proxy/distributed quota như membership invitation, cần hoàn thiện ở P2-09.

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
MEMBERSHIP_INVITATION_TTL=168h
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

Bộ test unit, HTTP và PostgreSQL integration bao phủ one-time flow, hash token, tenant
permissions, workspace onboarding, tenant list/detail/update/archive, optimistic
version, context CAS, switching, invitation create/preview/accept/replay/revoke/expiry/
concurrent accept, CSRF/session rotation, class list/keyset pagination, update CAS,
lifecycle/ownership authorization, enrollment transitions, invite-code HMAC/TTL/usage,
same-user/concurrent join, active enrollment projection, archive guard, cross-tenant
concealment, recent-auth và outbox.
Các integration test database chạy migration trong transaction/fixture có cleanup.

## Trạng thái triển khai

- Nền authentication ban đầu dùng migration `000004`; profile/identity dùng `000006`;
  tenant lifecycle/session-context CAS dùng `000007`; membership invitation dùng
  `000008`; class lifecycle/ownership dùng `000009`; class enrollment/invite code dùng
  `000010`. Các migration này đều có up/down path.
- `tutorhub-local` đã được provision ngày 2026-07-14 trong project `TutorHub V2`,
  instance `tutorhub-v2-dev`. Secret chỉ nằm trong `.env.local` đã Git-ignore.
- Browser smoke thật đã đạt: login/callback, `/api/v1/me`, reload giữ phiên,
  CSRF rotation, logout/revoke, post-logout redirect và route guard sau logout.
- Workspace onboarding và tenant selector đã hoàn thành cục bộ. Unit, HTTP, web và Neon
  integration test xác nhận tạo workspace đầu tiên, quyền `org_admin`, đổi tenant hợp lệ,
  từ chối tenant không có membership và vô hiệu hóa token phiên cũ.
- P2-02 đã bổ sung typed OpenAPI/client, tenant list/detail/update/archive, optimistic
  version, `tenant.view`/`tenant.manage`, durable success events và cache invalidation/UI
  states. `pnpm verify` xanh ngày 2026-07-18; integration-tag của migration/classroom/
  identity biên dịch xanh. Clean migration và PostgreSQL integration được CI dùng
  PostgreSQL 17 xác nhận sau khi push checkpoint.
- P2-03 đã bổ sung invitation HMAC/TTL/state machine, verified-identity accept
  transaction idempotent, revoke/re-invite policy, typed client và admin/public UI.
  `pnpm verify` xanh ngày 2026-07-18; PostgreSQL integration-tag compile local. Runtime
  chưa chạy local vì không nạp DB test env; CI PostgreSQL 17 sẽ xác nhận clean
  migration/lifecycle/concurrent-accept sau push.
- P2-04 đã bổ sung typed class list/update/archive/restore/ownership contract, CAS,
  shared-policy lifecycle permissions, transaction/outbox và classroom UI.
  `pnpm verify` xanh ngày 2026-07-18: web 55/55, API client 11/11, UI 6/6 cùng toàn
  bộ lint/typecheck/build/Storybook, Go test/vet và security checks.
  Migration/classroom/identity integration-tag compile xanh local; runtime PostgreSQL
  chưa chạy local vì không nạp DB test env và sẽ do CI PostgreSQL 17 xác nhận.
- P2-05 đã bổ sung typed enrollment/invite-code contract, purpose-bound HMAC, bounded
  TTL/usage, atomic join/rejoin/leave, authoritative per-class access projection,
  archive guard và transactional outbox. Migration/classroom/identity integration-tag
  compile xanh local; runtime migration `000010` và PostgreSQL integration chưa chạy
  local vì không nạp DB test env. Tài liệu này không tuyên bố staging đã áp dụng `000010`.
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
