# ADR-0019: Calendar renderer, bounded recurrence và conflict authority

- Trạng thái: **Accepted with explicit manual NVDA gate**
- Ngày chấp nhận quyết định: 2026-07-24
- Phạm vi: P3-CAL-01, P3-02A, P3-02B, P3-02C và các consumer Calendar về sau
- Bằng chứng kỹ thuật:
  [`docs/calendar/P3_CAL_01_SPIKE_EVIDENCE.md`](../calendar/P3_CAL_01_SPIKE_EVIDENCE.md)
- Điều kiện rollout còn mở: marker `PENDING_NVDA_REVIEW` trong tài liệu bằng chứng
  phải được reviewer đóng trước khi nối renderer vào route Calendar production.

## Bối cảnh

TutorHub cần Calendar chuyên nghiệp theo mental model Teams/Google nhưng domain phải
thuộc TutorHub: `ClassSession`, `StudyMeeting`, quyền tenant/class, civil time,
recurrence, exception, conflict, email/ICS và audit không được phụ thuộc vào object
hoặc database schema của renderer. P3-CAL-01 là decision spike; P3-01 chỉ tạo
ClassSession một lần và không kéo recurrence vào vertical slice đầu tiên.

Spike v7 cô lập nằm tại `apps/calendar-spike`; comparator v6 nằm tại
`apps/calendar-spike-v6`. Cả hai không được nối vào `apps/web`. V7 chạy React 19 +
Vite + StrictMode, render Day/Week/Month/Agenda, thử drag/resize/revert, DST
gap/overlap, keyboard alternative, density 500/1.000/2.000 item và Warm Academic
theme. Recurrence candidate Go nằm trong
`services/core-api/internal/spikes/calendarrecurrence`, chưa được import bởi module
schedule production.

## Tiêu chí quyết định

| Tiêu chí         | Điều kiện đạt                                                                                        |
| ---------------- | ---------------------------------------------------------------------------------------------------- |
| Parity renderer  | Day, Work week/Week, Month và Agenda; mobile mặc định Agenda                                         |
| Domain boundary  | Không import kiểu FullCalendar vào domain/API; adapter là ranh giới duy nhất                         |
| Interaction      | Drag/resize có optimistic revert; 409/stale không ghi đè mù; keyboard action tương đương             |
| Civil time       | IANA zone, DST gap bị reject; overlap chọn earlier/later; không dùng host timezone                   |
| Recurrence       | RFC subset bounded, iterator có context/deadline/cap; không `.All()`, không hourly/minutely/secondly |
| Accessibility    | WCAG 2.2 AA mục tiêu; Axe critical/serious = 0; focus/live announcement; NVDA là rollout gate        |
| Performance      | Đo 5 lần ở 500/1.000/2.000 item; ghi raw, p50/p95, long task, heap và bundle                         |
| Visual           | Warm Academic token scoped; không copy asset/font/trade dress Vauliys/Teams/Google                   |
| License/security | MIT Standard, giữ notices; không Premium/resource timeline/telemetry/remote asset                    |
| Operability      | Query/expansion có range, occurrence, deadline và payload cap cụ thể                                 |

## Các phương án đã cân nhắc

### FullCalendar Standard v7.0.1 — được chọn

`@fullcalendar/react@7.0.1` có MIT license, hỗ trợ React 19 và dùng
`temporal-polyfill@1.0.1`. V7 gom plugin Standard vào entrypoint của package
(`/daygrid`, `/timegrid`, `/list`, `/interaction`), có public theme hooks và hoạt động
trong StrictMode. Không import Premium hoặc package resource/timeline.

V7 thắng comparator parity-config v6 ở render và long-task tại cả ba density, đồng thời
giữ toàn bộ absolute budget. Navigation v7 nhanh hơn ở 500 item, chậm hơn khoảng
4,1%/11,6% ở 1.000/2.000 item nhưng vẫn trong relative guardrail 20%. V7 tốn thêm
khoảng 11,6% lazy JS gzip và `18,90 MiB` heap ở fixture 2.000 item, nhưng vẫn thấp hơn
nhiều so với absolute budget `80 MiB`. V6 fail render 500 item (`1.492 > 500 ms`) và
long-task 2.000 item (`404 > 400 ms`), dù heap nhỏ hơn. Chi tiết số đo nằm trong tài
liệu evidence.

### FullCalendar v6.1.21 fallback — không chọn

Comparator v6 dùng các package `react/core/daygrid/timegrid/list/interaction` tách
rời. Lượt đo cuối dùng cùng cấu hình projection với v7: `allDaySlot=false`,
`dayMaxEventRows=6`, `eventMaxStack=6`, cùng content height, fixture, view, timezone,
event order và phương pháp đo. V6 có lazy JS/CSS và heap nhỏ hơn, nhưng fail absolute
render budget ở 500 item và long-task budget ở 2.000 item. Comparator được giữ làm
evidence cô lập, không cài đồng thời hai major vào production.

