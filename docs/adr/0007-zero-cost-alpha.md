# ADR-0007: Zero-cost private alpha

- Status: Accepted
- Date: 2026-07-12
- Supersedes: Phần web hosting trong ADR-0006
- Review gate: Khi vượt free quota hoặc chuẩn bị pilot có người dùng thật

## Decision

Hạ tầng private alpha phải vận hành trong free tier và không tự động phát sinh phí:

- React/Vite static web: Cloudflare Pages Free.
- Go Core API/BFF: Hugging Face Docker Space CPU Basic Free.
- Lavie AI: Hugging Face Space Free hoặc API có hard free quota.
- PostgreSQL: Neon Free.
- Object storage: Backblaze B2, giới hạn ứng dụng thấp hơn 10 GB miễn phí.
- Media: LiveKit Build Free với hard cap.
- Redis: chưa triển khai; chỉ thêm Upstash Free khi có use case bắt buộc.

## Capacity envelope

Alpha được thiết kế cho khoảng 20-100 người dùng hoạt động, một phòng học đồng thời và 10-20 người/phòng. LiveKit participant-minutes và downstream bandwidth là giới hạn chính, không phải frontend.

## Guardrails

- Không gắn auto-upgrade hoặc chuyển sang pay-as-you-go nếu không có ADR/quyết định mới.
- Ứng dụng có quota nội bộ thấp hơn quota nhà cung cấp.
- Recording tắt mặc định và chỉ dùng cho kiểm thử có kiểm soát.
- Core API stateless; HF disk là ephemeral.
- Theo dõi usage của Neon, B2, LiveKit và HF trước khi mở thêm tenant.
- Khi chạm quota, tính năng trả lỗi có giải thích thay vì phát sinh chi phí.

## Consequences

Alpha không có SLA, có cold start/sleep và không dành cho nhiều lớp hàng ngày. Code vẫn phải đóng gói portable để chuyển Core API sang managed container và nâng plan media/database mà không đổi contract/domain.
