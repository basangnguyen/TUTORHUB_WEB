# Lộ trình giao hàng Web V2

## Phase 0 - Foundation (hoàn thành)

**Đầu ra:** phạm vi, MVP, system context, domain/permission, migration map, security baseline, ADR và backlog Phase 1.

**Exit gate:** không còn quyết định nền tảng quan trọng chưa ghi nhận; mọi secret V1 đã được lên kế hoạch rotate.

## Phase 1 - Engineering foundation (4-6 tuần)

- Monorepo, toolchain, lint/typecheck/test và CI.
- React shell, design tokens, Storybook và accessibility baseline.
- Go service skeleton, config, log, metrics, health, PostgreSQL migration.
- OIDC/BFF proof of concept và generated OpenAPI client.
- Local Docker Compose; staging tối thiểu.
- Neon staging, Backblaze B2 staging và các Hugging Face Docker Spaces tách biệt.

## Phase 2 - Identity, tenant và class (4-6 tuần)

- Session lifecycle, logout/revoke và tenant selector.
- User profile, membership, policy engine.
- Class CRUD, enrollment, invite và schedule/session.
- Authorization matrix tests và admin audit.

## Phase 3 - Classroom MVP (6-10 tuần)

- LiveKit token service và room policy.
- Prejoin/device flow.
- Mic, camera, screen share, participant, active speaker, reconnect.
- Lobby/admit/remove/mute policy.
- Chat cơ bản, metrics và E2E classroom path.

## Phase 4 - Classroom collaboration (6-8 tuần)

- Persistent chat và notification.
- Whiteboard tldraw/Yjs, snapshots và permission.
- File upload/download, antivirus pipeline và quota.
- Recording qua server-side egress và retention policy.

## Phase 5 - Learning workflows (8-12 tuần)

- Assignment, question bank, quiz/exam, attempt và grading.
- QuizHub game mode, scoring deterministic và analytics.
- Calendar, task và learning progress.

## Phase 6 - Expansion và global readiness

- Lavie AI theo tenant data policy.
- Social learning/moderation.
- Billing/subscription, organization admin và support tooling.
- Multi-region assessment, localization, compliance và disaster recovery.

## Nhánh native song song

- Desktop Tauri chỉ bắt đầu sau khi API và web classroom ổn định.
- Secure Exam tiếp tục Rust/native, dùng signed exam handoff.
- Mobile React Native bắt đầu sau khi domain/API và design system đã ổn định.

## Ước lượng nguồn lực

MVP thực tế thường cần khoảng 6-9 tháng với nhóm 5-8 người: tech lead, 2 frontend, 2 backend, QA automation, product/design và DevOps/SRE bán thời gian. Đây là ước lượng kế hoạch, không phải cam kết; phải điều chỉnh sau spike LiveKit và auth.

## Chiến lược phát hành

1. Developer preview nội bộ.
2. Private alpha với tenant thử nghiệm và dữ liệu giả/giới hạn.
3. Private beta có telemetry, support và load test.
4. Review gate hạ tầng Hugging Face; chuyển container host nếu không đạt availability/load.
5. Public beta theo khu vực.
6. General availability sau security, DR và legal gates.
