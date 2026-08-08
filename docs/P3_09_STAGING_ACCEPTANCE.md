# P3-09 staging acceptance: presigned B2 upload/download

## 1. Trạng thái và ranh giới

- Trạng thái hiện tại: `VERIFY`; code/disposable và B2 single/multipart provider gate đã đạt,
  exact candidate CI/security cùng shared staging/live acceptance còn mở.
- Kiến trúc: ADR-0027; P3-08/ADR-0026 vẫn là authority cho metadata và finalize.
- Shared Neon staging vẫn ở `25 false`; P3-09 không migrate hoặc deploy shared staging trước
  khi provider contract và toàn bộ disposable/local gate đạt.
- Không log/in URL ký, query string, key ID, application key, bucket credential, object key,
  filename hoặc checksum.

## 2. Credential và bucket preconditions

Nạp năm biến `B2_*` từ file local đã ignore trong cùng command chạy smoke; không in giá trị:

- `B2_ENDPOINT`, `B2_REGION`, `B2_BUCKET`;
- `B2_KEY_ID`, `B2_APPLICATION_KEY`.

Bucket phải private, không phải production. App key phải bị giới hạn vào đúng bucket/prefix
và có tối thiểu `readFiles`, `writeFiles`; exact-version cleanup cần `deleteFiles`. CORS phải
allowlist origin TutorHub, `PUT`/`GET`/`HEAD` và các signed header cần thiết. Nếu thiếu quyền
đổi CORS/lifecycle thì dừng tại gate đó và yêu cầu thao tác Backblaze thủ công.

## 3. Gate 0 — exact provider contract

Smoke dùng key ngẫu nhiên `smoke/p3-09-*`, payload nhỏ và không chứa dữ liệu người dùng:

1. Presign PUT TTL ngắn với exact key/method/content length/content type; không ký header
   checksum mà B2 không enforce.
2. PUT trực tiếp qua URL ký; không gửi credential B2 riêng.
3. Head Object đúng version selector và yêu cầu exact length/type, ETag, version ID.
4. Presign GET khóa đúng `versionId`, attachment disposition và media type; tải rồi so byte.
5. Xóa đúng test version; không đụng object có sẵn.
6. Không in URL/query/header ký hoặc giá trị `B2_*` ở success/error output.

PASS khi exact version/length/type/ETag và byte-match GET tồn tại. B2 không cung cấp SHA-256
authority cho presigned PUT; theo forward decision, P3-10 phải stream-hash exact version trước
`ready`. Feature vẫn off cho tới processing gate đó.

## 4. Core API gates sau provider PASS

### Upload capability

- Session + CSRF + expected tenant bắt buộc; chỉ creator của intent `pending` còn hạn.
- Mỗi lần cấp lại đều re-authorize active membership/class và `file.upload`.
- `expected_version` stale, archived class, expired intent, foreign tenant và inaccessible ID
  fail closed; không có URL trong problem body.
- URL hết hạn không dùng được. Signed request khóa exact length/type; finalize HEAD đúng
  version selector và fail-closed nếu size/type/ETag/version sai, không đổi PostgreSQL.

### Download capability

- Chỉ `ready`, non-deleted file sau authoritative `file.view`; pending/uploaded/processing/
  rejected đều không được cấp.
- Student chỉ tải file của lớp đang được phép; foreign tenant/inaccessible ID bị che `404`.
- GET khóa immutable `storage_version_id`, attachment disposition và stored media type; TTL
  tối đa hai phút.

### Privacy và resilience

