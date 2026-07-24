# P3-CAL-01 technical spike evidence

- Trạng thái decision spike: **DONE**
- ADR: ADR-0019 **Accepted with explicit manual NVDA gate**
- V7 package: `apps/calendar-spike`
- V6 comparator: `apps/calendar-spike-v6`
- Go spike: `services/core-api/internal/spikes/calendarrecurrence`
- Mục đích: kiểm chứng renderer/recurrence/theme trước khi nối `apps/web` hoặc module
  schedule production
- Rollout blocker còn mở: `PENDING_NVDA_REVIEW`

P3-CAL-01 chấp nhận lựa chọn kỹ thuật, không tuyên bố Calendar production-ready.
P3-02A phải đóng manual NVDA gate và lặp lại authorization/range/a11y/bundle gate trên
route thật trước rollout.

## Môi trường và candidate

| Thuộc tính     | Giá trị                                                         |
| -------------- | --------------------------------------------------------------- |
| Ngày đo        | 2026-07-24                                                      |
| Candidate base | `4f405b730feb36a6835bd392753813bdce3961af` + worktree P3-CAL-01 |
| OS             | Windows x64                                                     |
| Node           | `v24.15.0`                                                      |
| pnpm           | `11.7.0`                                                        |
| Playwright     | `1.61.1`, Chromium/Desktop Chrome device profile                |
| React/Vite     | React `19.2.7`, Vite `8.1.4`, StrictMode                        |

Commit cuối chứa evidence được xác định bởi Git history của file này; không chèn một
SHA dự đoán trước khi commit.

## Dependency snapshot

| Component                        | Pin      | License | Ghi chú                                                         |
| -------------------------------- | -------- | ------- | --------------------------------------------------------------- |
| `@fullcalendar/react`            | `7.0.1`  | MIT     | Standard only; `/daygrid`, `/timegrid`, `/list`, `/interaction` |
| `temporal-polyfill`              | `1.0.1`  | MIT     | dependency civil-time của FullCalendar v7                       |
| fallback `@fullcalendar/*`       | `6.1.21` | MIT     | comparator cô lập, không cài song song trong production         |
| `github.com/teambition/rrule-go` | `v1.8.2` | MIT     | candidate sau bounded adapter/cap/fuzz                          |
| `@axe-core/playwright`           | `4.12.1` | MPL-2.0 | dev-only, không nằm trong runtime bundle                        |

Root lockfile pin exact và có importer riêng cho hai spike. Không có Premium,
scheduler/resource timeline, telemetry hoặc remote runtime asset.
`apps/calendar-spike/THIRD_PARTY_NOTICES.md` giữ notice MIT của FullCalendar,
Temporal và rrule-go.

## Cấu trúc spike

```text
apps/calendar-spike/
  src/calendar/CalendarSurface.tsx       # domain ↔ renderer adapter
  src/calendar/FullCalendarSurface.tsx   # FullCalendar-only boundary
  src/calendar/civilTime.ts              # Temporal gap/overlap fixture
  src/calendar/domain.ts                 # optimistic mutation/revert contract
  src/calendar/fixtures.ts               # deterministic 48/500/1000/2000 items
  src/*.test.*                           # unit/DOM contract
  e2e/calendar-spike.spec.ts             # Playwright/Axe/a11y/perf/heap
  scripts/check-dependencies.*           # Premium/telemetry/license guard
  THIRD_PARTY_NOTICES.md

apps/calendar-spike-v6/                  # isolated v6.1.21 comparator
services/core-api/internal/spikes/calendarrecurrence/
                                           # bounded Go recurrence adapter
```

Go recurrence adapter vẫn ở `internal/spikes`, chưa được import bởi classroom/schedule
production service.

## Hard cap đã kiểm chứng

| Guard                  |      Cap |
| ---------------------- | -------: |
| Query window           | 366 ngày |
| Series horizon         | 730 ngày |
| Occurrence mỗi series  |      512 |
| Occurrence mỗi request |    2.000 |
| Deadline mặc định      |   250 ms |

Validator reject `SECONDLY`, `MINUTELY`, `HOURLY`, rule không bounded và payload vượt
cap. Adapter dùng `context.Context`; không gọi `.All()`.

## Lệnh tái lập

Từ repository root:

```powershell
corepack pnpm install --frozen-lockfile

corepack pnpm --filter @tutorhub/calendar-spike security:dependencies
corepack pnpm --filter @tutorhub/calendar-spike test:guard
corepack pnpm --filter @tutorhub/calendar-spike typecheck
corepack pnpm --filter @tutorhub/calendar-spike lint
corepack pnpm --filter @tutorhub/calendar-spike test
corepack pnpm --filter @tutorhub/calendar-spike build
corepack pnpm --filter @tutorhub/calendar-spike e2e

corepack pnpm --filter @tutorhub/calendar-spike-v6 typecheck
corepack pnpm --filter @tutorhub/calendar-spike-v6 lint
corepack pnpm --filter @tutorhub/calendar-spike-v6 test
corepack pnpm --filter @tutorhub/calendar-spike-v6 build
corepack pnpm --filter @tutorhub/calendar-spike-v6 e2e

go test ./services/core-api/internal/spikes/calendarrecurrence -count=1
go test ./services/core-api/internal/spikes/calendarrecurrence -run=^$ `
  -fuzz=FuzzCompileNeverPanics -fuzztime=30s
go test ./services/core-api/internal/spikes/calendarrecurrence -run=^$ `
  -fuzz=FuzzExpandStaysWithinCap -fuzztime=30s
go test ./services/core-api/internal/spikes/calendarrecurrence `
  -run=^$ -bench=BenchmarkExpandMaxQueryWindow -benchmem -count=5
```

Windows từng để lại preview process chiếm port 4174 khi Playwright teardown bị ngắt.
Chỉ được dừng đúng process preview đã xác định; không kill Node process tùy ý. Full E2E
trước lượt hardening cuối đạt `9/9`; sau fix Agenda/Axe, nhóm impacted accessibility/
regression đạt `3/3`. Consolidated v7 full rerun cuối đạt literal
`9 passed (23.6s)`, exit `0`. Comparator parity-config v6 cuối đạt
`4 passed (17.5s)`, exit `0`. Đây là terminal result thật, không phải phép cộng các
nhóm test rời.

## Acceptance matrix

| Gate                              | Evidence                                               | Trạng thái                  |
| --------------------------------- | ------------------------------------------------------ | --------------------------- |
| React 19/Vite/StrictMode          | typecheck, lint, 4 file/8 test, build                  | PASS local                  |
| Renderer/domain boundary          | adapter unit/DOM test                                  | PASS local                  |
| Pointer drag/resize/revert        | Playwright pointer fixture + 409 revert                | PASS local                  |
| Keyboard/Agenda/mobile            | keyboard alternative, Agenda, responsive tests         | PASS local                  |
| DST gap/overlap                   | HCM/New York unit fixtures                             | PASS local                  |
| Axe                               | critical/serious = 0; exact minor upstream waiver      | PASS with documented waiver |
| 200%/forced-colors/reduced-motion | Playwright accessibility group                         | PASS local                  |
| 500/1.000/2.000 performance       | 5 samples/density + heap at 2.000                      | PASS local                  |
| V6 comparator                     | typecheck/lint/test/build/E2E 4/4                      | PASS comparator             |
| Recurrence bounded                | unit/property/cap/fuzz/benchmark                       | PASS local                  |
| Dependency/license                | dependency guard, 3/3 guard test, notices, lock review | PASS local                  |
| Consolidated E2E cuối             | v7 `9 passed (23.6s)`; v6 `4 passed (17.5s)`, exit 0   | PASS local                  |
| NVDA manual                       | xem checklist cuối tài liệu                            | `PENDING_NVDA_REVIEW`       |

## V7 build và browser performance

Budget chấp nhận:

- render p95 <= `500/900/1.800 ms`;
- navigation p95 <= `350/500/800 ms`;
- long-task max <= `200/300/400 ms`;
- heap delta 2.000 item <= `80 MiB`;
- lazy JS/CSS gzip <= `300/40 KiB`.

Production interactive projection ban đầu giới hạn 500 item. Fixture 1.000/2.000 là
stress/density evidence, không phải cho phép render vô hạn.

### Final consolidated v7 summary

| Items | Render-ready p95 ms | Budget | Navigation p95 ms | Budget | Long-task max ms | Budget | Kết quả |
| ----: | ------------------: | -----: | ----------------: | -----: | ---------------: | -----: | ------- |
|   500 |                 152 |    500 |               204 |    350 |               79 |    200 | PASS    |
| 1.000 |                 164 |    900 |               327 |    500 |              198 |    300 | PASS    |
| 2.000 |                 201 |  1.800 |               548 |    800 |              315 |    400 | PASS    |

Raw và summary `min/p50/p95/max` của terminal rerun:

```text
500:
  render [152,97,93,89,90]       -> 89/93/152/152
  nav    [197,200,185,204,200]   -> 185/200/204/204
  long   [79,73,64,61,62]        -> 61/64/79/79
