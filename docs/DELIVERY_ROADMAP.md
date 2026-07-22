# Lộ trình giao hàng Web V2

> Bản tóm tắt điều hành. Chi tiết work package, dependency, SLO, rủi ro và exit gate nằm trong [MASTER_PLAN.md](MASTER_PLAN.md). Khi có khác biệt, Master Plan phiên bản mới hơn là nguồn có thẩm quyền.

| Thuộc tính          | Trạng thái                                    |
| ------------------- | --------------------------------------------- |
| Cập nhật            | 2026-07-22                                    |
| Phase hiện tại      | Phase 3 - Daily learning workspace            |
| Hoàn thành gần nhất | P3-00 backlog/architecture ngày 2026-07-22    |
| Việc tiếp theo      | P3-01 session scheduling và timezone          |
| Phạm vi             | Web-first; desktop/mobile/native là track sau |

## Chuỗi phase

| Phase | Tên                               |    Thời lượng kế hoạch | Kết quả chính                                              |
| ----: | --------------------------------- | ---------------------: | ---------------------------------------------------------- |
|     0 | Product và architecture baseline  |             Hoàn thành | Phạm vi, ADR, security/deployment baseline                 |
|     1 | Engineering Foundation            |             Hoàn thành | CI, web shell, API, database, auth, LiveKit spike, staging |
|     2 | Identity, tenant và class core    |             Hoàn thành | Multi-tenant, permission, class/enrollment                 |
|     3 | Daily learning workspace          |         Đang thực hiện | Lịch, persistent messaging, notification, file/Drive       |
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
[PHASE_3_BACKLOG.md](PHASE_3_BACKLOG.md): P3-00 đã `DONE`, P3-01 scheduling/timezone
đang `READY`; các task sau phải tuân dependency và exit gate trong backlog.
