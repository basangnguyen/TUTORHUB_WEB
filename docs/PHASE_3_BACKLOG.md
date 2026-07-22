# Backlog Phase 3 - Daily learning workspace

> Nguồn thực thi chi tiết cho Phase 3. Master Plan giữ mục tiêu và exit gate; tài
> liệu này giữ dependency, phạm vi, acceptance, API/schema, kiểm thử và Definition of Done.

## 1. Mục tiêu phase

Xây daily learning workspace đủ dùng cho pilot có kiểm soát:

1. teacher lên lịch buổi học đúng timezone;
2. teacher và student trao đổi bằng tin nhắn bền vững;
3. người dùng nhận notification mà lỗi delivery không rollback nghiệp vụ;
4. file lớn upload/download trực tiếp với Backblaze B2, không đi xuyên Core API;
5. worker xử lý outbox theo at-least-once, retry idempotent và dead-letter;
6. home, calendar, search và Class Files có đủ trạng thái vận hành.

**Thời lượng kế hoạch:** 5-7 tuần, chia theo dependency thay vì chạy song song mọi miền.

**Task vừa hoàn thành:** P3-00 Phase 3 backlog và architecture/contract baseline.

**Task hiện tại:** P3-01 Course session scheduling và timezone (`READY`).

## 2. Non-goal

- Classroom moderation, lobby và media lifecycle đầy đủ thuộc Phase 4.
- Whiteboard, breakout, recording và classroom tools thuộc Phase 5.
- Assignment, exam và QuizHub thuộc Phase 6.
- Lavie AI, social feed và search nâng cao thuộc Phase 7.
- Email/push provider production, billing và production SLO không nằm trong Phase 3.
- Không tự xây message broker, object storage, virus engine hoặc thumbnail service.
- Không thêm Redis, NATS, Kafka, microservice hoặc Kubernetes nếu chưa có tải/ADR.
- P3-01 không làm recurring series, reminder, calendar tổng hợp hoặc participant/media state.

## 3. Nguyên tắc bắt buộc

- OpenAPI đổi trước hoặc cùng implementation; generated TypeScript client không sửa tay.
- Tenant/class scope lấy từ session và repository authoritative; foreign ID bị conceal `404`.
- Mọi mutation nhạy cảm đi qua shared policy, audit và transactional outbox.
- Timestamp nghiệp vụ lưu dưới dạng instant UTC; civil time giữ IANA timezone và được
  kiểm tra DST theo ADR-0017.
- Worker chạy at-least-once; mọi handler phải idempotent theo outbox event ID.
- Notification failure không rollback business transaction đã commit.
- Binary không đi qua Core API; browser chỉ nhận presigned URL ngắn hạn và giới hạn scope.
- File chưa `ready` không được chia sẻ hoặc tải như artifact hợp lệ.
- Log, metric, audit và outbox không chứa token, cookie, signed URL, raw file content,
  message content không cần thiết hoặc PII thừa.
- Mỗi UI slice có loading, empty, filtered-empty, error, forbidden, offline/degraded và retry.

## 4. Trạng thái tổng hợp

| Task  | Nội dung                                      | Dependency                  | Trạng thái |
| ----- | --------------------------------------------- | --------------------------- | ---------- |
| P3-00 | Backlog + architecture/contract baseline      | Phase 2                     | DONE       |
| P3-01 | Course session scheduling và timezone         | P3-00                       | READY      |
| P3-02 | Calendar day/week/month và recurring series   | P3-01                       | TODO       |
| P3-03 | PostgreSQL outbox worker production shape     | P3-00                       | TODO       |
| P3-04 | In-app notification và preference             | P3-03                       | TODO       |
| P3-05 | Session reminder dispatch                     | P3-01, P3-03, P3-04         | TODO       |
| P3-06 | Direct/class conversation                     | P3-00, Phase 2 policy       | TODO       |
| P3-07 | Persistent message, unread và read receipt    | P3-03, P3-06                | TODO       |
| P3-08 | File metadata, upload intent và finalize      | P3-00, B2 baseline          | TODO       |
| P3-09 | Presigned B2 upload/download                  | P3-08                       | TODO       |
| P3-10 | Scan/metadata/thumbnail processing            | P3-03, P3-09                | TODO       |
| P3-11 | Class Files UI                                | P3-09, P3-10                | TODO       |
| P3-12 | Home dashboard và PostgreSQL search cơ bản    | P3-01, P3-04, P3-07, P3-11 | TODO       |
| P3-13 | Offline/retry drafts và Phase 3 quota closure | P3-02, P3-07, P3-11         | TODO       |
| P3-14 | Staging acceptance và đóng Phase 3            | P3-01 đến P3-13             | TODO       |

