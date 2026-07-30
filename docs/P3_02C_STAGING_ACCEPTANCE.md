# P3-02C Staging Acceptance

## Trạng thái

| Hạng mục | Giá trị |
| --- | --- |
| Phạm vi | Working hours, attendee/free-busy, audience, RSVP và participation lifecycle |
| Trạng thái task | `VERIFY` |
| Neon staging | `21 false` (migration/ACL smoke đã đối chiếu) |
| Migration mong đợi | `21 false` |
| Commit Core API | `32a770ac` |
| Render deployment | Live exact `32a770ac` (`dep-d9lc4boae00c73a4i8v0`) |
| Commit web | `32a770ac` |
| Cloudflare deployment | Live exact `32a770ac` |

Tài liệu này vừa là runbook vừa là sổ bằng chứng không nhạy cảm. P3-02C vẫn ở
`VERIFY`: một số happy-path staging đã đạt, nhưng các gate bắt buộc chưa đủ để chuyển
sang `DONE`.

### Cập nhật staging 2026-07-30

- Teacher đã lưu audience gồm attendee nội bộ và một external fixture; tải lại vẫn
  giữ đúng dữ liệu. PUT audience trả HTTP `200` trên Render.
- Student đúng vai trò `Học viên` chỉ thấy phần phản hồi của chính mình, không thấy
  email hoặc roster external. Student chọn `Tham gia`; sau reload nút vẫn ở trạng thái
  đã chọn và UI hiển thị `Đã lưu phản hồi.`.
- Backend ghi nhận self-RSVP `POST /responses` HTTP `200`, sau đó
  `GET /attendees` HTTP `200`; log có request ID và không chứa token/ciphertext.
- Regression tự động chạy lại ở local: focused Go calendar/classroom/http API suites
  đều đạt; API client đạt `27/27`; `corepack pnpm e2e:calendar` đạt `11/11`.
  Các suite bao phủ WorkingSchedule CAS, privacy của Scheduling Assistant, focus
  recovery sau RSVP `409` và public-RSVP confirm/fragment scrub, nhưng không phát
  hành capability trên shared staging.
- Render Free không cung cấp Render Shell cho service này. Vì vậy public RSVP
  capability fixture chưa thể phát hành bằng binary server-side theo runbook; gate
  này được ghi `BLOCKED`, không dùng raw token hoặc SQL shortcut.
- Teacher đã xác nhận lịch làm việc đã lưu lại sau reload với hai khoảng trong ngày,
  timezone IANA và một exception theo ngày. Scheduling Assistant trả về đúng 10 gợi ý
  có giới hạn; UI chỉ hiển thị trạng thái/reason cần thiết, không lộ nội dung lịch riêng.
- Một lifecycle fixture chỉ dùng cho staging đã được tạo, lưu audience nội bộ với yêu
  cầu RSVP, sau đó bị hủy bởi organizer. Sau reload trạng thái vẫn là `Đã hủy` và lịch
  sử được giữ lại. Không ghi identifier, email hoặc capability vào bằng chứng này.

## Phạm vi và ranh giới

P3-02C nghiệm thu:

- working schedule nhiều khoảng trong ngày, exception/ngày nghỉ và timezone IANA;
- truy vấn free/busy có kết quả giới hạn, sắp xếp xác định và không lộ dữ liệu lịch;
- audience nội bộ/bên ngoài cho one-time, series và occurrence;
- RSVP nội bộ và RSVP công khai qua capability;
- chuyển organizer, cancel/archive và vòng đời capability;
- tenant isolation, idempotency, optimistic concurrency, privacy và accessibility.

P3-02C chỉ tạo snapshot lời mời trung lập. Email, Amazon SES, ICS/MIME, retry gửi
thư và delivery effect `REQUEST`/`CANCEL` thuộc P3-05A/P3-CAL-02, không phải exit
gate của checklist này.

## Điều kiện trước khi chạy

