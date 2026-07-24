# ADR-0019: Calendar renderer, bounded recurrence và conflict authority

- Trạng thái: **Proposed**
- Ngày: 2026-07-24
- Phạm vi: P3-CAL-01, P3-02A, P3-02B, P3-02C và các consumer Calendar về sau
- Bằng chứng kỹ thuật: [`docs/calendar/P3_CAL_01_SPIKE_EVIDENCE.md`](../calendar/P3_CAL_01_SPIKE_EVIDENCE.md)

## Bối cảnh

TutorHub cần một Calendar chuyên nghiệp theo mental model Teams/Google nhưng domain
phải thuộc TutorHub: `ClassSession`, `StudyMeeting`, quyền tenant/class, civil time,
recurrence, exception, conflict, email/ICS và audit không được phụ thuộc vào object
hoặc database schema của renderer. P3-CAL-01 là decision gate; P3-01 chỉ tạo
ClassSession một lần và không được kéo recurrence vào vertical slice đầu tiên.

Spike cô lập nằm tại `apps/calendar-spike`. Nó chạy React 19 + Vite + StrictMode,
render các view Day/Week/Month/Agenda, thử drag/resize/revert, kiểm tra DST gap/overlap,
keyboard alternative và theme Warm Academic. Package này chưa được nối vào
`apps/web` production route.

## Tiêu chí quyết định

| Tiêu chí | Điều kiện đạt |
| --- | --- |
| Parity renderer | Day, Work week/Week, Month và Agenda; mobile mặc định Agenda |
| Domain boundary | Không import kiểu FullCalendar vào domain/API; adapter là ranh giới duy nhất |
| Interaction | Drag/resize có optimistic revert; 409/stale không ghi đè mù; keyboard action tương đương |
| Civil time | IANA zone, DST gap bị reject; overlap chọn earlier/later; không dùng host timezone |
| Recurrence | RFC subset bounded, iterator có context/deadline/cap; không `.All()`, không hourly/minutely/secondly |
| Accessibility | WCAG 2.2 AA mục tiêu; Axe critical/serious = 0; focus/live announcement; NVDA manual evidence |
| Performance | Đo 500/1.000/2.000 item và ghi p50/p95, long task, heap, bundle; không chấp nhận tăng đột biến |
| Visual | Warm Academic token scoped theo route/package; không copy asset/font/trade dress Vauliys/Teams/Google |
| License/security | MIT Standard; giữ notices; không Premium/resource timeline/telemetry/remote asset |
| Operability | Query/expansion có range, occurrence, deadline và payload cap cụ thể |

## Các phương án đã cân nhắc

### FullCalendar Standard v7.0.1 (đề xuất)

`@fullcalendar/react@7.0.1` có MIT license, React 17–19 và dùng `temporal-polyfill`
`^1.0.1`. V7 gom plugin Standard vào entrypoint của package (`/daygrid`,
`/timegrid`, `/list`, `/interaction`), có theme hooks và hỗ trợ React StrictMode.
V7 chỉ được dùng sau khi spike đạt các gate trong tài liệu evidence.

Không import Premium hoặc các package resource/timeline. Timeline/resource là nhu
cầu enterprise sau Phase 3 và có license boundary riêng.

### FullCalendar v6.1.x fallback

Baseline so sánh là v6.1.21 với các package `react/core/daygrid/timegrid/list/interaction`
tách rời. V6 chỉ thắng nếu v7 không đạt budget, accessibility hoặc compatibility.
Không cài đồng thời hai major trong production; chỉ build riêng trong spike.

### Schedule-X, React Big Calendar, TOAST UI, Cal.diy

Đã được so sánh ở P3-CAL-00. Các lựa chọn này không được chọn làm renderer Phase 3
vì thiếu một trong các tiêu chí: mobile Agenda parity, interaction/resize maturity,
Temporal/DST story, hoặc license/maintenance evidence. Có thể xem lại ở phase sau
nhưng không được âm thầm thay renderer sau khi ADR này accepted.

### Tự xây calendar renderer

Không chọn cho Phase 3. Tự vẽ grid, hit-testing, keyboard navigation và drag
interaction sẽ làm tăng rủi ro accessibility/performance mà không tạo lợi thế domain.
TutorHub chỉ tự sở hữu adapter, editor, policy, read model và recurrence authority.

## Quyết định đề xuất

