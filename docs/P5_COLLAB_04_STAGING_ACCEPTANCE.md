# P5-COLLAB-04 - Grant broker và revoke generation acceptance

Ngày chốt candidate: 2026-08-21
Trạng thái: `DONE` ở local candidate; chưa deploy/shared-staging, production whiteboard vẫn force-off.

## 1. Phạm vi đã hoàn thành

- Core API phát one-time grant ngẫu nhiên, chỉ lưu SHA-256 digest, TTL mặc định 30 giây và hard cap
  60 giây.
- Grant bind exact `tenant_id`, `actor_id`, `session_id`, `document_id`, capability, Origin,
  provider, generation và writer fence.
- Exchange là atomic single-winner; lần replay, binding sai hoặc grant hết hạn đều fail closed.
- Runtime nhận authority lease riêng và revalidate theo batch mỗi 750 ms; connection mất authority bị
  đóng bằng policy code `4403` trong thời gian hữu hạn.
- `view` luôn được runtime đặt read-only và mutation bị chặn server-side; `edit/present` chỉ được cấp
  khi current database authority còn hợp lệ.
- Suspend/close/restore document revoke cả pending grant lẫn active lease; membership, source,
  capability, generation, revoke generation, provider và writer fence được đối chiếu lại với
  PostgreSQL khi exchange/refresh.
- Internal exchange/validate endpoint dùng shared bearer token, constant-time digest comparison,
  strict/bounded JSON, `no-store`, `no-referrer` và generic privacy-safe error.
- Cấu hình fail closed: broker chỉ bật khi provider URL và internal runtime token cùng hợp lệ; token
  tối thiểu 32 ký tự; staging/production chỉ nhận `wss` origin không có credential/path/query.

## 2. Kiến trúc private-alpha đã chấp nhận

Broker hiện là atomic process-local store trong một Core API instance, phù hợp profile private alpha
đã chấp thuận. Restart Core API làm mất toàn bộ pending grant và lease, vì vậy runtime sẽ đóng
connection theo hướng fail closed. Trước khi chạy nhiều Core API instance phải thay store này bằng
shared atomic broker; việc scale/quota/backpressure thuộc P5-COLLAB-05/P5-COLLAB-09.

P5-COLLAB-04 không thêm migration. Repository dùng schema `000037` và exact tenant-scoped authority
query đã được disposable acceptance ở P5-COLLAB-03 xác nhận.

## 3. Cấu hình candidate

Chỉ ghi tên biến, không ghi giá trị:

- `COLLABORATION_CONTROL_PLANE_ENABLED`
- `COLLABORATION_PROVIDER_URL`
- `COLLABORATION_GRANT_TTL`
- `COLLAB_CONTROL_PLANE_TOKEN`

Các biến mới có default fail closed trong `.env.example`. Không có secret trong source, URL, browser
storage, audit, log hoặc client bundle.

## 4. Kết quả gate tự động

- Broker unit/security: one-time exchange, replay, expired, exact binding, forged field, rate limit,
  32 concurrent consumers/single winner và revoke purge đều PASS.
- Core service/HTTP: current-authority revalidation, lifecycle revoke, privacy headers, strict body,
  unauthorized internal request và no-secret-log đều PASS.
- Runtime: control-plane exact exchange/validation, read-only enforcement, authority-off và revoked
  lease bounded disconnect, snapshot recovery và cleanup-zero: 5 files/17 tests PASS.
- PostgreSQL integration-tag compile PASS; không cần kết nối hoặc write Neon mới cho task này.
- Full `pnpm verify` PASS ngày 2026-08-21: format, generated API, local/E2E infrastructure,
  security/actions, lint, typecheck, package tests/builds, Storybook, client bundle scan, Go tests và
  Go vet.
- `git diff --check` PASS; chỉ có cảnh báo line-ending LF/CRLF của Windows.

## 5. Boundary còn giữ đóng

- Không forward migration, không đổi shared staging, không deploy Render và không bật tenant feature.
- Production whiteboard giữ force-off tới P5-COLLAB-17.
- Data-plane transport đầy đủ, connection quota, payload/rate/backpressure và split-brain gate tiếp
  tục ở P5-COLLAB-05.
