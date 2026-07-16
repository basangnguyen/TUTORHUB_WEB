# ADR-0011: Render cho Core API staging và private alpha

- Status: Accepted
- Date: 2026-07-16
- Supersedes: phần lựa chọn nơi chạy Core API trong ADR-0006 và ADR-0007

## Bối cảnh

Kế hoạch ban đầu dùng Hugging Face Docker Space cho Go Core API. Tại thời điểm
cấp tài nguyên staging, tài khoản hiện tại chỉ cho phép Static Space miễn phí;
Docker Space yêu cầu gói trả phí. Core API đã được đóng gói OCI và không phụ
thuộc API riêng của Hugging Face, vì vậy có thể chuyển nơi chạy mà không đổi
contract ứng dụng.

Staging cần một HTTPS endpoint công khai cho BFF/OIDC callback, LiveKit webhook,
readiness check và same-origin proxy từ Cloudflare Pages.

## Quyết định

1. Cloudflare Pages phục vụ React SPA; Pages Function `/api/*` làm same-origin
   reverse proxy.
2. Render Web Service chạy Go Core API từ Dockerfile hiện có.
3. Neon PostgreSQL, Backblaze B2, LiveKit Cloud và ZITADEL tiếp tục là các dịch
   vụ managed của staging.
4. Secret của Core API chỉ nằm trong Render Environment; Cloudflare chỉ giữ
   `CORE_API_ORIGIN`. Không đưa database, OIDC, B2 hoặc LiveKit secret xuống web.
5. Hugging Face có thể dùng cho dịch vụ AI độc lập nhưng không còn là nơi chạy
   Core API staging.

## Hệ quả

- Cùng OCI image có thể chạy local, Render hoặc một managed container platform
  khác mà không đổi API contract.
- Trình duyệt chỉ giao tiếp với `https://tutorhub-web.pages.dev`; BFF session
  cookie không cần cơ chế CORS cross-origin.
- Render Free có thể spin down khi không hoạt động và gây cold start trên 50
  giây. Cấu hình này chỉ phù hợp staging/private alpha, không đáp ứng SLA
  production.
- Cloudflare và Render tự động triển khai commit mới nhất trên `main`. Rollback
  ứng dụng dùng previous deployment của provider; rollback schema dùng migration
  có kiểm soát.

## Xác minh

Ngày 2026-07-16, chủ dự án xác nhận toàn bộ smoke test staging đạt:

- `/health` và `/ready` trực tiếp trên Render và qua Cloudflare Pages;
- readiness của database và object storage đều `ready`;
- OIDC login/callback, `/me`, reload session, logout và đăng nhập lại;
- LiveKit camera, micro, screen share, reconnect và webhook idempotent;
- B2 PUT/GET/checksum/DELETE;
- Neon migrate/rollback/migrate với `dirty=false`.

Bằng chứng vận hành không chứa token, secret hoặc URL có credential.

## Điều kiện chuyển nền tảng

Phải chuyển khỏi Render Free trước public beta nếu cold start, availability,
regional latency, background work, concurrency hoặc observability không đạt SLO.
Nền tảng thay thế phải tiếp tục chạy OCI image và được ghi nhận bằng ADR mới.
