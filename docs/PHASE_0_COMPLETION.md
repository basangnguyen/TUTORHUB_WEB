# Biên bản hoàn thành Phase 0

- Ngày: 2026-07-11
- Phạm vi: Product discovery, architecture foundation và migration planning
- Nguồn tham chiếu: TutorHub V1 tại `D:\Ban_sao_du_an`
- Trạng thái: **Completed with tracked assumptions**

## 1. Công việc đã hoàn thành

- Kiểm kê cấu trúc V1, Maven dependencies, Java client/server packages, web resources, classroom stack và Lavie API.
- Xác định ranh giới giữa web capability và native Secure Exam capability.
- Khóa vertical slice Web MVP.
- Xác định system context, module backend, data ownership và communication pattern.
- Xây dựng mô hình tenant, role và permission MVP.
- Lập ma trận di chuyển từng phân hệ V1 sang V2.
- Thiết lập security baseline và security gates.
- Chấp nhận năm ADR nền tảng.
- Chuẩn bị backlog và exit gate cho Phase 1.
- Khởi tạo Git repository V2 và `.gitignore` ngăn secret/config local phổ biến.

## 2. Quyết định đã khóa cho Phase 1

1. Repository V2 độc lập; V1 là read-only reference trong quá trình migration.
2. Monorepo cho web, shared packages, Go API, infrastructure và docs.
3. React + TypeScript strict + Vite cho web.
4. Go modular monolith cho backend mới.
5. OpenAPI contract-first và generated TypeScript client.
6. OIDC Authorization Code + PKCE qua BFF/session cookie.
7. LiveKit Cloud cho classroom MVP.
8. PostgreSQL là system of record; Redis không giữ dữ liệu duy nhất.
9. Secure Exam tiếp tục là native companion, không triển khai OS lockdown bằng browser.
10. Managed services trước; không Kubernetes trong MVP.
11. Neon PostgreSQL là relational database provider cho MVP/private beta.
12. Hugging Face Docker Spaces là application hosting cho MVP/private beta.
13. Backblaze B2 là object storage; Cloudflare có thể tiếp tục làm lớp phân phối/cache.

## 3. Giả định cần xác minh ở Phase 1/discovery

- Identity provider cụ thể và mô hình chi phí theo MAU.
- Nhà cung cấp managed Redis, CDN/WAF chính thức và vùng triển khai đầu tiên.
- Quy mô đồng thời mục tiêu của private beta và khu vực người dùng đầu tiên.
- Data dictionary/schema production V1 và chất lượng dữ liệu cần di chuyển.
- Quyền license của asset/dataset được tái sử dụng.
- Chính sách recording, chat retention, trẻ em và AI theo thị trường phát hành.

Các giả định này không chặn scaffold kỹ thuật, nhưng chặn production launch hoặc data migration tương ứng.

## 4. Phát hiện V1 ảnh hưởng trực tiếp đến V2

- V1 có UI/logic phân tán giữa Swing, JCEF, JavaFX và nhiều resource web; V2 không được port theo từng màn hình một cách cơ học.
- V1 dùng Java socket packet và một số payload string legacy; V2 phải dùng typed/versioned API.
- Cấu hình endpoint và giá trị mặc định nhạy cảm từng nằm trong source V1; tất cả credential phải rotate và không copy sang V2.
- V1 đã có React/tldraw/Yjs/LiveKit nên có thể tái sử dụng kiến thức và một phần algorithm, nhưng JCEF bridge phải loại bỏ.
- Secure Exam phụ thuộc Rust/Win32/DPAPI nên thuộc desktop/native track.

## 5. Kết quả kiểm tra tài liệu

- Tất cả liên kết Markdown cục bộ resolve thành công.
- Không phát hiện pattern Hugging Face token, Gemini key, private key hoặc legacy secret đã biết trong repository V2.
- Repository V2 hiện chỉ chứa tài liệu và quy tắc; chưa có dependency/build artifact.
- ADR-0006 ghi nhận Neon, Hugging Face Spaces và Backblaze B2, kèm review gate trước public beta.

## 6. Exit gate

Phase 0 được xem là hoàn thành. Phase 1 được phép bắt đầu với task `P1-01 Repository và toolchain`, sau đó thực hiện theo thứ tự trong `PHASE_1_BACKLOG.md`.

Trước khi kết nối bất kỳ dịch vụ cloud thật nào, phải hoàn tất rotation credential V1 và tạo secret tách biệt cho local/staging/production.