1000:
  render [125,112,164,110,123]   -> 110/123/164/164
  nav    [304,306,307,327,309]   -> 304/307/327/327
  long   [198,111,107,102,106]   -> 102/107/198/198
2000:
  render [178,146,153,201,143]   -> 143/153/201/201
  nav    [548,527,490,540,529]   -> 490/529/548/548
  long   [315,248,247,254,257]   -> 247/254/315/315
```

Full run có `browserErrors=[]`.

Heap precise-memory:

```text
baseline:   6,363,908 bytes
fixture:   33,981,377 bytes
delta:     27,617,469 bytes = 26.34 MiB
budget:   80 MiB
```

V7 production build:

```text
Calendar JS gzip:  155.15 KiB
Calendar CSS gzip:   5.37 KiB
```

## V6 comparator và trade-off

### Final parity-config v6.1.21 summary

Comparator cuối dùng cùng projection config với v7: `allDaySlot=false`,
`dayMaxEventRows=6`, `eventMaxStack=6`, cùng content height, fixture, initial view,
timezone, event order và phép đo 5 lượt. Số p95 cuối:

| Items | Render-ready p95 ms | Budget | Navigation p95 ms | Budget | Long-task p95/max ms | Budget | Kết quả            |
| ----: | ------------------: | -----: | ----------------: | -----: | -------------------: | -----: | ------------------ |
|   500 |               1.492 |    500 |               216 |    350 |                  177 |    200 | **FAIL render**    |
| 1.000 |                 275 |    900 |               314 |    500 |                  266 |    300 | PASS               |
| 2.000 |                 413 |  1.800 |               491 |    800 |                  404 |    400 | **FAIL long-task** |

Render p95 500 chứa cold-first sample `1.492 ms`; không loại sample này khỏi kết quả.
Comparator full run có `browserErrors=[]`.

Raw và summary `min/p50/p95/max`:

```text
500:
  render [1492,138,145,138,135]  -> 135/138/1492/1492
  nav    [207,179,216,195,205]   -> 179/205/216/216
  long   [177,124,132,125,123]   -> 123/125/177/177
1000:
  render [275,217,203,214,214]   -> 203/214/275/275
  nav    [307,290,306,314,272]   -> 272/306/314/314
  long   [266,203,191,202,202]   -> 191/202/266/266
2000:
  render [413,334,352,345,344]   -> 334/345/413/413
  nav    [477,464,491,438,467]   -> 438/467/491/491
  long   [404,323,338,332,331]   -> 323/332/404/404
