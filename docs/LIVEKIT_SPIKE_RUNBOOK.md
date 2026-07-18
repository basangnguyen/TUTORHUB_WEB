# P1-07 LiveKit Spike Runbook

## 1. Mục tiêu và trạng thái

P1-07 chứng minh một lát cắt phòng học trực tuyến tối thiểu nhưng đúng ranh giới bảo
mật: người dùng đã đăng nhập mở lớp, kiểm tra thiết bị, nhận token ngắn hạn từ Core
API rồi tham gia cùng một phòng LiveKit. Frontend không giữ API key/secret và không
lưu room token trong `localStorage` hoặc `sessionStorage`.

Trạng thái sau exit gate Phase 1 và cập nhật P2-04:

- Backend token, permission, telemetry, webhook verification và idempotency: đã có.
- Web prejoin, room, camera/micro/screen share, listen-only và reconnect UI: đã có.
- OpenAPI, generated client, unit/HTTP/web tests và production build: đã có.
- Neon migration: version `5`, `dirty=false`.
- LiveKit staging project và smoke test thật 2-5 trình duyệt: đạt ngày 2026-07-16;
  credential chỉ nằm trong provider secret store/local file được ignore.

Cập nhật P2-04 ngày 2026-07-18: Core API và web chỉ cho bắt đầu luồng media khi class
đang active. Draft/archived bị chặn trước khi phát token hoặc ghi media event mới.

## 2. Kiến trúc luồng

1. Người dùng mở `/app/classrooms/{class_id}/prejoin` trong phiên BFF đã xác thực.
2. Trình duyệt kiểm tra MediaDevices cục bộ; component `PreJoin` không cần room token.
3. Khi người dùng chọn vào phòng, web xoay CSRF và gọi
   `POST /api/v1/classes/{class_id}/media-token`.
4. Core API lấy tenant, actor, session và permission từ server-side session; không nhận
   `tenant_id`, role hoặc grant từ body trình duyệt.
5. Core API kiểm tra class đang active, `class.view` + `session.join`, sau đó phát JWT
   TTL 5 phút. Quyền publish chỉ bật khi principal có `media.publish`; publish data
   luôn tắt trong spike.
6. Credential được chuyển sang route room bằng React navigation state, sau đó xóa khỏi
   browser history state và chỉ giữ trong component memory. Reload buộc quay lại prejoin.
7. LiveKit client tự reconnect; web gửi telemetry mã hóa theo stage/outcome, không gửi
   SDP, token, nội dung media hoặc thông báo lỗi thô.
8. LiveKit gửi webhook tới Core API. Endpoint xác minh JWT/body hash bằng thư viện
   chính thức và ghi event ID vào PostgreSQL để xử lý retry theo kiểu idempotent.

## 3. Cấu hình local và staging

Tạo **project riêng cho staging**, không dùng chung key với production. Lưu giá trị thật
trong `.env.local` khi chạy local và secret store của nền tảng khi deploy; không commit.

```dotenv
LIVEKIT_URL=wss://<staging-project>.livekit.cloud
LIVEKIT_API_KEY=<staging-api-key>
LIVEKIT_API_SECRET=<staging-api-secret>
LIVEKIT_TOKEN_TTL=5m
```

Quy tắc:

- `LIVEKIT_URL` bắt buộc dùng `wss://` ở staging/production.
- TTL hợp lệ từ 1 đến 15 phút; mặc định dự án là 5 phút.
- API secret chỉ cấp cho Core API và migration/deploy secret store.
- Không tạo biến `VITE_LIVEKIT_API_SECRET` hoặc đưa key/secret vào Cloudflare Pages.
- Local có thể dùng LiveKit server tự host qua `ws://`; đây không phải cấu hình staging.

## 4. Cấu hình webhook

Sau khi Core API staging có HTTPS công khai, cấu hình URL:

```text
https://<core-api-host>/api/v1/webhooks/livekit
```

Endpoint chỉ nhận `POST` với content type `application/webhook+json`. Header
`Authorization` và SHA-256 body hash được kiểm tra bởi `livekit/protocol/webhook`.
Không đặt webhook qua cache/CDN làm thay đổi body. LiveKit có thể retry nên database
dùng `event_id` làm khóa chính; duplicate vẫn trả `204`.

Migration:

```powershell
$env:DATABASE_MIGRATION_URL = "<direct-neon-url>"
pnpm db:migrate
pnpm db:version
```

Kết quả mong đợi: `5 false`.

## 5. Chạy local

```powershell
cd D:\TutorHub_V2
$env:PATH = "$(Resolve-Path '.tools\go\bin');$env:PATH"
pnpm install --frozen-lockfile
pnpm dev
```