- Dùng Neon branch `staging` hoặc disposable branch tạo từ staging; không chạy
  mutation acceptance trên production hoặc tenant có dữ liệu thật.
- Render Core API và Cloudflare Pages phải chạy đúng commit cần nghiệm thu.
- Có ba identity riêng: Organization Admin/manager, Teacher/organizer và Student/
  ordinary attendee. Dùng thêm một địa chỉ bên ngoài do người kiểm thử kiểm soát.
- Feature server `class_session_scheduling` được bật riêng cho tenant canary sau
  khi đã kiểm tra nhánh fail-closed. Kill switch
  `FEATURE_CONTROL_DISABLE_CLASS_SESSION_SCHEDULING` vẫn phải được nghiệm thu.
- Core API có cấu hình protected-data hợp lệ. Chỉ xác nhận biến cấu hình đã tồn tại;
  không đọc hoặc ghi giá trị của `CALENDAR_PROTECTED_DATA_KEY` hay credential khác.
- Capability công khai phải được phát hành bằng fixture/tool phía server do đội dự
  án kiểm soát. Không tự chèn raw token vào SQL và không lấy token từ log.

Không ghi database URL, cookie, storage state, email thật, capability token, URL
fragment, ciphertext hay key material vào tài liệu, terminal log, screenshot hoặc
artifact CI. Dùng alias đã che khi cần lưu bằng chứng.

## Database gate

### 1. Migration

Chạy bằng migration-owner connection trên staging/disposable branch:

```powershell
corepack pnpm db:version
corepack pnpm db:migrate
corepack pnpm db:version
```

Kết quả cuối bắt buộc:

```text
21 false
```

Hai migration trong phạm vi:

- `000020_calendar_availability_participation`
- `000021_calendar_invitation_snapshot_delivery_boundary`

Không chạy down migration `000020` trên shared staging vì down migration xóa dữ
liệu. Chỉ chạy vòng `up -> down -> up` trên database disposable.

### 2. Runtime ACL tối thiểu

Migration `000020` revoke `PUBLIC` nhưng không tự cấp quyền cho runtime role. Chạy
SQL sau bằng migration owner:

```sql
BEGIN;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_runtime;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.calendar_working_schedules,
    tutorhub.calendar_working_schedule_intervals,
    tutorhub.calendar_working_schedule_exceptions,
    tutorhub.calendar_working_schedule_exception_intervals,
    tutorhub.calendar_external_recipients,
    tutorhub.class_session_attendees,
    tutorhub.calendar_invitation_revisions,
    tutorhub.calendar_invitation_recipients,
    tutorhub.calendar_rsvp_capabilities,
    tutorhub.calendar_participation_mutation_receipts
FROM tutorhub_runtime;

GRANT SELECT, INSERT, UPDATE
ON tutorhub.calendar_working_schedules,
   tutorhub.calendar_external_recipients,
   tutorhub.class_session_attendees,
   tutorhub.calendar_rsvp_capabilities
TO tutorhub_runtime;

GRANT SELECT, INSERT, DELETE
ON tutorhub.calendar_working_schedule_intervals,
   tutorhub.calendar_working_schedule_exceptions
TO tutorhub_runtime;

GRANT SELECT, INSERT
ON tutorhub.calendar_working_schedule_exception_intervals,
   tutorhub.calendar_invitation_revisions,
   tutorhub.calendar_invitation_recipients,
   tutorhub.calendar_participation_mutation_receipts
TO tutorhub_runtime;

COMMIT;
```

Không cấp `TRUNCATE`, `REFERENCES` hoặc `TRIGGER`. Snapshot invitation và mutation
receipt là append-only; runtime không được `UPDATE` hoặc `DELETE` chúng.

### 3. ACL mismatch probe

Chạy query sau và yêu cầu **zero rows**:

