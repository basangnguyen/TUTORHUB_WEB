# Báo cáo nghiên cứu và thiết kế tab Lịch TutorHub V2

- Trạng thái: `PROPOSED`
- Ngày nghiên cứu: 2026-07-22 đến 2026-07-23
- Phạm vi: Phase 3, P3-01/P3-02/P3-03/P3-04/P3-05 và khả năng mở rộng về sau
- Nguồn: tài liệu chính thức Google, Microsoft, Zoom, ClassIn; RFC/WCAG; mã nguồn mở;
  source và ảnh TutorHub V1; kiến trúc TutorHub V2 hiện tại
- Tài liệu kiến trúc liên quan:
  [ADR-0017](adr/0017-class-session-scheduling-and-civil-time.md) và
  [ADR-0018](adr/0018-postgresql-leased-outbox-worker.md)

> Đây là báo cáo thiết kế và đề xuất, chưa phải bằng chứng tính năng đã được triển khai.
> Những quyết định mới về recurrence, conflict và thư viện phải được chốt bằng ADR trước
> khi sửa runtime.

## 1. Kết luận điều hành

Tab Lịch nên trở thành **learning operations hub** của TutorHub, không phải một bản sao
Google Calendar và cũng không chỉ là lưới ngày/tháng đẹp mắt. Một người dùng phải có thể:

1. biết hôm nay và sắp tới cần học/dạy gì;
2. tạo, đổi, hủy buổi học đúng quyền và đúng múi giờ;
3. phát hiện xung đột trước khi lưu;
4. chuẩn bị tài liệu/thiết bị trước lớp;
5. vào lớp đúng thời điểm bằng CTA phù hợp vai trò;
6. sau lớp mở attendance, recording, report, chat và file;
7. nhận nhắc lịch đáng tin cậy mà lỗi gửi không làm rollback lịch;
8. sử dụng hoàn chỉnh bằng bàn phím, screen reader và màn hình nhỏ.

Quyết định đề xuất:

- Dùng **FullCalendar Standard (MIT)** làm renderer/interaction engine sau một technical
  spike đạt yêu cầu; không dùng Premium trong Phase 3.
- TutorHub tự sở hữu domain, form, quyền, recurrence, conflict, reminder, LiveKit và
  external sync. Object của thư viện UI không được trở thành database schema.
- `classroom` tiếp tục sở hữu mutation của `ClassSession` theo ADR-0017. Calendar là
  read model tổng hợp, không tạo microservice và không gom mọi domain vào một bảng
  `calendar_events` chung.
- P3-01 vẫn triển khai session một lần trước. P3-02 thêm top-level calendar và recurrence
  theo series/exception; không clone vô hạn occurrence.
- Desktop có Day/Work week/Week/Month/Agenda; mobile mặc định Agenda. Year chỉ là
  navigation/heatmap về sau, không phải exit gate Phase 3.
- Recurrence phải hỗ trợ `chỉ buổi này`, `buổi này và các buổi sau`, `toàn bộ chuỗi`;
  giữ tombstone của occurrence bị hủy và identity ổn định.
- Phase 3 chỉ đồng bộ lịch nội bộ. Google/Microsoft two-way sync, public booking page và
  advanced resource scheduling thuộc phase sau, khi đã có ADR/provider/security model.

## 2. Phương pháp và giới hạn nghiên cứu

### 2.1 Nguồn đã dùng

- Google Calendar Help và Google Calendar API.
- Microsoft Teams Support và Microsoft Graph Calendar.
- Zoom Support, Zoom Calendar/Scheduler và developer documentation.
- ClassIn website, admin/LTI pages và teacher/student manual công khai.
- RFC 5545, PostgreSQL datetime, Unicode date/time, WCAG 2.2 và WAI-ARIA APG.
- Repository/tài liệu FullCalendar, Schedule-X, React Big Calendar, TOAST UI Calendar,
  Cal.diy, rrule.js và một số thư viện recurrence Go.
- Source read-only trong `D:\Ban_sao_du_an`; không đọc `.env*`, token, credential hoặc
  cấu hình production.

### 2.2 Quy tắc diễn giải

- `FACT`: nguồn chính thức hoặc source code xác nhận.
- `INFERENCE`: bài học/đề xuất cho TutorHub; không được mô tả như tính năng đối thủ.
- Không có tài liệu công khai không có nghĩa sản phẩm không có chức năng.
- Manual ClassIn công khai không đủ để suy ra backend hoặc UI phiên bản mới nhất.
- Tên/version thư viện có thể đổi; pin version chỉ sau spike và dependency review.

## 3. Tầm nhìn sản phẩm và thước đo thành công

### 3.1 North-star

> Trong tối đa ba thao tác, người dùng biết buổi học tiếp theo, trạng thái chuẩn bị và
> hành động cần làm; người có quyền có thể tạo hoặc đổi lịch mà không gây lỗi tenant,
> timezone, recurrence hay gửi nhắc trùng.

### 3.2 Persona và job-to-be-done

| Persona | Việc chính trong Lịch | View mặc định đề xuất |
| --- | --- | --- |
| Student | Xem agenda, nhận nhắc, Join, xem thay đổi/hủy và mở recap | Agenda hoặc Week |
| Teacher | Tạo/sửa series, kiểm tra xung đột, Prepare/Start, quản lý thay đổi | Work week |
| Co-teacher/TA | Xem lịch lớp, hỗ trợ chuẩn bị/join theo capability | Week |
| Organization Admin | Điều phối lớp/teacher, batch schedule, override có audit | Week + filter |
| Guest/Parent tương lai | Xem projection giới hạn, không thấy dữ liệu riêng tư | Agenda read-only |

### 3.3 KPI sản phẩm

- Tỷ lệ vào đúng buổi từ Calendar/Home.
- Tỷ lệ mutation lịch thành công và tỷ lệ `409` conflict/stale.
- Số xung đột được phát hiện trước khi lưu.
- Reminder đúng hạn, trùng, muộn và dead-letter.
- Thời gian tải view p50/p95 và số item trên visible range.
- Tỷ lệ hoàn tất bằng keyboard; lỗi Axe/WCAG.
- Số yêu cầu support do timezone/đổi lịch/mất link.

Không dùng số KPI giả định làm exit gate cho private alpha; bắt đầu thu baseline rồi mới
chốt SLO có số liệu.

## 4. Nghiên cứu đối thủ

### 4.1 Ma trận tổng hợp

| Năng lực | Google Calendar | Microsoft Teams | Zoom | ClassIn | Bài học cho TutorHub |
| --- | --- | --- | --- | --- | --- |
| Views | Day, Week, Month, Year, Schedule, 4 days | Day, Work week, Week, Month/agenda | Day, Work week, Week, Month, Agenda tùy client | Calendar/upcoming/finished được xác nhận | Day/Work week/Week/Month/Agenda |
| Tạo nhanh | Click/drag rồi mở chi tiết | Timeslot hoặc New | Quick scheduler/full editor | Course → Lesson | Quick create + full drawer |
| Recurrence | RRULE + occurrence + exception | Pattern + range | Daily/weekly/monthly/custom | Không đủ tài liệu | Series/exception chuẩn |
| Availability | Find a time, room, appointment | Scheduler/free-busy/room | Suggested time, buffer, lead time | Scheduling/batch ở admin | Conflict nội bộ trước; booking sau |
| Timezone | Event/calendar, secondary zone | Event/recurrence zone | Primary/secondary zone | Không đủ tài liệu | UTC + IANA + dual-zone label |
| Lifecycle | Event/meet/link/file | Prejoin/live/recap/chat/files | Reminder/Join/Start/assets | Prepare/Enter/Evaluation/Playback/Report | Lifecycle giáo dục |
| Admin | Share permissions | Meeting policies/groups | Account/group lock, delegate | Roster, scheduling, supervision | Quyền lịch tách quyền phòng |
| Sync | Token, push signal, ETag | Graph delta/change notification | Calendar integration/webhook | LTI calendar sync | Adapter provider ở phase sau |
| Accessibility | Keyboard/screen reader/agenda | Tài liệu keyboard rất chi tiết | WCAG 2.2 AA/VPAT | Không đủ bằng chứng | Agenda semantic + keyboard-first |

### 4.2 Google Calendar

Điểm đáng học:

