# Biên bản đóng Phase 4 - Classroom Media MVP

> **DONE - PHASE 4 HOÀN THÀNH NGÀY 2026-08-16**

## 1. Kết luận

| Thuộc tính                 | Kết quả                                                             |
| -------------------------- | ------------------------------------------------------------------- |
| Ngày rà soát               | 2026-08-16                                                          |
| Final repository candidate | `cca93c5402cb016c84111004b238f4efe9fa6c2a`                          |
| Exact backend deploy tree  | `3cd6448cb2fabc030d18ae3d3fbe4b0c6d4b287c`                          |
| Closure-record SHA         | Git history của file này; không tự nhúng để tránh circular SHA      |
| Task vừa hoàn thành        | P4-12 Exact staging acceptance và Phase 4 closure                   |
| Kết luận                   | **ĐẠT - Phase 4 hoàn thành**                                        |
| Task tiếp theo             | P5-COLLAB-00 research spike trước khi code collaboration/whiteboard |

`cca93c5` chứa toàn bộ backend source-resolver/concealment fix của `3cd6448`; commit sau chỉ sửa
web admission credential handoff. Vì vậy Render giữ exact backend tree `3cd6448`, còn Cloudflare
Pages chạy final web candidate `cca93c5`. Exact live canary đã chứng minh hai artifact tương thích.

## 2. Phạm vi đã hoàn thành

- MediaSpace authority cho official ClassSession/occurrence và member-owned StudyMeeting.
- RoomInstance lifecycle, LiveKit credential và signed webhook binding.
- Prejoin device/network, lobby/admission/invite, classroom shell và bounded layouts.
- Server roster, hand/reaction, host/co-host/TA moderation và persistent room chat.
- Reconnect, recovery successor, degraded audio-only, telemetry và redacted diagnostics export.
- Product launch/resolve path fail closed; khi P4 bật, UI không đi vào P1 class-wide authority.
- Windows 11 Chrome/Edge physical, NVDA/keyboard, 25/50 load, provider outage/rotation và cleanup.
- Server-evaluated kill switch; catalog/tenant default, recording, egress và optional effect vẫn off.

## 3. Trạng thái task

| Task                                      | Kết quả  |
| ----------------------------------------- | -------- |
| P4-00 Architecture/backlog baseline       | DONE     |
| P4-MEDIA-UX-00 Media UX research spike    | DONE     |
| P4-01 MediaSpace lifecycle/schema/API     | DONE     |
| P4-02 RoomInstance credential/webhook     | DONE     |
| P4-03 Prejoin device/network/join attempt | DONE     |
| P4-04 Lobby/admission/invite              | DONE     |
| P4-05 Classroom shell/controls/layouts    | DONE     |
| P4-06 Roster/hand/reaction                | DONE     |
| P4-07 Host/co-host/TA moderation          | DONE     |
| P4-08 Persistent in-room chat             | DONE     |
| P4-09 Reconnect/recovery/audio-only       | DONE     |
| P4-10 Telemetry/privacy/diagnostics       | DONE     |
| P4-11 Browser/device/load/outage          | DONE     |
| P4-12 Exact staging acceptance/closure    | **DONE** |

## 4. Exit gate evidence

| Gate                        | Evidence                                                                                                         | Kết quả |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------- |
| Exact candidate CI/security | `3cd6448`: Verify `31932897680`, Security `31932897725`; `cca93c5`: Verify `31946763549`, Security `31946763545` | PASS    |
| Browser E2E/Cloudflare      | Browser E2E xanh trên cả hai fix; Cloudflare checks `95130445446` và `95164026803`                               | PASS    |
| PostgreSQL disposable       | Ledger `36 false`, exact ACL; resolver/authority/concurrency/privacy PASS; snapshot `dirty=false`                | PASS    |
| Shared final snapshot       | `ledger=36`, `dirty=false`, retention violation `0`, enabled media override `0`                                  | PASS    |
| Official physical canary    | Teacher Chrome + Student Edge, lobby/admit, 2 participants, end/terminal/cleanup                                 | PASS    |
| Kill switch                 | Admin tắt cả hai media capability; UI và database final snapshot cùng xác nhận off                               | PASS    |
| Provider/load/recovery      | P4-11 25/50, key rotation, sustained outage/existing-room recovery, cleanup zero                                 | PASS    |
| Accessibility/privacy       | NVDA/keyboard/reflow/forced colors/reduced motion; token memory-only/no-store/redaction                          | PASS    |
| Recording/effect boundary   | Recording/egress off; optional processor off; production effect `None`                                           | PASS    |
| Phase 3 carry-over safety   | Notification/email/worker/file-processing carry-over không bị bật hoặc đánh dấu PASS                             | PASS    |