```sql
WITH target_tables(table_name) AS (
    VALUES
        ('calendar_working_schedules'),
        ('calendar_working_schedule_intervals'),
        ('calendar_working_schedule_exceptions'),
        ('calendar_working_schedule_exception_intervals'),
        ('calendar_external_recipients'),
        ('class_session_attendees'),
        ('calendar_invitation_revisions'),
        ('calendar_invitation_recipients'),
        ('calendar_rsvp_capabilities'),
        ('calendar_participation_mutation_receipts')
),
verbs(privilege_type) AS (
    VALUES
        ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'),
        ('TRUNCATE'), ('REFERENCES'), ('TRIGGER')
),
expected_grants(table_name, privilege_type) AS (
    VALUES
        ('calendar_working_schedules', 'SELECT'),
        ('calendar_working_schedules', 'INSERT'),
        ('calendar_working_schedules', 'UPDATE'),
        ('calendar_working_schedule_intervals', 'SELECT'),
        ('calendar_working_schedule_intervals', 'INSERT'),
        ('calendar_working_schedule_intervals', 'DELETE'),
        ('calendar_working_schedule_exceptions', 'SELECT'),
        ('calendar_working_schedule_exceptions', 'INSERT'),
        ('calendar_working_schedule_exceptions', 'DELETE'),
        ('calendar_working_schedule_exception_intervals', 'SELECT'),
        ('calendar_working_schedule_exception_intervals', 'INSERT'),
        ('calendar_external_recipients', 'SELECT'),
        ('calendar_external_recipients', 'INSERT'),
        ('calendar_external_recipients', 'UPDATE'),
        ('class_session_attendees', 'SELECT'),
        ('class_session_attendees', 'INSERT'),
        ('class_session_attendees', 'UPDATE'),
        ('calendar_invitation_revisions', 'SELECT'),
        ('calendar_invitation_revisions', 'INSERT'),
        ('calendar_invitation_recipients', 'SELECT'),
        ('calendar_invitation_recipients', 'INSERT'),
        ('calendar_rsvp_capabilities', 'SELECT'),
        ('calendar_rsvp_capabilities', 'INSERT'),
        ('calendar_rsvp_capabilities', 'UPDATE'),
        ('calendar_participation_mutation_receipts', 'SELECT'),
        ('calendar_participation_mutation_receipts', 'INSERT')
),
matrix AS (
    SELECT
        target_tables.table_name,
        verbs.privilege_type,
        expected_grants.table_name IS NOT NULL AS expected
    FROM target_tables
    CROSS JOIN verbs
    LEFT JOIN expected_grants
      ON expected_grants.table_name = target_tables.table_name
     AND expected_grants.privilege_type = verbs.privilege_type
)
SELECT
    table_name,
    privilege_type,
    expected,
    has_table_privilege(
        'tutorhub_runtime',
        format('tutorhub.%I', table_name),
        privilege_type
    ) AS actual
FROM matrix
WHERE has_table_privilege(
          'tutorhub_runtime',
          format('tutorhub.%I', table_name),
          privilege_type
      ) IS DISTINCT FROM expected
ORDER BY table_name, privilege_type;
```

Xác nhận thêm runtime role không có đặc quyền owner:

```sql
SELECT
    rolname,
    rolsuper,
    rolcreaterole,
    rolcreatedb,
    rolbypassrls,
    pg_has_role('tutorhub_runtime', 'neondb_owner', 'member')
        AS runtime_member_of_owner
FROM pg_roles
WHERE rolname = 'tutorhub_runtime';
```

Kết quả bắt buộc: đúng một hàng, mọi cờ đặc quyền và
`runtime_member_of_owner` đều `false`. Nếu migration owner không phải
`neondb_owner`, thay tên role trong probe bằng migration-owner role thực tế.

## Automated gate

Chạy trên commit cần nghiệm thu:

```powershell
corepack pnpm verify
corepack pnpm e2e:calendar
```

Chỉ chạy `corepack pnpm test:integration` với PostgreSQL **disposable/local** có tên database
dành cho test. Không chạy integration suite có mutation hoặc migration `down` trên shared
staging. Với shared staging, dùng migration version/ACL probe ở trên và các smoke test có
phạm vi được kiểm soát ở phần dưới.

