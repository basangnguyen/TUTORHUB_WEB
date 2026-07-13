# ADR-0008: ZITADEL Cloud cho OIDC staging

- Status: Accepted
- Date: 2026-07-13

## Context

ADR-0005 đã khóa Authorization Code + PKCE qua Go BFF nhưng chưa chọn identity
provider. TutorHub V2 cần một IdP có OIDC chuẩn, MFA/passkey, khả năng tách client
theo môi trường, không yêu cầu tự vận hành thêm stateful service trong private alpha
và có thể bắt đầu trong giới hạn miễn phí.

## Decision

Dùng ZITADEL Cloud làm IdP cho local development có tương tác và staging trong
P1-06/P1-10. Tạo hai OIDC Web applications riêng:

1. `tutorhub-local` chỉ nhận callback và post-logout URL localhost.
2. `tutorhub-staging` chỉ nhận HTTPS callback và post-logout URL staging.

Core API chỉ phụ thuộc OpenID Connect discovery, Authorization Code, PKCE S256,
ID Token verification, UserInfo/revocation/end-session chuẩn. Không gọi management
API độc quyền của ZITADEL trong request path. Unit/integration test dùng fake OIDC
issuer cục bộ để không phụ thuộc mạng hoặc tài khoản cloud.

Browser không nhận access/refresh token. Go BFF đổi code, xác minh ID Token rồi phát
hành opaque session cookie `HttpOnly`; session và revoke state nằm trong PostgreSQL.

## Rationale

- ZITADEL Cloud Free hiện công bố 100 daily active users và các tính năng bảo mật,
  phù hợp development/private alpha có hard cap.
- ZITADEL hỗ trợ OIDC discovery, PKCE S256, RP-initiated logout, token revocation,
  MFA/passkeys và mô hình tổ chức.
- Managed IdP tránh vận hành Keycloak cùng database/backup/patching trên Hugging Face
  Free Space vốn có disk tạm và không có production SLA.
- Lớp provider-neutral cho phép chuyển sang Keycloak, Auth0, Entra ID hoặc nhà cung
  cấp OIDC khác bằng cấu hình và adapter, không thay session contract của browser.

## Alternatives

- **Keycloak self-host:** đầy đủ và mã nguồn mở nhưng tăng đáng kể vận hành, database,
  backup, patch bảo mật và availability; giữ làm phương án enterprise/on-premise.
- **Google OAuth trực tiếp:** phù hợp social login nhưng không phải lớp identity đa
  nhà cung cấp/organization hoàn chỉnh cho hệ sinh thái TutorHub.
- **JWT trong SPA/localStorage:** tiếp tục bị từ chối theo ADR-0005 vì tăng token
  exposure và khó revoke.
- **Tự xây password/MFA:** bị từ chối trong MVP vì phạm vi bảo mật quá lớn.

## Security constraints

- Bắt buộc state, nonce, PKCE S256 và browser-binding cookie cho mỗi auth flow.
- Callback URL phải khớp allowlist; `return_to` chỉ là path nội bộ.
- Local và staging không dùng chung client secret.
- Session cookie có HttpOnly, SameSite, Secure ở staging/production; request thay đổi
  trạng thái dùng CSRF token gắn với session.
- Không log authorization code, token, cookie, state, nonce hoặc client secret.
- Secret chỉ nằm trong `.env.local` ignored hoặc secret store của deployment.

## Consequences

- Chủ dự án vẫn phải tạo ZITADEL instance và hai application trước cloud smoke test.
- Free tier không phải SLA production; phải theo dõi DAU và có exit plan trước pilot.
- Back-channel logout, refresh token rotation, MFA policy và account linking cần gate
  riêng trước public beta.

## Official references

- ZITADEL pricing: https://zitadel.com/pricing
- OIDC endpoints và PKCE: https://zitadel.com/docs/apis/openidoauth/endpoints
- RP-initiated logout: https://zitadel.com/docs/guides/integrate/login/oidc/logout
- ZITADEL security capabilities: https://zitadel.com/docs
- Keycloak securing applications: https://www.keycloak.org/docs/latest/securing_apps/