### 1. Renderer và dependency

- Pin exact `@fullcalendar/react@7.0.1` và `temporal-polyfill@1.0.1`.
- Chỉ dùng entrypoint Standard của `@fullcalendar/react`; không thêm package plugin
  v7 rời hoặc Premium.
- Import theme/skeleton CSS chính thức; tùy biến bằng public theme palette hooks,
  event class/content hooks và token scoped. Không target class nội bộ `.fc-*`.
- FullCalendar event object chỉ tồn tại trong `FullCalendarSurface` adapter. Domain
  giữ `CalendarItem`/`CalendarMutation` độc lập.
- FullCalendar chỉ được nối vào production route sau khi automated browser evidence,
  manual NVDA review, license/security review và root lockfile review đạt.
- `temporal-polyfill@1.0.1` được dùng độc lập trong P3-01 để validate civil time,
  DST gap/overlap trước khi FullCalendar được chấp nhận; việc này không đưa renderer
  hoặc kiểu FullCalendar vào production domain.

### 2. Theme/IA/a11y

Package dùng semantic Warm Academic tokens:

| Token | Giá trị |
| --- | --- |
| canvas | `#FDFDF5` |
| surface-alt | `#EFEEDC` |
| ink | `#282828` |
| brand-ink | `#343831` |
| accent-fill | `#C5DE9B` |
| info-fill | `#DDEDEB` |
| gold | `#8C6A49` |
| link | `#6F5037` |
| muted | `#65655D` |
| border | `#D8D6C5` |
| border-strong | `#838276` |

Không dùng màu làm tín hiệu duy nhất. Event có nhãn, category text/icon, focus ring
và trạng thái live announcement. Agenda/list là đường tương đương cho keyboard,
mobile và thao tác không dùng pointer. Mục tiêu contrast: text >= 4.5:1, UI/focus
>= 3:1; phải kiểm tra bằng Axe và manual forced-colors/200% zoom.

### 3. Civil time và recurrence authority

- Backend lưu instant UTC (`starts_at`, `ends_at`) cùng IANA timezone và civil-time
  intent; renderer chỉ nhận projection.
- Recurrence lưu series + rule + exception, không clone vô hạn và không dùng
  `7*24h` để suy ra tuần.
- Phase 3 subset ban đầu: DAILY/WEEKLY, `INTERVAL`, `COUNT` xor `UNTIL`, `BYDAY`
  cần bounded. MONTHLY/YEARLY chỉ bật khi toàn bộ fixture/conformance pass.
  Reject `SECONDLY`, `MINUTELY`, `HOURLY`, rule không kết thúc và phần tử BY* chưa
  được contract.
- DST gap trả lỗi rõ ràng hoặc yêu cầu organizer tạo exception; overlap phải chọn
  `earlier` hoặc `later`. Không tự sửa âm thầm sang giờ khác.
- Occurrence key ổn định được tạo từ series id + civil tuple gốc + IANA zone +
  overlap choice; không lấy vị trí trong mảng expansion làm identity.
- `edit one`, `edit following`, `edit all` là command khác nhau. `edit following`
  tách series mới tại boundary và phải migrate/giữ exception trước boundary; nếu
  không thể thì reject transaction, không âm thầm mất exception.
- Engine Go được bọc bởi adapter có `context.Context`, deadline, occurrence cap,
  range cap và payload cap. Cấm `.All()`; chỉ cho `Between()` sau khi validator
  chứng minh upper bound dưới hard cap.
- Hard cap đề xuất để benchmark/contract hóa: query window <= 366 ngày, series
  horizon <= 730 ngày, <= 512 occurrence/series, <= 2.000 occurrence/request,
  deadline mặc định <= 250 ms. Đây là ngưỡng decision gate, không phải lời hứa
  production trước khi load test.

### 4. Conflict/resource authority

- ClassSession conflict authoritative chỉ chạy khi class/teacher resource dependency
  đã có contract. P3-01 one-time class session chỉ kiểm tra class-scoped overlap
  cần thiết; không giả lập teacher free/busy bằng client.
- Mutation có expected version. Renderer có thể optimistic nhưng server 409/stale
  luôn là authority; callback `revert()`/undo khôi phục trạng thái.
- WorkingSchedule, free/busy, suggested-time và attendee dependency thuộc P3-02C,
  nhưng occurrence/timezone identity của ADR này là nền tảng bắt buộc.