Ghi kết quả vào bảng cuối tài liệu. `corepack pnpm e2e:calendar` bao gồm regression
P3-02C cho working schedule CAS/exception, Scheduling Assistant privacy reason,
internal RSVP `409` focus recovery và public RSVP fragment scrubbing/explicit confirm.
Mock E2E này không thay thế live staging: vẫn phải chạy checklist UI/authorization,
public capability và NVDA trên deployment đúng commit.

## API và authorization smoke

### Feature control

- [ ] Khi feature tắt, working schedule, availability, audience, RSVP và organizer
      transfer đều fail closed trước query/mutation.
- [ ] Khi deployment kill switch bật, tenant override không thể bật ngược.
- [ ] Khi feature bật cho tenant canary, chỉ actor có quyền tương ứng mới mutate.

### Working hours và availability

- [ ] Lưu nhiều interval trong một ngày, IANA timezone, ngày nghỉ, out-of-office và
      exception có interval; reload vẫn giữ đúng dữ liệu.
- [ ] `expected_version` cũ trả `409`; payload hợp lệ đang mở không bị mất.
- [ ] Ngày DST gap/fold tạo kết quả xác định; khoảng thời gian được xử lý theo
      boundary nửa mở.
- [ ] External/no-sync hiển thị `unknown`, không được suy diễn thành `free`.
- [ ] Query 1–31 ngày, 1–50 participant, duration 15–480 phút và step
      15/30/60 hoạt động; vượt hard cap bị từ chối.
- [ ] Kết quả tối đa 20 suggestion, không duyệt quá 2.000 candidate start và đáp
      ứng deadline 250 ms theo ADR-0023.
- [ ] Cùng input cho cùng output/order; reason giải thích busy/outside-hours/
      unknown nhưng không lộ title, description, class, roster hay file.
- [ ] ID tenant/class/member chéo tenant trả concealment và không mutate.

### Audience và RSVP nội bộ

- [ ] Tổng internal + external sau dedupe không vượt 128 recipient.
- [ ] Add/remove/unchanged/role-change đúng cho one-time, series và occurrence.
- [ ] Metadata-only edit giữ RSVP; đổi role/time/organizer hoặc split áp dụng đúng
      reset rule; remove đóng response và revoke capability.
- [ ] Remove rồi add lại không tái sử dụng capability hoặc RSVP snapshot cũ.
- [ ] Student/ordinary attendee chỉ cập nhật RSVP của chính mình.
- [ ] Cùng idempotency key + cùng payload replay an toàn; cùng key + payload khác
      trả conflict và không nhân đôi receipt/snapshot.
- [ ] Hai mutation đồng thời với cùng `expected_version` chỉ có một winner; request
      còn lại trả `409` và không tạo partial state.
- [ ] Snapshot invitation giữ stable UID, sequence/source version tăng đơn điệu,
      append-only và không chứa delivery effect/MIME của P3-05A.

## Public RSVP

Endpoint:

- `POST /api/v1/calendar/invitations/resolve`
- `POST /api/v1/calendar/invitations/respond`
- trang công khai `/calendar/respond`

Checklist:

- [ ] Fixture phía server phát hành hai capability riêng theo purpose
      `resolve`/`respond`; token không xuất hiện trong database ở dạng thô.
- [ ] Link đặt token trong fragment
      `#resolve_token=...&respond_token=...`; trang scrub fragment trước request và
      không ghi token vào query string, cache key, storage, log hay telemetry.
- [ ] Trang resolve chỉ hiển thị snapshot tối thiểu; respond yêu cầu xác nhận rõ
      trước mutation.
- [ ] Respond từ Origin ngoài web origin cấu hình bị từ chối.
- [ ] Response có `Cache-Control: no-store`, `Referrer-Policy: no-referrer`,
      `X-Robots-Tag: noindex, nofollow`, CSP chặt và CORP same-origin.
