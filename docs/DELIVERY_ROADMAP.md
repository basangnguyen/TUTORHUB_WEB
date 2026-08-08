# Lộ trình giao hàng Web V2

> Bản tóm tắt điều hành. Chi tiết work package, dependency, SLO, rủi ro và exit gate nằm trong [MASTER_PLAN.md](MASTER_PLAN.md). Khi có khác biệt, Master Plan phiên bản mới hơn là nguồn có thẩm quyền.

| Thuộc tính          | Trạng thái                                      |
| ------------------- | ----------------------------------------------- |
| Cập nhật            | 2026-08-08                                      |
| Phase hiện tại      | Phase 4 entry open; Phase 3 carry-over tiếp tục |
| Hoàn thành gần nhất | P3-14-CORE `DONE` trên candidate `f5f1eb3`      |
| Mốc repository mới  | Core Exit exact CI/Render/Pages/live PASS       |
| Việc tiếp theo      | Lập backlog/ADR readiness cho Classroom Media MVP |
| Phạm vi             | Web-first; desktop/mobile/native là track sau   |

## Chuỗi phase

| Phase | Tên                               |    Thời lượng kế hoạch | Kết quả chính                                              |
| ----: | --------------------------------- | ---------------------: | ---------------------------------------------------------- |
|     0 | Product và architecture baseline  |             Hoàn thành | Phạm vi, ADR, security/deployment baseline                 |
|     1 | Engineering Foundation            |             Hoàn thành | CI, web shell, API, database, auth, LiveKit spike, staging |
|     2 | Identity, tenant và class core    |             Hoàn thành | Multi-tenant, permission, class/enrollment                 |
|     3 | Daily learning workspace          | Core Exit trước; full closure sau | Lịch chuyên nghiệp, poll core, chat, file; email/worker carry-over |
|     4 | Classroom Media MVP               |               6-8 tuần | Prejoin, LiveKit room, moderation, reconnect               |
|     5 | Classroom Collaboration           |              8-12 tuần | Whiteboard, quiz nhanh, tools, breakout, recording         |
|     6 | Assessment, Tasks và QuizHub      |              8-12 tuần | Assignment, exam, scoring, practice/game                   |
|     7 | Content, Social Learning và Lavie |              6-10 tuần | Feed/video có kiểm soát, AI/RAG theo quyền                 |
|     8 | Global Readiness                  | 8-12 tuần trước launch | Production hosting, SLO, DR, security, privacy             |
|     9 | V1 Cutover và Sunset              |        4-8 tuần/cohort | Import, reconciliation, rollout và V1 read-only            |

## Milestone

1. Engineering demo: auth -> class -> room spike.
2. Private alpha: quản lý lớp và classroom cơ bản.
3. Pilot: lịch, chat, file, media và whiteboard.
4. Learning beta: assignment, exam và QuizHub.
5. Public beta: content/AI có quota, SLO và support.
6. Regional GA: production readiness và cutover cohort đầu.
7. Global expansion: multi-region và profile webinar/broadcast riêng.

## Nguyên tắc tiến độ

- Không chuyển phase chỉ vì hết thời gian; phải đạt exit gate.
- Security, observability, accessibility và i18n bắt đầu từ Phase 1.
- Migration V1 thực hiện theo module/cohort, không big-bang.
- Render Free, Neon và LiveKit free tier chỉ phục vụ phát triển/private alpha.
- Classroom, webinar và broadcast là các capacity profile khác nhau.
- Desktop/mobile không làm chậm Web MVP; chỉ chuẩn bị API/domain contract.

Phase 2 đã hoàn thành; ma trận staging và biên bản đóng phase nằm tại
[P2_12_STAGING_ACCEPTANCE.md](P2_12_STAGING_ACCEPTANCE.md) và
[PHASE_2_COMPLETION.md](PHASE_2_COMPLETION.md). Backlog thực thi hiện hành là
[PHASE_3_BACKLOG.md](PHASE_3_BACKLOG.md): P3-00/P3-CAL-00/00B/00C,
P3-CAL-01, P3-01, P3-02A/B/C, P3-02D-A, P3-06/07A, P3-08/09, P3-11A/12/13
và P3-14-CORE đã `DONE`. ADR-0019 được
`Accepted; manual NVDA gate PASS`: FullCalendar Standard v7.0.1 cùng recurrence
caps `366 ngày/730 ngày/512/2.000/250 ms` được chấp nhận ở cấp decision spike, nhưng
COUNT phải giữ occurrence cuối trong horizon và YEARLY golden đã đạt. Full v7 E2E hậu
fix đạt `9 passed (23.6s)`; comparator parity v6 đạt `4 passed` nhưng fail render 500 và
long-task 2.000 absolute budget. Agenda/Axe hardening và NVDA đạt gate; FullCalendar đã
được nối route production và P3-02A đã đóng route-level authorization/range/bundle/a11y
gate. P3-02B recurrence/conflict và P3-02C working hours/attendee/free-busy/RSVP cũng
đã đạt staging acceptance. P3-03A repository/runtime foundation đã `VERIFY`; P3-03B
còn worker role/grants, durable host và crash/reclaim acceptance. P3-04 implementation
vẫn gate-off; không bật asynchronous notification delivery tới end user trước khi
P3-03B đạt.
ADR-0021 đã chốt P3-02D Native Availability Poll, member-owned Study Meeting và quyền
cho active member gồm student. P3-02D-A core đã `DONE` trên candidate `8585864` sau exact
Neon/ACL/concurrency, authenticated browser/API và manual NVDA acceptance; P3-02D-B
auto-close/fan-out/delivery vẫn là carry-over. AWS SES
đã được chọn làm transactional email provider target nhưng P3-CAL-02/ADR-0020 vẫn phải
xác minh sandbox/adapter/deliverability và production vẫn cần domain/DNS cùng
SPF/DKIM/DMARC. Calendar professional core, poll và transactional email/ICS/RSVP thuộc
Phase 3; các task sau phải tuân dependency và exit gate trong backlog. `P3-14-CORE` là
checkpoint cho phép bắt đầu Phase 4 sau khi lane runnable đạt gate; P3-03B/P3-04
activation, SES/domain/live interoperability và delivery/processing phụ thuộc worker
được giữ carry-over để đóng sau, không đánh dấu PASS do bỏ qua.
P3-CAL-01 không phải dependency kỹ thuật của session một lần P3-01; cả hai gate đã đạt.
P3-CAL-02 có thể chạy sandbox cô lập, nhưng mọi email/notification side effect runtime
vẫn phải chờ P3-03B. Core Exit đã đạt trên candidate `f5f1eb3`; bước tiếp theo là lập
backlog/ADR implementation-readiness cho Phase 4. `P3-02D-B` auto-close/fan-out/delivery
và các lane worker/provider tiếp tục là carry-over, không bị coi là PASS.

## Mô hình chuyển tiếp Phase 3 → Phase 4

- **Core Exit (`P3-14-CORE`):** P3-02A/B/C không regression; P3-02D-A, conversation/
  message persistence core và B2 file-transfer core đạt test, authorization, a11y và
  staging smoke. Carry-over register phải ghi owner/dependency/feature flags.
- **Được phép chuyển:** sau Core Exit, bắt đầu Phase 4 dù worker/SES/domain/processing
  gate còn `DEFERRED/VERIFY` hoặc `DEFERRED/TODO`.
- **Chưa phải full closure:** umbrella Phase 3/P3-14 chỉ `DONE` sau khi carry-over,
  side-effect safety, full staging acceptance và completion record đều đạt.
