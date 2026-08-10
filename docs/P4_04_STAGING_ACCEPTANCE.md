# P4-04 — Lobby, admission và explicit same-tenant invite staging acceptance

## 1. Trạng thái và ranh giới

- Ngày cập nhật: `2026-08-10`.
- Trạng thái hiện tại: `DONE` — implementation, full local/disposable verification, exact candidate
  CI/security, shared staging, deploy và live acceptance đều PASS.
- Forward migration mới trong source: `000031_media_lobby_admission_restore`.
- Disposable và shared đều PASS forward-only `30 false -> 31 false -> 31 false`; final ledger của
  cả hai là `31 false`. **Không chạy rollback `000031`**.
- Shared chỉ được forward sau disposable cùng exact CI/security PASS và quyền riêng; deploy chỉ bắt
  đầu sau shared ACL/read-only gate PASS.
- Hai feature `classroom_media_rooms` và `instant_study_rooms` tiếp tục force-off. `effect=None` là
  baseline và `CanPublishData=false`; participant `waiting` không nhận credential hoặc kết nối
  LiveKit.
- Không đọc, hiển thị hoặc log secret từ `.env*.local`. Disposable branch phải được giữ tới khi tất
  cả database gate P4-04 đạt.
- Owner preflight dùng confirmation riêng
  `P4_04_OWNER_PREFLIGHT=I_UNDERSTAND_P4_04_OWNER_PREFLIGHT_ONLY` cùng
  `P4_04_DISPOSABLE_CONFIRM=I_UNDERSTAND_P4_04_DISPOSABLE_ONLY`; gate fail-closed ở ledger khác
  `30 false`, direct/pooled URL không cùng Neon endpoint, role/database không đúng hoặc future-table
  default ACL không an toàn. Hai media feature tiếp tục force-off bằng compiled/deployment guardrail;
  không dùng global override count vì isolated integration fixture có thể giữ tenant test phục vụ audit.
- Shared preflight dùng cờ riêng
  `P4_04_SHARED_CONFIRM=I_UNDERSTAND_P4_04_SHARED_STAGING_ONLY`, không đặt đồng thời với
  `P4_04_DISPOSABLE_CONFIRM` và chỉ chạy `TestPostgresP404SharedOwnerPreflight` trước shared forward.

## 2. Local candidate đã triển khai

### 2.1 Contract, Core API và domain

- OpenAPI/generated client có self join-attempt poll/cancel, moderator admission queue cùng
  admit/deny/restore, và StudyMeeting member list/invite/revoke/restore.
- Mọi command bind exact tenant, MediaSpace, RoomInstance, actor và expected version. Mutation dùng
  CSRF, idempotency key, CAS và typed bounded error; inaccessible/foreign identity bị conceal.
- Moderator authority được server suy từ source hiện hành: owner/co-host/TA theo policy; client
  không tự khai role, room, grant hoặc provider identifier.
- Explicit invite chỉ dành cho active authenticated member của cùng tenant và member-owned
  StudyMeeting/instant meeting. Email chuẩn hóa chỉ là lookup input; response, audit, outbox và log
  không chứa raw email.
- Deny/remove là barrier bền vững. Rejoin chỉ được mở bằng explicit restore và vẫn phải reauthorize
  current tenant/source/membership trước admission hoặc credential mới.
- Lock order cùng terminalization bảo vệ race admit/deny/end/cancel/timeout; waiting timeout giải
  phóng capacity và kết thúc admission/participant atomically.

### 2.2 Web lobby và invite UX

- Waiting user có poll bounded, cancel/retry, timeout, denied, cancelled, meeting-ended và
  provider-unavailable states; terminal state nhận focus có chủ đích.
- Moderator lobby có loading/empty/error/forbidden/retry, admit/deny và stale-version recovery.
- Invite panel chỉ nhận exact email do người quản lý nhập; không tải hoặc enumerate tenant roster và
  không echo raw email sau mutation.
