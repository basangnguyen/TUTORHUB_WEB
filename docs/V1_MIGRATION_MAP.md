# Bản đồ di chuyển TutorHub V1 sang V2

## 1. Hiện trạng đã kiểm kê

V1 tại `D:\Ban_sao_du_an` là ứng dụng đa công nghệ:

- Java 21, Swing, FlatLaf và MigLayout cho desktop UI.
- JCEF 122 và JavaFX cho các màn hình web/media nhúng.
- Java socket server và PostgreSQL/Neon cho nghiệp vụ lõi.
- Node/Express/WebSocket, Yjs và LiveKit cho lớp học thời gian thực.
- React/Vite/tldraw, MathLive, Mermaid và Prism cho bảng trắng/công cụ dạy học.
- FastAPI cho Lavie chat, voice, vision, document và TTS.
- Backblaze B2/S3-compatible storage, Cloudflare/CDN và FFmpeg cho file/media.
- Rust/JNA/Win32 cho Secure Exam và kiểm soát thiết bị Windows.

Audit phát hiện V1 có cấu hình endpoint phân tán, một số giá trị mặc định nhạy cảm trong source và nhiều giao thức legacy dạng packet/string. Không được chuyển nguyên các cấu hình này sang V2.

## 2. Ma trận chức năng

| V1 | Nguồn tham chiếu chính | Quyết định V2 | Đợt |
|---|---|---|---|
| Login email/Google/Facebook | `LoginFrame`, `client/oauth`, `server/oauth` | Viết lại bằng OIDC/BFF; không tái sử dụng payload `DASHBOARD_GO` | MVP |
| Main shell/search/profile | `MainDashboard`, `HeaderPanel`, `ProfileTab`, `client/search` | Thiết kế React responsive, route-based | MVP/P1 |
| Classes/accepted/invite | `ClassManagerTab`, `AcceptedClassTab`, dialog lớp | Giữ quy tắc nghiệp vụ, viết API và UI mới | MVP |
| Schedule/tasks | `ScheduleTab`, `TaskTab`, CalendarFX | Chuyển model lịch; UI web mới | MVP/P1 |
| Live classroom | `BlackboardFrame`, `PreJoinDialog`, board resources | Tái dùng ý tưởng/flow; dùng LiveKit React components/API mới | MVP/P1 |
| Whiteboard/tools | `frontend-board`, `tldraw_board_v2.html` | Port có chọn lọc React/tldraw; bỏ bridge JCEF | P1 |
| Chat/Lavie | `ChatTab`, `client/ai`, `ai_chat.html`, `app.py` | Chat người-người P1; Lavie tách riêng P3 | P1/P3 |
| Drive/files | `DriveTab`, `client/drive`, B2 code | Viết lại presigned upload + metadata + scan | P1 |
| Exam/question bank | `client/exam`, server DAO/service | Chuẩn hóa domain/API; không bê DAO trực tiếp | P2 |
| QuizHub | `client/quizhub`, `resources/tse/quiz.html` | Port game engine có test deterministic | P2 |
| Home/Reels/Locket | `HomeTab`, `home-social`, `locket-web`, DAO | Di chuyển sau classroom; có moderation/privacy | P3 |
| AI coding agent | `client/ai/agent`, tool/patch/permission/MCP | Không chạy tool local từ web; thiết kế sandbox riêng | P3+ |
| Secure Exam | `client/exam`, `tutorhub_lockdown`, `rust-core` | Giữ native product/companion; web chỉ orchestration | Native |
| Admin/upgrade | `Admin*`, `UpgradeDialog`, `web-admin` | Viết lại sau tenant/billing foundation | P3 |

## 3. Phân loại khả năng tái sử dụng

### Có thể tái sử dụng trực tiếp có kiểm tra license

- Design assets không chứa dữ liệu người dùng.
- Pure TypeScript/JavaScript algorithm độc lập DOM/JCEF, sau khi có test.
- Tldraw custom shape logic tương thích phiên bản được chọn.
- Cấu trúc câu hỏi/quiz sau khi chuẩn hóa schema.

### Chỉ tái sử dụng như đặc tả

- Java Swing UI và navigation.
- Java socket packet protocol.
- DAO SQL gắn chặt schema cũ.
- JCEF bridge, JavaFX media bridge và local filesystem flow.
- Lavie client-side permission/tool execution.

### Giữ nguyên ở sản phẩm native

- Rust lockdown, Win32 API, keyboard/process/screen protection.
- DPAPI/Windows secure storage và device-level quick settings.
- Local FFmpeg engine nếu desktop V2 vẫn cần nén trước upload.

## 4. Chiến lược dữ liệu

1. Lập data dictionary V1 và xác định chủ sở hữu từng bảng.
2. Tạo schema V2 bằng migration có version; không cho ứng dụng tự `CREATE TABLE` ở runtime production.
3. Viết ETL idempotent theo từng aggregate: user/tenant -> class -> enrollment -> session.
4. Mapping ID cũ-mới được lưu trong bảng migration riêng.
5. Chạy dry-run, checksum/count reconciliation và báo cáo bản ghi lỗi.
6. Dual-read chỉ dùng tạm thời; tránh dual-write kéo dài.
7. Cutover bằng feature flag theo tenant và có rollback window.

## 5. Anti-corruption layer

Trong giai đoạn V1 và V2 cùng chạy, adapter legacy chuyển packet/model cũ sang contract V2. Domain V2 không import class Java, tên packet hoặc schema legacy. Mọi fallback phải có ngày loại bỏ và telemetry đo số lần sử dụng.

## 6. Rủi ro đã nhận diện

| Rủi ro | Xử lý |
|---|---|
| Secret/tokens từng xuất hiện trong source hoặc trao đổi | Rotate trước khi kết nối V2; secret manager; secret scanning |
| Encoding/mojibake tiếng Việt ở tài liệu và UI V1 | UTF-8 end-to-end, test fixture tiếng Việt |
| Giao thức string/packet khó version | OpenAPI, typed schema, versioning và compatibility test |
| Quyền hiện nằm ở UI hoặc handler rời rạc | Authorization policy tập trung và deny tests |
| Bảng trắng/resource rất lớn | Module hóa, lazy load, performance budget |
| Media tải lớn và phụ thuộc mạng | Prejoin, adaptive streaming, reconnect, upload multipart |
| Secure Exam bị hiểu nhầm là web capability | Native boundary rõ ràng và signed handoff |

## 7. Điều chưa xác minh trong Phase 0

- Chất lượng và tính đầy đủ của dữ liệu production V1.
- Số người dùng đồng thời và phân bố khu vực thực tế.
- Quyền pháp lý đối với toàn bộ asset/dataset V1.
- Chính sách retention hiện hành cho recording, chat và dữ liệu học sinh.

Các mục này là discovery bắt buộc trước production migration.
