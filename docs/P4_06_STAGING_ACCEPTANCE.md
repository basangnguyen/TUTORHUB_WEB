# P4-06 — Participant roster, hand raise và reaction staging acceptance

## 1. Trạng thái và ranh giới

- Trạng thái hiện tại: `DONE` ngày 2026-08-13. Local/disposable, exact candidate CI/security,
  shared staging, exact deploy và live feature-off/no-side-effect acceptance đều `PASS`.
- P4-06 thay fallback roster session-local của P4-05 bằng projection có version do Core API/
  PostgreSQL sở hữu. LiveKit chỉ cung cấp media transport và active-speaker/quality tức thời.
- `CanPublishData=false` tiếp tục là invariant; client metadata, provider identity, DataChannel,
  display name và client clock không phải authority cho roster, role, FIFO hoặc reaction.
- Feature `classroom_media_rooms` và `instant_study_rooms` tiếp tục deployment-force-off. Không bật
  shared/live capability chỉ để nghiệm thu candidate.
- Migration của task là forward-only `000032_media_participant_signals`. Không rollback. Disposable
  phải đạt toàn bộ database gate trước khi xin quyền forward shared staging.
- Actual 25/50 participant provider storm, physical browser/device và manual assistive-technology
  matrix vẫn thuộc P4-11. P4-06 phải đạt deterministic 25/50 projection/DOM/rate-limit tests nhưng
  không được suy các gate P4-11 là `PASS`.

Authority:

- [Phase 4 backlog](PHASE_4_BACKLOG.md), mục P4-06;
- [ADR-0030](adr/0030-authoritative-classroom-media-spaces-lifecycle-and-livekit-grants.md);
- [ADR-0031](adr/0031-classroom-media-ux-devices-layout-effects-and-signals.md);
- [P4-MEDIA-UX-00 research report](P4_MEDIA_UX_00_RESEARCH_REPORT.md).

## 2. Candidate contract

### 2.1 Privacy-safe participant projection

- Snapshot giới hạn tối đa 50 participant đang joining/connected/reconnecting và mang
  `projection_version`, `last_signal_sequence` cùng server time.
- Mỗi participant có `participant_key` opaque và `roster_sequence` tăng đơn điệu trong exact
  RoomInstance. Grid sort theo `roster_sequence`, tie-break bằng `participant_key`.
- Snapshot chỉ trả bounded display name, instance role và coarse connection state cần cho UI; không
  trả email, user ID, ParticipantSession ID, join-attempt ID hoặc provider participant identity.
- Signed LiveKit credential chỉ chiếu `participant_key` opaque để nối media track với server roster;
  browser không được tự sửa metadata và không thể dùng attribute này để nâng quyền.
- Client lấy snapshot khi connect, reconnect, tab resume, command timeout hoặc phát hiện version/
  sequence gap. Snapshot mới thắng event/response cũ; duplicate và out-of-order không rollback state.

### 2.2 Hand raise

- Typed Core API command: self `hand_raise`/`hand_lower`; moderator `hand_lower_one`/
  `hand_lower_all`. Server reload tenant, active membership, source access, exact RoomInstance và
  ParticipantSession tại mutation.
- Một participant có tối đa một active hand. FIFO theo `signal_sequence` và `accepted_at` của server;
  client time, request arrival trên browser và display name không tham gia order.
- Stable idempotency key bảo đảm retry không tạo queue entry thứ hai. Raise/lower lặp hội tụ về cùng
  snapshot. Active speaker không tự hạ tay.
- Hand được dọn khi participant left/removed hoặc RoomInstance terminal. Grid không reorder theo hand.
- Initial cross-instance limits: actor tối đa 6 hand command/60 giây; room tối đa 120/60 giây;
  moderator lower-all tối đa 6/60 giây. `429` trả bounded `Retry-After`.

### 2.3 Reaction

- Exact enum: `thumbs_up`, `clap`, `heart`, `celebrate`, `laugh`, `surprised`.
- Server cấp opaque event ID, sequence, accepted/expiry time. Mỗi event hết hạn sau 10 giây và không
  trở thành chat, audit, attendance hoặc grade.
- Signal idempotency receipt giữ tối đa 24 giờ; replay trong cửa sổ trả cùng kết quả. Reaction đã hết
  hạn và receipt quá hạn chỉ được hard-delete qua hai maintenance function bounded/
  `SECURITY DEFINER`/`FOR UPDATE SKIP LOCKED`; Core API runtime không có direct table `DELETE`.
- Cùng reaction trong cửa sổ server 750 ms được gộp; snapshot giữ tối đa 50 bounded summary cluster,
  UI animate tối đa 3 visual cluster và count hiển thị `99+`, không tạo DOM/live-region queue vô hạn.
- Initial cross-instance limits: actor 3/5 giây và 20/60 giây; RoomInstance 100/5 giây. Unknown,
  malformed hoặc oversized payload bị reject trước khi lưu/fan-out và không raw-log payload.
