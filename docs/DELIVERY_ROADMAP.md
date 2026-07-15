# Lộ trình giao hàng Web V2

> Bản tóm tắt điều hành. Chi tiết work package, dependency, SLO, rủi ro và exit gate nằm trong [MASTER_PLAN.md](MASTER_PLAN.md). Khi có khác biệt, Master Plan phiên bản mới hơn là nguồn có thẩm quyền.

| Thuộc tính          | Trạng thái                                                                         |
| ------------------- | ---------------------------------------------------------------------------------- |
| Cập nhật            | 2026-07-15                                                                         |
| Phase hiện tại      | Phase 1 - Engineering Foundation                                                   |
| Hoàn thành gần nhất | P1-08A CI/security baseline đã merge cùng nền tảng Phase 1 qua PR #4 tại `82261c6` |
| Việc tiếp theo      | P1-10 Cloud foundation, sau đó P1-08B preview/staging deployment                   |
| Phạm vi             | Web-first; desktop/mobile/native là track sau                                      |

## Chuỗi phase

| Phase | Tên                               |    Thời lượng kế hoạch | Kết quả chính                                              |
| ----: | --------------------------------- | ---------------------: | ---------------------------------------------------------- |
|     0 | Product và architecture baseline  |             Hoàn thành | Phạm vi, ADR, security/deployment baseline                 |
|     1 | Engineering Foundation            |               4-6 tuần | CI, web shell, API, database, auth, LiveKit spike, staging |
|     2 | Identity, tenant và class core    |               4-6 tuần | Multi-tenant, permission, class/enrollment                 |
|     3 | Daily learning workspace          |               5-7 tuần | Lịch, persistent messaging, notification, file/Drive       |
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
- Hugging Face/Neon/LiveKit free tier chỉ phục vụ phát triển/private alpha.
- Classroom, webinar và broadcast là các capacity profile khác nhau.
- Desktop/mobile không làm chậm Web MVP; chỉ chuẩn bị API/domain contract.