- `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, CSP/response headers hiện hữu.
- Không URL/query/object key/provider proof trong log, audit, metric, error hoặc client cache.
- Signing/provider unavailable trả typed `503`; không mutate lifecycle/quota.
- Retry upload/finalize idempotent không double-count quota hoặc đổi object identity.

## 5. Multipart/abort gate

- Trước khi expose multipart, complete phải trả immutable version mà P3-10 có thể stream-hash;
  composite ETag không được coi là SHA-256.
- Upload ID phải được server bind bền vững với tenant/file/creator; part number, part length và
  part checksum được ký chính xác.
- Complete reauthorize và verify part manifest; abort idempotent; expired/abandoned upload có
  cleanup và lifecycle safety net.
- Không đánh dấu P3-09 `DONE` nếu chỉ single PUT đạt mà multipart/abort gate chưa được đóng hoặc
  backlog chưa được điều chỉnh bằng quyết định kiến trúc rõ ràng.

## 6. Exit decision

Chuyển `IN PROGRESS -> VERIFY` sau provider PASS, unit/HTTP/OpenAPI/client/security tests và
local `pnpm verify` PASS. Chỉ chuyển `VERIFY -> DONE` sau exact candidate CI/security xanh,
diff/secret review, disposable/live acceptance phù hợp và evidence được ghi vào tài liệu này.

## 7. Provider evidence ngày 2026-08-08 — historical decision input

- Local object-storage presign unit gate PASS; adapter khóa HTTPS, method, key, content length,
  content type, TTL và version-bound GET; không có public Core API route nào được nối.
- B2 exact PUT, length/type, ETag và version ID: PASS.
- `HeadObject(ChecksumMode=ENABLED)` không trả SHA-256: FAIL.
- Negative test cùng độ dài nhưng sai byte với signed `x-amz-checksum-sha256`: B2 vẫn nhận:
  FAIL.
- Negative test với `x-amz-content-sha256` nằm trong SigV4 signed headers: B2 vẫn nhận: FAIL.
- Đặt actual SHA-256 làm SigV4 presigned payload hash: cả payload sai và đúng đều bị HTTP 403,
  nên không tương thích: FAIL.
- Version cleanup cho object smoke đã chạy; output không chứa URL/query/object key/credential.

Kết luận lịch sử: checksum gate ban đầu không đạt. Không được đổi
`stored_checksum_sha256` thành giá trị do browser tự khai. Owner sau đó đã duyệt forward
boundary ở mục 8.

## 8. Forward design `000027` và evidence disposable

- Owner duyệt ADR-0027 forward boundary ngày 2026-08-08: finalize chỉ ghi exact-version
  size/MIME/ETag; P3-10 phải stream-hash + scan đúng version trước `ready`.
- Migration `000027` xóa riêng checksum proof không được B2 chứng minh trên
  `uploaded/processing`, giữ expected checksum, object/version và quota. Constraint mới yêu
  cầu stored SHA-256 khớp expected trước `ready`; down migration fail-closed và không được chạy
  trong acceptance này.
- Disposable: `26 false -> 27 false -> 27 false`; không rollback. Lần đầu gặp dirty marker
  sau constraint rejection; exact-schema preflight chứng minh transaction vẫn ở schema
  `000026`, sau đó chỉ sửa ledger về `26 false` rồi forward lại thành công.
- Exact runtime ACL PASS: Core API có SELECT và exact column INSERT/UPDATE cho intent/finalize;
  không còn `UPDATE(stored_checksum_sha256)`, không table INSERT/UPDATE, DELETE, TRUNCATE,
  REFERENCES, TRIGGER, ownership hoặc role đặc quyền; PUBLIC zero-grant.
- PostgreSQL content integration PASS bằng exact runtime login: ownership, stale version,
  concurrent intent/finalize, quota, archived class, tenant concealment, pre-ready denial và
  ready-only exact-version download đều đạt.
- B2 smoke forward PASS: exact PUT, HEAD đúng version selector, size/MIME/ETag/version,
  version-bound GET byte-match và exact-version cleanup; không in URL/query/credential/key.
- Full local `pnpm verify` PASS trên exact working tree: formatting, generated OpenAPI,
  local/E2E infrastructure, security actions/bundle, lint, typecheck, tests, builds,
  Storybook, toàn bộ Go test và Go vet đều xanh.
- Full local `pnpm verify` PASS trên exact working tree: formatting, generated OpenAPI,
  local/E2E infrastructure, security actions/bundle, lint, typecheck, tests, builds,
  Storybook, toàn bộ Go test và Go vet đều xanh.
- Public contract đã nối `POST /upload-capability`, version-bound `POST /finalize` và
  ready-only `POST /download-capability`; response `no-store/no-referrer`, session + CSRF +
  expected-tenant bắt buộc. `file_uploads` vẫn off; shared staging chưa migrate/deploy.
- P3-09 vẫn `IN PROGRESS` cho tới khi full candidate/security và multipart/abort/expiry gate
  được đóng hoặc scope được thay đổi bằng quyết định kiến trúc riêng.

## 9. Exact ACL delta cho `000027`

Chạy bằng migration owner, thay `tutorhub_runtime` bằng runtime role thật. Sau full P3-08
provisioning, thu hồi toàn bộ column UPDATE trên `content_files` rồi cấp lại allowlist không
có `stored_checksum_sha256`:

```sql
BEGIN;

