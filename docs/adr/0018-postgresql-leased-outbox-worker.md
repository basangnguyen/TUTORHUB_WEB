# ADR-0018: Worker lease PostgreSQL cho transactional outbox

- Trạng thái: Accepted
- Ngày: 2026-07-22
- Phạm vi: P3-03 và các notification/message/file side effect về sau

## Bối cảnh

Core API đã ghi transactional outbox cùng business mutation, nhưng chưa có worker
production shape để claim, retry và quan sát event. Phase 3 cần reminder,
notification, message projection và file processing mà lỗi delivery không được
rollback transaction nghiệp vụ đã commit.

Private alpha đang dùng Go modular monolith và Neon PostgreSQL. Chưa có số liệu tải để
biện minh Redis queue, NATS, Kafka, Kubernetes hoặc một microservice mới. Worker vẫn
phải chạy nhiều replica an toàn, phục hồi sau crash và xử lý poison event có giới hạn.

## Quyết định

1. Thêm entry point `services/core-api/cmd/worker` trong cùng repository, Go module và
   OCI image với Core API. Đây là process độc lập; HTTP API không chạy polling loop.
2. PostgreSQL outbox là source of truth. Worker claim batch hữu hạn theo thứ tự ổn định
   bằng transaction và `FOR UPDATE SKIP LOCKED`, ghi `lease_owner`, `lease_token` và
   `leased_until`; nhiều replica không được sở hữu cùng lease còn hiệu lực. Ack/retry/
   dead-letter phải so khớp fencing token nên owner cũ không thể cập nhật sau reclaim.
3. Delivery là at-least-once. Không tuyên bố exactly-once. Handler registry typed theo
   event name/version; unknown event và payload sai schema đi theo failure policy.
4. Mỗi downstream effect phải idempotent bằng `source_outbox_event_id` hoặc dedupe key
   ổn định có unique constraint. Handler hoàn tất effect trước khi đánh dấu event
   published; crash ở khoảng giữa có thể redeliver nhưng không tạo effect thứ hai.
5. Lỗi retryable tăng attempt và đặt `available_at` bằng exponential backoff có cap và
   jitter. Lỗi permanent hoặc vượt `max_attempts` chuyển sang dead-letter retained,
   không bị polling vô hạn và không xóa bằng cleanup thông thường.
6. Dead-letter giữ event ID/type/version, tenant/resource ID tối thiểu, attempt,
   timestamps và error code redacted. Không lưu token, cookie, signed URL, message/file
   content hoặc stack trace chứa credential trong payload/log/metric.
7. Graceful shutdown ngừng nhận lease mới, chờ handler đang chạy trong deadline và
   không đánh dấu success nếu handler chưa hoàn tất. Lease hết hạn được process khác
   reclaim.
8. Worker dùng database role riêng có `SELECT/UPDATE` tối thiểu trên outbox; API runtime
   chỉ cần `INSERT`. Migration không ép `tenant_id` thành `NOT NULL` vì identity/system
   event hiện hữu có thể là global và phải được xử lý bằng event context an toàn.
9. Metrics dùng label bounded `event_type`, `handler`, `outcome`; có counters cho claim,
   success, retry, dead-letter và gauges/age cho backlog. Correlation dùng request/event
   ID, không dùng PII làm label.
10. P3-03 phải có unit và PostgreSQL integration tests cho contention, crash/reclaim,
   duplicate delivery, retry schedule, poison event, dead-letter và shutdown.
11. Event cũ của Phase 1/2 không bị blanket mark published. Handler registry chỉ claim
    type/version đã đăng ký; unknown/historical event được giữ nguyên để inspect hoặc
    cutover theo allowlist, không tự ack hoặc dead-letter âm thầm.
12. Provider deployment/feasibility được xác minh trong P3-03. Render Free web service
    có spin-down không được xem là durable worker. ADR này không tuyên bố
    Render hiện đã chạy worker và không thêm provider/library mới ở P3-00.

## Hệ quả

- API và worker có thể deploy/scale độc lập nhưng vẫn chia sẻ code, schema, policy và
  release artifact; chưa phát sinh distributed service boundary mới.
- PostgreSQL chịu thêm polling/locking nên batch, interval, index và backlog age phải
  được đo. Khi tải thực tế vượt budget, ADR mới có thể thay transport mà giữ event/
  handler contract.
- At-least-once buộc mọi handler idempotent; handler không có dedupe proof không được
  đưa vào production registry.
- Retained dead-letter cần runbook inspect/replay/purge có authorization và audit trước
  khi vận hành production.

## Phương án đã cân nhắc

### Poll outbox trong HTTP API

Đơn giản khi deploy một process nhưng gắn side effect vào lifecycle web, khó scale và
dễ tạo nhiều loop không kiểm soát khi API tăng replica.

### Đánh dấu published trước khi gọi handler

Tránh duplicate nhưng có thể mất event khi process crash sau mark; không đáp ứng exit
gate không mất side effect đã commit.

### Thêm Redis/NATS/Kafka ngay

Chưa chấp nhận vì tăng provider, secret, runbook và failure mode trước khi có số liệu
tải. PostgreSQL đã nằm trong transaction boundary và đủ để chứng minh private alpha.
