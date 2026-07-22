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

**P2-05 đã triển khai:** owner vẫn là implicit từ `owner_user_id`; mọi class role khác
được resolve từ `class_enrollments` persisted và chỉ state `active` mới cấp quyền.
`viewer_access` được server project cho từng class, nên browser/session không được tự
khai role hoặc suy quyền media từ tenant membership. Suspended/left/removed không giữ
class/media privilege; organization role và owner exception vẫn đi qua shared policy.
Direct enroll, suspend/remove, invite-code join và leave đều reauthorize tenant/class/
membership authoritative trong transaction và ghi success event bằng outbox.

**P2-06 đã triển khai:** roster list/single/bulk API tiếp tục tenant/class-scoped và
reauthorize membership/enrollment authoritative trong transaction. Sau
`enrollment.manage`, shared policy bắt buộc hierarchy nghiêm ngặt `org_admin > owner >
teacher/co_teacher > teaching_assistant > student/guest`; actor không thể mutate chính
mình, peer, cấp role bằng/cao hơn hoặc mutate/gán owner. Owner chỉ đổi qua ownership
transfer. Server trả action/capability projection; UI không tự suy quyền từ role hoặc
global session permission.

Roster cursor không chứa display name/email và được bind với tenant, class, normalized
search và status để chặn reuse khác scope/filter. Search coi `%`/`_` là literal. Bulk
giới hạn 50 user ID duy nhất, một action mỗi request và trả domain failure theo item;
item commit độc lập nên client bắt buộc refetch sau 5xx trước khi retry. Archived class
chỉ đọc roster lịch sử, không cho mutation.

**P2-07 đã triển khai:** `audit.view` chỉ cấp cho active `org_admin`; query audit reload
tenant/membership authoritative, bắt buộc path tenant trùng active tenant và dùng 404
concealment cho cross-scope. `audit_events` tách khỏi mutable outbox, tenant-owned và có
trigger `ALWAYS` từ chối update/delete/truncate. Changed success được append cùng
business transaction/outbox; authenticated no-op, denial, domain failure và panic có
fallback audit theo server-generated request-instance. Invitation accept chỉ bind target
tenant sau lookup token authoritative; bulk roster bind thêm target user server-owned để
dedupe từng item sau commit không chắc chắn. Không có update/delete API.

Transfer ownership yêu cầu `auth_time` của principal/session trong 10 phút, tái dùng
semantics recent-auth P2-01. Luồng hiện chưa force một OIDC authorization mới bằng
`max_age`/`prompt`, nên đây chưa phải step-up tuyệt đối và phải được tăng cường trước
các môi trường/rủi ro yêu cầu xác thực lại bắt buộc.

LiveKit token và media event mới chỉ được xử lý khi class active và class access
authoritative cho phép join/publish. Archive chặn direct enrollment, create/join invite
code và credential/media request mới nhưng vẫn cho list/revoke code lịch sử và leave;
nó không thể thu hồi JWT đã cấp hoặc tự kick participant đang kết nối. TTL ngắn và room
moderation về sau là các lớp kiểm soát bổ sung. Role roster mới được áp dụng cho token
cấp sau mutation; JWT đã phát và participant đang kết nối không đổi retroactively.

## 4. Web security

- CSP nghiêm ngặt, không phụ thuộc inline script/eval.
- CSRF protection cho request dùng cookie; CORS allowlist chính xác.
- DOM output được encode/sanitize; Markdown/HTML/diagram render trong policy hoặc sandbox.
- Artifact/preview không đáng tin chạy trong sandboxed iframe, tách origin khi cần.
- Security headers: HSTS, frame-ancestors, nosniff, referrer policy và permissions policy.
- Rate limit theo IP, session, tenant và action; có chống brute force và abuse.

Membership invitation dùng token CSPRNG 256-bit có version prefix. Share URL giữ token
trong fragment, web xóa fragment ngay; preview/accept chỉ nhận token trong POST JSON body
và trả `Cache-Control: no-store`, `Referrer-Policy: no-referrer`. P2-09 chỉ nhận client
prefix do Cloudflare Pages ký bằng `EDGE_CONTEXT_SECRET`; Core API xác minh timestamp,
method, path và prefix trước khi dùng shared PostgreSQL fixed-window limiter. Skew cấu
hình mặc định `2m` và không được vượt `5m`. Chữ ký thiếu/sai/quá hạn bị bỏ qua và
fallback về direct peer prefix; storage lỗi mới chặn request. Database chỉ lưu bucket
SHA-256 domain-separated theo limiter version/purpose/prefix, không lưu địa chỉ client
thô và không dùng cùng digest để liên kết client giữa các purpose.

