# P4-12 — Exact staging acceptance và Phase 4 closure

Ngày mở contract: 2026-08-16. Trạng thái: `IN PROGRESS`.

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

- [ ] Diff review đúng phạm vi, generated API không drift, candidate secret scan sạch.
- [ ] Format, lint, typecheck, unit, build, Storybook và Go test/vet PASS.
- [ ] Source resolver HTTP/service/repository authorization tests PASS.
- [ ] Web launch tests PASS cho host create/start, attendee resolve/join, 404 not-ready, feature-off
      fallback và feature-on P1 exclusion.
- [ ] GitHub Verify, Security và Browser E2E PASS trên cùng candidate SHA.

## 4. Disposable PostgreSQL gate

Dùng branch disposable mới clone từ exact shared staging. File local đề xuất:
`.env.p4-12-disposable.local`; chỉ nạp trong cùng process và chỉ báo boolean/bounded count.

- [ ] Owner preflight đúng branch/database/role, ledger hiện tại sạch.
- [ ] Forward-only migration tới latest và chạy lại idempotent; không rollback.
- [ ] Exact runtime/PUBLIC/maintenance ACL và function ownership/search path PASS.
- [ ] Full media lifecycle/lobby/moderation/signal/chat/recovery/diagnostics matrix PASS.
- [ ] Source resolver official/member-owned tenant/role/concealment matrix PASS.
- [ ] Start/start, start/end, invite/revoke, admit/deny, reconnect/recovery barriers PASS.
- [ ] Retention purge bounded batch và `SKIP LOCKED` PASS.
- [ ] Final snapshot latest `dirty=false`, không side effect ngoài fixtures có cleanup.

## 5. Shared staging và deploy gate

Chỉ chạy sau disposable + exact candidate CI xanh và có quyền owner rõ ràng.

- [ ] Shared owner preflight và ledger sạch.
- [ ] Forward latest một lần; nếu không có migration mới thì ghi rõ `latest -> latest`, không giả
      forward.
- [ ] Re-provision exact ACL nếu migration/owner object thay đổi; final snapshot sạch.
- [ ] Deploy exact SHA lên Render và Cloudflare Pages; health/version cùng trỏ exact candidate.
- [ ] Deployment guardrail mở cho media nhưng catalog/tenant default vẫn off.

## 6. Exact canary role và lifecycle matrix

Chỉ bật tenant override cho một workspace canary đã chỉ định.

- [ ] Official one-time ClassSession: owner/co-teacher start; TA/student join; guest/foreign conceal.
- [ ] Recurring occurrence identity không tạo trùng space và không sửa toàn series.
- [ ] Member-owned StudyMeeting: owner create/start/invite; explicit member join; non-member conceal;
      admin safety không tự trở thành member.
- [ ] Lobby wait/admit/deny/restore, lock/join, revoke/rejoin và capacity 50 đúng authority.
- [ ] Host/co-host/TA moderation matrix và provider effect state đúng contract.
- [ ] Persistent room chat reload/reconnect được; actor bị remove/revoke không gửi tiếp.
- [ ] Short reconnect, sustained outage, recovery successor và audio-only degrade PASS.
- [ ] Diagnostics export chỉ Organization Admin, redacted/no-store/audited; purge retention PASS.
- [ ] Signed webhook signature/replay/out-of-order/unknown/stale instance fail closed.
- [ ] P1 class-wide media-token route trả `410` trong canary và UI không tạo P1 URL.

## 7. Browser, accessibility, load và privacy

- [ ] Declared support: Windows 11 Chrome/Edge physical; Chromium/Firefox automation supplement;
      Safari/VoiceOver, physical Firefox và low-end vẫn ghi `UNAVAILABLE` nếu chưa có thiết bị.
- [ ] NVDA/keyboard, 200% reflow, 360p forced colors/reduced motion và focus recovery PASS.
- [ ] Profile 25/50 giữ join success `>=99%`, TTM p95 `<10 s`, Core API health và cleanup zero.
- [ ] Token chỉ ở memory; no-store/referrer policy; bundle/log/audit/diagnostic không rò secret,
      provider ID, device label hoặc raw network data.
- [ ] Recording/egress và optional effect processor vẫn off; supported effect là `None`.

## 8. Kill-switch drill và closure

- [ ] Tắt tenant override làm P4 API fail closed ngay, không mint credential mới; room đang hoạt động
      được end/cleanup có kiểm soát theo runbook.
- [ ] Bật lại override không tạo duplicate space/instance và exact source resolve phục hồi.
- [ ] Tắt canary sau acceptance trừ khi owner quyết định giữ pilot; ghi rõ final effective state.
- [ ] Tạo `docs/PHASE_4_COMPLETION.md` với exact SHA/run/check/evidence và declared support matrix.
- [ ] Cập nhật README, Project State, Master Plan, Agent Coordination và backlog sang `DONE` chỉ khi
      toàn bộ gate bắt buộc xanh.
