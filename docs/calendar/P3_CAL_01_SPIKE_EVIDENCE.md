# P3-CAL-01 technical spike evidence

- Trạng thái: automated local evidence đã thu thập; browser/manual gate còn mở;
  ADR-0019 vẫn `PROPOSED`
- Package: `apps/calendar-spike`
- Go spike: `services/core-api/internal/spikes/calendarrecurrence`
- Mục đích: kiểm chứng renderer/recurrence/theme trước khi nối `apps/web` hoặc
  production API

## Dependency snapshot

| Component | Pin | License | Ghi chú |
| --- | --- | --- | --- |
| `@fullcalendar/react` | `7.0.1` | MIT | Standard only; entrypoint `/daygrid`, `/timegrid`, `/list`, `/interaction` |
| `temporal-polyfill` | `1.0.1` | MIT | peer dependency của FullCalendar v7 |
| fallback `@fullcalendar/*` | `6.1.21` | MIT | chỉ build so sánh cô lập, không cài production đồng thời |
| `github.com/teambition/rrule-go` | `v1.8.2` | MIT | candidate; release history cũ, adapter phải bounded |
| `@axe-core/playwright` | `4.12.1` | MPL-2.0 | dev-only Axe runner, không nằm runtime app |

Metadata được lấy từ npm/Go module registry ngày 2026-07-23; root lockfile được cập
nhật và review ngày 2026-07-24. Không lấy dependency
Premium, scheduler/resource timeline hoặc telemetry.

## Cấu trúc spike

```text
apps/calendar-spike/
  src/calendar/CalendarSurface.tsx       # domain ↔ renderer adapter
  src/calendar/FullCalendarSurface.tsx   # FullCalendar-only boundary
  src/calendar/civilTime.ts               # Temporal gap/overlap fixture
  src/calendar/domain.ts                  # optimistic mutation/revert contract
  src/calendar/fixtures.ts                # 48/500/1000/2000 deterministic items
  src/*.test.*                            # unit/DOM contract
  e2e/calendar-spike.spec.ts              # Playwright + Axe + mobile/perf
  scripts/check-dependencies.*            # Premium/telemetry/license guard
```

Go recurrence adapter được giữ trong `internal/spikes`, chưa được import bởi
production classroom service.

## Lệnh tái lập

Từ root:

```powershell
corepack pnpm install --frozen-lockfile
corepack pnpm --filter @tutorhub/calendar-spike security:dependencies
corepack pnpm --filter @tutorhub/calendar-spike test:guard
corepack pnpm --filter @tutorhub/calendar-spike typecheck
corepack pnpm --filter @tutorhub/calendar-spike lint
corepack pnpm --filter @tutorhub/calendar-spike test
corepack pnpm --filter @tutorhub/calendar-spike e2e
go test ./services/core-api/internal/spikes/calendarrecurrence -count=1
go test ./services/core-api/internal/spikes/calendarrecurrence -run=^$ \
  -fuzz=Fuzz -fuzztime=30s
go test ./services/core-api/internal/spikes/calendarrecurrence \
  -bench=. -benchmem -count=5
```

Root lockfile đã có importer `apps/calendar-spike`, exact runtime pin và dev-only test
dependencies. Kết quả dưới đây là local evidence, chưa phải CI hoặc staging evidence.

## Automated acceptance matrix

| Gate | Test/evidence | Trạng thái |
| --- | --- | --- |
| React 19/Vite/StrictMode | typecheck, 8 unit/DOM test, build | PASS local |
| Renderer adapter | `CalendarSurface.test.ts` | PASS local |
| Drag/resize/revert | `domain.test.ts` pass; browser conflict fixture | PARTIAL; E2E pending |
| DST gap/overlap | `civilTime.test.ts`, HCM/New York fixtures | PASS local |
| Keyboard/Agenda/mobile | Playwright keyboard + mobile tests | BLOCKED/pending |
| Axe | `@axe-core/playwright` desktop scan | BLOCKED/pending |
| 500/1000/2000 performance | browser p50/p95 log | pending valid browser run |
| Recurrence bounded | Go golden/property/fuzz/cap/benchmark | PASS local |
| Dependency/license | `security:dependencies`, guard unit, lock review | PASS local |

## Kết quả automated local

Ngày 2026-07-24:

```text
dependency guard: pass
guard unit:       2/2 pass
TypeScript:       pass
unit/DOM:         4 files, 8 tests pass
Vite build:       pass
Calendar JS gzip: 154.94 KiB
Calendar CSS gzip: 5.35 KiB
Go recurrence:    golden/property/cap/fuzz/benchmark pass
```

Các package Go P3-01 liên quan cũng đạt test local, nhưng được ghi ở
`docs/PROJECT_STATE.md`; tài liệu này chỉ làm evidence cho spike.

Playwright Chromium có khởi chạy nhưng không hoàn tất navigation/teardown trong môi
trường local và bị timeout. Lượt này không tạo pass giả: không có Axe report hoặc
performance sample hợp lệ. Cần chạy lại trên CI-like Chromium hoặc môi trường browser
ổn định trước khi thay đổi trạng thái ADR.

## Performance run log

Ghi mỗi run bằng JSON, không chỉ ghi pass/fail:

```text
commit:
machine:
browser:
node:
fixtureCount | renderReadyMs | p50 | p95 | navigationP95 | longTaskMax | heapDelta
```

Tạm thời E2E chỉ có catastrophic guardrail 5/8/12 giây cho 500/1000/2000 item.
Ngưỡng quyết định chính thức nằm trong ADR-0019 và chỉ được đánh dấu đạt sau khi
đã thu thập tối thiểu 5 lần chạy trên CI-like Chromium. Không dùng benchmark Go
thay cho browser renderer evidence.

Một benchmark Go đại diện cho expansion 500 occurrence:

```text
UTC:              ~0.552 ms, ~218 KiB, ~3,532 allocs
Asia/Ho_Chi_Minh: ~1.16 ms,  ~242 KiB, ~4,529 allocs
America/New_York: ~1.80 ms,  ~242 KiB, ~4,529 allocs
```

Đây là số tham khảo của một local run, không phải p50/p95, CI baseline hoặc SLO.

## Manual/browser review chưa hoàn tất

- Playwright Chromium desktop/mobile, Axe và 500/1.000/2.000 browser performance.
- NVDA/Windows: kiểm tra heading/landmark, event label, view switch, focus restore
  sau revert và live announcement.
- Zoom 200%, Windows High Contrast/forced-colors, reduced-motion.
- Kiểm tra pointer drag/resize thực tế trên desktop và keyboard-only equivalent.
- Không tuyên bố production-ready hoặc ADR `Accepted` trước khi reviewer ký các mục này.

## Security/license notes

- Guard đọc package manifest, source và root lock để reject Premium/resource,
  remote CSS/script và analytics/telemetry.
- Standard MIT cần giữ copyright/license notice khi phân phối.
- `@axe-core/playwright` chỉ là dev dependency phục vụ kiểm thử; không đưa vào
  Calendar runtime bundle.
- Không đọc hoặc ghi `.env*.local`; spike không chứa secret, token hay URL có credential.
