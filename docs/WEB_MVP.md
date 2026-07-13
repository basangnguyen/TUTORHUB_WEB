# Định nghĩa Web MVP

## 1. Vertical slice

```text
Đăng nhập
  -> Xem hồ sơ và tenant hiện tại
  -> Xem/tạo lớp
  -> Gửi hoặc nhập mã mời
  -> Xem lịch phiên học
  -> Prejoin kiểm tra mic/camera/loa
  -> Backend cấp quyền và LiveKit token
  -> Tham gia phòng
  -> Camera, mic, screen share, chat, participant list
  -> Giáo viên mute/remove/admit theo quyền
  -> Rời phòng và đóng session local
```

## 2. Epic và tiêu chí chấp nhận

### MVP-01 Identity

- Đăng nhập qua OIDC Authorization Code + PKCE.
- Backend tạo session cookie `HttpOnly`, `Secure`, `SameSite` phù hợp.
- `/api/v1/me` trả user, tenant, role và permission hiệu lực.
- Logout thu hồi session server-side.
- Route riêng tư không render dữ liệu trước khi kiểm tra session.

### MVP-02 Organization và role

- Một người dùng có thể thuộc nhiều tenant nhưng chỉ thao tác trong tenant đang chọn.
- Role tối thiểu: Organization Admin, Teacher, Student, Guest.
- API từ chối thao tác vượt quyền dù UI đã ẩn nút.

### MVP-03 Classes

- Giáo viên tạo, sửa, lưu trữ lớp.
- Học sinh tham gia bằng lời mời hoặc mã có hạn dùng.
- Danh sách lớp có phân trang cursor, loading/empty/error state.
- Chỉ thành viên được phép xem thông tin lớp.

### MVP-04 Sessions và schedule

- Giáo viên tạo phiên học theo thời gian.
- Người học xem trạng thái upcoming/live/ended.
- Chỉ role phù hợp được mở phòng hoặc kết thúc phiên.

### MVP-05 Prejoin

- Liệt kê thiết bị mic/camera sau khi người dùng cấp quyền.
- Cho phép bật/tắt, xem preview, chọn thiết bị và kiểm tra lỗi quyền.
- Không tự động mở camera trước hành động rõ ràng của người dùng.
- Hiển thị hướng dẫn khi thiết bị đang bị ứng dụng khác sử dụng.

### MVP-06 Live classroom

- Token LiveKit do backend cấp theo user, tenant, class, session và role.
- Camera, micro, screen share, active speaker, reconnect và quality state hoạt động.
- Participant list phân biệt teacher/student/guest.
- Giáo viên có quyền admit, mute track và remove participant theo policy.
- Khi rời phòng, client unpublish track, disconnect và giải phóng thiết bị.

### MVP-07 Chat cơ bản

- Tin nhắn phiên học có định danh người gửi và timestamp server.
- Nội dung được sanitize; không render HTML tùy ý.
- MVP có thể dùng backend realtime riêng để lưu chat; LiveKit DataChannel chỉ dùng cho tín hiệu tạm thời.

### MVP-08 Vận hành

- Có structured log, request ID, error tracking và health/readiness endpoint.
- Có dashboard tỷ lệ join phòng, lỗi media, API latency và auth failure.
- Có feature flag tắt lớp học mới mà không rollback toàn hệ thống.

## 3. Màn hình bắt buộc

1. Login/callback/error.
2. Tenant selector nếu người dùng có nhiều tenant.
3. App shell và hồ sơ tối thiểu.
4. Class list, class detail, create/edit class.
5. Join by code/invite.
6. Session detail và prejoin.
7. Classroom với stage, participant rail, chat drawer và toolbar.
8. Permission denied, not found, offline và unexpected error.

## 4. Definition of Done

- OpenAPI và generated client được cập nhật.
- Authorization test có cả allow và deny case.
- Unit/integration test đạt ngưỡng do CI quy định.
- Playwright có happy path và lỗi quan trọng.
- Keyboard navigation và screen reader labels được kiểm tra.
- Không có secret trong bundle frontend hoặc log.
- Có migration, rollback và telemetry cho thay đổi production.