- [ ] Token malformed, expired, revoked, superseded hoặc response window đã đóng
      đều trả thông báo chung, không giúp phân biệt recipient/session tồn tại.
- [ ] Rate cap resolve đạt 10/phút/token và 30/phút/IP; respond đạt
      5/10 phút/token và 20/10 phút/IP; `429` có `Retry-After`.
- [ ] Replay cùng idempotency key an toàn; key collision hoặc stale state trả
      `409` không thay đổi RSVP lần hai.
- [ ] Sau audience remove, organizer transfer, cancel hoặc archive, capability cũ
      không còn dùng được.

API không mở capability-issuance endpoint cho người dùng; P3-05A mới tạo delivery
link trong luồng business. Riêng nghiệm thu staging dùng binary kiểm soát trong
container Core API và tuyệt đối không tự tạo token bằng SQL:

```sh
P3_02C_STAGING_FIXTURE_ENABLED=true /app/tutorhub-calendar-rsvp-fixture \
  --tenant-id <staging-tenant-uuid> \
  --actor-id <authorized-teacher-or-admin-uuid> \
  --class-id <staging-class-uuid> \
  --invitation-revision-id <immutable-revision-uuid> \
  --invitation-recipient-id <external-recipient-uuid> \
  --confirm ISSUE-P3-02C-STAGING-CAPABILITIES
```

Guard vận hành:

- chỉ chạy trong Render Shell của deployment staging với `APP_ENV=staging`; binary
  từ chối development/test/production, mặc định vẫn bất hoạt nếu thiếu opt-in tạm thời
  `P3_02C_STAGING_FIXTURE_ENABLED=true`, và không đọc file `.env*.local`;
- actor, tenant, class, revision và recipient phải là fixture staging do operator
  kiểm soát; role của actor được đọc từ active membership trong database (không nhận
  role từ cờ CLI), rồi service vẫn authorize `session.schedule` và feature control;
- lệnh chỉ in đúng một link `/calendar/respond#resolve_token=...&respond_token=...`
  lên stdout sau khi cả hai capability được phát hành; database chỉ giữ digest;
- copy link trực tiếp sang cửa sổ nghiệm thu. Không paste link/token vào chat, tài liệu,
  screenshot, command history, ticket, log hoặc artifact;
- nếu lệnh lỗi, stdout không chứa link. Có thể chạy lại với cùng source IDs: issuance
  mới rotate capability cùng purpose; raw token của lượt lỗi không thể khôi phục;
- sau nghiệm thu phải kiểm tra remove/transfer/cancel/archive revoke link cũ và không
  ghi link thật vào bảng bằng chứng. Chỉ ghi `PASS/FAIL`, UTC, commit và mã lỗi an toàn.

## Organizer, cancellation và archive

- [ ] Chỉ actor được phép mới chuyển organizer; dùng CAS, idempotency và audit.
- [ ] Transfer giữ stable UID, tăng sequence/source version, reset RSVP theo policy
      và rotate/revoke toàn bộ capability cũ.
- [ ] Organizer cũ mất quyền ngay sau transaction; organizer mới có quyền đúng.
- [ ] Organizer bị disable/remove không được thay ngầm; session vào trạng thái cần
      xử lý/blocked theo policy.
- [ ] Cancel one-time/series/occurrence đóng response và revoke capability trong
      cùng transaction nhưng vẫn giữ lịch sử.
- [ ] Archive class áp dụng cùng lifecycle closure; không còn public RSVP hợp lệ.
- [ ] Fault/concurrency không tạo trạng thái nửa vời giữa attendee, snapshot,
      capability và receipt.

## Privacy và UI/accessibility

- [ ] Ordinary participant chỉ thấy RSVP của mình và aggregate được policy cho
      phép; guest list/individual status cần capability phù hợp.
- [ ] Availability không trả dữ liệu lớp/lịch riêng tư; ciphertext và fingerprint
      không xuất hiện ở API response hoặc log.
