# ADR-0003: Go modular monolith cho backend mới

- Status: Accepted
- Date: 2026-07-11

## Decision

Backend V2 bắt đầu bằng một Go modular monolith, một PostgreSQL và ranh giới module nội bộ. Java server V1 tiếp tục chạy trong giai đoạn di chuyển nhưng không là nền tảng backend mới.

## Rationale

Go phù hợp dịch vụ mạng đồng thời, binary triển khai gọn và hiệu quả tài nguyên. Modular monolith giảm độ phức tạp distributed transaction, service discovery và vận hành so với microservice sớm.

## Alternatives

- Java/Spring Boot: hệ sinh thái enterprise mạnh và gần V1, nhưng mục tiêu V2 đã chọn Go cho backend mới; Java vẫn được dùng ở adapter legacy nếu cần.
- Node/NestJS: chia sẻ TypeScript nhưng event-loop và runtime governance cần kỷ luật cho workload đa dạng.
- Microservices ngay: bị từ chối do tăng chi phí vận hành khi domain và tải chưa ổn định.

## Consequences

Module không truy cập bảng của nhau tùy tiện. Dùng transaction outbox cho event. Chỉ tách service khi có ownership, scaling hoặc reliability requirement cụ thể.