### Schedule-X, React Big Calendar, TOAST UI, Cal.diy

Các lựa chọn đã được so sánh ở P3-CAL-00 và không được chọn cho Phase 3 vì thiếu một
trong các tiêu chí: mobile Agenda parity, interaction/resize maturity, Temporal/DST
story hoặc license/maintenance evidence. Không được âm thầm thay renderer sau ADR này;
thay đổi cần ADR superseding.

### Tự xây calendar renderer

Không chọn. Tự vẽ grid, hit-testing, keyboard navigation và drag interaction tăng rủi
ro accessibility/performance mà không tạo lợi thế domain. TutorHub tự sở hữu adapter,
editor, policy, read model và recurrence/conflict authority.

## Quyết định

### 1. Renderer và dependency

- Pin exact `@fullcalendar/react@7.0.1` và `temporal-polyfill@1.0.1`.
- Chỉ dùng entrypoint Standard của `@fullcalendar/react`; không thêm package plugin v7
  rời, Premium hoặc resource/timeline.
- Import theme/skeleton CSS chính thức; tùy biến qua public theme palette hooks,
  event class/content hooks và token scoped. Không target class nội bộ `.fc-*`.
- FullCalendar event object chỉ tồn tại trong `FullCalendarSurface` adapter. Domain giữ
  `CalendarItem`/`CalendarMutation` độc lập.
- Route Calendar phải lazy-load; initial Home không được kéo renderer.
- `temporal-polyfill@1.0.1` có thể dùng trong domain civil-time adapter, nhưng kiểu
  FullCalendar không được rò vào production domain/API.
- Renderer chỉ được nối vào `apps/web` sau khi marker `PENDING_NVDA_REVIEW` được đóng
  và P3-02A lặp lại authorization/range/bundle/a11y gate trên route thật.

### 2. Theme/IA/a11y

Package dùng semantic Warm Academic tokens:

| Token         | Giá trị   |
| ------------- | --------- |
| canvas        | `#FDFDF5` |
| surface-alt   | `#EFEEDC` |
| ink           | `#282828` |
| brand-ink     | `#343831` |
| accent-fill   | `#C5DE9B` |
| info-fill     | `#DDEDEB` |
| gold          | `#8C6A49` |
| link          | `#6F5037` |
| muted         | `#65655D` |
| border        | `#D8D6C5` |
| border-strong | `#838276` |

Không dùng màu làm tín hiệu duy nhất. Event có nhãn, category text/icon, focus ring và
live announcement. Agenda/list là đường thao tác semantic cho keyboard, mobile và
thiết bị hỗ trợ. Danh sách lớn mở progressive `24 -> 48 -> toàn bộ` bằng nút có
`aria-controls`; ở fixture 51 item, lượt cuối hiển thị `51/51`, nên không được gọi chỉ
24 item đầu là đường tương đương. Contrast mục tiêu: text >= 4.5:1, UI/focus >= 3:1.

Axe cho phép đúng một waiver upstream FullCalendar: `empty-table-header`, impact
`minor`, tối đa một violation chứa đúng một node. Target node phải đúng
`div[role="rowheader"]`, HTML phải là node `role="rowheader"` có `aria-label="Timed"`
và class `fc-*`; cả scope renderer
`[data-calendar-renderer="fullcalendar-standard"]` lẫn toàn trang phải chỉ có đúng một
node `div[role="rowheader"][aria-label="Timed"]`. Critical/serious phải bằng 0; sai
rule/impact/node count/target/HTML/scope hoặc có finding khác đều làm fail. Zoom 200%,
forced-colors, reduced-motion, pointer drag/resize và keyboard alternative là E2E gate.
NVDA manual review vẫn là explicit rollout gate, không được suy từ Axe.

### 3. Civil time và recurrence authority

- Backend lưu instant UTC (`starts_at`, `ends_at`) cùng IANA timezone và civil-time
  intent; renderer chỉ nhận projection.
- Recurrence lưu series + rule + exception, không clone vô hạn và không dùng `7*24h`
  để suy ra tuần.
- Phase 3 subset ban đầu: DAILY/WEEKLY, `INTERVAL`, `COUNT` xor `UNTIL`, `BYDAY` được
  bounded. MONTHLY/YEARLY chỉ bật khi fixture/conformance tương ứng pass; spike hiện có
  YEARLY golden explicit `BYMONTH/BYMONTHDAY` và boundary test.