Candidate đầu `fb2df3e` có Verify `31932573656` FAIL và Security `31932573661` PASS; closure không dùng
candidate đó để suy PASS. Concealment fix và admission-race fix sau đó đều được hậu kiểm bằng exact
CI/security/deploy/live acceptance.

## 5. Declared support matrix

| Môi trường                         | Trạng thái                                    |
| ---------------------------------- | --------------------------------------------- |
| Windows 11 Chrome/Edge physical    | SUPPORTED/PASS trong private-alpha matrix     |
| NVDA + keyboard                    | SUPPORTED/PASS                                |
| Chromium/Firefox/WebKit automation | PASS supplement                               |
| Interactive classroom 2-50         | PASS trên maximum tested profile 50           |
| Safari/VoiceOver physical          | `UNAVAILABLE`; chưa công bố support           |
| Firefox physical                   | `UNAVAILABLE`; automation không thay physical |
| Low-end physical device            | `UNAVAILABLE`; chưa công bố support           |
| Webinar/broadcast/large-event      | Ngoài phạm vi Phase 4                         |

## 6. Trạng thái hạ tầng và dữ liệu cuối

- P4-12 không có migration mới; disposable/shared giữ version `36`, `dirty=false`; không rollback.
- Disposable branch được giữ lại theo chỉ dẫn owner. Dữ liệu override fixture trên disposable không
  phải shared final state.
- Shared tenant canary kết thúc với `classroom_media_rooms=false` và
  `instant_study_rooms=false`; catalog/default vẫn off.
- Direct Render health `ok`, readiness database/object storage `ready`; exact product canary PASS.
- Không secret, cookie, database URL, LiveKit key/token hoặc raw diagnostics được ghi vào repository.

## 7. Giới hạn và follow-up không bị che giấu

- Render Free/Neon/LiveKit free tier chỉ là staging/private alpha, không phải production SLO.
- Safari/VoiceOver, physical Firefox và low-end hardware cần bổ sung trước khi mở support tương ứng.
- Phase 3 deferred carry-over vẫn mở: durable worker, notification/email/SES/domain và các lane
  file-processing phụ thuộc worker không tự động hoàn thành vì Phase 4 đã đóng.
- Recording/egress thuộc Phase 5+ và phải có consent/retention/authority riêng trước khi bật.
- P5 phải bắt đầu bằng P5-COLLAB-00 research/ADR; không thêm whiteboard/collaboration runtime trước
  khi engine, license, document authority và sync topology được chốt.

## 8. Quyết định chuyển phase

- [x] P4-00, P4-MEDIA-UX-00 và P4-01 đến P4-12 đều `DONE`.
- [x] Exact local/disposable/CI/staging/browser/device/load acceptance đã lưu trong repository.
- [x] Kill switch và controlled cleanup đã chứng minh; final canary off.
- [x] Recording/egress/effect và Phase 3 carry-over vẫn fail closed.
- [x] Declared support không suy PASS cho thiết bị chưa có.

**Quyết định: ĐẠT - Phase 4 hoàn thành ngày 2026-08-16; được phép bắt đầu P5-COLLAB-00.**

Nếu closure-record docs-only thất bại Verify hoặc Security sau push, phải mở lại P4-12, sửa regression
và cập nhật biên bản; không được giữ `DONE` chỉ dựa trên candidate runtime trước đó.