- Toolbar ổn định gồm Today, previous/next, range title và view switcher.
- Mini-calendar, danh sách calendar/lớp, màu/filter ở sidebar.
- Progressive disclosure: thao tác phổ biến trong quick create; form chi tiết chỉ mở khi
  cần attendee, timezone, recurrence, reminder, permission hoặc file.
- Schedule/Agenda là view quan trọng cho mobile và assistive technology.
- Event có attendee/RSVP, visibility, guest permission, location, conference và file.
- Recurrence dùng master series; occurrence có `originalStartTime`; sửa một lần là
  exception. “This and following” cắt series cũ và tạo phần mới.
- Reminder của từng người tách khỏi default calendar và notification về thay đổi.
- Push notification chỉ báo resource đã đổi; consumer phải chạy incremental sync.

Nguồn:
[views](https://support.google.com/calendar/answer/6110849?co=GENIE.Platform%3DDesktop&hl=en-GB),
[create event](https://support.google.com/calendar/answer/72143?hl=en-uk),
[event resource](https://developers.google.com/workspace/calendar/api/v3/reference/events),
[recurring events](https://developers.google.com/workspace/calendar/api/guides/recurringevents),
[incremental sync](https://developers.google.com/workspace/calendar/api/guides/sync),
[push](https://developers.google.com/workspace/calendar/api/guides/push),
[version/ETag](https://developers.google.com/workspace/calendar/api/guides/version-resources),
[Find a time](https://support.google.com/calendar/answer/6294878?co=GENIE.Platform%3DDesktop&hl=EN),
[accessibility](https://support.google.com/calendar/answer/16271522?hl=EN).

Không nên sao chép:

- General-purpose personal calendar đầy đủ trong Phase 3.
- Appointment booking, room inventory và external sync trước khi lịch lớp ổn định.
- Cho frontend tự quản recurring instances hoặc gửi hàng trăm notification khi sửa series.

### 4.3 Microsoft Teams

Điểm đáng học:

- Work week là default hợp lý cho teacher; hỗ trợ time scale và multiple calendars.
- Scheduling Assistant đặt free/busy ngay trong luồng tạo meeting.
- `Show as`, required/optional attendee, room/location và timezone có semantics rõ.
- Có quyền xem, được mời và nhận notification là ba trạng thái khác nhau.
- Past event vẫn hữu ích nhờ recap, recording, transcript, note, task, chat và file.
- Graph tách recurrence pattern/range, query occurrence bằng bounded `calendarView`, có
  delta sync và timezone preference.
- Policy tạo private/channel meeting được admin kiểm soát độc lập.

Nguồn:
[Teams Calendar](https://support.microsoft.com/en-US/teams/meetings/get-started-with-the-microsoft-teams-calendar),
[schedule](https://support.microsoft.com/en-US/teams/meetings/schedule-a-meeting-in-microsoft-teams),
[multiple calendars](https://support.microsoft.com/en-US/teams/meetings/view-multiple-calendars-in-microsoft-teams),
[screen reader scheduling](https://support.microsoft.com/en-US/accessibility/teams/use-a-screen-reader-to-schedule-a-meeting-in-microsoft-teams),
[Graph overview](https://learn.microsoft.com/en-us/graph/outlook-calendar-concept-overview),
[calendarView](https://learn.microsoft.com/en-us/graph/api/calendar-list-calendarview?view=graph-rest-1.0),
[recurrence pattern](https://learn.microsoft.com/en-us/graph/api/resources/recurrencepattern?view=graph-rest-1.0),
[recurrence range](https://learn.microsoft.com/en-us/graph/api/resources/recurrencerange?view=graph-rest-1.0),
[delta](https://learn.microsoft.com/en-us/graph/delta-query-events),
[findMeetingTimes](https://learn.microsoft.com/en-us/graph/api/user-findmeetingtimes?view=graph-rest-1.0).

Không nên sao chép:

- Độ phức tạp Exchange/Outlook, work-location và organization resource ở MVP.
- Channel calendar semantics nếu TutorHub chưa có channel domain.

### 4.4 Zoom Calendar và Scheduler

Điểm đáng học:

- Quick editor và full editor; click-drag tạo block.
- CTA thay đổi theo vai trò/trạng thái: Start, Join, Edit, Copy, RSVP.
- Primary/secondary timezone hiển thị song song.
- Unsaved-change protection và lựa chọn gửi/cập nhật invite.
- Scheduler có buffer, minimum notice, date override và conflict calendar.
- Tách calendar event identity khỏi video meeting identity dù UI trình bày liền mạch.
- OAuth/calendar integration có reauthorization, ACL và sync failure mode rõ.

Nguồn:
[scheduled meetings](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060655),
[Calendar Client](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060791),
[recurring meetings](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0064248),
[Calendar settings](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0074552),
[Scheduler](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0058092),
[availability](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0080971),
[sharing](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0084752),
[developer Calendar](https://developers.zoom.us/docs/calendar/),
[accessibility](https://www.zoom.com/en/accessibility/).

Không nên sao chép:

- “No fixed time” recurring meeting.
- Public booking/CRM appointment vào lịch lớp Phase 3.
- Ba màn Home/Meeting/Calendar lặp dữ liệu nhưng khác behavior.

### 4.5 ClassIn

ClassIn công khai ít chi tiết calendar hơn ba nền tảng còn lại, nhưng cho mental model
giáo dục tốt nhất:

1. tạo Course/Lesson;
2. chuẩn bị bảng/tài liệu trước lớp;
3. Enter theo join window;
4. dạy/học;
5. evaluation;
6. playback/teaching report/learning data.

Admin có scheduling, roster, custom permissions, supervision, recording và analytics;
LTI page xác nhận upcoming/ongoing/finished, playback, learning analytics và calendar
synchronization.

Nguồn:
[teacher manual](https://www.classin.com/classin-teacher-manual-an-ultimate-guide-to-classin/),
[student manual](https://www.classin.com/classin-student-manual-a-step-by-step-guide-to-getting-started/),
[administrator](https://www.classin.com/administrator/),
[LTI](https://www.classin.com/lti/),
[pricing/features](https://www.classin.com/pricing/).

Không đủ bằng chứng công khai để khẳng định chi tiết recurrence, DST, free/busy,
keyboard accessibility, optimistic concurrency hoặc sync protocol của ClassIn. Không
hard-code join window 20/10 phút; TutorHub phải dùng policy cấu hình.

## 5. Nghiên cứu mã nguồn mở và quyết định build-vs-adopt

### 5.1 Ma trận

| Dự án | License/phạm vi | Điểm mạnh | Rủi ro | Kết luận |
| --- | --- | --- | --- | --- |
| [FullCalendar](https://github.com/fullcalendar/fullcalendar) | Standard MIT; Premium cần commercial license hoặc AGPL phù hợp | React tốt, Day/Week/Month/List, range fetch, drag/resize, constraints, a11y docs | Resource timeline Premium; phải kiểm tra keyboard, CSS, bundle và major mới | Lựa chọn số 1 cho renderer |
| [Schedule-X](https://github.com/schedule-x/schedule-x) | Core MIT; drag/drop và resize đã chuyển Premium ở dòng mới | API hiện đại, React, Temporal, responsive, component slots | Chức năng chỉnh lịch chuyên nghiệp tạo license/vendor dependency | Chỉ dùng comparator |
| [React Big Calendar](https://github.com/jquense/react-big-calendar) | MIT | React-native mental model, controlled state, DnD addon | Tự làm nhiều recurrence/timezone/a11y; localizer/CSS burden | Phương án dự phòng |
| [TOAST UI Calendar](https://github.com/nhn/tui.calendar) | MIT | Week/month, popup, drag/resize | Release/wrapper cũ, a11y/timezone/recurrence yếu; cần tắt usage statistics | Không chọn cho greenfield |
| [Cal.diy](https://github.com/calcom/cal.diy) | Community source/license phải kiểm tra tại version pin | Nguồn học availability, booking, buffer, lead time, provider adapter | Là cả hệ thống khác stack, không phải component | Chỉ đọc logic, không fork/embed |
| [rrule.js](https://github.com/jkbrzt/rrule) | BSD-3-Clause | RRULE/RDATE/EXDATE, range expansion, TZID | Quy ước JavaScript Date “floating/UTC” dễ gây DST bug | Preview client nếu thật cần |
| [rrule-go](https://github.com/teambition/rrule-go) | MIT | Go, API range, RFC-style rules | Maintenance và conformance phải audit; không tự động tin README | Candidate backend spike |

### 5.2 Quyết định đề xuất

Adopt:

- `@fullcalendar/react`;
- Standard plugins cần thiết: `daygrid`, `timegrid`, `list`, `interaction`;
- locale vi/en;
- lazy-loaded chỉ ở route Calendar.

Tự xây:

- `CalendarSurface` adapter của TutorHub;
- quick create, full editor, detail drawer;
- domain/API/schema;
- permission/tenant isolation;
- recurrence/exception/conflict;
- reminder/notification/outbox;
- attendance/LiveKit/classroom links;
- external provider sync.

Không làm:

- lưu FullCalendar Event Object vào database;
- cho FullCalendar tự là source of truth;
- import Premium package vô tình;
- dùng `rrule.js` làm authority phía server;
- fork Cal.diy hoặc Nextcloud Calendar vào monorepo.

Nguồn:
[FullCalendar license](https://fullcalendar.io/license),
[React integration](https://fullcalendar.io/docs/react),
[accessibility](https://fullcalendar.io/docs/accessibility),
[drag/resize](https://fullcalendar.io/docs/event-dragging-resizing),
[RRule plugin](https://fullcalendar.io/docs/rrule-plugin),
[Schedule-X v4](https://schedule-x.dev/blog/schedule-x-v4),
[Schedule-X recurrence](https://schedule-x.dev/docs/calendar/plugins/recurrence).

### 5.3 Spike bắt buộc trước khi thêm dependency

Spike là task riêng, không đưa code thử vào production route cho tới khi đạt:

1. React 19, TypeScript strict, Vite và StrictMode.
2. Lazy chunk; đo bundle trước/sau, không kéo Premium hoặc telemetry.
3. 500, 1.000 và 2.000 item trong visible range.
4. Locale Việt/Anh, tuần bắt đầu thứ Hai, 12/24 giờ.
5. Timed, all-day, multi-day, qua nửa đêm.
6. `Asia/Ho_Chi_Minh`, `America/New_York` gap/overlap và secondary timezone.
7. Drag/resize optimistic, API `409`, `revert()` và undo.
8. Keyboard-only, NVDA, Axe, focus order và live announcement.
9. Desktop/tablet/mobile; Agenda không phụ thuộc time-grid.
10. Theme bằng design token, không fork CSS lõi.
11. Dependency/license/security review và pin exact version.

Nếu một tiêu chí bắt buộc không đạt, thử React Big Calendar hoặc build surface giới hạn;
không hạ tiêu chuẩn accessibility để giữ thư viện.

## 6. Audit TutorHub V1

### 6.1 Phạm vi đã đọc

- `D:\Ban_sao_du_an\src\main\java\com\mycompany\tutorhub_enterprise\client\ScheduleTab.java`
- `...\client\CreateEventDialog.java`
- `...\models\CalendarEventModel.java`
- `...\models\CalendarTaskModel.java`
- `...\models\CalendarPollModel.java`
- `...\models\TutorScheduleModel.java`
- `...\server\dao\CalendarEventDAO.java`
- `...\server\dao\CalendarTaskDAO.java`
- `...\server\dao\CalendarPollDAO.java`
- `...\server\dao\TutorScheduleDAO.java`
- `D:\Ban_sao_du_an\src\main\resources\calendar-theme.css`
- `D:\Ban_sao_du_an\docs\Báo cáo đồ án\anh\tablich.jpg`
- `...\formkhaosatlich.jpg` và `...\emailkhaosatlich.jpg`

V1 chỉ được đọc làm nguồn nghiệp vụ; không sửa và không lấy secret/cấu hình sang V2.

### 6.2 Chức năng thực tế

`ScheduleTab` dài 1.427 dòng, dùng CalendarFX JavaFX nhúng trong Swing `JFXPanel`.
Nó có:

- toolbar Hôm nay, trước/sau, Ngày/Tuần/Tháng/Năm;
- all-day toggle và panel bên phải;
- tạo event, task và availability poll;
- event fields về guest, reminder, video, location, description, attachment;
- hiển thị event/task/poll;
- popover chi tiết, gửi lại email, xóa;
- drag/resize;
- email gửi nền bằng `SwingWorker`;
- poll heatmap/public-link concept thể hiện trong ảnh.

Các mốc code chính:

- khởi tạo: `ScheduleTab.java:99-118`;
- toolbar: `197-314`;
- reload: `356-435`;
- email: `466-550`;
- create flow: `552-935`;
- side panel: `942-1049`;
- popover: `1061-1232`.

### 6.3 Điểm tốt nên giữ ở mức ý tưởng

- Lịch là điểm hợp nhất buổi học, deadline/task và khảo sát lịch.
- Click/drag tạo nhanh, all-day, timed event và popover.
- Availability poll/heatmap hữu ích cho học bù hoặc office hour.
- Event card có join link, location, description, guest và file.
- Side panel gần với agenda/management view.
- Email được tách khỏi insert nên lỗi email không rollback event.
- Visual hierarchy của ảnh `tablich.jpg` sạch và gần mental model quen thuộc.

### 6.4 Phần chỉ là vỏ hoặc lỗi

- “Tuần” gọi `showMonthPage()`, nên nút chọn Tuần vẫn hiện month grid
  (`ScheduleTab.java:275-280`).
- Edit event/poll, “Tùy chọn khác”, “Mở Link” và “Xem Maps” thiếu handler.
- Reminder hiện trong form nhưng không đi vào model/save.
- Poll guest/reminder/location/video/file không được persist.
- Attachment event chỉ là absolute local path và DAO không insert.
- `repeatType`, class/student/parent/visibility là field chết; recurrence không tồn tại.
- Một Calendar object được tạo cho mỗi event; màu phụ thuộc thứ tự tải.
- Task insert không ghi `created_by` trong khi query dựa vào field này.
- Save đóng modal/reload cả khi insert thất bại; delete bỏ qua failure.
- Drag sang ngày khác tính ngày mới nhưng SQL chỉ đổi time, giữ DATE cũ.
- Poll web đẹp trong ảnh nhưng source web không nằm trong repo; chưa thể xác nhận end-to-end.

### 6.5 Rủi ro không được mang sang V2

- `currentTutorId = 2` hard-code (`ScheduleTab.java:88`).
- UI cho nhiều role nhưng query cùng tutor ID.
- Desktop JDBC trực tiếp; không API/policy authoritative.
- Query/update/delete không tenant, class membership hoặc version.
- `LocalDateTime`/`Timestamp` không UTC/IANA/DST.
- All-day là `00:00`–`23:59`, không phải date range exclusive-end.
- Guest/date-list/attachment lưu JSON string hoặc local path.
- Hard delete, không cancel/tombstone/audit/outbox.
- Jitsi link tự sinh, không token/lobby/policy.
- Full-history query không range/pagination; một reload chạy nhiều full-list query.
- DB chạy trên JavaFX thread; Swing/JavaFX thread confinement sai.
- Inline CSS, icon mạng ngoài, fixed width và không accessibility state.
- Không có calendar test, authorization test, timezone/DST test.

Kết luận: V1 là prototype ý tưởng tốt nhưng kiến trúc lịch không đạt production. Không
port model, DAO, CalendarFX hoặc threading; chỉ chuyển các user job đã được xác nhận.

## 7. Baseline và khoảng trống TutorHub V2

### 7.1 Nền đã đúng

- React + TypeScript strict + Vite, TanStack Query và design system.
- Go modular monolith; `classroom` sở hữu class policy/lifecycle.
- Tenant/class authorization và conceal foreign ID đã có.
- Class/user/tenant đã có IANA timezone.
- Transactional outbox, audit, request ID và typed Problem Details đã có nền.
- ADR-0017 chốt UTC instant + IANA timezone, DST validation, optimistic version,
  bounded range và session một lần.
- ADR-0018 chốt worker process riêng, PostgreSQL lease/fencing, at-least-once,
  idempotency, retry và dead-letter.

### 7.2 Chưa có

- route `/app/calendar` và navigation item;
- migration `class_sessions`;
- session OpenAPI/client/backend;
- calendar read model/range query;
- recurrence/exception semantics;
- conflict engine;
- worker runtime/reminder delivery;
- calendar UI và test.

`apps/web/src/App.test.tsx` hiện còn khẳng định link “Lịch” không xuất hiện. Vì vậy mọi
ảnh mockup hoặc i18n `nav.calendar` hiện không phải chức năng đã chạy.

## 8. Information architecture và trải nghiệm đề xuất

### 8.1 Route và URL state

Route chính:

```text
/app/calendar
/app/calendar?view=week&date=2026-07-23
/app/calendar?view=month&date=2026-07-01&class_ids=...
/app/calendar?view=agenda&from=2026-07-23
```

Nguyên tắc:

- view/date/filter ở URL để back/forward, bookmark và deep link hoạt động;
- timezone viewer lấy từ profile, không cho URL thay authorization;
- event detail dùng drawer và URL/deep link có ID opaque;
- filter ID lạ phải bị server conceal, không lộ tên lớp;
- đổi workspace xóa cache/range/filter tenant cũ rồi về Today.

### 8.2 Bố cục desktop

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ Hôm nay  ‹  ›  20–26 Tháng 7, 2026   [Tìm kiếm] [Ngày|Tuần|Tháng|Agenda] [+]│
├──────────────────┬───────────────────────────────────────────┬───────────────┤
│ Mini month       │ All-day / announcements                   │ Detail drawer │
│                  ├───────────────────────────────────────────┤ (khi chọn)    │
│ My schedule      │                                           │               │
│ ☑ Buổi học       │       Time grid / month grid / agenda     │ Class         │
│ ☑ Deadline       │                                           │ Time + zone   │
│ ☑ Exam           │                                           │ Teacher       │
│                  │                                           │ Prepare/Join  │
│ Lớp học          │                                           │ Reminder      │
│ ☑ Mạng máy tính  │                                           │ Recurrence    │
│ ☑ Toán 12        │                                           │ Files/recap   │
│ Timezone         │                                           │               │
└──────────────────┴───────────────────────────────────────────┴───────────────┘
```

Sidebar có thể collapse. Detail drawer không làm đổi kích thước grid trên màn hình nhỏ;
ở tablet/mobile nó thành bottom sheet hoặc page.

### 8.3 Toolbar

Bắt buộc:

- `Hôm nay`;
- previous/next;
- range title được đọc bởi screen reader;
- view selector;
- search/filter;
- `Tạo buổi học` nếu có capability;
- timezone badge khi viewer/event khác class timezone.

Không lặp toolbar nội bộ của thư viện như V1. Chỉ có một nguồn navigation.

### 8.4 Views

#### Day

- timeline chi tiết, all-day row, current-time indicator;
- phù hợp ngày nhiều buổi;
- click/drag range để tạo;
- overlapping event có layout rõ và không che CTA.

#### Work week

- Thứ Hai–Thứ Sáu mặc định cho teacher; cho phép cấu hình ngày làm việc.
- Không loại cuối tuần khỏi dữ liệu; chỉ đổi cách hiển thị.

#### Week

- View chính để dạy/học; thể hiện overlap, break và giờ ngoài working hours.

#### Month

- Dùng chip compact; overflow thành `+N lịch khác`;
- event spanning nhiều ngày giữ continuity;
- không nhét mô tả/attendee vào cell;
- chọn ngày mở Agenda của ngày đó.

#### Agenda

- semantic list nhóm theo ngày;
- default mobile và fallback accessibility;
- infinite/bounded pagination rõ;
- mỗi item có time, class, title, status, timezone và CTA.

#### Year

- Không nằm trong exit gate Phase 3. Có thể là heatmap/navigator về sau; tránh làm một
  view tốn công nhưng ít hỗ trợ hành động học tập.

### 8.5 Quick create

Mở bằng:

- click slot;
- drag time range;
- nút `+`;
- keyboard shortcut;
- từ class detail.

Chỉ gồm:

- class;
- title mặc định từ class;
- start/end;
- timezone;
- one-time/repeat cơ bản;
- nút Save và `Tùy chọn khác`.

Quick create không có raw attendee email. Participant lấy từ roster authoritative.

### 8.6 Full editor

Nhóm field theo progressive disclosure:

1. **Cơ bản:** class, title, description.
2. **Thời gian:** date/time, duration, timezone, repeat, preview occurrences.
3. **Người dạy/người học:** roster-derived participants, co-teacher, visibility.
4. **Lớp trực tuyến:** join policy, prepare window, device preflight, room link read-only.
5. **Tài liệu:** link tới Class Files; không upload binary qua form Calendar.
6. **Nhắc lịch:** default + per-user preference.
7. **Nâng cao:** location, conflict override reason, audit-visible note.

Form có:

- unsaved-change guard;
- validation summary + inline errors;
- Save draft/schedule theo policy;
- lựa chọn notification khi reschedule/cancel;
- recurrence scope prompt;
- preview timezone và occurrence bị ảnh hưởng.

### 8.7 Detail drawer và CTA theo lifecycle

| Trạng thái | Teacher | Student | Admin |
| --- | --- | --- | --- |
| Scheduled | Edit, Prepare, Cancel | Xem chi tiết/countdown | Edit/override/audit |
| Preparing | Prepare, kiểm tra thiết bị | Xem tài liệu | Monitor |
| Joinable | Start/Join qua prejoin | Join qua prejoin | Join/observe nếu policy |
| Live | Rejoin/manage | Join/rejoin | Supervise nếu policy |
| Ended/processing | Attendance/evaluation | Chờ recap | Audit |
| Ready | Recording/report/files | Recording/report/files | Analytics |
| Cancelled | Xem lý do/history | Xem thay đổi | Audit/restore không mặc định |

P3-01/P3-02 chỉ có public lifecycle `scheduled/cancelled`; các CTA media sâu vẫn
deferred theo ADR-0017. UI có thể reserve layout nhưng không giả chức năng.

### 8.8 Drag, resize và undo

- Chỉ hiện affordance nếu server capability cho phép.
- Keyboard phải có action tương đương `Đổi thời gian`.
- Drag series luôn hỏi scope.
- Client cập nhật optimistic nhưng giữ snapshot.
- API `409` stale/conflict gọi `revert()` và mở compare/retry.
- Thành công có toast với `Hoàn tác`; undo là mutation mới có expected version, không
  đơn thuần đổi state local.
- Live region đọc “Đã chuyển …” hoặc lý do revert.
- Không cho kéo event đang live/ended/cancelled.

### 8.9 Trạng thái UI bắt buộc

| State | Hành vi |
| --- | --- |
| Initial loading | Skeleton giữ layout toolbar/sidebar/grid |
| Range loading | Giữ dữ liệu cũ, progress nhẹ, không nhấp nháy trắng |
| Empty | Giải thích không có lịch; CTA theo quyền |
| Filtered empty | Nêu filter đang ẩn dữ liệu và nút reset |
| Error | Typed message, request ID, retry đúng range |
| Forbidden | Không render dữ liệu cache cũ |
| Offline | Read cached range, cấm mutation hoặc xếp draft đúng policy |
| Stale | Badge “Có thay đổi mới”, refetch không phá thao tác đang nhập |
| Partial/degraded | Tách lỗi session/reminder/file, không hỏng toàn page |
| Conflict | Hiển thị resource/time bị trùng, không lộ title riêng tư |
| Archived class | Read history; không tạo/sửa |

## 9. Phạm vi chức năng theo ưu tiên

### 9.1 Phase 3 bắt buộc

- top-level Calendar route và navigation;
- Day/Work week/Week/Month/Agenda;
- mini-calendar và class/type/status filters;
- one-time class session CRUD/cancel;
- quick create, full editor, detail drawer;
- recurrence cơ bản daily/weekly/monthly, end date/count;
- edit/cancel one/following/all;
- class/teacher conflict ở backend, student warning;
- UTC/IANA/DST semantics;
- role-aware action và prejoin link;
- in-app reminder qua worker/outbox;
- URL state, loading/empty/error/forbidden/offline/stale;
- vi/en, keyboard, screen reader, responsive;
- audit/version/idempotency/tenant isolation;
- unit/integration/E2E/a11y/performance acceptance.

### 9.2 Nên có ngay sau core nếu còn ngân sách Phase 3

- secondary timezone;
- working hours;
- admin batch schedule/import template;
- availability suggestion nội bộ;
- read-only ICS export;
- density preference;
- event search trong visible/near-term range;
- recurrence preview và conflict heatmap.

### 9.3 Phase sau

- Google/Microsoft/Zoom two-way sync;
- public booking page;
- delegated calendar sharing;
- room/campus/resource inventory;
- parent calendar;
- cross-tenant invitation;
- AI suggested time/automatic reschedule;
- advanced attendance/learning analytics;
- native desktop offline mutation.

### 9.4 Non-goal

- Không xây đối thủ đầy đủ của Exchange/Google Calendar trong Phase 3.
- Không cho student tạo arbitrary organization event.
- Không lưu token calendar provider trong browser/localStorage.
- Không để frontend tự cấp LiveKit room/token.
- Không gửi email/push production khi provider/runbook chưa được chốt.
- Không hỗ trợ recurrence vô hạn ở private alpha; yêu cầu end date hoặc bounded count.

## 10. Domain model đề xuất

### 10.1 Boundary

```mermaid
flowchart LR
    UI["Calendar UI"] --> CQ["Calendar query/read model"]
    UI --> CS["Classroom session commands"]
    CS --> DB[(PostgreSQL)]
    CQ --> DB
    CS --> OB["Transactional outbox"]
    OB --> WK["Worker"]
    WK --> NF["Notification/reminder projection"]
    CS --> LK["Live room policy<br/>Phase 4"]
    AS["Assignment/Exam domains<br/>Phase 6"] --> CQ
```

- `classroom` sở hữu class session mutation và policy.
- `calendar` là application/read-model layer tổng hợp item từ domain, không sở hữu mọi
  lifecycle.
- `notification` và worker chỉ tiêu thụ event đã commit.
- Live room có identity riêng; session tham chiếu room khi cần, không đồng nhất hai ID.
- Assignment/exam về sau implement projection contract, không insert trực tiếp vào bảng
  session.

### 10.2 Aggregate và projection

#### ClassSession

Occurrence một lần của P3-01:

- `id`, `tenant_id`, `class_id`;
- title/description allowlist;
- `starts_at`, `ends_at` instant UTC;
- `timezone` IANA;
- status, version;
- creator/updater, cancelled metadata;
- audit/outbox metadata;
- optional `series_id`, immutable `occurrence_key` và original civil tuple khi P3-02
  materialize lifecycle instance.

#### ClassSessionSeries

Master recurrence:

- series identity/tenant/class;
- local start date/time và IANA timezone;
- duration;
- normalized RRULE subset;
- end date hoặc count;
- lifecycle/version/sequence;
- default metadata;
- `split_from_series_id` khi edit following.

#### ClassSessionException

Key bởi `(series_id, occurrence_key)`:

- cancel tombstone hoặc override;
- giữ original civil tuple gồm local datetime, IANA timezone và overlap choice/offset;
- override start/end/timezone/title/teacher nếu cho phép;
- version và reason;
- không xóa identity lịch sử.

#### ClassSessionOccurrence

Chỉ materialize khi occurrence cần durable lifecycle riêng như attendance, room,
recording hoặc audit detail. Occurrence chưa materialize được expand trong bounded range
với stable opaque key sinh từ canonical original civil tuple.

#### CalendarItem

Read projection typed:

```text
id, source_type, source_id, occurrence_key
title, starts_at, ends_at, all_day
display_timezone, class_id, class_title
status, color_token, icon
viewer_capabilities
primary_action
recurrence_summary
version
```

`CalendarItem` không nhận mutation chung; client gọi endpoint của source domain.

### 10.3 Vì sao không dùng một bảng `calendar_events`

Một generic table như V1 làm mờ:

- ai sở hữu lifecycle;
- quyền class/assignment/exam;
- attendance/room/recording;
- validation đặc thù;
- retention và privacy.

Read model chung vẫn cho UX hợp nhất mà không phá domain boundary.

## 11. Timezone, civil time và all-day

### 11.1 Timed session

Theo ADR-0017:

- PostgreSQL `timestamptz` cho instant;
- IANA timezone riêng để giữ civil-time intent;
- request mutation có RFC 3339 offset rõ và timezone;
- server kiểm tra zone, offset và local round-trip;
- DST gap bị từ chối;
- DST overlap cần offset disambiguation.

PostgreSQL lưu `timestamptz` nội bộ theo UTC và không giữ original timezone, vì vậy
timezone riêng là bắt buộc.
[PostgreSQL datetime](https://www.postgresql.org/docs/current/datatype-datetime.html)

### 11.2 Recurring session

Series phải giữ:

- `dtstart_local`;
- IANA timezone;
- duration;
- RRULE;
- start-of-week/locale semantics nếu rule cần.

Mỗi occurrence được resolve bằng timezone database tại thời điểm expand. Không cộng
`7 * 24h` trên UTC cho lịch “9:00 mỗi thứ Hai”, vì sẽ lệch wall time qua DST.

### 11.3 DST policy đề xuất cho ADR-0019

- Không silently tạo hoặc bỏ một buổi học.
- Form preview toàn bộ occurrence trong bounded series/term.
- Gap occurrence yêu cầu organizer chọn exception: chuyển tới thời điểm hợp lệ hoặc hủy.
- Overlap occurrence yêu cầu earlier/later offset; lưu lựa chọn trong exception.
- Tzdata/version thay đổi phải có regression suite và cảnh báo nếu projection đổi.

RFC 5545 là chuẩn interoperability, nhưng UX giáo dục được phép chặt hơn để không vô
tình bỏ lớp. Nguồn chuẩn:
[RFC 5545](https://datatracker.ietf.org/doc/html/rfc5545).

### 11.4 All-day

Class session Phase 3 là timed. Khi calendar nhận holiday/announcement:

- lưu `start_date`/`end_date` dạng `DATE`;
- `end_date` exclusive;
- không giả lập `00:00–23:59`;
- không chuyển all-day theo timezone thành giờ khác.

## 12. Recurrence và exception semantics

### 12.1 Subset Phase 3

- Daily.
- Weekly với một hoặc nhiều weekday.
- Monthly theo ngày hoặc nth weekday nếu spike/conformance đạt.
- Interval.
- End by date hoặc occurrence count.
- Không “never ends” trong private alpha.
- Không cho nhập raw RRULE text từ UI.

Server serialize canonical RRULE để tương thích ICS, nhưng API form dùng schema typed.

### 12.2 Scope sửa

#### Chỉ buổi này

- tạo/update exception keyed bằng immutable `occurrence_key`;
- giữ series master;
- occurrence key không đổi;
- nếu đã có attendance/room, update durable occurrence với version.

#### Buổi này và các buổi sau

- cắt series cũ trước original start;
- tạo series mới với metadata/rule mới;
- link `split_from_series_id`;
- giữ past history và audit.

#### Toàn bộ chuỗi

- update master cho future virtual occurrences;
- occurrence đã live/ended/attendance không bị viết lại;
- UI phải nói rõ phạm vi thực tế nếu series có history.

#### Hủy

- hủy một occurrence tạo tombstone;
- hủy series không hard delete;
- reminder pending bị supersede async;
- student vẫn có thể thấy cancelled item trong khoảng retention để hiểu thay đổi.

### 12.3 Stable identity

Không dùng array index, expanded UTC instant hoặc timestamp hiển thị làm ID. Canonical
identity đề xuất cho ADR-0019 gồm:

- series ID;
- original local civil datetime;
- original IANA timezone;
- overlap choice/original UTC offset nếu civil time bị lặp;
- optional durable occurrence ID.

Server trả `occurrence_key` opaque, có thể sinh deterministic UUID từ tuple trên. Exception
và durable occurrence phải persist cả key lẫn tuple; `starts_at` UTC chỉ là kết quả resolve,
không phải identity. Thay đổi timezone/start/rule làm đổi tập occurrence phải split/tạo
series revision tại effective boundary thay vì âm thầm tái định danh occurrence cũ.

Đây là khóa liên kết attendance, recording, reminder, audit và external provider mapping,
kể cả khi tzdata về sau thay đổi cách civil time được ánh xạ sang instant.

### 12.4 Expansion

- Chỉ expand trong query range bounded.
- Có hard cap item/range/series.
- Cache theo tenant/viewer/range/filter/version.
- Không gọi `all()` trên recurrence không bounded.
- Server authority; client chỉ preview bằng cùng test vector.

## 13. Conflict và availability

### 13.1 Phân loại

| Conflict | Mặc định | Ghi chú |
| --- | --- | --- |
| Cùng class có hai session trùng | Hard block | Trừ admin override có reason nếu policy cho |
| Teacher/co-teacher bị trùng | Hard block hoặc admin override | Phải kiểm tra participant authoritative |
| Student có hai lớp trùng | Soft warning | Không lộ title lớp người khác |
| Room/resource trùng | Hard block khi resource domain có | Phase sau |
| External calendar busy | Warning | Chỉ sau provider sync |
| Ngoài working hours | Warning | Preference, không phải authorization |
| Quá sát giờ/minimum notice | Policy block/warning | Theo tenant/class capability |

### 13.2 Privacy

Free/busy projection chỉ trả:

- busy/tentative/free;
- range;
- resource opaque;
- override capability.

Không trả title, class, attendee hoặc description nếu viewer không có quyền.

### 13.3 Race condition

- Frontend preview không phải authority.
- Backend recheck trong transaction.
- Mutation có expected version và idempotency key.
- Lock/order theo tenant + class + teacher resource để hai request đồng thời không cùng
  vượt qua check.
- Nếu dùng PostgreSQL exclusion/range index, phải chứng minh hoạt động với recurrence
  virtual occurrence; không ép schema phức tạp trước spike.

### 13.4 Availability poll

Ý tưởng V1 đáng giữ nhưng không nằm trong core P3-02. Thiết kế tương lai:

- poll thuộc tenant/class;
- proposed date range và slot granularity;
- participant từ roster hoặc invite token có hạn;
- vote normalize theo slot, không JSON string;
- heatmap chỉ hiện aggregate nếu viewer không có quyền xem cá nhân;
- finalize poll tạo session qua command chuẩn, vẫn conflict-check;
- token không nằm trong query log, raw token chỉ trả một lần.

## 14. Authorization và privacy

### 14.1 Quyền

- `session.schedule`: create/update/cancel theo shared policy.
- Read dùng authoritative class viewer projection.
- Org admin, class owner/co-teacher và teacher đủ capability mới mutate.
- Active student/TA xem; capability chi tiết do server trả.
- Draft/archived class không nhận schedule mutation; history vẫn đọc đúng policy.
- Foreign tenant/class/session/series ID conceal `404`.

### 14.2 Viewer capabilities

Client không suy từ role. API trả tối thiểu:

```text
can_view
can_edit
can_cancel
can_reschedule
can_prepare
can_join
can_start
can_override_conflict
can_view_participants
```

### 14.3 Dữ liệu nhạy cảm

- Không log attendee, description, join token, signed URL hoặc notification body.
- Search/index không tạo snippet vượt quyền.
- Calendar cache key chứa tenant + user + filter/range.
- Workspace switch/archive/logout hủy query và xóa cache tenant cũ.
- Public ICS URL nếu có phải là capability có thể rotate/revoke; không dùng access token.
- External OAuth token chỉ ở backend secret store, không localStorage.

## 15. API đề xuất

### 15.1 P3-01 source-domain API

Giữ theo backlog:

```http
GET  /api/v1/classes/{class_id}/sessions?from=&to=&cursor=
POST /api/v1/classes/{class_id}/sessions
GET  /api/v1/classes/{class_id}/sessions/{session_id}
PATCH /api/v1/classes/{class_id}/sessions/{session_id}
POST /api/v1/classes/{class_id}/sessions/{session_id}/cancel
```

### 15.2 P3-02 aggregate read API

```http
GET /api/v1/calendar/items
  ?from=RFC3339
  &to=RFC3339
  &types=class_session,deadline,exam
  &class_ids=...
  &statuses=...
  &cursor=...
```

Quy tắc:

- `from/to` bắt buộc và có maximum span.
- Kết quả occurrence trong range, không trả master vô hạn.
- Stable order `(starts_at, source_type, occurrence_key)`.
- Opaque cursor bind tenant/user/range/filter.
- ETag/version cho range hoặc item.
- `Cache-Control: private, no-store` nếu session data nhạy cảm; nếu cache client thì
  không để shared CDN cache.

### 15.3 Recurrence command API

Contract nên typed, ví dụ:

```json
{
  "local_start": "2026-08-03T09:00:00",
  "timezone": "Asia/Ho_Chi_Minh",
  "duration_minutes": 90,
  "recurrence": {
    "frequency": "weekly",
    "interval": 1,
    "weekdays": ["MO", "WE"],
    "ends": { "type": "on_date", "date": "2026-12-31" }
  },
  "expected_version": 3
}
```

Occurrence mutation phải nêu:

```json
{
  "scope": "this_occurrence",
  "occurrence_key": "opaque-occurrence-key",
  "expected_version": 3,
  "idempotency_key": "..."
}
```

### 15.4 Problem types

- invalid time range/duration;
- invalid IANA timezone;
- offset mismatch;
- DST gap/overlap ambiguity;
- recurrence rule/range too large;
- occurrence not in series;
- stale version;
- schedule conflict;
- archived/draft class;
- permission/feature/quota;
- idempotency conflict.

Problem response không được lộ resource khác tenant. Conflict detail chỉ gồm data viewer
được phép thấy.

## 16. Backend và persistence

### 16.1 Module

Trong `services/core-api/internal/modules/classroom`:

- model/validation session;
- command service;
- PostgreSQL repository tenant-scoped;
- policy adapter;
- audit/outbox;
- recurrence engine adapter sau ADR-0019.

Calendar aggregate query có thể bắt đầu là application service cùng modular monolith,
không cần microservice. Chỉ tách package rõ:

```text
internal/modules/classroom/session_*.go
internal/modules/calendar/query_*.go
internal/modules/notification/...
cmd/worker
```

Tên cuối cùng theo convention repo khi implementation; không đổi public package hiện có
nếu không cần.

### 16.2 Migration dự kiến

P3-01:

- `000014_class_sessions`;
- tenant/class/time indexes;
- status/version/check constraints;
- forward/down path trên disposable branch.

P3-02 sau ADR:

- series table;
- exception table;
- optional occurrence materialization;
- dedupe/unique keys;
- range indexes dựa trên query plan thực tế.

Không hard-code runtime role theo môi trường. Migration role/grant theo runbook hiện tại.

### 16.3 Transaction

Một mutation thành công:

1. load class/viewer authoritative;
2. validate lifecycle/capability/timezone/recurrence;
3. acquire conflict locks;
4. recheck conflict;
5. insert/update aggregate với expected version;
6. append audit allowlist;
7. insert outbox event;
8. commit;
9. worker xử lý reminder/notification.

Không gửi email, tạo LiveKit token hoặc gọi provider trước commit.

### 16.4 Event contract

Ví dụ:

```text
class_session.scheduled.v1
class_session.rescheduled.v1
class_session.cancelled.v1
class_session.series_split.v1
class_session.occurrence_overridden.v1
```

Payload chỉ có ID, tenant/class context tối thiểu, timestamps/version và actor ID nếu cần;
không chứa full description, raw attendee email, token hoặc signed URL.

### 16.5 Idempotency

- Create/reschedule/cancel nhận idempotency key khi retry có thể tự động.
- Store fingerprint của normalized command và response identity.
- Cùng key + khác payload trả conflict.
- Worker effect dedupe theo source outbox event ID hoặc stable reminder key.

## 17. Frontend architecture

### 17.1 Route boundary

```text
apps/web/src/pages/CalendarPage.tsx
apps/web/src/features/calendar/
  api.ts
  queries.ts
  model.ts
  CalendarSurface.tsx
  FullCalendarSurface.tsx
  CalendarToolbar.tsx
  CalendarSidebar.tsx
  CalendarAgenda.tsx
  SessionQuickCreate.tsx
  SessionEditor.tsx
  SessionDetailDrawer.tsx
  recurrence/
  accessibility/
```

Đây là định hướng, không bắt buộc tạo đúng mọi file trước khi code cần đến. Tránh một
component monolith kiểu V1.

### 17.2 Adapter renderer

`CalendarSurface` nhận TutorHub `CalendarItemViewModel` và phát semantic commands:

```text
onRangeChange
onSelectRange
onOpenItem
onMoveItem
onResizeItem
```

FullCalendar-specific type không đi vào app service, API client hoặc form domain.

### 17.3 Data fetching

- TanStack Query key: tenant + viewer timezone + range + filters.
- Prefetch previous/next range có budget.
- Abort old request khi chuyển workspace/range nhanh.
- `keepPreviousData`/placeholder để grid không flash.
- Mutation patch exact cached item, invalidate affected ranges.
- Recurrence mutation invalidates all intersecting cached ranges.
- Không fetch full history.

### 17.4 Date/time

- Format bằng `Intl.DateTimeFormat` và Unicode locale semantics.
- Không tự parse ISO bằng string slicing.
- Temporal có thể dùng qua polyfill trong isolated adapter nếu spike chứng minh lợi ích;
  không dựa vào native Temporal khi browser support chưa đủ.
- Backend test vector là authority; client preview phải đối chiếu cùng fixtures.

Nguồn:
[Unicode LDML dates](https://unicode.org/reports/tr35/tr35-dates.html),
[Temporal warning](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Temporal/ZonedDateTime/toPlainDateTime).

### 17.5 Color và visual

- Color theo stable class/type token, không theo thứ tự load như V1.
- Mỗi type/status có icon/text/shape, không chỉ màu.
- Contrast đạt AA ở normal, hover, selected, disabled.
- Cancelled dùng text + strike/icon; không chỉ giảm opacity.
- Current time, today và selection không tranh chấp màu.
- Respect `prefers-reduced-motion` và high-contrast/forced-colors.

## 18. Worker, notification và reminder

### 18.1 Flow

```mermaid
sequenceDiagram
    participant U as User
    participant API as Core API
    participant DB as PostgreSQL
    participant W as Worker
    participant N as Notification

    U->>API: Schedule/reschedule/cancel
    API->>DB: Business row + audit + outbox
    DB-->>API: Commit
    API-->>U: Success
    W->>DB: Claim with lease/fencing
    W->>N: Upsert/supersede reminder
    N-->>W: Idempotent result
    W->>DB: Ack/retry/dead-letter
```

### 18.2 Reminder model

- default theo tenant/class/event type;
- per-user override;
- in-app Phase 3;
- email/push adapter về sau;
- stable key `(user, occurrence, channel, offset, schedule_version)`;
- reschedule/cancel supersede reminder cũ;
- preference được áp dụng lúc delivery;
- late policy bounded: gửi trễ có ích hay bỏ + audit/metric;
- không gửi hàng trăm notification khi sửa series; batch/digest theo policy.

### 18.3 Operational requirements

- backlog age, due lag, success/retry/dead-letter;
- lease reclaim/duplicate delivery tests;
- runbook inspect/replay có authorization và audit;
- Render Free web spin-down không được xem là durable worker;
- provider/deployment shape phải được chứng minh ở P3-03.

## 19. External calendar interoperability

### 19.1 Phase 3

- Không two-way sync.
- Có thể thêm read-only ICS export sau core nếu security review đạt.
- Mọi dữ liệu nội bộ vẫn do Core API làm source of truth.

### 19.2 Thiết kế để không chặn tương lai

Provider mapping cần:

- provider/account/calendar/event ID;
- internal source + occurrence identity;
- ETag/version;
- sync cursor/delta token;
- webhook channel/subscription expiry;
- tombstone;
- last successful sync/error/retry state.

### 19.3 Sync algorithm tương lai

- full sync ban đầu;
- incremental sync giữ nguyên query qua pagination;
- chỉ commit cursor sau trang cuối;
- webhook chỉ là signal, luôn delta fetch;
- invalid cursor/HTTP 410 → controlled full resync;
- renew subscription trước expiry;
- periodic reconciliation để phục hồi missed notification;
- conflict policy explicit: source priority, compare version, manual resolution.

Không lưu provider token trong frontend, log hoặc outbox payload.

## 20. Accessibility và responsive

### 20.1 Chuẩn

- WCAG 2.2 AA.
- WAI-ARIA grid chỉ dùng nếu thực hiện đầy đủ keyboard/focus semantics.
- Agenda semantic list là đường sử dụng hạng nhất, không phải fallback nghèo tính năng.

Nguồn:
[WCAG 2.2](https://www.w3.org/TR/WCAG22/),
[ARIA grid](https://www.w3.org/WAI/ARIA/apg/patterns/grid/),
[date picker dialog](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/examples/datepicker-dialog/),
[keyboard interface](https://www.w3.org/WAI/ARIA/apg/practices/keyboard-interface/).

### 20.2 Keyboard baseline

- `T`: Today.
- Arrow/PageUp/PageDown: navigation có hướng dẫn và không cướp phím ngoài grid.
- `D/W/M/A`: đổi view khi shortcut setting bật.
- `C` hoặc nút accessible: create.
- Enter/Space: mở item.
- Context action cho reschedule thay drag.
- Escape: đóng popover/drawer và trả focus đúng trigger.
- Delete/cancel luôn confirmation theo lifecycle.

Shortcut phải discoverable và không xung đột browser/screen reader.

### 20.3 Focus và announcement

- roving tabindex trong grid nếu dùng;
- focus visible rõ;
- modal/drawer trap đúng và restore focus;
- validation focus summary/field đầu lỗi;
- live region cho range loaded, drag result, conflict và save;
- event accessible name gồm title, time, class, status;
- không đọc hàng nghìn cell/event khi mở page.

### 20.4 Responsive

| Width | Hành vi |
| --- | --- |
| Desktop | Sidebar + grid + optional detail drawer |
| Tablet | Sidebar collapse; drawer overlay; Week/3-day |
| Mobile | Agenda default; Day/3-day; bottom-sheet detail; floating create nếu có quyền |

Không thu nhỏ month grid desktop thành chữ không đọc được.

### 20.5 Drag alternative và target size

WCAG 2.2 yêu cầu alternative cho dragging và target size phù hợp. Mọi drag/resize đều có
form/action bằng click/keyboard. Touch dùng long-press chỉ là enhancement.

## 21. Performance, cache và offline

### 21.1 Budget ban đầu để đo

- Route Calendar lazy load; không làm Home bundle tăng lớn khi chưa mở Calendar.
- Visible range response bounded; hard cap occurrence/item.
- Không query hơn range cần cho view + small prefetch window.
- Không N+1 class/participant/reminder.
- Measure render/query p50/p95 với 500–2.000 item.
- Month overflow collapse; agenda virtualize nếu measurement cần.

Các ngưỡng cuối cùng phải chốt sau spike, không lấy con số giả làm SLO.

### 21.2 Cache

- client cache tenant/user/range/filter scoped;
- no shared CDN cache cho private data;
- mutation invalidate đúng intersecting ranges;
- cancel logout/workspace-switch request;
- không persist sensitive detail lâu dài.

### 21.3 Offline

Phase 3:

- read-only cached agenda/range;
- banner rõ timestamp “Cập nhật lần cuối”;
- tạo/sửa bị disable hoặc chỉ lưu local draft không nhạy cảm;
- không khẳng định đã schedule khi server chưa commit.

Offline mutation chỉ làm khi có encrypted/policy-safe queue, idempotency, conflict
resolution và replay UX đầy đủ.

## 22. Testing và quality gates

### 22.1 Unit

- validation start/end/duration;
- timezone offset round-trip;
- recurrence parse/normalize/expand;
- exception/split semantics;
- capability-to-CTA mapping;
- range/filter/query key;
- reminder dedupe/supersede.

### 22.2 Golden timezone/recurrence

Ít nhất:

- `Asia/Ho_Chi_Minh`;
- `UTC`;
- `America/New_York` spring gap/fall overlap;
- `Europe/London`;
- month/year boundary;
- leap day;
- month “31” behavior;
- timezone switch;
- series edit one/following/all;
- tzdata regression fixtures.

### 22.3 Property/conformance

- generated occurrence luôn nằm trong bounded range;
- no duplicate occurrence key;
- exception override đúng một original occurrence;
- split union không mất/trùng future occurrence;
- expansion có cap và không loop vô hạn;
- RFC 5545 examples phù hợp subset hỗ trợ.

### 22.4 PostgreSQL integration

- tenant/class isolation;
- concurrent create/update/conflict;
- expected version;
- idempotency;
- audit + outbox atomic;
- cancel replay no-op;
- range indexes/query plan;
- worker lease/reclaim/dead-letter.

### 22.5 Authorization/security

- org admin/teacher/owner/co-teacher/student/TA/guest matrix;
- draft/active/archived class;
- foreign tenant/class/session/series/occurrence ID;
- cache after workspace switch;
- conflict detail privacy;
- no token/PII in log/audit/outbox;
- rate/quota abuse với range và recurrence.

### 22.6 Web

- component tests cho states/form/drawer/agenda;
- Playwright teacher create/reschedule/cancel và student view/join;
- recurrence scope and stale `409` revert;
- offline/read-cache;
- vi/en;
- Axe/NVDA/keyboard;
- responsive/visual regression;
- bundle and dependency guard.

### 22.7 Staging acceptance

- migration up/down/up trên disposable Neon branch;
- deploy exact commit/image;
- teacher/student/admin roles;
- cross-timezone reload;
- worker paused/failing mà schedule vẫn commit;
- reminder retry không duplicate;
- rollback application giữ schema compatible;
- public health/readiness và same-origin proxy.

## 23. Observability

Metrics bounded:

- calendar range request count/duration/result size;
- recurrence expansion duration/count/rejection;
- schedule mutation outcome/problem type;
- conflict detected/override;
- stale version;
- reminder due lag/success/retry/dead-letter;
- worker backlog age;
- external sync metrics chỉ khi provider được thêm.

Không dùng user/class/event ID làm metric label. Log correlation bằng request/event ID;
PII và nội dung lớp bị redacted.

## 24. Delivery plan calendar-first

### Gate C0 — Báo cáo và quyết định

- [x] Research Google/Teams/Zoom/ClassIn.
- [x] Audit TutorHub V1.
- [x] So sánh OSS/build-vs-adopt.
- [x] Product/UX/backend/security/test design.
- [ ] Owner review báo cáo.
- [ ] Mở ADR-0019 ở trạng thái `PROPOSED`, ghi alternatives và tiêu chí spike.
- [ ] Dependency decision sau FullCalendar spike.

### Gate C1 — Technical spike

- FullCalendar Standard prototype không nối production API.
- A11y, performance, bundle, timezone, drag/revert.
- Go recurrence candidate conformance/maintenance spike.
- Cập nhật và chấp nhận ADR-0019 từ bằng chứng spike.
- Chốt component/recurrence dependency hoặc phương án tự xây; chưa code production
  recurrence khi ADR vẫn `PROPOSED`.

### P3-01 — One-time session vertical slice

- migration/OpenAPI/generated client;
- shared policy/backend/audit/outbox;
- class detail minimal UI;
- timezone/DST/version/idempotency tests;
- staging teacher/student acceptance.

### P3-02A — Professional calendar shell

- top-level route/navigation;
- Day/Work week/Week/Month/Agenda;
- range query/filter/URL state;
- quick create/detail/full editor;
- responsive/a11y/state coverage.

### P3-02B — Recurrence và conflict

- series/exception migration/contract;
- one/following/all;
- backend expansion/conflict;
- drag/resize/revert/undo;
- golden/property/integration/E2E tests.

### P3-03/P3-04/P3-05 — Worker, notification, reminder

- worker production shape;
- in-app projection/preference;
- reminder materialization/delivery/supersede;
- staging failure/retry/dead-letter acceptance.

### Sau calendar core

Quay lại P3-06 đến P3-14 theo backlog. Home dùng Calendar read model cho “sắp tới”,
không tự viết query/session semantics thứ hai.

## 25. Quyết định cần ADR-0019

Trước P3-02B phải chốt:

1. schema series/exception/occurrence materialization;
2. recurrence subset và giới hạn;
3. DST gap/overlap cho recurrence;
4. edit one/following/all và history đã live;
5. stable occurrence identity;
6. hard/soft conflict và admin override;
7. recurrence engine Go sau conformance spike;
8. FullCalendar dependency sau UI spike;
9. all-day scope;
10. ICS/export boundary.

Không đổi ADR-0017: P3-01 vẫn session một lần và `classroom` vẫn sở hữu mutation.

## 26. Definition of Done để gọi là “tab Lịch chuyên nghiệp”

Chỉ được dùng cụm này khi:

- route thật, không phải placeholder;
- Day/Work week/Week/Month/Agenda đạt;
- create/edit/cancel/reschedule one-time và recurrence đúng;
- timezone/DST và occurrence exception test xanh;
- backend conflict authoritative;
- role-aware CTA và tenant isolation;
- reminder async idempotent;
- loading/empty/error/forbidden/offline/stale đầy đủ;
- keyboard/screen reader/mobile đạt;
- drag có alternative/revert/undo;
- performance/bundle budget đạt bằng số đo;
- audit/outbox/log redaction đạt;
- staging teacher/student/admin acceptance và rollback đạt;
- docs/backlog/state được cập nhật.

Một lưới lịch có event nhưng thiếu các điều kiện trên chỉ được gọi là calendar foundation,
không phải lịch production.

## 27. Danh mục nguồn chuẩn

### Standards

- [RFC 5545 iCalendar](https://datatracker.ietf.org/doc/html/rfc5545)
- [PostgreSQL datetime](https://www.postgresql.org/docs/current/datatype-datetime.html)
- [Unicode Date/Time LDML](https://unicode.org/reports/tr35/tr35-dates.html)
- [WCAG 2.2](https://www.w3.org/TR/WCAG22/)
- [WAI-ARIA grid](https://www.w3.org/WAI/ARIA/apg/patterns/grid/)

### Google

- [Calendar API events](https://developers.google.com/workspace/calendar/api/v3/reference/events)
- [Recurring events](https://developers.google.com/workspace/calendar/api/guides/recurringevents)
- [Incremental sync](https://developers.google.com/workspace/calendar/api/guides/sync)
- [Push notifications](https://developers.google.com/workspace/calendar/api/guides/push)
- [Calendar accessibility](https://support.google.com/calendar/answer/16271522?hl=EN)

### Microsoft

- [Teams Calendar](https://support.microsoft.com/en-US/teams/meetings/get-started-with-the-microsoft-teams-calendar)
- [Schedule Teams meeting](https://support.microsoft.com/en-US/teams/meetings/schedule-a-meeting-in-microsoft-teams)
- [Graph Calendar](https://learn.microsoft.com/en-us/graph/outlook-calendar-concept-overview)
- [Graph calendarView](https://learn.microsoft.com/en-us/graph/api/calendar-list-calendarview?view=graph-rest-1.0)
- [Graph delta events](https://learn.microsoft.com/en-us/graph/delta-query-events)

### Zoom

- [Zoom Calendar Client](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060791)
- [Viewing scheduled meetings](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0060655)
- [Zoom Scheduler](https://support.zoom.com/hc/en/article?id=zm_kb&sysparm_article=KB0058092)
- [Zoom Calendar developer docs](https://developers.zoom.us/docs/calendar/)

### ClassIn

- [Teacher manual](https://www.classin.com/classin-teacher-manual-an-ultimate-guide-to-classin/)
- [Student manual](https://www.classin.com/classin-student-manual-a-step-by-step-guide-to-getting-started/)
- [Administrator](https://www.classin.com/administrator/)
- [LTI](https://www.classin.com/lti/)

### Open source

- [FullCalendar](https://github.com/fullcalendar/fullcalendar)
- [FullCalendar license](https://fullcalendar.io/license)
- [Schedule-X](https://github.com/schedule-x/schedule-x)
- [React Big Calendar](https://github.com/jquense/react-big-calendar)
- [TOAST UI Calendar](https://github.com/nhn/tui.calendar)
- [Cal.diy](https://github.com/calcom/cal.diy)
- [rrule.js](https://github.com/jkbrzt/rrule)
- [rrule-go](https://github.com/teambition/rrule-go)

## 28. Bước tiếp theo được khuyến nghị

1. Owner review và chấp nhận/điều chỉnh báo cáo này.
2. Mở ADR-0019 ở trạng thái `PROPOSED`, ghi alternatives và acceptance của spike.
3. Chạy FullCalendar/Go recurrence spike có số đo.
4. Finalize/chấp nhận ADR-0019 và dependency decision từ bằng chứng spike.
5. Triển khai P3-01 contract-first.
6. Triển khai P3-02A rồi P3-02B theo gate, không làm một lần toàn bộ.