- Reduced motion bỏ animation bay nhưng giữ icon/count/text. Screen reader nhận summary polite tối đa
  một lần/2 giây; own ACK có status riêng và không đọc danh sách tên người reaction.

### 2.4 HTTP và concurrency

- `GET /api/v1/media/spaces/{space_id}/participants` lấy bounded authoritative snapshot.
- `POST /api/v1/media/spaces/{space_id}/signals` nhận exact RoomInstance/version/projection,
  idempotency key và typed command; không nhận actor/tenant/role từ body.
- Mọi query/mutation dùng exact `tenant_id`, composite FK và row/advisory lock order đã chấp nhận.
  Snapshot reader giữ shared tenant feature-control lock; mutation giữ exclusive lock. PostgreSQL
  `clock_timestamp()` sau authority/version lock là server time duy nhất cho TTL/rate/FIFO timestamp.
  Foreign/inaccessible identifiers bị conceal `404`; stale expected version trả typed `409`.
- Rate-limit state dùng PostgreSQL transaction để an toàn qua nhiều Core API instance; storage failure
  fail closed `503`, không fallback local-memory authority.
- Response chứa full bounded snapshot để retry/offline/sequence-gap hội tụ mà không cần DataChannel.

## 3. Local verification — `PASS`

- [x] OpenAPI/generated client exact và `pnpm api:check` PASS.
- [x] Go unit tests PASS cho authorization, idempotency, FIFO, lower-one/all, lifecycle cleanup,
      reaction allowlist/TTL/grouping/rate policy và privacy-safe projection.
- [x] HTTP tests PASS cho auth/CSRF/expected-tenant, validation, concealment, no-store và typed
      `403/404/409/429/503`.
- [x] Web unit/integration tests PASS cho snapshot/gap/resume/refetch, canonical roster ordering,
      command retry, rate-limit feedback và stale-response discard.
- [x] Classroom fixture 2/5/25/50 PASS; roster/reaction DOM và polite live-region remain bounded.
- [x] Axe/keyboard/focus/320 CSS px/200%/forced-colors/reduced-motion gates PASS.
- [x] `CanPublishData=false`, participant attribute immutability và privacy/no-secret scan PASS.
- [x] Full `pnpm verify` PASS trên exact source candidate: `64/64` web files, `400/400` web tests,
      `50/50` API client tests, all Go test/vet, lint/typecheck/build/Storybook/security; PostgreSQL
      integration-tag compile/vet PASS và không được coi là runtime database evidence. Windows local
      run dùng sandbox-safe temporary `GOCACHE` và hoàn tất trong `92.4 s`.
- [x] Relevant isolated Playwright/P4-06 browser suite PASS `7/7` trên exact local tree qua
      `playwright.media-fixture.config.ts`; fixture dùng production shell, Vite loopback, không DB,
      provider websocket hoặc media capture.
- [x] Security review chốt shared-read/exclusive-write feature lock, PostgreSQL clock sau authority
      lock, receipt retention/purge và exact hand INSERT ACL dùng database `DEFAULT true`; focused
      regression, integration-tag compile/vet và `git diff --check` đều PASS.

## 4. Neon disposable — `PASS`

Ba URL owner/runtime/maintenance được nạp từ ignored local env file trong cùng process. Harness không
in URL, password, hostname, role name hoặc connection string; evidence chỉ chứa status/count an toàn.

- [x] Read-only owner preflight xác thực ba role riêng biệt trên cùng disposable database: owner và
      maintenance dùng direct connection, runtime dùng pooled connection; ledger ban đầu `31 false`.
- [x] Forward-only `31 false -> 32 false`; chạy `db:migrate` lại giữ `32 false`; không rollback.
- [x] Provision exact runtime column ACL cho relation/column P4-06; PUBLIC giữ zero privilege; runtime
      không có table-wide, DDL, ownership, migration-role hoặc schema-create privilege.
- [x] Reuse exact maintenance login hiện có với schema `USAGE` + chỉ hai function `EXECUTE`; runtime/
      PUBLIC không `EXECUTE`, maintenance không direct DML/DDL/ownership.
- [x] Xác nhận P4-06 chỉ đọc `users(id, display_name)` trong repository, không cấp thêm hoặc thu hồi
      `users` SELECT dùng chung đã được các phase trước chấp nhận; `rate_limit_windows` giữ exact
      SELECT/INSERT/UPDATE và không có DELETE/TRUNCATE/REFERENCES/TRIGGER.
- [x] PostgreSQL integration PASS cho tenant/source/participant authorization, foreign-ID concealment,
      participant-key uniqueness, roster sequence, same-key concurrency và stale version.
- [x] Hand FIFO/idempotency/lower one/all, participant/room terminal cleanup và snapshot resync PASS.
- [x] Reaction TTL/grouping/allowlist và cross-instance limits actor `3/5 s`, actor `20/60 s`, room
      `100/5 s` PASS với deterministic database windows; unknown payload không được lưu.
