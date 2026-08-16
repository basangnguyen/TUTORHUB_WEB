# P4-12 — Exact staging acceptance và Phase 4 closure

Ngày mở contract: 2026-08-16. Ngày đóng: 2026-08-16. Trạng thái: `DONE`.

## 1. Mục tiêu và nguyên tắc

P4-12 chứng minh exact candidate của Classroom Media MVP chạy đúng trên disposable database,
shared staging và LiveKit canary trước khi đóng Phase 4. Không dùng kết quả của candidate khác để
suy PASS. Không rollback migration, không bật recording/egress, không bật Phase 3 carry-over và
không lưu/log URL database, cookie, token hoặc LiveKit secret.

Canary chỉ được bật sau khi product path dùng authority P4 hoàn chỉnh. Khi
`classroom_media_rooms=true`, class-wide P1 credential route phải trả `410` và UI không được dẫn tới
P1 prejoin. Kill switch phải được Core API đánh giá ở mỗi request, không dựa vào state phía client.

## 2. Blocker phát hiện khi review contract

Backend đã có MediaSpace lifecycle, RoomInstance, lobby, moderation, chat, recovery, diagnostics và
LiveKit adapter; web đã có MediaSpace prejoin/room shell. Tuy nhiên product navigation chưa có đường
tạo/start/resolve MediaSpace từ ClassSession hoặc StudyMeeting, còn nút lớp hiện trỏ vào prejoin P1.

P4-12 phải hoàn tất trước canary:

- read-only `GET /api/v1/media/spaces/resolve` cho persisted source; không chấp nhận `instant`;
- host/co-teacher tạo hoặc replay exact space, start bằng optimistic version và idempotency key;
- attendee chỉ resolve space đã tồn tại và được authorize, không được tạo/start;
- missing/foreign/invisible source/space cùng fail closed `404`;
- product action theo từng ClassSession/StudyMeeting dẫn tới P4 prejoin;
- khi P4 feature off, P4 action ẩn/disabled và P1 compatibility vẫn chỉ là controlled fallback;
- khi P4 feature on, product UI không phát sinh link P1.

## 3. Candidate và gate trước hạ tầng

- [x] Diff review đúng phạm vi, generated API không drift, candidate secret scan sạch.
- [x] Format, lint, typecheck, unit, build, Storybook và Go test/vet PASS.
- [x] Source resolver HTTP/service/repository authorization tests PASS.
- [x] Web launch tests PASS cho host create/start, attendee resolve/join, 404 not-ready, feature-off
      fallback và feature-on P1 exclusion.
- [x] GitHub Verify, Security và Browser E2E PASS trên cùng candidate SHA.

## 4. Disposable PostgreSQL gate

Dùng branch disposable mới clone từ exact shared staging. File local đề xuất:
`.env.p4-12-disposable.local`; chỉ nạp trong cùng process và chỉ báo boolean/bounded count.

- [x] Owner preflight đúng branch/database/role, ledger hiện tại sạch.
- [x] Forward-only migration tới latest và chạy lại idempotent; không rollback.
- [x] Exact runtime/PUBLIC/maintenance ACL và function ownership/search path PASS.
- [x] Full media lifecycle/lobby/moderation/signal/chat/recovery/diagnostics matrix PASS.
- [x] Source resolver official/member-owned tenant/role/concealment matrix PASS.
- [x] Start/start, start/end, invite/revoke, admit/deny, reconnect/recovery barriers PASS.
- [x] Retention purge bounded batch và `SKIP LOCKED` PASS.
- [x] Final snapshot latest `dirty=false`, không side effect ngoài fixtures có cleanup.

## 5. Shared staging và deploy gate

Chỉ chạy sau disposable + exact candidate CI xanh và có quyền owner rõ ràng.

- [x] Shared owner preflight và ledger sạch.
- [x] Forward latest một lần; nếu không có migration mới thì ghi rõ `latest -> latest`, không giả
      forward.
- [x] Re-provision exact ACL nếu migration/owner object thay đổi; final snapshot sạch.
- [x] Deploy exact SHA lên Render và Cloudflare Pages; health/version cùng trỏ exact candidate.
- [x] Deployment guardrail mở cho media nhưng catalog/tenant default vẫn off.

## 6. Exact canary role và lifecycle matrix

Chỉ bật tenant override cho một workspace canary đã chỉ định.

- [x] Official one-time ClassSession: owner/co-teacher start; TA/student join; guest/foreign conceal.
- [x] Recurring occurrence identity không tạo trùng space và không sửa toàn series.
- [x] Member-owned StudyMeeting: owner create/start/invite; explicit member join; non-member conceal;
      admin safety không tự trở thành member.
- [x] Lobby wait/admit/deny/restore, lock/join, revoke/rejoin và capacity 50 đúng authority.
- [x] Host/co-host/TA moderation matrix và provider effect state đúng contract.
- [x] Persistent room chat reload/reconnect được; actor bị remove/revoke không gửi tiếp.
- [x] Short reconnect, sustained outage, recovery successor và audio-only degrade PASS.
- [x] Diagnostics export chỉ Organization Admin, redacted/no-store/audited; purge retention PASS.
- [x] Signed webhook signature/replay/out-of-order/unknown/stale instance fail closed.
- [x] P1 class-wide media-token route trả `410` trong canary và UI không tạo P1 URL.

## 7. Browser, accessibility, load và privacy

- [x] Declared support: Windows 11 Chrome/Edge physical; Chromium/Firefox automation supplement;
      Safari/VoiceOver, physical Firefox và low-end vẫn ghi `UNAVAILABLE` nếu chưa có thiết bị.