- Với rule `COUNT`, compile phải duyệt đủ tới occurrence cuối trong iterator đã chặn
  bằng series horizon; nếu COUNT không thể hoàn tất trong 730 civil-day từ DTSTART thì
  reject `ErrSeriesHorizonExceeded`. Chỉ kiểm tra giá trị COUNT <= 512 là chưa đủ.
- Reject `SECONDLY`, `MINUTELY`, `HOURLY`, rule không kết thúc và phần tử BY* chưa có
  contract.
- DST gap trả lỗi rõ hoặc yêu cầu organizer tạo exception; overlap phải chọn
  `earlier`/`later`. Không tự sửa âm thầm sang giờ khác.
- Occurrence key ổn định gồm series ID + civil tuple gốc + IANA zone + overlap choice;
  không dùng vị trí trong mảng expansion.
- `edit one`, `edit following`, `edit all` là command khác nhau. `edit following` tách
  series mới tại boundary và phải giữ/migrate exception trước boundary; nếu không thể
  thì reject transaction.
- Engine Go được bọc bởi adapter có `context.Context`, deadline, occurrence/range/
  payload cap. Cấm `.All()`; chỉ dùng `Between()` sau khi validator chứng minh bound.
- Hard cap được chấp nhận:
  - query window tối đa **366 ngày**;
  - series horizon tối đa **730 ngày**;
  - tối đa **512 occurrence/series**;
  - tối đa **2.000 occurrence/request**;
  - deadline mặc định tối đa **250 ms**.
- Hard cap là contract safety cho Phase 3, không phải production SLO; P3-02B phải
  benchmark lại khi adapter rời `internal/spikes`.

### 4. Conflict/resource authority

- ClassSession conflict authoritative chỉ chạy khi class/resource dependency có
  contract. P3-01 one-time chỉ kiểm tra class-scoped overlap cần thiết; client không
  tự suy teacher free/busy.
- Mutation có expected version. Renderer có thể optimistic nhưng server 409/stale luôn
  là authority; `revert()`/undo khôi phục projection.
- WorkingSchedule, free/busy, attendee và suggested-time thuộc P3-02C. Suggested-time
  chỉ nhận timed source có domain contract; all-day/source chưa có phải source-gated.
- P3-02C định nghĩa privacy/resource authority; ADR này chỉ khóa occurrence/timezone
  identity và renderer boundary.

## Bundle/performance budget được chấp nhận

Production interactive projection ban đầu giới hạn **500 item** trong range đang xem.
Fixture 1.000/2.000 là stress/density gate để chứng minh bounded degradation, không
phải cho phép client render vô hạn.

- Calendar lazy JS gzip <= 300 KiB; CSS gzip <= 40 KiB; initial Home tăng <= 5 KiB.
- Render-ready p95 <= **500/900/1.800 ms** ở 500/1.000/2.000 item.
- Range/view navigation p95 <= **350/500/800 ms** ở 500/1.000/2.000 item.
- Long-task max <= **200/300/400 ms** ở 500/1.000/2.000 item.
- Heap settled delta <= 80 MiB ở 2.000 item.
- Console/React errors bằng 0; không có StrictMode duplicate side effect.
- Relative v7/v6 delta là guardrail so sánh, không thay absolute budget. Delta xấu hơn
  20% chỉ được chấp nhận khi ghi rõ metric/density, lý do và comparator không đạt toàn bộ
  absolute gate; bundle/heap trade-off phải được ghi, không được chỉ ghi pass/fail.

Budget navigation và long-task được điều chỉnh từ ngưỡng Proposed 250/400/700 và một
long-task 200 ms duy nhất thành density-aware 350/500/800 và 200/300/400. Lý do: range
navigation thay toàn bộ DOM projection, còn 1.000/2.000 là stress fixture; ngưỡng mới
vẫn bắt v7 giữ response bounded và loại v6 do long task tăng không phù hợp. Raw output,
min/p50/p95/max và lượt thất bại phải được giữ trong evidence.

## Bằng chứng chấp nhận ngày 2026-07-24

- V7 typecheck, lint, 4 file/8 unit test, build, dependency guard và 3/3 guard test đạt.
  Build: JS gzip `155.15 KiB`, CSS gzip `5.37 KiB`.
- Full E2E trước lượt hardening cuối đạt `9/9`; sau fix Agenda/Axe, nhóm impacted
  accessibility/regression đạt `3/3`; consolidated full rerun cuối đạt literal
  `9 passed (23.6s)`, exit `0`.
- Browser evidence đạt pointer drag/resize/revert, keyboard/Agenda progressive
  `24 -> 48 -> 51`, Axe, zoom 200%, forced-colors, reduced-motion và performance/heap.
  Critical/serious Axe = 0; waiver exact upstream minor `empty-table-header` bị khóa
  đúng một node/target/HTML/scope như mục Accessibility.
