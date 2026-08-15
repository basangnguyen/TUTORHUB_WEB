# ADR 0032: Classroom reconnect, recovery instance và degraded audio-only

- Status: Accepted
- Date: 2026-08-15
- Scope: P4-09
- Extends: ADR-0030 và ADR-0031

## Context

Classroom shell hiện đã nhận các sự kiện `Reconnecting`, `Reconnected` và `Disconnected` của
LiveKit, đồng thời có degradation controller giảm dần lượng video nhận. Tuy nhiên, client chưa
phân biệt reconnect transport ngắn với disconnect không thể phục hồi, chưa có command tạo
RoomInstance recovery, và mức audio-only chưa buộc camera local ngừng phát.

LiveKit SDK tự thực hiện resume/full reconnect cho gián đoạn transport; client đang kết nối còn
nhận token refresh từ provider. Vì vậy TutorHub không mint credential mới trong sự kiện reconnect
ngắn. Khi kết nối đã terminal, browser phải quay lại Core API để tải authority hiện tại thay vì dùng
lại token, role hoặc RoomInstance đã cache.

## Decision

### 1. Ba luồng phục hồi tách biệt

1. `Reconnecting -> Reconnected` là transport recovery do LiveKit SDK sở hữu. MediaSpace,
   RoomInstance và ParticipantSession không đổi; TutorHub chỉ hiển thị trạng thái và giữ draft/chat.
2. `Disconnected` terminal luôn xóa credential khỏi memory. `PARTICIPANT_REMOVED`, `ROOM_DELETED`
   và thao tác rời chủ động không auto-rejoin. Mất mạng/unknown chỉ được quay lại prejoin sau khi
   Core API tải lại MediaSpace và xác nhận authority hiện tại.
3. Provider RoomInstance bị `failed` có thể được host/co-host có quyền start hiện tại recovery qua
   `POST /api/v1/media/spaces/{space_id}/recover`. Command tạo RoomInstance mới dưới cùng
   MediaSpace; conversation, lịch và business room không bị nhân đôi.

Browser không lưu LiveKit credential trong localStorage/sessionStorage. Rejoin/recovery luôn tạo
join attempt và credential mới cho exact RoomInstance hiện hành.

### 2. Recovery command và concurrency barrier

Request chỉ nhận `expected_space_version`, `expected_room_instance_id`,
`expected_room_instance_version` và stable `idempotency_key`; không nhận tenant, provider room,
provider SID, role hay grant.

Trong một PostgreSQL transaction, Core API khóa tenant control, MediaSpace và RoomInstance mới
nhất. Recovery chỉ hợp lệ khi:

- MediaSpace vẫn `open`, source vẫn cho phép host start/recover;
- exact expected instance là attempt mới nhất, trạng thái `failed`, version khớp;
- không có instance `provisioning|active|closing` khác;
- feature vẫn bật và actor/membership/source authority vẫn hợp lệ.

Transaction tạo opaque RoomInstance mới với `attempt_number = previous + 1`, tăng MediaSpace
version, ghi receipt và `media_space.recovered.v1`. Partial unique index hiện có tiếp tục bảo đảm
tối đa một active intent. Replay cùng key/fingerprint trả projection hiện tại; key reuse khác payload
trả conflict. End/lock/remove/enrollment revoke và source terminal luôn thắng recovery/rejoin.

Provider `EnsureRoom` và activation chạy sau commit, giống start. Provider failure trả typed `503`
nhưng không rollback recovery intent; retry cùng idempotency key hội tụ vào cùng opaque room.

### 3. Provider failure projection

Signed `room_finished` cho instance `active` hoặc `provisioning` chuyển instance sang `failed` với
bounded code `provider_room_finished`, expire lobby admission và chuyển participant còn hoạt động
sang `failed`. MediaSpace vẫn `open` để host có thể chọn recovery hoặc end. GET MediaSpace chỉ chiếu
`recovery_room_instance` khi không có active instance và latest instance là `failed`; provider name,
SID và raw error không xuất hiện.

`viewer_operations.can_recover` chỉ true khi exact feature và source authority hiện tại cho phép.
Nút recovery vẫn là convenience projection; endpoint luôn reauthorize.

### 4. Audio-only là fail-soft một chiều

Degradation tiếp tục theo thứ tự: giảm số video -> giảm quality -> stage-only -> audio-only. Khi
vào audio-only, client unsubscribe remote video và gọi tắt camera local nếu đang publish. Microphone,
remote audio, leave, reconnect status và accessibility controls vẫn hoạt động.

Hệ thống không tự bật lại camera khi chất lượng phục hồi hoặc sau reconnect. Người dùng phải chủ
động bật camera; điều này tránh camera surprise và giữ consent. Degradation state chỉ sống trong
tab/exact RoomInstance.

## Security, privacy và operations

- Migration `000035_media_room_recovery` chỉ mở rộng exact receipt allowlist/result invariant;
  không thêm bảng hay runtime privilege.
- Log/audit/outbox không chứa token, provider identity, raw provider error, SDP/ICE, IP, device label
  hoặc nội dung chat/media.
- Recovery không dùng quota start theo giờ để outage không biến thành lockout; mỗi failed attempt
  chỉ có thể sinh một successor nhờ exact version/idempotency/one-active barrier.
- Feature deployment guardrail tiếp tục force-off đến khi disposable, CI/security, shared staging
  và live feature-off acceptance PASS.

## Verification

- Unit/API contract: validation, authorization, idempotency, stale versions và provider convergence.
- PostgreSQL integration: exactly-one recovery dưới concurrency, tenant isolation, failed-only,
  end/source/revocation thắng, exact runtime/PUBLIC ACL và migration idempotency.
- Web: reconnect ngắn không gọi API, terminal disconnect xóa escrow và reauthorizes, removed/deleted
  không auto-rejoin, audio-only tắt local camera nhưng giữ audio/leave.
- LiveKit isolated gate kiểm tra transient reconnect và provider failure khi capability vẫn không
  được bật cho người dùng shared staging.

## References

- LiveKit token refresh and grants: https://docs.livekit.io/frontends/reference/tokens-grants/
- LiveKit connection lifecycle: https://docs.livekit.io/intro/basics/connect/
- LiveKit `RoomEvent`: https://docs.livekit.io/reference/client-sdk-js/enums/RoomEvent.html
- LiveKit `DisconnectReason`: https://docs.livekit.io/reference/client-sdk-js/enums/DisconnectReason.html