## Ngưỡng bundle/performance quyết định

Spike phải ghi raw output với commit, Node/Chrome/OS và fixture count. Ngưỡng tạm
để so sánh (có thể điều chỉnh trong ADR sau khi evidence thực tế giải thích rõ):

- Calendar lazy JS gzip <= 300 KiB; Calendar CSS gzip <= 40 KiB; initial Home
  bundle không tăng quá 5 KiB vì route phải lazy.
- Render ready p95 <= 500/900/1.800 ms tương ứng 500/1.000/2.000 item.
- Range/view navigation p95 <= 250/400/700 ms; không có long task > 200 ms.
- Heap settled delta <= 80 MiB ở 2.000 item; không có console error hoặc React
  StrictMode duplicate side-effect.
- V7 không được tệ hơn fallback v6 quá 20% ở cùng máy/fixture nếu cả hai đều pass
  absolute budget.

Các ngưỡng này không được dùng để che giấu kết quả: evidence phải ghi cả p50, p95,
min/max và lần chạy thất bại.

## Hệ quả

### Tích cực

- Domain TutorHub không bị khóa vào schema/API của thư viện UI.
- V7 có thể cung cấp interaction và view parity nhanh, trong khi recurrence/DST/
  conflict vẫn authoritative ở Go.
- Agenda/keyboard path và route-scoped theme được kiểm thử trước khi nối production.

### Chi phí/rủi ro

- FullCalendar v7 còn mới; phải giữ v6 fallback evidence và pin exact.
- `temporal-polyfill` là dependency runtime thêm vào bundle Calendar.
- `rrule-go` v1.8.2 có release history cũ; adapter/cap/fuzz bắt buộc và cần xem lại
  maintenance trước production.
- NVDA manual review và cross-client/ICS/email vẫn là gate riêng P3-CAL-02/P3-05A.

## Kết quả spike local ngày 2026-07-24

- FullCalendar Standard `7.0.1` chạy trong package cô lập `apps/calendar-spike`;
  không có Premium/resource timeline, telemetry hoặc remote runtime asset.
- Dependency/license guard, strict typecheck, 8 unit/DOM test và production build
  đạt. Output build ghi nhận Calendar JS gzip `154.94 KiB`, CSS gzip `5.35 KiB`,
  đều dưới bundle budget tạm.
- Adapter/revert contract và DST fixtures cho `Asia/Ho_Chi_Minh` cùng
  `America/New_York` đạt unit test. Đây chưa thay thế pointer/keyboard E2E.
- Go recurrence adapter đạt golden/property/resource-cap test, fuzz và benchmark
  local. Một run đại diện cho 500 occurrence: UTC khoảng `552 µs`, Ho Chi Minh
  khoảng `1.16 ms`, New York khoảng `1.80 ms`; số này chỉ là evidence kỹ thuật,
  không phải browser p95 hay production SLO.
- Root lockfile đã được cập nhật và dependency guard kiểm tra exact pins.
- Playwright Chromium local chưa hoàn tất navigation/teardown và bị timeout. Không
  có kết quả Axe/browser performance hợp lệ; keyboard/NVDA, 200% zoom,
  forced-colors và reduced-motion vẫn chờ manual review.

Vì các gate browser/manual còn mở, spike chưa chứng minh production readiness,
ADR vẫn `PROPOSED` và FullCalendar chưa được nối vào `apps/web`.

## Trạng thái và điều kiện chuyển Accepted

ADR hiện **PROPOSED**. Chỉ chuyển `Accepted` khi:

1. `apps/calendar-spike` build/typecheck/unit/E2E/Axe xanh, gồm DST và revert;
2. Go adapter golden/property/fuzz/benchmark/resource-exhaustion xanh;
3. raw performance/bundle evidence đạt ngưỡng hoặc có quyết định điều chỉnh được
   ghi rõ;
4. manual keyboard + NVDA checklist, 200% zoom, forced-colors và reduced-motion
   được reviewer ký xác nhận (spike hiện chưa giả định đã có);
5. dependency/license/security guard xanh, không Premium/telemetry/remote asset;
6. root lockfile cập nhật exact và review không kéo dependency ngoài ý muốn.

Nếu mục 4 chưa có, ADR phải giữ Proposed hoặc ghi rõ `Accepted with explicit manual
NVDA gate` và không được gọi production-ready.

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
