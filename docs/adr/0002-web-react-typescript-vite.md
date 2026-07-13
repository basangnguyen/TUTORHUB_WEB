# ADR-0002: React, TypeScript và Vite cho web

- Status: Accepted
- Date: 2026-07-11

## Decision

Dùng React với TypeScript strict và Vite để xây web client.

## Rationale

V1 đã có kinh nghiệm React/tldraw trong bảng trắng. React có hệ sinh thái phù hợp LiveKit, tldraw, accessibility và component testing; TypeScript tạo contract rõ khi sản phẩm có nhiều miền. Vite phù hợp SPA/app shell và developer feedback nhanh.

## Alternatives

- Next.js: có lợi cho SSR/SEO, nhưng web app lớp học chủ yếu sau đăng nhập; có thể dùng site marketing riêng nếu cần.
- Vue/Svelte: tốt về kỹ thuật nhưng giảm khả năng tái sử dụng kinh nghiệm và thư viện React hiện có.
- Vanilla HTML/JS: đơn giản ban đầu nhưng khó quản lý state/phân quyền/UI quy mô lớn.

## Consequences

Phải kiểm soát bundle, tránh state toàn cục quá mức và thiết kế component accessibility từ đầu. Không bê nguyên HTML/JCEF legacy vào React.