- UI fail-closed khi quyền bị thu hồi; state thiết bị/effect không thay đổi authority, admission hay
  capacity. Credential chỉ được handoff sau trạng thái admitted theo boundary P4-03.

### 2.3 Forward database/security change

- Migration `000031` chỉ mở rộng allowlist receipt cho member/admission mutation và thêm
  `rejoin_restored_at`/`rejoin_restored_by` vào `media_participant_sessions`.
- Restore marker có same-tenant membership FK, consistency check và partial index cho removed
  participant chưa được restore. Migration không revive terminal ParticipantSession và không mở
  rộng public/anonymous authority.
- Exact runtime ACL candidate chỉ cấp column-level `SELECT`/`INSERT`/`UPDATE` cần thiết; runtime
  không có table-wide privilege, `DELETE`, DDL, schema `CREATE`, ownership, migration-role membership
  hoặc `BYPASSRLS`.
- Audit/outbox dùng allowlist event bounded cho member invite/revoke/restore và admission
  admit/deny/cancel/restore/expire; không có email, token, provider/device identifier hoặc media
  content.

## 3. Local verification evidence

| Gate                                                                  | Kết quả                                                |
| --------------------------------------------------------------------- | ------------------------------------------------------ |
| `pnpm api:generate`                                                   | PASS                                                   |
| `pnpm api:check`                                                      | PASS                                                   |
| API client Vitest                                                     | `7` files, `49` tests PASS                             |
| Web TypeScript typecheck                                              | PASS                                                   |
| Web Vitest                                                            | `59` files, `305` tests PASS                           |
| Focused P4-04 web tests                                               | `23/23` PASS                                           |
| Web targeted lint, build, E2E typecheck, Prettier, `git diff --check` | PASS                                                   |
| `go test ./services/core-api/...`                                     | PASS                                                   |
| `go vet ./services/core-api/...`                                      | PASS                                                   |
| Media integration build-tag compile                                   | PASS; không chạy external PostgreSQL test              |
| Full `pnpm verify` trên exact candidate                               | PASS với workspace-local Go cache                      |
| P4-04 Playwright runtime                                              | `3/3` PASS trên temporary isolated Vite fixture        |
| Security/static sign-off                                              | PASS guard, audit, ACL, lock/TTL/privacy/restore gates |

Các kết quả trên chỉ chứng minh local candidate. Integration-tag compile không phải bằng chứng
runtime PostgreSQL; không có database URL, LiveKit key hoặc external provider được nạp trong gate
này.

## 4. Neon disposable — `PASS`

Chỉ chạy trên disposable branch được xác nhận, tự nạp URL từ ignored local env file trong cùng
command và chỉ in tên gate/kết quả boolean. Không in connection string hoặc secret.

1. [x] Owner preflight xác nhận migration/runtime URL direct/pooled cùng exact disposable Neon
       endpoint, khác role, cùng database, ledger đầu vào `30 false`, role/default-ACL safety và
       compiled/deployment guardrail giữ hai feature false.
2. [x] Chạy forward migration `30 false -> 31 false`; chạy `db:migrate` lại xác nhận idempotent
       `31 false -> 31 false`.
3. [x] Re-provision exact P4-04 runtime ACL bằng owner credential; xác nhận effective/direct/default
       ACL, role membership, ownership, schema/PUBLIC và future-table safety.
4. [x] Chạy `TestPostgresMediaLifecycleRuntimeExactACL` bằng runtime role.
5. [x] Chạy `TestPostgresMediaLobbyAdmissionInviteRaceAndRestoreBarrier`: same-tenant invite,
       foreign/inactive concealment, waiting zero credential, stale instance, admit/deny/end race,
       cancel/timeout capacity release, denied/removed restore barrier và privacy event payload.
6. [x] Chạy lại các media lifecycle/authority/concurrency/quota/privacy và
       RoomInstance/credential/signed-webhook integration gate liên quan.