- V7 p95 render ở 500/1.000/2.000 item: `152/164/201 ms`; navigation:
  `204/327/548 ms`; long-task max: `79/198/315 ms`; `browserErrors=[]`.
- V7 heap delta 2.000 item: `26,34 MiB`, dưới 80 MiB.
- Comparator v6 parity-config có p95 render/navigation/long-task ở
  500/1.000/2.000 item lần lượt `1.492/275/413`, `216/314/491` và
  `177/266/404 ms`. V6 fail render 500 (`1.492 > 500`) và long-task 2.000
  (`404 > 400`). Build v6: JS gzip `139.00 KiB`, CSS gzip `0.35 KiB`; heap delta
  `7,44 MiB`.
- Go recurrence post-fix unit/resource test đạt; fuzz 10 giây đạt
  `238.755`/`199.088` executions. Benchmark 366
  occurrence, count=5: UTC `706.580 ns/op`, Ho Chi Minh `674.220 ns/op`, New York
  `984.380 ns/op`; chi tiết memory/allocation nằm trong evidence.
- COUNT validation chứng minh occurrence cuối nằm trong series horizon 730 civil-day;
  DAILY/WEEKLY/MONTHLY/YEARLY boundary test và YEARLY golden đều đạt.
- Root lockfile pin exact; `THIRD_PARTY_NOTICES.md` giữ MIT notice cho FullCalendar,
  Temporal và rrule-go. Guard reject Premium/resource, telemetry và remote runtime
  asset.
- Manual browser kiểm tra semantic main/headings/region/grid/buttons/Agenda và live
  conflict announcement. Sau 409, focus vẫn ở action `Dời sau 30 phút`.

Raw và phương pháp đầy đủ nằm trong tài liệu evidence. Các lượt grouped E2E đã cung cấp
evidence cho decision spike; consolidated one-command rerun vẫn là pre-commit
verification của thay đổi, không thay đổi decision.

## Explicit manual NVDA gate

ADR chấp nhận renderer/recurrence decision nhưng **không tuyên bố production-ready**
cho tới khi reviewer đóng `PENDING_NVDA_REVIEW`. Review phải chạy NVDA tạm thời hoặc
môi trường NVDA đã kiểm soát, kiểm tra:

Lượt yêu cầu mở runtime cho review ngày 2026-07-24 đã hết thời gian chờ approval, nên
không tạo được bằng chứng NVDA. Đây là trạng thái **pending**, không phải PASS hoặc FAIL,
và không được thay bằng Axe result.

1. heading/landmark và tên vùng Calendar;
2. event label, time, category và trạng thái;
3. view switch/Agenda không mất context;
4. keyboard drag alternative và focus restore sau revert;
5. live announcement khi server trả 409.

Nếu review fail, P3-02A không được nối renderer vào route thật; phải sửa spike và cập
nhật ADR/evidence. Cross-client ICS/email vẫn là gate riêng P3-CAL-02/P3-05A.

## Hệ quả

### Tích cực

- Domain TutorHub không bị khóa vào schema/API renderer.
- V7 cung cấp interaction/view parity nhanh; recurrence/DST/conflict vẫn authoritative
  ở Go/domain.
- Agenda/keyboard path và route-scoped Warm Academic theme đã có automated evidence.
- Hard cap ngăn recurrence expansion hoặc DOM projection không bounded.

### Chi phí/rủi ro

- FullCalendar v7 còn mới; phải pin exact và giữ v6 comparator/evidence khi upgrade.
- `temporal-polyfill` tăng runtime bundle Calendar.
- `rrule-go` v1.8.2 có release history cũ; adapter/cap/fuzz và maintenance review là
  bắt buộc trước production.
- NVDA còn là explicit manual rollout gate; Axe không thay thế screen-reader review.
- P3-CAL-01 chỉ là spike/decision. P3-02A/B/C vẫn phải triển khai contract,
  authorization, migration, integration/E2E và staging rollout riêng.

## Nguồn tham khảo

- [FullCalendar React](https://fullcalendar.io/docs/react)
- [Temporal polyfill](https://fullcalendar.io/docs/temporal-polyfill)
- [FullCalendar v6 → v7](https://fullcalendar.io/docs/upgrading-from-v6)
- [FullCalendar CSS migration](https://fullcalendar.io/docs/upgrading-from-v6-css)
- [FullCalendar license](https://fullcalendar.io/license)
- [FullCalendar Premium boundary](https://fullcalendar.io/premium)
- [`eventInteractive`](https://fullcalendar.io/docs/eventInteractive)
- [Event dragging/resizing and revert](https://fullcalendar.io/docs/event-dragging-resizing)
- [`rrule-go` API/source](https://pkg.go.dev/github.com/teambition/rrule-go)