## 5. Dependency graph

```mermaid
flowchart LR
    P300["P3-00 Baseline"] --> P301["P3-01 Scheduling"]
    P300 --> P303["P3-03 Worker"]
    P300 --> P306["P3-06 Conversations"]
    P300 --> P308["P3-08 File metadata"]
    P301 --> P302["P3-02 Calendar/recurrence"]
    P301 --> P305["P3-05 Reminders"]
    P303 --> P304["P3-04 Notifications"]
    P304 --> P305
    P306 --> P307["P3-07 Messages"]
    P303 --> P307
    P308 --> P309["P3-09 B2 transfer"]
    P309 --> P310["P3-10 File processing"]
    P303 --> P310
    P309 --> P311["P3-11 Class Files"]
    P310 --> P311
    P301 --> P312["P3-12 Home/search"]
    P304 --> P312
    P307 --> P312
    P311 --> P312
    P302 --> P313["P3-13 Offline/quota"]
    P307 --> P313
    P311 --> P313
    P313 --> P314["P3-14 Closure"]
```

P3-01 và P3-03 có thể triển khai tuần tự trên `main`; không cần chạy đồng thời để đạt
tiến độ. P3-04/P3-05/P3-07/P3-10 không được bypass worker foundation.

## 6. P3-00 Backlog và architecture/contract baseline

**User outcome:** agent mới biết chính xác thứ tự Phase 3, task hiện tại và các quyết
định không được tự suy từ hội thoại.

### Definition of Done

- [x] Tạo backlog có task ID, dependency, scope, acceptance và exit gate.
- [x] Chọn P3-01 scheduling/timezone là vertical slice implementation đầu tiên.
- [x] ADR-0017 chốt instant/civil time, DST, lifecycle và recurrence boundary.
- [x] ADR-0018 chốt PostgreSQL leased outbox worker, retry và dead-letter.
- [x] Xác nhận không thêm provider/library/service ở P3-00.
- [x] Đồng bộ README, Project State, Agent Coordination, Delivery Roadmap và Master Plan.

## 7. P3-01 Course session scheduling và timezone

**User outcome:** teacher lên lịch một buổi học; người có quyền xem lớp thấy đúng thời
gian; teacher có thể sửa hoặc hủy mà không làm lẫn tenant/lớp.

### Scope

- Class-scoped session một lần, không recurrence.
- Lifecycle public của P3-01: `scheduled -> cancelled`; schema dự phòng `live/ended`
  nhưng chỉ Phase 4 được nối transition media.
- Create, list theo bounded range, detail, update metadata/time và cancel idempotent.
- UTC instant + IANA timezone; request có RFC3339 offset rõ và kiểm tra round-trip DST.
- Optimistic `version`; audit/outbox trong cùng transaction với mutation.
- Minimal class-session UI trên class detail; calendar tổng hợp thuộc P3-02.

### API/schema dự kiến

- Migration `000014_class_sessions` có forward/down path.
- `GET/POST /api/v1/classes/{class_id}/sessions`.
- `GET/PATCH /api/v1/classes/{class_id}/sessions/{session_id}`.
- `POST /api/v1/classes/{class_id}/sessions/{session_id}/cancel`.
- Permission mới `session.schedule`; read dùng class viewer projection authoritative.
- Không tin `tenant_id`, owner, role hoặc status do client tự khai.

### Acceptance

- [ ] Org admin, organization teacher, class owner và co-teacher tạo/sửa/hủy đúng quyền.
- [ ] TA/student active chỉ xem; unenrolled user bị deny; foreign IDs bị conceal `404`.
- [ ] Draft/archived class không tạo hoặc sửa lịch; archived history vẫn đọc được.
- [ ] `starts_at < ends_at`, duration/range bị giới hạn và timezone phải là IANA hợp lệ.
- [ ] DST gap bị từ chối; DST overlap chỉ nhận khi offset disambiguate đúng.
- [ ] Concurrent stale update trả `409`; cancel lặp lại là idempotent no-op.
- [ ] Mutation ghi audit/outbox redacted, không chứa description đầy đủ hoặc PII thừa.
- [ ] UI có vi/en, keyboard flow, loading/empty/error/forbidden/offline/retry.
- [ ] Unit, PostgreSQL integration, authorization/IDOR và Playwright teacher/student xanh.