- [ ] Foreign tenant/class/session/series/occurrence đều conceal `404`.
- [ ] Calendar hiển thị working hours, busy/unknown và reason bằng text/icon; màu
      không phải tín hiệu duy nhất.
- [ ] Primary và secondary timezone được gắn nhãn rõ.
- [ ] Keyboard có luồng tương đương cho mở panel, chọn attendee, RSVP, confirm và
      đóng dialog; focus hợp lý sau success/error/`409`.
- [ ] Heading, landmark, accessible name và live status được NVDA đọc đúng.
- [ ] Loading, empty, error, forbidden, rate-limited và retry đều có trạng thái rõ.

## Observability và log privacy

- [ ] Request ID có mặt ở lỗi có cấu trúc và audit cho mutation quan trọng.
- [ ] Có thể quan sát timeout, conflict, rate limit và lifecycle rejection mà không
      log email, title, description, token, ciphertext, RSVP payload hay roster.
- [ ] Không upload screenshot/trace/video chứa URL fragment, cookie hoặc thông tin
      người học.

## Kết quả nghiệm thu

| Gate | Kết quả | Bằng chứng không nhạy cảm |
| --- | --- | --- |
| Migration `21 false` | `PASS` | Neon staging version `21`, dirty `false` |
| Runtime ACL mismatch = zero rows | `PASS` | Probe trả zero rows |
| Runtime role isolation | `PASS` | Runtime role không có owner/superuser/bypass đặc quyền |
| `corepack pnpm verify` | `PASS` | Local exact commit `32a770ac` |
| Focused regression | `PASS` | Go calendar/classroom/http API; API client `27/27`; Calendar E2E `11/11` |
| `corepack pnpm test:integration` | `NOT RUN` | Không chạy mutation suite trên shared staging; focused PostgreSQL coverage đã PASS local |
| Feature/kill-switch smoke | `NOT RUN` | Chưa có live fail-closed evidence |
| Working schedule/free-busy API | `PARTIAL` | Teacher reload giữ hai interval, timezone IANA và exception; Scheduling Assistant trả 10 gợi ý bounded, privacy-safe. DST/cap/foreign-tenant edge còn mở |
| Audience/internal RSVP/concurrency | `PARTIAL` | Audience PUT/reload, Student self-RSVP/reload và audience lifecycle fixture PASS; idempotency/concurrency/diff lifecycle live còn mở |
| Public RSVP/capability | `BLOCKED` | Render Free không có Shell để chạy server-issued fixture binary |
| Organizer/cancel/archive | `PARTIAL` | Organizer tạo rồi hủy lifecycle fixture trên staging; reload giữ `Đã hủy` và lịch sử. Transfer, archive và capability revocation live còn mở |
| Tenant/privacy/IDOR | `PARTIAL` | Student privacy projection PASS; full cross-tenant/IDOR matrix chưa chạy |
| UI/keyboard/NVDA | `PARTIAL` | Semantic/detail/RSVP path đã kiểm tra; keyboard/NVDA đầy đủ cho P3-02C chưa chạy |
| Observability/log privacy | `PARTIAL` | Request ID/status log đã đối chiếu; full timeout/rate/lifecycle privacy matrix còn mở |

## Exit gate

P3-02C chỉ chuyển `DONE` khi:

- database là `21 false`, exact ACL và runtime-role isolation đều đạt;
- cùng commit được deploy lên Render và Cloudflare;
- automated gate, API/authorization, concurrency và privacy smoke đạt;
- public RSVP được kiểm tra qua capability do server phát hành, không có đường tắt
  raw token;
- organizer transfer, cancel/archive, UI keyboard/NVDA và log privacy đều đạt;
- commit/deployment/result không nhạy cảm được ghi vào bảng trên.

Nếu bất kỳ gate bắt buộc nào chưa chạy hoặc bị chặn, trạng thái vẫn là `VERIFY`.
