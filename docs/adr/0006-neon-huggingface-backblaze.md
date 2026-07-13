# ADR-0006: Neon, Hugging Face Spaces và Backblaze B2

- Status: Accepted for MVP and private beta
- Date: 2026-07-11
- Review gate: Before public beta or when reliability/load targets are not met

## Decision

TutorHub V2 sử dụng:

- **Neon PostgreSQL** làm relational system of record.
- **Hugging Face Spaces (Docker)** để chạy web/API và các server ứng dụng trong giai đoạn MVP/private beta.
- **Backblaze B2** qua S3-compatible API làm object storage cho file, ảnh, video và artifact.
- **LiveKit Cloud** tiếp tục xử lý WebRTC media; media không đi xuyên qua Hugging Face server.

Cloudflare có thể tiếp tục làm lớp DNS/CDN/cache phía trước nội dung phù hợp, nhưng Backblaze B2 vẫn là nơi lưu binary gốc.

## Deployment topology ban đầu

Mỗi workload có vòng đời khác nhau được tách thành một Space riêng:

1. `tutorhub-web`: phục vụ React build hoặc chuyển hướng sang static hosting khi cần.
2. `tutorhub-core-api`: Go modular monolith và BFF.
3. `tutorhub-realtime`: chỉ tạo khi persistent WebSocket/SSE không phù hợp đặt cùng core API.
4. `tutorhub-ai`: Lavie/AI workload, không chia process với core API.

Một Space không được chạy đồng thời database, object storage hoặc nhiều service không liên quan chỉ để giảm số deployment.

## Rationale

- Tái sử dụng các dịch vụ và kinh nghiệm vận hành đã có từ V1.
- Neon giảm công việc quản trị PostgreSQL trong giai đoạn đầu.
- Docker Spaces cho phép đóng gói Go/Node/Python thống nhất và triển khai nhanh.
- Backblaze B2 tương thích S3, phù hợp presigned upload và tách binary khỏi database/server disk.

## Constraints

- Disk của Hugging Face Space được xem là **ephemeral**. Không lưu session, upload, queue hoặc dữ liệu nghiệp vụ chỉ trên local filesystem.
- Space có thể restart, sleep hoặc thay đổi tài nguyên tùy cấu hình. API phải stateless và chịu được restart.
- Không giả định Hugging Face cung cấp SLA, autoscaling, multi-region hoặc network control tương đương nền tảng container production chuyên dụng.
- WebSocket dài hạn, background worker và job queue phải được spike/test trước khi đưa vào flow quan trọng.
- Free/low-cost tier không được xem là bằng chứng đủ cho public production.

## Security

- Mọi credential được lưu bằng Hugging Face Space Secrets, không đặt trong Docker image, source hoặc frontend bundle.
- Neon dùng TLS, pooled connection string cho workload web và database role tối thiểu quyền.
- Backblaze dùng application key giới hạn bucket/capability; browser chỉ nhận presigned URL ngắn hạn.
- LiveKit API secret chỉ nằm ở core API Space.
- Production, staging và development dùng project/database/bucket/key tách biệt.

## Exit and migration strategy

Core API phải stateless, đóng gói OCI và không phụ thuộc API riêng của Hugging Face. Nếu private beta không đạt availability, latency, concurrency hoặc vận hành background job, cùng image sẽ được chuyển sang managed container platform hoặc Kubernetes mà không đổi domain contract.

Hugging Face là quyết định hosting của giai đoạn đầu, không phải ràng buộc vĩnh viễn cho kiến trúc toàn cầu.
