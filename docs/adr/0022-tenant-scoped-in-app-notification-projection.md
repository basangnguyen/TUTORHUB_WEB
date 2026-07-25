# ADR-0022: Tenant-scoped in-app notification projection và preference

- Trạng thái: **Accepted for controlled canary; end-user activation blocked by P3-03B**
- Ngày: 2026-07-25
- Phạm vi: P3-04, nền cho P3-05A và các notification nghiệp vụ về sau
- Liên quan: ADR-0015, ADR-0017, ADR-0018, ADR-0020 (reserved/provider gate)

## Bối cảnh

TutorHub cần một notification center tenant/user-scoped, trạng thái unread/read và
preference theo người dùng. Notification là side effect sau commit: lỗi worker hoặc
delivery không được rollback mutation nghiệp vụ. P3-03A đã cung cấp leased outbox worker,
nhưng production registry đang rỗng có chủ ý và startup ACL probe cấm mọi quyền trên bảng
khác `outbox_events`.

Các event `class_session.*.v1` hiện có không phải notification intent: chúng không chứa
recipient snapshot đầy đủ và một số event cũ vẫn đang unpublished. Đăng ký trực tiếp các
event này sẽ claim lịch sử, có thể tạo audience drift hoặc gửi thông báo muộn/sai người.
P3-03B cũng chưa đạt durable-host, staging grant và crash/reclaim gate, nên không được có
end-user activation trong P3-04.

## Quyết định

### 1. Projection và source of truth

1. Tạo `tutorhub.notifications` làm immutable notification projection, ngoại trừ
   `read_at`. Mọi row bắt buộc có `tenant_id`, `recipient_user_id`,
   `source_outbox_event_id`, `effect_key`, `template_key`, thời điểm nghiệp vụ và context
   JSON bounded và chỉ nhận object có string value ở database layer. Mỗi business
   template handler về sau phải validate exact key allowlist trước khi insert; canary
   hiện tại chỉ chấp nhận context rỗng.
2. Dedupe bằng unique `(source_outbox_event_id, recipient_user_id, effect_key)`. Handler
   dùng `INSERT ... ON CONFLICT DO NOTHING`, không dựa vào provider exactly-once.
3. Feed sắp theo `(created_at DESC, id DESC)`, không theo `occurred_at`, để projection đến
   muộn vẫn xuất hiện sau cursor đã cấp.
4. Không lưu email, tên, description, token, URL có credential hoặc notification body tự
   do trong event/context. Client render localized copy từ `template_key` và context có
   schema cố định.

### 2. Preference

1. `tutorhub.notification_preferences` self-scoped theo `(tenant_id, user_id)` và có
   `version` tăng đơn điệu.
2. Không có row nghĩa là default ảo: in-app bật, email tắt trong giai đoạn chưa có
   ADR-0020/provider gate, reminder offset 15 phút và quiet-hours tắt.
3. PUT là full replacement có `expected_version`; lần đầu insert chỉ chấp nhận version 0,
   các lần sau update theo compare-and-swap. Conflict trả HTTP 409.
4. Quiet-hours lưu local wall time + IANA timezone và chỉ hợp lệ khi cả start/end có mặt.
   P3-04 chỉ lưu preference; scheduling/delivery semantics thuộc P3-05A.

### 3. API và isolation

1. Endpoint không nhận `tenant_id` hoặc `user_id` từ body/query. Tenant và recipient luôn
   lấy từ authenticated active tenant/session principal. Header bắt buộc
   `X-TutorHub-Expected-Tenant-ID` chỉ là assertion chống cache/workspace race phía client:
   server so khớp nó với active tenant và trả `409 notification_scope_changed` khi lệch;
   header này không bao giờ chọn tenant hoặc cấp quyền.
2. API gồm list keyset, unread count bounded, mark-one-read, mark-all-read và GET/PUT
   preference. Mutation yêu cầu CSRF; foreign tenant/user ID được conceal thành 404.
3. Cursor base64url strict có version và scope hash gồm tenant, actor, filter và limit.
4. Response đặt `Cache-Control: no-store`, `Referrer-Policy: no-referrer`,
   `Vary: Cookie, X-TutorHub-Expected-Tenant-ID`.
5. Realtime giai đoạn đầu là bounded polling; chưa thêm SSE/WebSocket.

### 4. Controlled canary và activation gate

