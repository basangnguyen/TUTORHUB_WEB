# TutorHub V2

TutorHub V2 là phiên bản web-first của hệ sinh thái TutorHub. Dự án được xây dựng mới, còn `D:\Ban_sao_du_an` chỉ là nguồn tham chiếu nghiệp vụ và dữ liệu của TutorHub V1.

- Repository chính thức: [basangnguyen/TUTORHUB_WEB](https://github.com/basangnguyen/TUTORHUB_WEB)
- Thư mục phát triển chuẩn: `D:\TutorHub_V2`
- Nhánh mặc định: `main`; remote chuẩn: `origin`

## Trạng thái

- Phase 0 và **Phase 1 - Engineering Foundation** đã hoàn thành ngày 2026-07-16.
- **Phase 2 - Identity, tenant và class core** đã hoàn thành và được owner sign-off
  ngày 2026-07-22. P2-00 đến P2-12, staging acceptance, application rollback/redeploy
  và exit gate đều đạt.
- Hiện đang thực hiện **Phase 3 - Daily learning workspace**. P3-00,
  P3-CAL-00/00B/00C, P3-CAL-01 và P3-01 đã `DONE`. ADR-0019 chấp nhận
  FullCalendar Standard v7.0.1 qua adapter, recurrence Go bounded
  `366/730/512/2.000/250 ms`, COUNT occurrence-last horizon validation, YEARLY golden
  và Warm Academic theme. Full v7 E2E hậu fix đạt `9 passed (23.6s)`; comparator parity v6
  `4 passed` nhưng fail absolute budget ở render 500/long-task 2.000. NVDA vẫn là
  explicit rollout gate trước khi renderer được nối vào route production, nên chưa
  được mô tả Calendar như chức năng runtime. Task triển khai tiếp theo là P3-03
  PostgreSQL outbox worker.
- ADR-0021 đã `Accepted` để P3-02D xây Native Availability Poll do TutorHub sở hữu:
  active member gồm student có thể tạo poll/Study Meeting của mình; secure public link
  không phải booking và không phụ thuộc When2meet. Đây mới là architecture/backlog,
  chưa có runtime.
- AWS SES đã được owner chọn làm transactional email provider target cho Phase 3.
  P3-CAL-02/ADR-0020 vẫn phải xác minh account/region/sandbox/quota, adapter, webhook và
  deliverability; trước khi có domain chỉ được thử bằng identity cá nhân do owner kiểm
  soát và đã verify trong SES sandbox. Production vẫn chờ domain/DNS cùng
  SPF/DKIM/DMARC; chưa có email runtime.
- Web MVP nền đã chạy trên staging: Cloudflare Pages -> same-origin `/api/*` -> Go
  Core API trên Render; dữ liệu dùng Neon, file dùng Backblaze B2, media dùng LiveKit
  Cloud và xác thực dùng ZITADEL.
- Exit gate Phase 1 đã đạt cho Verify/Security CI, OIDC/session/logout,
  health/readiness, migration/rollback, B2, LiveKit 2-5 người, webhook idempotent và
  local developer experience.
- Repository hiện do một người duy trì và push trực tiếp `main`; ngoại lệ quản trị
  này được giới hạn trong development/staging/private alpha theo ADR-0012.
- Master Plan web-first 2.2 và backlog Phase 3 là nguồn kế hoạch hiện hành.
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
22. [Biên bản hoàn thành Phase 2](docs/PHASE_2_COMPLETION.md)
23. [Backlog Phase 3](docs/PHASE_3_BACKLOG.md)
24. [Nghiên cứu và thiết kế product/technical tab Lịch](docs/CALENDAR_PRODUCT_TECHNICAL_DESIGN.md)
25. [Chính sách báo cáo lỗ hổng](SECURITY.md)
26. [ADR-0011: Render cho Core API staging/private alpha](docs/adr/0011-render-core-api-staging.md)
27. [ADR-0012: Direct-main khi một người duy trì](docs/adr/0012-single-maintainer-direct-main-governance.md)
28. [ADR-0013: Shared organization/class authorization policy](docs/adr/0013-shared-organization-class-authorization-policy.md)
29. [ADR-0014: Append-only tenant audit log](docs/adr/0014-append-only-tenant-audit-log.md)
30. [ADR-0015: Server-evaluated feature controls và quotas](docs/adr/0015-server-evaluated-feature-controls-and-quotas.md)
31. [ADR-0016: Idempotent V1 fixture import](docs/adr/0016-idempotent-v1-fixture-import.md)
32. [ADR-0017: Class session scheduling và civil time](docs/adr/0017-class-session-scheduling-and-civil-time.md)
33. [ADR-0018: PostgreSQL leased outbox worker](docs/adr/0018-postgresql-leased-outbox-worker.md)
34. [ADR-0019: Calendar renderer, bounded recurrence và conflict authority](docs/adr/0019-calendar-renderer-recurrence-and-conflict.md)
35. [ADR-0021: Native Availability Poll và member-owned Study Meeting](docs/adr/0021-native-availability-polls-and-member-owned-study-meetings.md)

Các quyết định kiến trúc đã chấp nhận nằm trong `docs/adr`.

## Nguyên tắc

- Web-first, API-first, contract-first.
- Modular monolith trước; chỉ tách service khi có số liệu vận hành chứng minh nhu cầu.
- Managed services trước; không dùng Kubernetes trong MVP.
- Multi-tenant và phân quyền được thiết kế từ đầu.
- Secure Exam tiếp tục là sản phẩm native riêng, không giả định trình duyệt web có thể khóa hệ điều hành.
- Di chuyển theo Strangler Pattern, không big-bang rewrite.
