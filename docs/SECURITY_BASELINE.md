# Chuẩn bảo mật TutorHub V2

## 1. Chuẩn tham chiếu

- OWASP ASVS Level 2 làm baseline cho web application; chức năng quản trị/thi cử nhạy cảm được đánh giá cao hơn theo rủi ro.
- OWASP Top 10, API Security Top 10 và cheat sheets dùng trong review.
- NIST Secure Software Development Framework cho SDLC.
- Threat model được cập nhật khi thêm trust boundary hoặc dữ liệu nhạy cảm.

## 2. Identity và session

- OIDC Authorization Code + PKCE; không tự xây password protocol nếu có thể dùng IdP đáng tin cậy.
- Backend/BFF giữ token nhà cung cấp; browser nhận opaque session cookie.
- Cookie `HttpOnly`, `Secure`, giới hạn domain/path và SameSite phù hợp.
- Session có idle timeout, absolute timeout, revoke, refresh rotation và device history.
- MFA/Passkey bắt buộc cho Platform Admin; khuyến nghị cho Org Admin/Teacher.
- Step-up authentication cho export dữ liệu, đổi quyền, billing và thao tác an toàn cao.

## 3. Authorization

- Deny by default; kiểm tra tenant, membership, action và resource ở server.
- Không tin role, tenant ID, owner ID do client gửi.
- LiveKit grant và file presigned URL được cấp ngắn hạn, tối thiểu quyền.
- Thao tác quản trị, recording, exam và AI tool có audit event bất biến.

## 4. Web security

- CSP nghiêm ngặt, không phụ thuộc inline script/eval.
- CSRF protection cho request dùng cookie; CORS allowlist chính xác.
- DOM output được encode/sanitize; Markdown/HTML/diagram render trong policy hoặc sandbox.
- Artifact/preview không đáng tin chạy trong sandboxed iframe, tách origin khi cần.
- Security headers: HSTS, frame-ancestors, nosniff, referrer policy và permissions policy.
- Rate limit theo IP, session, tenant và action; có chống brute force và abuse.

## 5. Dữ liệu và secret

- Secret nằm trong secret manager/KMS; repository chỉ có `.env.example` chứa tên biến.
- Giai đoạn HF Spaces dùng Space Secrets; Neon runtime/migration role và B2 application key phải tách quyền.
- Encryption in transit bắt buộc; at-rest theo managed service và phân loại dữ liệu.
- Không log token, password, API key, nội dung private chat, raw recording hoặc tài liệu học sinh.
- Backup mã hóa, có retention và restore test định kỳ.
- Signed URL ngắn hạn; file upload kiểm tra kích thước, MIME thực, checksum và malware.

## 6. Chuỗi cung ứng

- Lockfile bắt buộc; dependency update tự động có review.
- CI chạy secret scan, SAST, dependency/container scan và tạo SBOM release.
- Artifact được ký; deployment dùng short-lived workload identity thay static cloud key.
- Branch `main` được bảo vệ, PR review và status checks bắt buộc.

## 7. Privacy và an toàn giáo dục

- Recording, biometric/voice, trẻ em và AI training phải có consent và policy rõ.
- Tenant cấu hình retention; hỗ trợ export/xóa dữ liệu theo luật áp dụng.
- Data residency và cross-border transfer phải được đánh giá trước mở toàn cầu.
- Nội dung xã hội cần report, block, moderation và cơ chế xử lý khiếu nại.

Đây là baseline kỹ thuật, không thay thế tư vấn pháp lý về GDPR, COPPA hoặc luật địa phương.

## 8. Security gates

| Gate | Điều kiện |
|---|---|
| Merge | Không có secret; test authorization pass; SAST/dependency scan không có lỗi chặn |
| Staging | Threat model cập nhật; migration/rollback hợp lệ; DAST smoke pass |
| Public beta | Không Critical/High chưa xử lý; pentest các flow quan trọng; incident runbook có người chịu trách nhiệm |
| Production scale | Restore/DR drill, key rotation, audit review, privacy retention và abuse monitoring hoạt động |

## 9. Hành động khẩn cấp từ V1

1. Thu hồi toàn bộ token/API key đã từng hiển thị trong source, ảnh hoặc hội thoại.
2. Không sao chép giá trị mặc định `API_AUTH_TOKEN` hay endpoint credential từ `AppConfig.java`.
3. Tạo secret riêng cho local/staging/production.
4. Bật secret scanning ngay khi repository V2 được đưa lên Git hosting.
5. Không dùng Hugging Face persistent/local disk làm nơi giữ session, upload hoặc dữ liệu nghiệp vụ duy nhất.
