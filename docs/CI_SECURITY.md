# CI/CD and Security Runbook

## 1. Scope

P1-08A thiết lập pipeline kiểm tra và baseline bảo mật kho mã. P1-08B và P1-10 đã hoàn thành ngày 2026-07-16: web chạy trên Cloudflare Pages, Core API chạy trên Render, còn Neon, Backblaze B2, LiveKit và ZITADEL dùng resource staging tách biệt.

## 2. Required workflows

### Verify

`Quality and integration` runs on pull requests to `main`, pushes to `main` and manual dispatch. It provisions PostgreSQL 17 and executes:

1. installation from the committed pnpm lockfile;
2. local GitHub Actions policy validation;
3. classroom and identity integration tests against real PostgreSQL;
4. format, generated OpenAPI client, lint, typecheck, unit test, production build, Storybook build, client-bundle secret check, Go test and Go vet.

### Security

The workflow runs on pull requests, pushes to `main`, manual dispatch and every Monday:

| Check                            | Purpose                                                       | Blocking threshold             |
| -------------------------------- | ------------------------------------------------------------- | ------------------------------ |
| `Secret scan`                    | Scan complete Git history with Gitleaks                       | Any verified secret finding    |
| `Dependency review`              | Inspect dependencies newly introduced by a pull request       | New High/Critical advisory     |
| `CodeQL (javascript-typescript)` | SAST for browser and TypeScript code                          | Code scanning policy in GitHub |
| `CodeQL (go)`                    | SAST for the Core API                                         | Code scanning policy in GitHub |
| `Repository vulnerability scan`  | Trivy filesystem dependency, secret and misconfiguration scan | Fixed High/Critical finding    |
| `Core API container scan`        | Build and scan the production Docker image                    | Fixed High/Critical finding    |

CodeQL and SARIF uploads are not granted write access for untrusted fork pull requests. The workflow never uses `pull_request_target`.

## 3. Local commands

Chạy cùng các gate trước khi push trực tiếp lên `main`:

```powershell
pnpm install --frozen-lockfile
pnpm security:test
pnpm security:actions
pnpm test:integration
pnpm verify
```

`pnpm security:bundle` expects `apps/web/dist` and is already executed after the production web build by `pnpm verify:web`.

## 4. GitHub Actions supply-chain policy

- Every external action is referenced by a complete 40-character commit SHA.
- The human-readable release is retained as an adjacent comment for review.
- Every checkout disables persisted Git credentials.
- Every job has a timeout and both workflows cancel superseded runs.
- Workflow-wide permission is `contents: read`; only CodeQL/SARIF jobs receive `security-events: write`.
- An action update must arrive through a reviewed Dependabot pull request or be verified against the publisher's official repository. Run `pnpm security:actions` after editing a workflow.

## 5. Repository settings checklist

These controls are configured in GitHub and cannot be proven by repository files alone:

### Actions

- Allow actions from GitHub and verified publishers required by the workflows.
- Require actions to be pinned to a full-length commit SHA when the repository setting is available.
- Keep workflow permissions at read-only by default and do not allow Actions to approve pull requests.

### Security and analysis

- Enable dependency graph, Dependabot alerts and Dependabot security updates.
- Enable code scanning, secret scanning, push protection and private vulnerability reporting when available for the repository plan.
- Review unresolved CodeQL, Gitleaks and Trivy findings before every release.

### Ruleset for `main`

Dự án hiện do một người duy trì và dùng GitHub làm nơi lưu trữ/lịch sử mã nguồn. Quy trình mặc định là commit và push trực tiếp lên `main`, vì vậy không bật điều kiện bắt buộc pull request hoặc approval.

- Chạy `pnpm verify` trước mỗi lần push lên `main`.
- Giữ workflow Verify và Security chạy trên mọi push lên `main` để tạo bằng chứng hậu kiểm.
- Chặn force push và xóa nhánh `main`; không dùng `git push --force`.
- Bật dependency graph, Dependabot, code scanning, secret scanning và push protection khi gói GitHub hỗ trợ.
- Chỉ dùng nhánh tạm/PR cho thay đổi rủi ro cao, migration phá vỡ tương thích hoặc khi cần review độc lập.

Đây là đánh đổi có chủ đích: CI chạy sau push không thể ngăn một commit lỗi đi vào
`main` như required checks trước merge. Vì vậy kiểm tra cục bộ là gate bắt buộc.
Kiểm tra ngày 2026-07-16 cho thấy repository chưa có ruleset công khai; không có đủ
bằng chứng để khẳng định các security switch cấp repository đã bật. Trạng thái này
được chấp nhận có thời hạn trong ADR-0012 cho development/staging/private alpha và
không còn được ghi như một kiểm soát đã triển khai. Trước pilot/public beta hoặc khi
có người duy trì thứ hai phải thay bằng ruleset và review độc lập.

## 6. Triage and exceptions

1. Reproduce a finding on the exact commit and identify the owning module.
2. Prefer upgrading, removing or isolating the affected component.
3. A suppression must link to an issue, explain why the finding is not exploitable, name an owner and include an expiry date.
4. Never suppress a confirmed credential. Revoke and replace it, then remove it from all reachable history and artifacts.
5. A temporary CI outage may be bypassed only by the repository owner after recording the failed check, risk, approval and follow-up issue.

## 7. Trạng thái P1-08B

P1-08B đã hoàn thành ngày 2026-07-16 với các bằng chứng sau:

- Cloudflare Pages tự động triển khai web từ `main` và proxy same-origin `/api/*` tới Core API.
- Render triển khai Core API bằng OCI container; `/health` và `/ready` đạt cả trực tiếp lẫn qua Cloudflare.
- Migration `up/down/up`, trạng thái `dirty=false`, database/runtime role và migration role tách biệt đã được smoke test trên Neon staging.
- ZITADEL staging login/callback, `/me`, reload session, logout và đăng nhập lại đều đạt.
- LiveKit camera, mic, screen share, reconnect 2-5 người và webhook signature/idempotency đều đạt.
- Backblaze B2 least-privilege key và chu trình PUT/GET/checksum/DELETE đều đạt.

P1-08 đã đóng cùng exit gate Phase 1. Phần CI/security kỹ thuật đã đạt; phần quản trị
GitHub dùng ngoại lệ ADR-0012 và là rủi ro chuyển tiếp phải xử lý trước pilot.
