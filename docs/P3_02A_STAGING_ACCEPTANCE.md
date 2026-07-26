# P3-02A Staging Acceptance

## Kết luận

- Ngày nghiệm thu: 2026-07-26.
- Phạm vi: Professional Calendar shell, read projection, display preference và thao tác
  one-time ClassSession đã được P3-01 cấp quyền.
- Trạng thái: `DONE`.
- Commit runtime được nghiệm thu: `0606813`.
- Web: `https://tutorhub-web.pages.dev`.
- Core API: `https://tutorhub-core-api.onrender.com`.

## Automated gate

- Go test, vet và integration-tag compile đạt.
- Generated API client: 22/22.
- Web unit/integration: 176/176; lint, typecheck và production build đạt.
- Calendar production guard, security suite 19/19 và 20 cặp contrast token đạt.
- Production-route Playwright: 8/8, gồm authorization/range/pagination, Axe,
  semantic Agenda, zoom 200%, forced-colors, reduced-motion và visual snapshots
  desktop/tablet/mobile.
- Manual NVDA: PASS cho heading/landmark, event label/time/category, view switch/Agenda,
  keyboard alternative, focus restore sau `409` và live announcement.

| Visible item | Render p95 | Navigation p95 | Long task max | ADR-0019 budget |
| ------------ | ---------- | -------------- | ------------- | --------------- |
| 500          | 177 ms     | 266 ms         | 102 ms        | Đạt             |
| 1.000        | 310 ms     | 481 ms         | 180 ms        | Đạt             |
| 2.000        | 570 ms     | 716 ms         | 326 ms        | Đạt             |

Calendar lazy chunk của production build là khoảng 298,64 kB, gzip 82,29 kB.

## Deployment và public smoke

- Neon staging đã được migrate sạch `14 false -> 17 false` trước vòng nghiệm thu.
- Exact runtime ACL cho calendar preference/projection đã được probe xanh.
- Render Auto-Deploy đang tắt; commit `0606813` được deploy bằng **Deploy latest commit**.
- Render xác nhận service `Live` tại đúng commit.
- Các endpoint sau đều trả HTTP 200:
  - `https://tutorhub-core-api.onrender.com/health`
  - `https://tutorhub-core-api.onrender.com/ready`
  - `https://tutorhub-web.pages.dev/api/health`
  - `https://tutorhub-web.pages.dev/api/ready`
- Frontend xử lý fail-closed nhưng tương thích với capability response cũ chưa có
  `in_app_notifications`; regression test đã khóa hành vi này.

## Browser acceptance

Browser staging đã được kiểm tra với phiên Admin:

1. Calendar shell tải thành công và hiển thị empty-state đúng; không còn crash hoặc
   false-forbidden.
2. Chuyển từ Week sang Agenda cập nhật URL thành
   `/app/calendar?view=agenda&date=2026-07-26`.
3. Preference drawer tải đủ timezone, locale, hour format, week start, default view,
   density, time scale và secondary timezone.
4. Quick Create hiển thị đúng trạng thái `Không có lớp có thể lên lịch` khi principal
   không có lớp phù hợp.

Không có mutation nào được tạo trong vòng browser acceptance này.

## Giới hạn phạm vi

P3-02A không bao gồm recurrence/conflict, participant/free-busy/RSVP, email/ICS hoặc
Availability Poll/Study Meeting. Các phần đó lần lượt thuộc P3-02B, P3-02C,
P3-CAL-02/P3-05A và P3-02D/P3-05B. Worker side effect vẫn phải chờ P3-03B/P3-04 durable
host và crash/reclaim gate.