### Rollout/rollback

- Feature mặc định chỉ mở cho staging/private alpha sau migration và CI.
- Rollback application không cần down migration; down chỉ chạy trên disposable branch.
- Không xóa row session để rollback UI; endpoint mới có thể tắt qua feature control.

## 8. P3-02 Calendar day/week/month và recurring series

- Calendar tổng hợp theo viewer timezone nhưng hiển thị class timezone khi khác biệt.
- Bounded date range, server-side query và URL state cho day/week/month.
- Recurrence là series + occurrence, không clone vô hạn; edit-one/edit-future/cancel có
  semantics và ADR bổ sung trước implementation.
- DST gap/overlap, month boundary, leap day và timezone switch có golden tests.
- Reminder không nằm trong transaction lịch; P3-05 tiêu thụ event sau commit.

## 9. P3-03 PostgreSQL outbox worker production shape

- Thực thi ADR-0018 bằng `services/core-api/cmd/worker` trong cùng modular monolith/image.
- Lease batch bằng `FOR UPDATE SKIP LOCKED` cùng fencing token; stale owner không thể
  ack/retry/dead-letter sau khi lease bị reclaim.
- At-least-once, exponential backoff có cap/jitter, max attempts và dead-letter retained.
- Handler registry typed; downstream effect idempotent theo `source_outbox_event_id`.
- Worker dùng database role tối thiểu riêng; API runtime chỉ cần `INSERT` outbox.
- Không ép `tenant_id` thành `NOT NULL`; identity/system event global phải có context an toàn.
- Event Phase 1/2 không bị blanket mark published; chỉ claim event type/version allowlist.
- Graceful shutdown không nhận lease mới và không đánh dấu success khi handler chưa xong.
- Metric label bounded theo event/handler/outcome; log chỉ giữ error code redacted.
- Unit, PostgreSQL integration, crash/reclaim, duplicate delivery và poison-event tests.
- Provider activation là staging work của task; không nhét worker loop vào HTTP API và
  không xem Render Free web service có spin-down là durable worker.

## 10. P3-04 In-app notification và preference

- Tenant/user-scoped notification projection, unread/read và preference versioned.
- Worker tạo notification từ event đã commit; lỗi delivery không rollback business row.
- API list keyset pagination, unread count, mark one/all read và update preference.
- Không gửi email/push production; adapter ngoài chỉ thêm khi có ADR/provider decision.
- Realtime ban đầu có thể dùng bounded polling; SSE chỉ thêm khi contract/failure mode rõ.

## 11. P3-05 Session reminder dispatch

- Reminder được materialize từ session schedule sau commit và có dedupe key ổn định.
- Update/cancel session hủy/supersede reminder cũ; timezone/DST không làm gửi hai lần.
- Worker claim theo due time; retry idempotent; late delivery có bounded policy.
- Notification preference được áp dụng lúc delivery, không làm mất audit nghiệp vụ.

## 12. P3-06 Direct/class conversation

- Conversation class-scoped và direct same-tenant; không cho client tự khai participant.
- Class conversation membership lấy từ enrollment authoritative.
- Direct conversation có canonical participant set để create lặp không sinh duplicate.
- Archive class giữ history nhưng policy viết mới phải được chốt rõ.
- Tạo ADR transport/retention/moderation trước P3-07 nếu cần SSE/WebSocket.

## 13. P3-07 Persistent message, unread và read receipt

- REST write/read là source of truth; LiveKit DataChannel không lưu chat bền vững.
- Keyset pagination, client message ID idempotent, server timestamp và edited/deleted state.
- Unread/read receipt theo user/conversation, update monotonic và tenant-scoped.
- Message content không đi vào audit/outbox/log; event chỉ giữ ID/metadata allowlist.
- Reconnect không mất message đã commit; duplicate submit không tạo message thứ hai.

## 14. P3-08 File metadata, upload intent và finalize

- File state: `pending -> uploaded -> processing -> ready/rejected`; delete/retention tách rõ.
- Intent tạo object ID/key opaque, quota reservation và presigned scope ngắn hạn.
- Finalize kiểm tra size/checksum/content metadata server-side, không tin tên/MIME client.
- File chưa `ready` không xuất hiện trong share/download projection.

## 15. P3-09 Presigned B2 upload/download

- Browser upload/download trực tiếp B2; Core API không proxy binary lớn.
- URL ngắn hạn, exact method/key/content length/checksum và least-privilege capability.
- Download chỉ cấp sau authorization authoritative và file `ready`.
- Retry multipart, abort, expiry và checksum mismatch có test/smoke staging.