Class invite code áp dụng cùng boundary nhưng dùng prefix `thciv1_` và purpose HMAC
`class-invite-code-v1`. Raw token chỉ trả một lần trong fragment `/class-invite#token=...`;
web xóa fragment ngay, giữ token trong memory và chỉ gửi qua POST JSON body có session +
CSRF. Token không được đưa vào path/query, browser storage, Query key/cache hoặc log.
TTL bị giới hạn 15 phút-30 ngày, usage 1-1000; transaction lock và conditional update
bảo đảm lượt cuối atomically chuyển `exhausted`, còn active replay không tiêu thụ lượt.
Join dùng cùng signed edge context và shared PostgreSQL limiter như membership
invitation, tách purpose để không dùng chung bucket. Render origin vẫn không tin
`Forwarded`/`X-Forwarded-For` do client gửi trực tiếp.

P2-10 áp strict JSON object cho toàn bộ mutation: unknown field, duplicate field kể cả
khác hoa/thường, trailing JSON, payload không phải object và body vượt giới hạn đều bị
từ chối trước service. UUID ở path/query/body nhạy cảm chỉ nhận dạng canonical, non-nil.
Class cursor v2 bind active tenant cùng filter; class/roster/audit decoder từ chối unknown
hoặc trailing payload. Cursor vẫn là untrusted pagination anchor và không thay thế
tenant/class predicate trong SQL. Scope hash hiện không phải chữ ký HMAC; đây là finding
Low đã ghi trong `docs/P2_10_SECURITY_MATRIX.md`, không phải căn cứ cấp quyền.

## 5. Dữ liệu và secret

- Secret nằm trong secret manager/KMS; repository chỉ có `.env.example` chứa tên biến.
- Giai đoạn HF Spaces dùng Space Secrets; Neon runtime/migration role và B2 application key phải tách quyền.
- Encryption in transit bắt buộc; at-rest theo managed service và phân loại dữ liệu.
- Không log token, password, API key, nội dung private chat, raw recording hoặc tài liệu học sinh.
- Backup mã hóa, có retention và restore test định kỳ.
- Signed URL ngắn hạn; file upload kiểm tra kích thước, MIME thực, checksum và malware.

Membership invitation và class invite code trong database chỉ giữ purpose-bound HMAC
32 byte, không giữ raw token. Outbox payload dùng allowlist actor/status/role/expiry/
usage/membership ID, không chứa email, token, token hash hoặc session identifier;
structured-log regression test kiểm tra raw token không xuất hiện.

Event `class.enrollment.role_changed` chỉ lưu tenant/aggregate/class/user/actor ID,
role trước/sau, status và source allowlist; không lưu display name, email hay token.

Audit metadata chỉ nhận object phẳng do server tạo, key/value bounded và chặn token,
secret, password, cookie, session, email, name, description, payload, request body,
SQL, stack, raw error và hash. `source_ip_prefix` giảm còn IPv4 `/24` hoặc IPv6 `/56`;
user agent chỉ giữ SHA-256; cả hai không được trả qua API. Public projection chỉ chứa
actor ID/display name hiện hành, action/resource/outcome, request ID, redacted metadata
và timestamp, đồng thời trả `Cache-Control: no-store`/`Referrer-Policy: no-referrer`.

Nếu PostgreSQL không khả dụng thì failed attempt không thể ghi durable; Core API giữ
response gốc và phát structured `audit_write_failed` chỉ có request ID/action/status,
không log raw database error. Retention, privacy erasure, partitioning, export vận hành
và dedicated maintenance role là gate Phase 8 trước production scale.

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

| Gate             | Điều kiện                                                                                               |
| ---------------- | ------------------------------------------------------------------------------------------------------- |
| Merge            | Không có secret; test authorization pass; SAST/dependency scan không có lỗi chặn                        |
| Staging          | Threat model cập nhật; migration/rollback hợp lệ; DAST smoke pass                                       |
| Public beta      | Không Critical/High chưa xử lý; pentest các flow quan trọng; incident runbook có người chịu trách nhiệm |
| Production scale | Restore/DR drill, key rotation, audit review, privacy retention và abuse monitoring hoạt động           |

## 9. Hành động khẩn cấp từ V1

1. Thu hồi toàn bộ token/API key đã từng hiển thị trong source, ảnh hoặc hội thoại.
2. Không sao chép giá trị mặc định `API_AUTH_TOKEN` hay endpoint credential từ `AppConfig.java`.
3. Tạo secret riêng cho local/staging/production.
4. Bật secret scanning ngay khi repository V2 được đưa lên Git hosting.
5. Không dùng Hugging Face persistent/local disk làm nơi giữ session, upload hoặc dữ liệu nghiệp vụ duy nhất.