1. P3-04 chỉ đăng ký event riêng `notification.in_app_canary.requested.v1`, aggregate
   `notification_intent`, tenant required. Payload strict chỉ có recipient UUID và kind
   `system.worker_canary`; không có arbitrary title/body/email.
2. Registry chỉ đăng ký handler khi `OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY=true`; mặc định
   `false`. Khi false registry tiếp tục rỗng và không claim event.
3. Web/product visibility dùng feature `in_app_notifications`, mặc định catalog bật nhưng
   bị deployment guardrail ép tắt trừ khi
   `FEATURE_CONTROL_ENABLE_IN_APP_NOTIFICATIONS=true`. Trước khi
   P3-03B đạt, cả hai biến phải giữ false trên staging/production.
4. Canary event chỉ được migration owner/operator insert thủ công theo runbook sau khi đã
   tạo recipient fixture hợp lệ. Không có public API phát canary.
   API feed luôn loại `system.worker_canary`, nên acceptance row không thể thành nội dung
   end-user kể cả khi feature visibility bị cấu hình nhầm.
5. Event nghiệp vụ thật về sau phải là notification intent version mới chứa recipient
   snapshot được chốt trong cùng transaction, hoặc một fan-out contract riêng. Worker
   không query roster hiện tại để suy diễn audience.

### 5. Least privilege

1. API role: `SELECT` notification, column-level `UPDATE(read_at)`, và
   `SELECT/INSERT/UPDATE` preference; không được INSERT notification hoặc DELETE/TRUNCATE.
2. Worker role khi gate bật: giữ exact quyền P3-03 trên outbox và chỉ có column-level
   `INSERT` vào notification projection. Handler không cần SELECT nhờ unique constraint.
3. Startup capability probe nhận explicit effect-capability contract. Gate tắt vẫn yêu
   cầu ACL P3-03 cũ; gate bật yêu cầu đúng notification INSERT allowlist và fail closed nếu
   thiếu hoặc dư quyền.
4. Migration không hardcode role môi trường. Runbook migration owner áp direct grants và
   chạy probe bằng chính API/worker login.

## Các phương án không chọn

- Đăng ký `class_session.*.v1`: payload thiếu recipient snapshot và sẽ claim event lịch sử.
- Worker đọc roster/business tables lúc delivery: audience drift và phá least privilege.
- Gọi SES/email provider từ P3-04: vượt ADR-0020/P3-05A provider và deliverability gate.
- SSE ngay từ đầu: tăng connection/runtime complexity trước khi polling có số liệu tải.
- Cho frontend tự tạo notification: phá server authorization và nguồn sự thật.

## Hệ quả

- Có thể hoàn thiện/test toàn bộ projection, API, preference và UI khi P3-03B chưa xong,
  nhưng môi trường end-user vẫn không nhìn thấy hoặc nhận notification.
- Capability probe và runbook phức tạp hơn vì quyền worker phụ thuộc effect được bật; đổi
  gate mà chưa đổi grant sẽ fail startup an toàn.
- P3-04 kết thúc ở `VERIFY` cho tới khi migration/grant staging, canary duplicate +
  crash/reclaim và feature activation gate được nghiệm thu. P3-03B vẫn là blocker cho
  side effect tới người dùng.
- Email mặc định tắt; P3-05A/ADR-0020 phải quyết định transactional email, suppression,
  provider event và quiet-hours delivery trước khi bật channel này.
- Trước khi mở rộng cho volume lớn phải thêm retention projection và chiến lược batch
  cho mark-all-read; P3-04 giữ truy vấn tenant/user-scoped đúng và chưa claim scale gate.

## Acceptance

1. Duplicate/redelivery cùng source event không tạo row thứ hai.
2. Tenant/user IDOR, cursor scope tamper và foreign notification ID đều fail closed.
3. Mark-one/read-all idempotent; preference CAS trả conflict đúng.
4. Registry gate-off rỗng; gate-on chỉ allowlist canary version chính xác.
5. Startup ACL probe từ chối thiếu grant, table-level/excess grant, membership/owner và
   quyền trên bảng nghiệp vụ.
6. Mutation nghiệp vụ vẫn commit khi worker/notification projection lỗi.
7. Web có loading, empty, filtered-empty, error, forbidden/retry và polling bounded; gate
   deployment tắt thì không mount query/bell.