- [x] Reaction/receipt bounded purge, invalid batch, future-row preservation và two-transaction
      `FOR UPDATE SKIP LOCKED` PASS dưới exact maintenance login; receipt replay boundary 24 giờ PASS.
- [x] Final read-only postflight giữ `32 false`, hai media feature effective false và P4-06 side-effect
      count bằng `0`; disposable branch được giữ lại sau khi toàn bộ database gate đã được báo cáo.

Harness được harden trong candidate trước lần PASS cuối: timestamp fixture có explicit cast, cleanup
theo dependency order, rate window deterministic, confirmation riêng cho P4-06 và read-only
preflight/postflight ba principal. Những sửa này đã được compile/vet/focused-test trước khi chạy lại
disposable. Branch được giữ lại; không shared migration, deploy, rollback hoặc branch deletion.

## 5. Exact candidate CI/security — `PASS`

- [x] Diff chỉ chứa source/test/docs P4-06; loại `.env*.local`, `.tmp-gocache/`, token, provider
      identifier, browser profile, screenshot riêng tư và generated artifact không cần thiết.
- [x] Exact runtime candidate `d773641f796076b90f31a876ee840a427db43372` được commit/push trực
      tiếp `main`, không force-push, sau full local verification PASS với `64/64` web files,
      `400/400` web tests và `50/50` API client tests.
- [x] GitHub Verify `31664828854` và Security `31664828820` PASS trên exact full SHA; secret scan,
      CodeQL Go/JavaScript-TypeScript và dependency/container checks không có regression liên quan.
- [x] Focused `TestMediaSpaceScopeMismatchStopsBeforeService` cùng ba HTTP privacy/CSRF/rate tests
      PASS; authenticated wrong-tenant scope mismatch trả typed `409 media_space_scope_changed`
      trước service call.
- [x] CI log/artifact không chứa database/provider credential, participant identity hoặc private media.

## 6. Shared staging — `PASS`

- [x] Shared owner preflight PASS trước khi forward; migration giữ forward-only recovery boundary và
      không chạy rollback.
- [x] Forward-only `31 false -> 32 false`; chạy migrate lại giữ `32 false`. Exact runtime ACL
      provision và focused final gate đều PASS; hai media feature vẫn force-off.
- [x] Final shared snapshot giữ version `32`, `dirty=false`, `media_features=false` và toàn bộ P4-06
      aggregate count bằng `0`; `livekit_webhook_events=16` là dữ liệu có sẵn và không đổi.

## 7. Deploy/live acceptance — `PASS`

- [x] Render deployment `dep-d9ul9q6417fc738gfa3g` ở trạng thái `Live` và Cloudflare Pages đều chạy
      exact candidate `d773641f796076b90f31a876ee840a427db43372`.
- [x] Direct Render và Pages health/ready/status đạt `6/6` HTTP `200`, `Cache-Control: no-store` và
      có request ID.
- [x] Anonymous GET participants và POST signals trên Render/Pages đạt `4/4` typed `401` problem JSON,
      no-store/no-referrer/nosniff, safe `Vary`, có request ID, không `Set-Cookie` và không lộ dữ liệu
      nhạy cảm. Authenticated wrong-tenant được chứng minh bằng exact-candidate gate ở mục 5, không
      được suy thành một live positive signal flow.
- [x] Authenticated Organization Admin `/app/workspace` xác nhận classroom/instant media feature off;
      synthetic room/prejoin fail closed/conceal, không tải LiveKit/processor/media/token và console
      có `0` warning/error.
- [x] Không chạy positive signal flow hoặc
      bật canary tạm trên shared. Enabled-path projection/raise/lower/moderator/reaction/retry/resync
      dùng bằng chứng isolated local/disposable đã PASS.
- [x] Live browser accessibility/privacy/resource/console audit PASS: `main=1`, `h1=1`, `nav=1`,
      `37/37` exposed controls có accessible name, không duplicate ID, external LiveKit/processor
      resource, console warning/error, credential/raw exception, DataChannel signal, email/session/
      provider identity hoặc token trong rendered DOM/URL/log. Storage safety tiếp tục được bảo đảm
      bởi committed automated gate; browser acceptance không đọc hoặc xuất session storage/cookie.
- [x] Read-only post-live snapshot giữ version `32`, `dirty=false`, exact ACL, media feature off,
      toàn bộ P4-06 count bằng `0` và `livekit_webhook_events=16` có sẵn không đổi.

P4-06 chuyển `IN PROGRESS -> VERIFY -> DONE` ngày 2026-08-13 trên exact candidate
`d773641f796076b90f31a876ee840a427db43372`. Không rollback; disposable branch được giữ lại;
`CanPublishData=false` và hai media feature tiếp tục force-off. Physical/manual/load/outage/effect
gates vẫn `UNVERIFIED — P4-11`, không được suy `PASS` từ slice này. P4-07 là task runnable tiếp theo.
