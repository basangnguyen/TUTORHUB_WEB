# ADR 0033: Classroom join telemetry, privacy và diagnostics export

- Status: Accepted
- Date: 2026-08-15
- Scope: P4-10
- Extends: ADR-0030, ADR-0031 và ADR-0032

## Context

P1-07 có endpoint telemetry class-wide chỉ validation rồi ghi log kèm tenant/class/actor/attempt.
Đường này không gắn với `MediaSpace/RoomInstance/ParticipantSession`, không có retention và tạo
high-cardinality log. P4-10 cần dữ liệu đủ để đo join/reconnect nhưng không biến telemetry thành
attendance, fingerprint hoặc nơi nhận raw lỗi/provider data.

## Decision

### 1. Canonical event là bounded server-bound diagnostic

Client chỉ gửi `event_id`, exact `room_instance_id`, `join_attempt_id`, enum `stage`, `outcome`,
`error_code`, `network_quality`, `media_path` và `duration_ms`. Core API re-resolve exact active
tenant, actor, join attempt và participant session; client không gửi tenant/user/participant/provider
identity. JSON đóng, tối đa 4 KiB, duration `0..600000 ms` và mọi enum dùng allowlist.

Không endpoint nào nhận token/secret/cookie, SDP, ICE, IP, device ID/label, frame/audio, exception,
stack, provider SID/name hoặc nội dung chat. Raw error chỉ được map ở client sang error code hữu hạn.
P1 class-wide telemetry tiếp tục force-off cùng legacy media và không còn là P4 authority.

### 2. Persistence và retention

Migration `000036` thêm `media_join_diagnostics`. Mỗi row tenant-scoped và FK đến exact space,
room-instance, participant-session. `retention_until = recorded_at + 30 days` là constraint cứng.
Runtime chỉ có exact column `SELECT/INSERT`; không có `UPDATE/DELETE` hay function execute.

`purge_expired_media_join_diagnostics(batch_size)` là static `SECURITY DEFINER`, batch `1..1000`,
schema-qualified, `FOR UPDATE SKIP LOCKED`; chỉ maintenance role hiện hữu được `EXECUTE`. PUBLIC và
Core API runtime bị revoke. Purge không chạy opportunistic trên request path.

### 3. Support export và aggregate

`POST /api/v1/media/diagnostics/export` chỉ cho active `org_admin` trong active tenant, yêu cầu CSRF
và expected-tenant assertion. Range tối đa 31 ngày, limit `1..1000`. Response luôn `private,
no-store`, `no-referrer`, `nosniff`; event chỉ có pseudonymous session reference, enum, duration và
timestamp. Không xuất UUID user/participant/room/attempt, source title, provider identity hay raw
error.

Export trả aggregate cùng payload: distinct join attempts, successful time-to-media, join success
rate, p95 time-to-media và reconnect succeeded/failed. Aggregate không có dimension tenant, user,
space, room hoặc participant. Export chỉ được trả sau khi audit `media_diagnostics.export` ghi thành
công với metadata bounded (`range_hours`, `row_count`, `truncated`). Audit failure làm export fail
closed.

### 4. Client lifecycle

P4 room flow phát event ở các transition hữu ích: join-attempt accepted, credential accepted,
media connected, reconnect started/succeeded và terminal disconnect. Event là best-effort và không
được chặn Join/Leave. Device/effect choices không được persist; `media_path` chỉ là coarse
`audio_video|audio_only|listen_only|unknown`.

Background diagnostics không được tự gọi CSRF rotation vì rotation vô hiệu token trước đó và có thể
phá lệnh join/moderation đang chạy. Generated client chỉ giữ latest CSRF token trong memory; emitter
dùng token đó hoặc bỏ event nếu token thiếu/stale. Không persist token, không retry bằng rotation.
Export do Admin chủ động vẫn dùng chuỗi rotate-then-submit chuẩn và chờ kết quả.

## Verification

- Unit/HTTP: closed JSON, enum/range validation, tenant/actor binding, org-admin-only export,
  no-store/audit fail-closed và redaction.
- PostgreSQL: exact FK/tenant isolation/idempotency, metric math, 30-day retention, runtime/PUBLIC/
  maintenance ACL và concurrent SKIP LOCKED purge trên disposable branch.
- Web/client: bounded mapper, lifecycle emission không chặn media, admin-only diagnostics panel,
  no storage/URL/device-label/raw-error leakage.
