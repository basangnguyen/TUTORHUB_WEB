# Phạm vi sản phẩm TutorHub V2

## 1. Tầm nhìn

TutorHub V2 là nền tảng học trực tuyến đa thiết bị dành cho tổ chức giáo dục, giáo viên và người học. Trục sản phẩm là lớp học trực tuyến kết hợp video, cộng tác bảng trắng, nội dung học tập, đánh giá và giao tiếp trong một hệ thống thống nhất.

V2 không phải bản chuyển giao diện Java sang trình duyệt. Đây là tái cấu trúc sản phẩm theo web-first, API-first và multi-tenant để về sau dùng chung backend cho web, desktop Tauri và mobile React Native.

## 2. Nhóm người dùng

| Nhóm | Nhu cầu chính |
|---|---|
| Platform Admin | Quản trị tenant, vận hành, an toàn và hỗ trợ |
| Organization Admin | Quản lý tổ chức, thành viên, chính sách và báo cáo |
| Teacher | Tạo lớp, tổ chức buổi học, giao nội dung và đánh giá |
| Teaching Assistant | Hỗ trợ lớp theo quyền được ủy quyền |
| Student | Tham gia lớp, học trực tuyến, làm bài và xem tài liệu |
| Guest | Tham gia buổi học giới hạn qua lời mời |

## 3. Miền sản phẩm mục tiêu

1. Identity, organization và phân quyền.
2. Lớp học, tuyển sinh, lịch và phiên học trực tuyến.
3. Video conference, chat, participant moderation và recording.
4. Whiteboard cộng tác và công cụ dạy học.
5. File, tài liệu, nội dung và chia sẻ.
6. Bài tập, ngân hàng câu hỏi, đề thi, QuizHub và kết quả.
7. Tin nhắn, thông báo và tìm kiếm.
8. Lavie AI với quyền, audit và dữ liệu được kiểm soát.
9. Social learning gồm bảng tin, Reels và Locket.
10. Subscription, billing và quản trị nền tảng.
11. Secure Exam native companion.

## 4. Thứ tự ưu tiên

| Cấp | Phạm vi |
|---|---|
| P0 - Web MVP | Auth, hồ sơ tối thiểu, danh sách lớp, tạo/tham gia lớp, prejoin, phòng LiveKit, chat cơ bản, participant list |
| P1 - Classroom Core | Lobby, moderation, screen share, reactions, persistent chat, whiteboard, file, lịch, thông báo |
| P2 - Learning | Assignment, question bank, exam, QuizHub, grading, analytics |
| P3 - Expansion | Lavie AI, social learning, search nâng cao, admin/billing, marketplace |
| Native track | Secure Exam, device control, OS lockdown, local media engine |

## 5. Ngoài phạm vi Web MVP

- Secure Exam khóa hệ điều hành.
- Reels/Locket và mạng xã hội đầy đủ.
- AI coding agent và thực thi lệnh trên máy người dùng.
- Breakout room nâng cao, webinar quy mô lớn và livestream CDN.
- Billing production, marketplace và thanh toán đa quốc gia.
- Migration toàn bộ dữ liệu V1 trong một lần.

## 6. Chỉ số thành công ban đầu

- Tỷ lệ đăng nhập thành công >= 99,5% khi nhà cung cấp định danh hoạt động.
- Tỷ lệ vào phòng thành công >= 99% trên tập thiết bị/băng thông được hỗ trợ.
- API p95 mục tiêu dưới 300 ms cho thao tác CRUD không gồm upload/media.
- LCP mục tiêu dưới 2,5 giây trên trang chính ở kết nối kiểm thử chuẩn.
- Không còn lỗ hổng Critical/High chưa xử lý trước public beta.
- Có thể khôi phục backup staging bằng runbook đã kiểm chứng.

Các giá trị trên là mục tiêu kỹ thuật ban đầu, phải hiệu chỉnh bằng telemetry sau private beta.

## 7. Nguyên tắc không thương lượng

- Tenant isolation và authorization do server thực thi.
- Không hardcode secret trong client hoặc repository.
- Tính năng nhạy cảm phải có audit log.
- Accessibility và i18n được xây từ đầu, tối thiểu tiếng Việt và tiếng Anh.
- Dữ liệu trẻ em, recording và AI cần chính sách đồng ý, lưu giữ và xóa rõ ràng.