Mở web Vite, đăng nhập ZITADEL, chọn workspace, mở một lớp và chọn **Vào phòng học
trực tuyến**. Nếu ba biến LiveKit chưa có, Core API trả Problem Details `503` và web
hiển thị retry; không dùng token giả hoặc đưa secret xuống web để bỏ qua lỗi này.

## 6. Ma trận smoke test 2-5 người

| Ca kiểm tra       | Thao tác                                       | Kết quả bắt buộc                                 |
| ----------------- | ---------------------------------------------- | ------------------------------------------------ |
| Teacher + student | Hai profile trình duyệt vào cùng lớp           | Cùng room, thấy/nghe nhau                        |
| 5 người           | Mở 5 profile hoặc thiết bị                     | Grid ổn định, số người đúng, không tràn layout   |
| Camera/micro      | Bật/tắt và đổi thiết bị                        | Track cập nhật, không reload trang               |
| Screen share      | Teacher bật/tắt chia sẻ                        | Track màn hình xuất hiện rồi được thu hồi        |
| Listen-only       | Role không có `media.publish`                  | Không hiện nút camera/micro/share; vẫn subscribe |
| Mất mạng ngắn     | Ngắt mạng 5-15 giây rồi bật lại                | Hiện reconnecting, tự trở lại connected          |
| Mất mạng dài      | Giữ offline quá thời gian reconnect            | Hiện disconnected và có đường vào lại            |
| Reload room       | Reload trực tiếp route room                    | Không khôi phục JWT; yêu cầu về prejoin          |
| Token hết hạn     | Giữ credential quá hạn trước connect           | Không kết nối; yêu cầu token mới                 |
| Draft/archived    | Gọi token/event cho class không active          | Core API trả conflict, không phát JWT/ghi event  |
| Sai tenant/quyền  | Dùng class ngoài active tenant hoặc thiếu join | Core API trả 403/404, không phát JWT             |
| Webhook retry     | Gửi lại cùng event ID hợp lệ                   | Lần hai trả 204 và không tạo bản ghi trùng       |

Ghi lại browser/version, thiết bị, thời gian kết nối, reconnect duration và lỗi có mã.
Không chụp token, Authorization header hoặc secret trong bằng chứng kiểm thử.

## 7. Telemetry tối thiểu

Endpoint client: `POST /api/v1/classes/{class_id}/media-events`.

Stage hợp lệ: `token`, `connect`, `connected`, `media`, `reconnecting`, `reconnected`,
`disconnected`, `leave`. Outcome hợp lệ: `started`, `succeeded`, `failed`. `error_code`
chỉ nhận mã chuẩn hóa tối đa 64 ký tự; `duration_ms` tối đa 10 phút.

Không ghi:

- LiveKit access token, API key hoặc API secret.
- URL có credential, SDP, ICE candidate hoặc IP chi tiết của người dùng.
- Audio/video frame, chat payload hoặc nội dung màn hình chia sẻ.
- Chuỗi exception thô từ trình duyệt vào telemetry nghiệp vụ.

Endpoint token và telemetry cùng đọc trạng thái class authoritative; draft/archived
không được bắt đầu hoặc tiếp tục request media mới. Archive không thu hồi JWT LiveKit
đã cấp và không kick participant đang ở trong room; JWT ngắn hạn giảm cửa sổ hiệu lực,
còn revoke/kick chủ động thuộc moderation/session lifecycle về sau.

## 8. Lệnh verification

```powershell
pnpm api:check
pnpm --filter @tutorhub/api-client test
pnpm --filter @tutorhub/web lint
pnpm --filter @tutorhub/web typecheck
pnpm --filter @tutorhub/web test
pnpm --filter @tutorhub/web build
go test ./services/core-api/...
go vet ./services/core-api/...
```

P1-07 chỉ chuyển `DONE` sau khi project staging được tạo, webhook staging hoạt động và
ma trận bắt buộc teacher + student, camera/micro, screen share và reconnect có bằng
chứng đạt. Code local xanh nhưng thiếu smoke thật chỉ được ghi `IN_PROGRESS`/`REVIEW`.

## 9. Phạm vi chưa giải quyết

- Enrollment, class invite code, roster, lịch `ClassSession` và quyền theo từng lớp
  thuộc phase domain sau. P2-04 mới cung cấp active/archive guard; invite code chỉ bắt
  đầu ở P2-05.
- Recording, E2EE, moderation, waiting room, breakout room và large-room topology không
  nằm trong spike.
- Chat persistence không dùng data channel LiveKit trong phase này.
- Production region, quota, TURN quality, egress và cost guard cần đo ở staging trước
  khi chốt kiến trúc vận hành.
