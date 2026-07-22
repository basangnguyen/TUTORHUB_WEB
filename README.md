# TutorHub V2

TutorHub V2 là phiên bản web-first của hệ sinh thái TutorHub. Dự án được xây dựng mới, còn `D:\Ban_sao_du_an` chỉ là nguồn tham chiếu nghiệp vụ và dữ liệu của TutorHub V1.

- Repository chính thức: [basangnguyen/TUTORHUB_WEB](https://github.com/basangnguyen/TUTORHUB_WEB)
- Thư mục phát triển chuẩn: `D:\TutorHub_V2`
- Nhánh mặc định: `main`; remote chuẩn: `origin`

## Trạng thái

- Phase 0 và **Phase 1 - Engineering Foundation** đã hoàn thành ngày 2026-07-16.
- Hiện đang thực hiện **Phase 2 - Identity, tenant và class core**; P2-00 đến P2-11
  đã hoàn thành. P2-12 staging acceptance và đóng phase đang ở `VERIFY/IN PROGRESS`:
  Playwright acceptance đã bổ sung kiểm tra usage của invite link, archive chặn join
  mới và audit actor/resource/request ID. Checkpoint `3c48964` đã đạt Verify/Security
  và Cloudflare deployment check; Neon staging đã ở `13 false`, role split/default ACL,
  importer idempotency và public probe đều đạt. Render đã live release candidate
  `3c48964` (deploy `dep-d9gaiturnols73c75qp0`) và 6/6 public probe sau deploy đạt.
  7 UI scenarios S01-S07; S09 provider rollback/redeploy và owner sign-off vẫn đang chờ.
- Web MVP nền đã chạy trên staging: Cloudflare Pages -> same-origin `/api/*` -> Go
  Core API trên Render; dữ liệu dùng Neon, file dùng Backblaze B2, media dùng LiveKit
  Cloud và xác thực dùng ZITADEL.
- Exit gate Phase 1 đã đạt cho Verify/Security CI, OIDC/session/logout,
  health/readiness, migration/rollback, B2, LiveKit 2-5 người, webhook idempotent và
  local developer experience.
- Repository hiện do một người duy trì và push trực tiếp `main`; ngoại lệ quản trị
  này được giới hạn trong development/staging/private alpha theo ADR-0012.
- Master Plan web-first 2.1 và backlog Phase 2 là nguồn kế hoạch hiện hành.
- Không sao chép secret, token hoặc cấu hình production từ V1.

## Tài liệu bắt buộc đọc

1. [Quy trình phát triển và checklist](docs/AGENT_COORDINATION.md)
2. [Trạng thái hiện tại](docs/PROJECT_STATE.md)
3. [Kế hoạch tổng thể](docs/MASTER_PLAN.md)
4. [Phạm vi sản phẩm](docs/PRODUCT_SCOPE.md)
5. [Web MVP](docs/WEB_MVP.md)
6. [Bối cảnh hệ thống](docs/SYSTEM_CONTEXT.md)
7. [Mô hình miền và quyền](docs/DOMAIN_MODEL.md)
8. [Bản đồ di chuyển V1](docs/V1_MIGRATION_MAP.md)
9. [Chuẩn bảo mật](docs/SECURITY_BASELINE.md)
10. [Deployment baseline](docs/DEPLOYMENT_BASELINE.md)
11. [Lộ trình giao hàng](docs/DELIVERY_ROADMAP.md)
12. [Backlog Phase 1](docs/PHASE_1_BACKLOG.md)
13. [Biên bản hoàn thành Phase 0](docs/PHASE_0_COMPLETION.md)
14. [Biên bản hoàn thành Phase 1](docs/PHASE_1_COMPLETION.md)
15. [Backlog Phase 2](docs/PHASE_2_BACKLOG.md)
16. [Database foundation và migration runbook](docs/DATABASE.md)
17. [LiveKit spike và smoke-test runbook](docs/LIVEKIT_SPIKE_RUNBOOK.md)
18. [Design system và hướng dẫn sử dụng component](docs/DESIGN_SYSTEM.md)
19. [CI/CD và security runbook](docs/CI_SECURITY.md)
20. [Browser E2E local/staging](docs/E2E_TESTING.md)
21. [P2-12 staging acceptance](docs/P2_12_STAGING_ACCEPTANCE.md)
22. [Biên bản Phase 2 (chỉ chốt khi exit gate đạt)](docs/PHASE_2_COMPLETION.md)
23. [Chính sách báo cáo lỗ hổng](SECURITY.md)
24. [ADR-0011: Render cho Core API staging/private alpha](docs/adr/0011-render-core-api-staging.md)
25. [ADR-0012: Direct-main khi một người duy trì](docs/adr/0012-single-maintainer-direct-main-governance.md)
26. [ADR-0013: Shared organization/class authorization policy](docs/adr/0013-shared-organization-class-authorization-policy.md)
27. [ADR-0014: Append-only tenant audit log](docs/adr/0014-append-only-tenant-audit-log.md)
28. [ADR-0015: Server-evaluated feature controls và quotas](docs/adr/0015-server-evaluated-feature-controls-and-quotas.md)
29. [ADR-0016: Idempotent V1 fixture import](docs/adr/0016-idempotent-v1-fixture-import.md)

Các quyết định kiến trúc đã chấp nhận nằm trong `docs/adr`.

## Nguyên tắc

- Web-first, API-first, contract-first.
- Modular monolith trước; chỉ tách service khi có số liệu vận hành chứng minh nhu cầu.
- Managed services trước; không dùng Kubernetes trong MVP.
- Multi-tenant và phân quyền được thiết kế từ đầu.
- Secure Exam tiếp tục là sản phẩm native riêng, không giả định trình duyệt web có thể khóa hệ điều hành.
- Di chuyển theo Strangler Pattern, không big-bang rewrite.
