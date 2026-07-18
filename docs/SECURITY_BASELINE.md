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

**P1-06 đã triển khai:** state, nonce, browser binding và PKCE `S256`; flow one-time;
ID token signature/issuer/audience/expiry; verified email; keyed hash session/CSRF;
idle + absolute timeout; server-side revoke; cookie `__Host-` ở HTTPS và `/me` không
lộ session ID. MFA/passkey, device history và refresh token rotation vẫn là gate sau.

## 3. Authorization

- Deny by default; kiểm tra tenant, membership, action và resource ở server.
- Không tin role, tenant ID, owner ID do client gửi.
- LiveKit grant và file presigned URL được cấp ngắn hạn, tối thiểu quyền.
- Thao tác quản trị, recording, exam và AI tool có audit event bất biến.

**P2-03 đã triển khai:** `tenant.manage_members` chỉ cấp cho active `org_admin` trong
đúng active tenant; create/revoke được repository reauthorize trong transaction.
Invitation không cấp `org_admin`; accept yêu cầu session, CSRF và active verified linked
identity khớp exact normalized provider email. Cross-tenant và terminal token đều dùng
uniform unavailable response để giảm enumeration.

**P2-04 đã triển khai:** `class.archive` và `class.transfer_ownership` chỉ cấp cho
active `org_admin` hoặc owner implicit từ `owner_user_id` của đúng class; teacher và
co-teacher không được suy rộng hai quyền lifecycle này. Update/archive/restore/transfer
dùng `expected_version` CAS, tenant scope server-side, authoritative membership
reauthorization và transactional outbox trong cùng business transaction. Ownership
target phải là active member cùng tenant đủ điều kiện `class.create`.

Transfer ownership yêu cầu `auth_time` của principal/session trong 10 phút, tái dùng
semantics recent-auth P2-01. Luồng hiện chưa force một OIDC authorization mới bằng
`max_age`/`prompt`, nên đây chưa phải step-up tuyệt đối và phải được tăng cường trước
các môi trường/rủi ro yêu cầu xác thực lại bắt buộc.

LiveKit token và media event mới chỉ được xử lý khi class active. Archive chặn credential
mới nhưng không thể thu hồi JWT đã cấp hoặc tự kick participant đang kết nối; TTL ngắn
và room moderation về sau là các lớp kiểm soát bổ sung.

## 4. Web security

- CSP nghiêm ngặt, không phụ thuộc inline script/eval.
- CSRF protection cho request dùng cookie; CORS allowlist chính xác.
- DOM output được encode/sanitize; Markdown/HTML/diagram render trong policy hoặc sandbox.
- Artifact/preview không đáng tin chạy trong sandboxed iframe, tách origin khi cần.
- Security headers: HSTS, frame-ancestors, nosniff, referrer policy và permissions policy.
- Rate limit theo IP, session, tenant và action; có chống brute force và abuse.

Membership invitation dùng token CSPRNG 256-bit có version prefix. Share URL giữ token
trong fragment, web xóa fragment ngay; preview/accept chỉ nhận token trong POST JSON body
và trả `Cache-Control: no-store`, `Referrer-Policy: no-referrer`. Bounded local limiter
theo action/`RemoteAddr` là guard private-alpha; P2-09 phải bổ sung trusted-proxy/origin
authentication và distributed limiter trước khi tăng lưu lượng.

## 5. Dữ liệu và secret

- Secret nằm trong secret manager/KMS; repository chỉ có `.env.example` chứa tên biến.
- Giai đoạn HF Spaces dùng Space Secrets; Neon runtime/migration role và B2 application key phải tách quyền.
- Encryption in transit bắt buộc; at-rest theo managed service và phân loại dữ liệu.
- Không log token, password, API key, nội dung private chat, raw recording hoặc tài liệu học sinh.
- Backup mã hóa, có retention và restore test định kỳ.
- Signed URL ngắn hạn; file upload kiểm tra kích thước, MIME thực, checksum và malware.

Invitation database chỉ giữ purpose-bound HMAC 32 byte, không giữ raw token. Outbox
payload dùng allowlist actor/status/role/expiry/membership ID, không chứa email, token,
token hash hoặc session identifier; structured-log regression test kiểm tra raw token
không xuất hiện.

## 6. Chuỗi cung ứng

- Lockfile bắt buộc; dependency update tự động có review.
- CI chạy secret scan, SAST, dependency/container scan và tạo SBOM release.
- Artifact được ký; deployment dùng short-lived workload identity thay static cloud key.
- Branch `main` được bảo vệ, PR review và status checks bắt buộc.

**P1-08A đã triển khai:** workflow Verify/Security, action pin bằng full commit SHA, quyền workflow tối thiểu,
Gitleaks, Dependency Review, CodeQL, Trivy filesystem/container, client-bundle secret guard, CODEOWNERS,
Dependabot và private vulnerability policy. SBOM, ký artifact, workload identity và deployment gate thuộc
P1-08B/các phase release; cấu hình ruleset và security switches trên GitHub phải được xác nhận theo
`docs/CI_SECURITY.md`.

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