7. [x] Read-only final probe xác nhận ledger `31 false`, hai feature false và không có broad runtime
       privilege. Không chạy rollback; giữ disposable branch sau báo cáo.

Evidence ngày `2026-08-10`:

- ignored env metadata, disposable pair/same endpoint, khác shared endpoint và owner preflight:
  PASS; không in URL hoặc credential;
- forward/idempotent: `30 false -> 31 false -> 31 false`; final read-only ledger `31 false`,
  `rollback_run=false`;
- ACL provision `24.300s`; exact runtime/default/future-table ACL `14.470s`;
- lobby/admission/invite race + restore barrier `67.808s`;
- lifecycle/authority/concurrency/quota/privacy `169.858s`;
- RoomInstance/credential/signed-webhook `230.397s`;
- full `pnpm verify` trên post-disposable exact local tree PASS trong `138.5s`; media feature
  config/deployment guardrail tests PASS. Không nạp LiveKit credential và không xóa disposable.

## 5. Exact candidate CI/security — `PASS`

1. [x] Chạy full `pnpm verify`, review exact diff và secret scan; loại `.env*.local`,
       `.tmp-gocache/` và mọi test artifact khỏi candidate.
2. [x] Commit/push exact candidate lên `main` theo repository workflow.
3. [x] GitHub Verify PASS cho quality/integration, browser E2E và local-environment smoke.
4. [x] GitHub Security PASS cho secret scan, CodeQL, dependency/repository scan và container scan.
5. [x] Ghi exact full SHA và workflow run IDs; không suy PASS từ local run.

Evidence ngày `2026-08-10`:

- exact accepted runtime candidate: `735a5e5579d6e5efe7c4efca2b8a48c3de1b1f23`;
- full local `pnpm verify`, diff review, commit hooks và secret guard đều PASS; `.env*.local`,
  `.tmp-gocache/` không thuộc candidate;
- GitHub Verify run `31356295542`: quality/integration, browser E2E, local-environment smoke và exact
  PostgreSQL media integration đều PASS;
- GitHub Security run `31356295518`: secret scan, CodeQL Go/JavaScript-TypeScript, repository
  vulnerability và container scan đều PASS; dependency review được skip đúng thiết kế cho push.

## 6. Shared staging — `PASS`

Shared chỉ được bắt đầu sau disposable report + exact CI/security PASS và owner cho phép forward.

1. [x] Wrapper so sánh endpoint xác nhận shared khác disposable mà không in URL; shared-only
       `TestPostgresP404SharedOwnerPreflight` xác nhận ledger `30 false`, direct/pooled cùng exact
       shared endpoint, đúng database/roles/default ACL và không có cờ disposable/legacy.
2. [x] Forward-only shared `30 false -> 31 false -> 31 false`; không rollback.
3. [x] Re-provision exact P4-04 ACL idempotently bằng fresh shared confirmations, rồi chạy
       `TestPostgresMediaLifecycleRuntimeExactACLSharedReadOnly`; không gọi migration runner trong
       read-only probe và coi `SKIP`/`no tests to run` là FAIL.
4. [x] Đặt `P4_04_SHARED_SNAPSHOT_CONFIRM=I_UNDERSTAND_P4_04_SHARED_READ_ONLY_SNAPSHOT`, rồi chạy
       `TestPostgresP404SharedReadOnlySnapshot` trước deploy và sau live acceptance. Hai dòng
       `P4_04_SHARED_SNAPSHOT` phải giống hệt; probe chỉ dùng transaction `REPEATABLE READ READ ONLY`,
       chỉ xuất ledger/aggregate count và xác nhận không có media feature override đang bật.
5. [x] Không chạy mutation/concurrency fixture hoặc provider smoke trên shared.
6. [x] Final shared ledger `31 false`; runtime role không có broad privilege và feature vẫn false.

Evidence ngày `2026-08-10`:

- endpoint/role/database shape và shared owner preflight PASS mà không in URL/credential;
- forward/idempotent: `30 false -> 31 false -> 31 false`; không rollback;
- exact ACL provision cùng `TestPostgresMediaLifecycleRuntimeExactACLSharedReadOnly` PASS;
- read-only snapshot trước và sau live giống hệt: `version=31`, `dirty=false`,
  `media_spaces=0`, `media_room_instances=0`, `media_space_members=0`,
  `media_admission_requests=0`, `media_participant_sessions=0`,
  `media_space_mutation_receipts=0`, `media_provider_webhook_receipts=0`,
  `livekit_webhook_events=16`, `media_outbox=0`, `media_audit=0` và
  `enabled_media_feature_overrides=0`.

## 7. Exact deploy và live acceptance — `PASS`

1. [x] Deploy exact candidate SHA lên Render và xác nhận Cloudflare Pages trỏ cùng SHA.
2. [x] Direct Render + Pages proxy health/readiness/status đạt `200 + no-store`.
3. [x] Anonymous lobby/admission/member routes trả `401` với problem JSON, privacy/cache headers,
       request ID và không lộ tenant/room/user/email/provider data.
4. [x] Authenticated Organization Admin xác nhận hai media feature vẫn off; lobby/prejoin route
       fail-closed, có bounded retry và không tạo media/provider side effect.
5. [x] Read-only before/after probe giữ ledger `31 false` và các media relation không đổi trong
       feature-off live acceptance.
6. [x] Actual lobby/admission flow dùng isolated browser + disposable PostgreSQL/LiveKit test
       project; không temporary-enable shared/live feature chỉ để demo.

Evidence ngày `2026-08-10`:

- Cloudflare Pages check `93356654021`, deployment `7f180bf1-3736-4e40-a6f3-7ced6df2be27` và
  Render deployment `dep-d9sli7740ujc73dfic70` đều PASS cho exact runtime SHA
  `735a5e5579d6e5efe7c4efca2b8a48c3de1b1f23`;
- direct Render và Pages proxy đạt `6/6` health/readiness/status checks;
- `11` P4-04 routes qua hai origin đạt `22/22` anonymous privacy checks: `401` problem JSON,
  no-store/privacy headers, request ID, không Set-Cookie và không lộ dữ liệu;
- authenticated Organization Admin xác nhận hai media capability vẫn off; prejoin inaccessible
  route fail-closed, Retry bounded, zero video/audio/iframe, URL sạch credential và console sạch;
- isolated P4-04 Playwright actual lobby/admission flow đạt `3/3`; shared không được temporary-enable;
- shared snapshot sau live giống hệt snapshot trước live, nên feature-off acceptance không tạo
  database hoặc provider side effect.

## 8. VERIFY -> DONE

- [x] Contract, Go repository/service/HTTP, generated client và web lobby/invite candidate đã có.
- [x] Focused local unit/typecheck/build/compile gates ở mục 3 PASS.
- [x] Runtime Playwright P4-04 `3/3` PASS trên isolated fixture; temporary config đã được xóa.
- [x] Full exact local `pnpm verify` PASS.
- [x] Disposable forward `30 -> 31`, exact ACL và PostgreSQL integration gates PASS; không rollback.
- [x] Exact candidate GitHub Verify/Security PASS.
- [x] Shared forward/ACL read-only gate PASS theo quyền riêng.
- [x] Exact deploy và live feature-off/privacy/no-side-effect acceptance PASS.
- [x] Ghi đủ exact SHA/run/deploy/ledger evidence và chuyển P4-04 `VERIFY -> DONE`.

## 9. Quyết định trạng thái

P4-04 chuyển `VERIFY -> DONE` ngày `2026-08-10`. Exact candidate CI/security, disposable/shared
forward-only migration và ACL, Render/Cloudflare deploy, live privacy/feature-off/no-side-effect
acceptance đều PASS. Final disposable/shared ledger là `31 false`; không rollback, không bật hai
media capability và disposable branch tiếp tục được giữ.