## 16. P3-10 Scan/metadata/thumbnail processing

- Chọn scanner/thumbnail runtime bằng spike/ADR; không tự nhận container hiện tại đủ tải.
- Worker xử lý idempotent; timeout/provider failure giữ file không-shareable.
- Malware/suspicious file thành `rejected`, không public; metadata redacted và bounded.
- Thumbnail là derived object có lifecycle theo source, không thay binary gốc.

## 17. P3-11 Class Files UI

- Teacher upload/quản lý; student chỉ xem/tải file được chia sẻ đúng lớp.
- UI có progress, resume/retry, checksum failure, processing, rejected và ready states.
- Không render active content nguy hiểm; download disposition/MIME được kiểm soát.
- Cache key chứa tenant/class và bị purge khi switch/archive/role change.

## 18. P3-12 Home dashboard và PostgreSQL search cơ bản

- Home gom session sắp tới, unread notification/message và file gần đây bằng bounded query.
- Search PostgreSQL chỉ trên resource actor được phép; không trả snippet vượt quyền.
- Không thêm Elasticsearch/vector store khi PostgreSQL chưa có bằng chứng không đủ.
- Partial provider/module failure degrade từng card, không làm hỏng toàn dashboard.

## 19. P3-13 Offline/retry drafts và Phase 3 quota closure

- Chỉ draft không nhạy cảm được lưu client; không lưu token/signed URL/message đã gửi.
- Retry mutation dùng idempotency key khi có khả năng submit lại tự động.
- Mở rộng feature catalog/quota cho scheduling, message và file bằng server enforcement.
- Quota rejection có typed problem, bounded metric và cleanup path; không xóa dữ liệu cũ.

## 20. P3-14 Staging acceptance và exit gate

### Acceptance scenarios

- [ ] Teacher tạo/sửa/hủy session; student thấy đúng timezone qua reload.
- [ ] Calendar day/week/month và recurrence vượt DST đúng semantics.
- [ ] Message không mất sau reconnect/reload; unread/read đúng user.
- [ ] Business mutation vẫn thành công khi notification delivery tạm lỗi.
- [ ] Worker crash/reclaim, retry và dead-letter không tạo duplicate effect.
- [ ] File lớn upload trực tiếp B2; finalize/checksum/scan/share/download đúng trạng thái.
- [ ] Foreign tenant/class/user/file/message IDs đều bị deny/conceal và không mutate.
- [ ] Home/search chỉ trả resource được phép; partial failure có degraded state.
- [ ] Deploy, migration up/down/up và application rollback smoke đạt trên staging.

### Exit gate Phase 3

- Message không mất sau reconnect và duplicate submit không tạo duplicate.
- Upload lớn không đi qua Core API.
- File chưa `ready` không được chia sẻ/tải.
- Worker retry/idempotency/dead-letter được test trên PostgreSQL thật.
- Timezone/DST tests và staging smoke đạt.
- Notification failure không rollback nghiệp vụ.
- Verify, Security, provider parity và staging acceptance đều xanh.
- Biên bản `PHASE_3_COMPLETION.md` được sign-off trước khi chuyển phase.

## 21. Thứ tự sprint

| Sprint | Task chính             | Kết quả demo                                      |
| ------ | ---------------------- | ------------------------------------------------- |
| 0      | P3-00                  | Backlog + ADR baseline                            |
| 1      | P3-01, P3-03           | Session một lần + worker PostgreSQL               |
| 2      | P3-02, P3-04, P3-05    | Calendar, notification và reminder               |
| 3      | P3-06, P3-07           | Conversation và persistent message               |
| 4      | P3-08, P3-09           | Upload intent/finalize + B2 direct transfer       |
| 5      | P3-10, P3-11           | File processing + Class Files UI                  |
| 6      | P3-12, P3-13, P3-14    | Home/search, quota/offline và staging closure     |

## 22. Việc cần làm ngay

1. P3-00 đã `DONE`; Phase 3 bắt đầu ngày 2026-07-22.
2. Thực hiện P3-01 contract-first: migration `000014`, policy, OpenAPI/client, backend,
   minimal UI và test timezone/DST/tenant isolation.
3. Không đưa recurrence, reminder, worker hoặc calendar tổng hợp vào P3-01.
4. Sau P3-01, thực hiện P3-03 trước notification, message side-effect hoặc file processing.
5. Giữ file cá nhân ngoài scope và không đọc/commit `.env*.local`.