REVOKE UPDATE (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, status, version,
    upload_expires_at, stored_size_bytes, stored_media_type,
    stored_checksum_sha256, storage_etag, storage_version_id, uploaded_at,
    processing_at, ready_at, rejected_at, deleted_at, deletion_reason,
    created_at, updated_at
) ON TABLE tutorhub.content_files FROM tutorhub_runtime;

GRANT UPDATE (
    status, version, stored_size_bytes, stored_media_type,
    storage_etag, storage_version_id, uploaded_at,
    deleted_at, deletion_reason, updated_at
) ON TABLE tutorhub.content_files TO tutorhub_runtime;

COMMIT;
```

P3-10 worker phải dùng role/ACL riêng để ghi checksum và processing lifecycle; không nới lại
Core API runtime role.

## 10. Forward design `000028` và disposable multipart evidence

- ADR-0027 đã chốt multipart không dùng composite ETag làm SHA-256. Migration
  `000028_content_file_multipart_uploads` thêm durable session/issued-part ownership; provider
  upload ID chỉ nằm trong PostgreSQL, public API chỉ nhận UUID TutorHub.
- API initiate/part/complete/abort đã nối session + CSRF + expected tenant, creator, pending
  file, expected version và intent expiry. Chỉ một session `active/completing` được giữ; single
  PUT bị chặn trong thời gian đó. Complete chỉ nhận manifest liên tục đúng toàn bộ part đã cấp,
  non-final part tối thiểu 5.000.000 byte, rồi trả immutable version cho exact finalize.
- Full local `pnpm verify` PASS sau OpenAPI/generated client, Go unit/HTTP/storage/migration và
  API client multipart tests.
- Candidate `0b65c9ca` của single-PUT/version-bound checkpoint đã đạt GitHub Verify và Security;
  exact multipart candidate đã sẵn sàng push sau provider PASS.
- Neon disposable owner/runtime preflight PASS; forward-only `27 false -> 28 false`, exact
  runtime ACL cho `content_files`, `tenant_file_usage`, multipart sessions và parts PASS.
  Runtime không có table INSERT/UPDATE/DELETE/TRUNCATE; bảng parts chỉ có SELECT + exact column
  INSERT, không cần UPDATE sau khi bỏ row-lock dư thừa.
- Toàn bộ PostgreSQL content integration PASS và final ledger giữ `28 false`: ownership/tenant
  concealment, single-PUT barrier, same-part retry, changed-length conflict, exact manifest,
  immutable completion version, completed-vs-abort conflict, idempotent abort và lazy expiry.
- Bucket-admin preflight xác nhận cấu hình cũ rỗng; provision idempotent PASS với một CORS rule
  allow origin `https://tutorhub-web.pages.dev`, `PUT`/`GET`/`HEAD`, expose `ETag` và
  `x-amz-version-id`, cùng hai lifecycle rule abort incomplete sau một ngày cho `tenants/` và
  `smoke/`. Runtime app key không được nâng quyền.
- B2 single/multipart smoke PASS: part PUT/CORS/exposed ETag, complete immutable version,
  exact-version GET byte-match, explicit abort và cleanup đều thành công. Shared staging vẫn
  `25 false`, chưa deploy.

### Provider gate result

- Admin key mới đã qua Native B2 authorization và endpoint-match preflight; secret không được in.
- S3 read-back xác nhận CORS/lifecycle đúng semantics và idempotent. Backblaze chuẩn hóa CORS rule
  ID nên verification dựa trên origin/method/header/max-age, không dựa vào provider-generated ID.
- Helper bucket-admin chỉ tồn tại tạm thời, không commit. Admin key có thể bị thu hồi sau khi
  owner xác nhận không còn cần cấu hình bucket; Core API tiếp tục dùng runtime key giới hạn.
- Exact multipart candidate `04e30649` đã push; Security PASS. Verify Browser E2E đã tái hiện race
  ở feedback/focus sau gửi tin nhắn trong hai attempt: lần đầu feedback đã hiện nhưng composer chưa
  focus, lần sau feedback bị cache refresh làm chậm quá timeout. Bản sửa không chờ invalidation để
  báo success, đồng thời chỉ focus khi mutation hết pending; regression test giữ refresh pending vẫn
  PASS. Full local `pnpm verify` PASS; host không có Docker nên exact Playwright phải được CI xác minh.
- Bước còn lại là exact race-fix candidate CI/security, forward shared staging, provision exact ACL,
  deploy và live acceptance trước `VERIFY -> DONE`.