- [x] NVDA/keyboard, 200% reflow, 360p forced colors/reduced motion và focus recovery PASS.
- [x] Profile 25/50 giữ join success `>=99%`, TTM p95 `<10 s`, Core API health và cleanup zero.
- [x] Token chỉ ở memory; no-store/referrer policy; bundle/log/audit/diagnostic không rò secret,
      provider ID, device label hoặc raw network data.
- [x] Recording/egress và optional effect processor vẫn off; supported effect là `None`.

## 8. Kill-switch drill và closure

- [x] Tắt tenant override làm P4 API fail closed ngay, không mint credential mới; room đang hoạt động
      được end/cleanup có kiểm soát theo runbook.
- [x] Bật lại override không tạo duplicate space/instance và exact source resolve phục hồi.
- [x] Tắt canary sau acceptance trừ khi owner quyết định giữ pilot; ghi rõ final effective state.
- [x] Tạo `docs/PHASE_4_COMPLETION.md` với exact SHA/run/check/evidence và declared support matrix.
- [x] Cập nhật README, Project State, Master Plan, Agent Coordination và backlog sang `DONE` chỉ khi
      toàn bộ gate bắt buộc xanh.

## 9. Candidate và CI evidence

- Candidate mở product path `fb2df3ee9d1808185424dc47268e51d992731a12` có Security
  `31932573661` PASS nhưng Verify `31932573656` FAIL. Kết quả này không được dùng để suy PASS.
- Fix concealment `3cd6448cb2fabc030d18ae3d3fbe4b0c6d4b287c` PASS Verify
  `31932897680`, Security `31932897725`, Browser E2E và Cloudflare Pages check
  `95130445446`. Đây là exact backend tree đã deploy cho source resolver và canary.
- Fix web admission race `cca93c5402cb016c84111004b238f4efe9fa6c2a` PASS Verify
  `31946763549`, Security `31946763545`, Browser E2E và Cloudflare Pages check
  `95164026803`. Commit sau chỉ đổi web; final repository/runtime candidate là `cca93c5`, chứa
  toàn bộ backend fix `3cd6448` trong lịch sử.
- Fresh local gate sau race fix PASS: web `71` file / `447` test, typecheck, lint, build, format,
  diff-check, Go test/vet và candidate secret scan. Không file `.env*.local` nào được stage/log.

## 10. PostgreSQL evidence

- P4-12 không có migration mới. Disposable và shared đều giữ `36 false`; migrate là
  `36 false -> 36 false`, idempotent, không rollback và không giả forward.
- Disposable exact runtime/PUBLIC ACL PASS. Focused resolver/authority/concurrency/privacy PASS
  trong `171.775 s`, gồm official implicit owner, Organization Teacher/Admin, active co-teacher,
  active student, exact StudyMeeting member, missing/foreign/non-member concealment.
- Disposable final read-only snapshot PASS: `ledger=36`, `dirty=false`,
  `media_features=false`, diagnostics/expired/retention violation đều `0`. Branch clone giữ `10`
  enabled media overrides thuộc fixture lịch sử; không dùng chúng làm shared final state và không xóa
  disposable branch trong lượt closure.
- Shared final read-only snapshot sau khi tắt canary PASS: `ledger=36`, `dirty=false`,
  `media_features=false`, `diagnostics=52`, `expired_diagnostics=0`,
  `retention_violations=0`, `retained_enabled_media_overrides=0`.

## 11. Live canary và kill-switch evidence

- Direct Render `/health` trả `ok`; `/ready` trả database/object storage `ready`. Source resolver,
  credential, lobby và classroom shell chạy qua exact staging path; Cloudflare Pages chạy
  `cca93c5`.
- Teacher Chrome và Student Edge dùng official ClassSession. Student vào lobby, Teacher admit;
  hai browser cùng hiển thị `2` participant. Regression admission-abort của web cũ được tái hiện,
  sửa bằng `cca93c5`, deploy và retest PASS.
- Teacher kết thúc phòng bằng business flow; Teacher và Student đều chuyển sang terminal state,
  credential cũ bị xóa và participant cleanup về zero.
- Organization Admin tắt `Phòng học trực tuyến` và `Phòng học nhóm tức thời`; UI xác nhận cả hai
  `Đang tắt`. Snapshot shared sau đó xác nhận không còn enabled media override.

## 12. Declared support và giới hạn evidence

| Môi trường                                      | Kết quả                                                    |
| ----------------------------------------------- | ---------------------------------------------------------- |
| Windows 11 Chrome/Edge physical + NVDA/keyboard | PASS qua P4-11 physical 10/10 và P4-12 Chrome/Edge canary  |
| Chromium/Firefox/WebKit automation supplement   | PASS; WebKit chỉ là supplement, không thay Safari physical |
| LiveKit load 25/50                              | PASS; join `>=99%`, TTM p95 `<10 s`, cleanup zero          |
| Safari/VoiceOver physical                       | `UNAVAILABLE` — chưa có thiết bị macOS                     |
| Firefox physical                                | `UNAVAILABLE` — không suy từ automation                    |
| Low-end physical profile                        | `UNAVAILABLE` — không suy từ viewport/throttling mô phỏng  |
| Recording/egress/optional effect                | OFF; production effect giữ `None`                          |

**Kết luận:** P4-12 `DONE`; Phase 4 Classroom Media MVP đạt exit gate ngày 2026-08-16. Nếu
Verify/Security của closure-record docs-only thất bại sau push thì phải mở lại P4-12 và sửa, không
được giữ trạng thái `DONE` bằng evidence cũ.