```

V6 build/heap:

```text
Calendar JS gzip: 139.00 KiB
Calendar CSS gzip: 0.35 KiB
heap baseline:     5,163,617 bytes
heap fixture:     12,969,078 bytes
heap delta:        7,805,461 bytes = 7.44 MiB
```

V7 tăng khoảng `11,6%` lazy JS gzip và `18,90 MiB` heap delta so với v6; settled heap
v7 là `26,34 MiB/80 MiB`, còn v6 tốt hơn ở `7,44 MiB`. V7 nhanh hơn v6 ở render và
long-task cả ba density. Navigation v7 nhanh hơn ở 500 item, chậm hơn khoảng
4,1%/11,6% tại 1.000/2.000 item nhưng vẫn trong relative guardrail 20%. V6 fail hai
absolute gate: render 500 `1.492 > 500 ms` và long-task 2.000 `404 > 400 ms`, trong
khi v7 đạt toàn bộ absolute budget với render `152/164/201 ms`, navigation
`204/327/548 ms` và long-task `79/198/315 ms`. Vì vậy v7 vẫn được chọn; comparator v6
không đi vào production dependency graph.

## Go recurrence evidence

Unit/golden/property/resource-cap tests đạt. `COUNT` không còn chỉ được giới hạn số học
`<= 512`: compiler dùng iterator đã chặn bằng horizon để chứng minh occurrence cuối
nằm trong 730 civil-day từ DTSTART; nếu không đủ COUNT thì trả
`ErrSeriesHorizonExceeded`. Boundary tests bao phủ DAILY/WEEKLY/MONTHLY/YEARLY và
golden bổ sung YEARLY explicit `BYMONTH=7;BYMONTHDAY=20;COUNT=2`. Fuzz:

```text
FuzzCompileNeverPanics:   10 s, 238,755 executions, PASS
FuzzExpandStaysWithinCap: 10 s, 199,088 executions, PASS
```

Benchmark `BenchmarkExpandMaxQueryWindow`, 366 DAILY occurrence, `count=5`:

| Zone             |     Time/op |     Memory/op | Allocations/op |
| ---------------- | ----------: | ------------: | -------------: |
| UTC              | `706580 ns` | `194969 B/op` |  `2589 allocs` |
| Asia/Ho_Chi_Minh | `674220 ns` | `212537 B/op` |  `3321 allocs` |
| America/New_York | `984380 ns` | `212496 B/op` |  `3321 allocs` |

Đây là local spike evidence, không phải browser p95 hoặc production SLO. P3-02B phải
benchmark lại khi code được đưa ra khỏi `internal/spikes`.

## Accessibility/browser evidence

- Semantic snapshot có `main`, headings, named region, grid, buttons, Agenda và DST
  content.
- Pointer drag và resize đạt; keyboard alternative dùng command rõ thay cho gesture.
- Conflict fixture phát live message:
  `Phát hiện xung đột phiên bản (409). Lịch đã được hoàn tác.`
- Sau revert, focus vẫn ở action `Dời sau 30 phút`.
- Agenda là đường semantic cho keyboard/mobile. Fixture 51 item mở progressive
  `24 -> 48 -> 51`; nút `Hiển thị thêm` có `aria-controls`, count live đổi tương ứng và
  biến mất sau `51/51`. Chỉ trạng thái 24/51 không được gọi là tương đương toàn bộ.
- Zoom 200% không có horizontal overflow; forced-colors và reduced-motion đạt E2E.
- Axe critical/serious = 0. Waiver duy nhất là upstream FullCalendar rule
  `empty-table-header`, impact `minor`; test chỉ cho phép tối đa một violation chứa đúng
  một node có target exact `div[role="rowheader"]`, HTML `role="rowheader"` +
  `aria-label="Timed"` + class `fc-*`. Cả scope
  `[data-calendar-renderer="fullcalendar-standard"]` và toàn trang phải có đúng một
  `div[role="rowheader"][aria-label="Timed"]`. Sai rule/impact/node count/target/HTML/
  scope hoặc xuất hiện finding khác đều fail.

### `PENDING_NVDA_REVIEW`

Lượt yêu cầu mở runtime NVDA ngày 2026-07-24 đã hết thời gian chờ approval; không có
review nào được thực hiện và không có kết quả để suy diễn. Marker vì vậy vẫn là
**pending**, không phải PASS/FAIL. Agent chính/reviewer điền kết quả thực tế ở đây,
không suy diễn từ Axe:

```text
NVDA version:
Mode: temporary / installed test environment
Reviewer:
Date:
Heading/landmark:
Event label/time/category:
View switch/Agenda context:
Keyboard alternative:
Focus restore after 409:
Live announcement:
Result: PASS / FAIL
```

Trước khi marker này được đóng bằng `PASS`, ADR-0019 không được gọi
production-ready và FullCalendar không được nối vào route Calendar thật. Nếu `FAIL`,
P3-02A bị chặn cho tới khi sửa spike và chạy lại review.

## Security/license notes

- Guard đọc package manifest, source, notice và root lock để reject Premium/resource,
  remote CSS/script cùng analytics/telemetry.
- Guard test `3/3` đạt; Standard MIT notices được giữ trong
  `THIRD_PARTY_NOTICES.md`.
- `@axe-core/playwright` chỉ là dev dependency; không vào Calendar runtime bundle.
- Spike không đọc/ghi `.env*.local` và không chứa secret, token hoặc URL có credential.
