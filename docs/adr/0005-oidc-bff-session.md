# ADR-0005: OIDC qua BFF và session cookie

- Status: Accepted
- Date: 2026-07-11

## Decision

Dùng OIDC Authorization Code + PKCE. Go backend đóng vai trò BFF, giữ token với identity provider và phát hành opaque session cookie cho browser.

## Rationale

Giảm token exposure trong JavaScript và tránh lưu refresh token ở localStorage. Session server-side hỗ trợ revoke, device management, step-up authentication và audit tốt hơn payload legacy V1.

## Alternatives

- JWT trực tiếp trong SPA/localStorage: bị từ chối vì tăng rủi ro token theft và revoke phức tạp.
- Tự xây email/password auth hoàn toàn: bị từ chối trong MVP vì tăng phạm vi bảo mật.

## Consequences

Cần CSRF protection, session store, cookie policy, refresh rotation và high availability. IdP cụ thể vẫn là quyết định triển khai ở Phase 1 và phải hỗ trợ migration/portability.
